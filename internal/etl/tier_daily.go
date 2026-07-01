package etl

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/ghernis/focus_dt/internal/focus"
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
	effective := p.castCost("f.effective_cost")
	qty := p.castCost("f.pricing_quantity")
	appSK := p.applicationSKExpr()
	env := p.environmentExpr()
	joins := p.appContextJoins()
	subJoin := p.subAccountJoin()
	monthFilter := monthEq("f.billing_period_start", month)

	q := fmt.Sprintf(`
		SELECT f.charge_date, f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s,
		  s.tier_code, s.tier_rank, f.sku_sk, SUM(%s), SUM(%s)
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		INNER JOIN dim_sku s ON f.sku_sk = s.sku_sk AND s.is_tier_meter = 1
		WHERE f.sub_account_sk IS NOT NULL
		  AND f.resource_sk IS NOT NULL
		  AND f.sku_sk IS NOT NULL
		  AND %s
		GROUP BY f.charge_date, f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s,
		  s.tier_code, s.tier_rank, f.sku_sk`,
		appSK, env, effective, qty, subJoin, joins, p.applicationDimJoin(), monthFilter, appSK, env)

	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byTier := map[tierDayTierKey]tierDailyRow{}
	for rows.Next() {
		var chargeDate, billingMonth, provider, tierCode, environment string
		var resourceSK, serviceSK, appSKVal, skuSK int64
		var tierRank int
		var costStr, qtyStr string
		if err := rows.Scan(&chargeDate, &billingMonth, &provider, &resourceSK, &serviceSK, &appSKVal, &environment,
			&tierCode, &tierRank, &skuSK, &costStr, &qtyStr); err != nil {
			return nil, err
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
	insertSQL := `INSERT INTO fact_resource_tier_daily (
		charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
		tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO fact_resource_tier_daily (
		charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
		tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14)`
	}
	for _, r := range rows {
		_, err := tx.ExecContext(ctx, p.q(insertSQL),
			r.chargeDate, r.billingMonth, r.provider, r.resourceSK, r.serviceSK, r.applicationSK, r.environment,
			r.tierCode, r.tierRank, r.tierSkuSK,
			formatCost(r.tierUnitRate), formatCost(r.tierCost), formatCost(r.tierQty), refreshed,
		)
		if err != nil {
			return fmt.Errorf("fact_resource_tier_daily: %w", err)
		}
	}
	return nil
}
