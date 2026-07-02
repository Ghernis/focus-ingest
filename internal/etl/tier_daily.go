package etl

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
	"github.com/ghernis/focus_dt/internal/sqlserver"
)

type tierDayKey struct {
	chargeDate   string
	billingMonth string
	provider     string
	resourceSK   int64
	serviceSK    int64
}

type tierDayTierKey struct {
	tierDayKey
	tierCode  string
	tierSkuSK int64
}

func (p *Processor) buildFactResourceTierDaily(ctx context.Context, tx *sql.Tx, month string) ([]tierDailyRow, error) {
	engine, err := loadTierRulesEngine()
	if err != nil {
		return nil, err
	}

	effective := p.castCost("f.effective_cost")
	qty := p.castCost("f.pricing_quantity")
	appSK := p.applicationSKExpr()
	env := p.environmentExpr()
	joins := p.appContextJoins()
	subJoin := p.subAccountJoin()
	monthFilter := p.monthEq("f.billing_period_start", month)
	serviceNameExpr := `COALESCE(NULLIF(TRIM(svc.service_name), ''), NULLIF(TRIM(s.service_name), ''), '')`

	q := fmt.Sprintf(`
		SELECT f.charge_date, f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s,
		  f.sku_sk, %s, s.sku_price_id, s.sku_meter, s.tier_code, s.tier_rank, s.is_tier_meter,
		  SUM(%s), SUM(%s)
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		INNER JOIN dim_sku s ON f.sku_sk = s.sku_sk
		INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
		WHERE f.sub_account_sk IS NOT NULL
		  AND f.resource_sk IS NOT NULL
		  AND f.sku_sk IS NOT NULL
		  AND %s
		GROUP BY f.charge_date, f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s,
		  f.sku_sk, %s, s.sku_price_id, s.sku_meter, s.tier_code, s.tier_rank, s.is_tier_meter`,
		appSK, env, serviceNameExpr, effective, qty, subJoin, joins, p.applicationDimJoin(), monthFilter, appSK, env, serviceNameExpr)

	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byTier := map[tierDayTierKey]tierDailyRow{}
	for rows.Next() {
		var chargeDate, billingMonth, provider, environment, serviceName string
		var resourceSK, serviceSK, appSKVal, skuSK int64
		var skuPriceID, skuMeter sql.NullString
		var storedTierCode sql.NullString
		var storedTierRank sql.NullInt32
		var isTierMeter interface{}
		var costStr, qtyStr string
		if err := rows.Scan(&chargeDate, &billingMonth, &provider, &resourceSK, &serviceSK, &appSKVal, &environment,
			&skuSK, &serviceName, &skuPriceID, &skuMeter, &storedTierCode, &storedTierRank, &isTierMeter, &costStr, &qtyStr); err != nil {
			return nil, err
		}
		tierCode, tierRank, ok := resolveTierForFact(
			engine,
			provider,
			serviceName,
			skuPriceID.String,
			skuMeter.String,
			storedTierCode.String,
			int(storedTierRank.Int32),
			isTierMeterTruthy(isTierMeter),
		)
		if !ok {
			continue
		}
		k := tierDayTierKey{
			tierDayKey: tierDayKey{
				chargeDate:   focus.DateOnly(chargeDate),
				billingMonth: focus.DateOnly(billingMonth),
				provider:     provider,
				resourceSK:   resourceSK,
				serviceSK:    serviceSK,
			},
			tierCode:  tierCode,
			tierSkuSK: skuSK,
		}
		prev := byTier[k]
		prev.chargeDate = k.chargeDate
		prev.billingMonth = k.billingMonth
		prev.provider = provider
		prev.resourceSK = resourceSK
		prev.serviceSK = serviceSK
		prev.applicationSK = appSKVal
		prev.environment = environment
		prev.tierCode = tierCode
		prev.tierRank = tierRank
		prev.tierSkuSK = skuSK
		prev.tierCost += parseDecimal(costStr)
		prev.tierQty += parseDecimal(qtyStr)
		byTier[k] = prev
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dominant := pickDominantTierDaily(byTier)
	if err := p.insertTierDailyRows(ctx, tx, dominant, p.refreshedUTCParam()); err != nil {
		return nil, err
	}
	return dominant, nil
}

func resolveTierForFact(engine *tierRulesEngine, provider, serviceName, skuPriceID, skuMeter, storedTierCode string, storedTierRank int, storedTierMeter bool) (string, int, bool) {
	storedTierCode = strings.TrimSpace(storedTierCode)
	if storedTierMeter && storedTierCode != "" {
		return storedTierCode, storedTierRank, true
	}
	match, ok := engine.matchSKU(provider, serviceName, skuPriceID, skuMeter)
	if !ok {
		return "", 0, false
	}
	return match.TierCode, match.TierRank, true
}

func isTierMeterTruthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int32:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case []byte:
		s := strings.TrimSpace(string(t))
		return s == "1" || strings.EqualFold(s, "true")
	case string:
		s := strings.TrimSpace(t)
		return s == "1" || strings.EqualFold(s, "true")
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		return s == "1" || strings.EqualFold(s, "true")
	}
}

func pickDominantTierDaily(byTier map[tierDayTierKey]tierDailyRow) []tierDailyRow {
	byDay := map[tierDayKey][]tierDailyRow{}
	for k, row := range byTier {
		row.tierUnitRate = unitRate(row.tierCost, row.tierQty)
		byDay[k.tierDayKey] = append(byDay[k.tierDayKey], row)
	}
	var out []tierDailyRow
	for _, tiers := range byDay {
		if len(tiers) == 0 {
			continue
		}
		best := tiers[0]
		for _, t := range tiers[1:] {
			if t.tierCost > best.tierCost || (t.tierCost == best.tierCost && t.tierSkuSK < best.tierSkuSK) {
				best = t
			}
		}
		out = append(out, best)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].chargeDate != out[j].chargeDate {
			return out[i].chargeDate < out[j].chargeDate
		}
		if out[i].resourceSK != out[j].resourceSK {
			return out[i].resourceSK < out[j].resourceSK
		}
		return out[i].serviceSK < out[j].serviceSK
	})
	return out
}

func (p *Processor) insertTierDailyRows(ctx context.Context, tx *sql.Tx, rows []tierDailyRow, refreshed interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	const tierDailyCols = 14
	insertSQL := `INSERT INTO fact_resource_tier_daily (
		charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
		tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc
	) VALUES `
	if p.Dialect == "sqlserver" {
		chunk := sqlserver.ChunkRows(tierDailyCols)
		for start := 0; start < len(rows); start += chunk {
			end := start + chunk
			if end > len(rows) {
				end = len(rows)
			}
			if err := p.insertTierDailyChunk(ctx, tx, rows[start:end], refreshed, insertSQL); err != nil {
				return err
			}
		}
		return nil
	}
	oneRow := `INSERT INTO fact_resource_tier_daily (
		charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
		tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	stmt, err := tx.PrepareContext(ctx, oneRow)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.chargeDate, r.billingMonth, r.provider, r.resourceSK, r.serviceSK, r.applicationSK, r.environment,
			r.tierCode, r.tierRank, r.tierSkuSK,
			formatCost(r.tierUnitRate), formatCost(r.tierCost), formatCost(r.tierQty), refreshed,
		); err != nil {
			return fmt.Errorf("fact_resource_tier_daily: %w", err)
		}
	}
	return nil
}

func (p *Processor) insertTierDailyChunk(ctx context.Context, tx *sql.Tx, rows []tierDailyRow, refreshed interface{}, prefix string) error {
	if len(rows) == 0 {
		return nil
	}
	const tierDailyCols = 14
	var b strings.Builder
	b.WriteString(prefix)
	args := make([]interface{}, 0, len(rows)*tierDailyCols)
	n := 1
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < tierDailyCols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "@p%d", n)
			n++
		}
		b.WriteByte(')')
		args = append(args,
			r.chargeDate, r.billingMonth, r.provider, r.resourceSK, r.serviceSK, r.applicationSK, r.environment,
			r.tierCode, r.tierRank, r.tierSkuSK,
			formatCost(r.tierUnitRate), formatCost(r.tierCost), formatCost(r.tierQty), refreshed,
		)
	}
	if err := sqlserver.CheckParamCount(len(args)); err != nil {
		return fmt.Errorf("fact_resource_tier_daily chunk (%d rows): %w", len(rows), err)
	}
	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("fact_resource_tier_daily: %w", err)
	}
	return nil
}
