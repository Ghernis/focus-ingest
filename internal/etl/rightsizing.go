package etl

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

const (
	changeDownsize = "DOWNSIZE"
	changeUpsize   = "UPSIZE"
	changeNeutral  = "NEUTRAL"
)

type skuMonthAgg struct {
	month        string
	provider     string
	resourceSK   int64
	serviceSK    int64
	applicationSK int64
	environment  string
	skuSK        int64
	skuCost      float64
	skuQty       float64
}

type resourceMonthMeta struct {
	provider      string
	serviceSK     int64
	applicationSK int64
	environment   string
	totalCost     float64
}

type dominantSku struct {
	skuSK   int64
	cost    float64
	qty     float64
	unitRate float64
}

type dailySkuAgg struct {
	chargeDate string
	skuSK      int64
	cost       float64
	qty        float64
}

type serviceRightsizingRollup struct {
	unitSavings  float64
	costDelta    float64
	momCount     int
	intraCount   int
	downsizeCnt  int
	upsizeCnt    int
}

func (p *Processor) rebuildRightsizingForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	month = focus.DateOnly(strings.TrimSpace(month))
	if month == "" {
		return nil
	}
	if err := p.deleteRightsizingForMonth(ctx, tx, month); err != nil {
		return err
	}

	skuAggs, resourceTotals, err := p.loadResourceSkuMonthAggs(ctx, tx, month, priorBillingMonth(month))
	if err != nil {
		return err
	}
	if len(skuAggs) == 0 {
		return nil
	}

	currentDominant := dominantSkuByResourceMonth(skuAggs, month)
	priorDominant := dominantSkuByResourceMonth(skuAggs, priorBillingMonth(month))
	skuKeys, err := p.loadSkuNaturalKeys(ctx, tx)
	if err != nil {
		return err
	}

	rollups := map[string]*serviceRightsizingRollup{}
	now := p.nowUTC()

	if err := p.insertMoMRightsizing(ctx, tx, month, priorBillingMonth(month), currentDominant, priorDominant, resourceTotals, skuAggs, rollups, now, skuKeys); err != nil {
		return err
	}
	if err := p.insertIntraMonthRightsizing(ctx, tx, month, rollups, now, skuKeys); err != nil {
		return err
	}
	if err := p.insertRightsizingSummary(ctx, tx, month, rollups, now); err != nil {
		return err
	}
	return p.updateSavingsSummaryRealized(ctx, tx, month, rollups)
}

func (p *Processor) rebuildRightsizingAllMonths(ctx context.Context, tx *sql.Tx) error {
	months, err := p.distinctFactBillingMonths(ctx, tx)
	if err != nil {
		return err
	}
	for _, m := range months {
		if err := p.rebuildRightsizingForMonth(ctx, tx, m); err != nil {
			return fmt.Errorf("rightsizing %s: %w", m, err)
		}
	}
	return nil
}

func (p *Processor) deleteRightsizingForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	m := monthEq("month_start", month)
	for _, table := range []string{
		"agg_resource_rightsizing_monthly",
		"agg_resource_rightsizing_intramonth",
		"agg_rightsizing_summary_monthly",
	} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+m); err != nil {
			return fmt.Errorf("delete %s: %w", table, err)
		}
	}
	return nil
}

func (p *Processor) distinctFactBillingMonths(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT billing_period_start FROM fact_focus_cost_daily ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		m = focus.DateOnly(strings.TrimSpace(m))
		if m != "" {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

func (p *Processor) loadResourceSkuMonthAggs(ctx context.Context, tx *sql.Tx, months ...string) ([]skuMonthAgg, map[string]map[int64]resourceMonthMeta, error) {
	if len(months) == 0 {
		return nil, nil, nil
	}
	var filters []string
	for _, m := range months {
		if m = focus.DateOnly(strings.TrimSpace(m)); m != "" {
			filters = append(filters, monthEq("f.billing_period_start", m))
		}
	}
	if len(filters) == 0 {
		return nil, nil, nil
	}

	effective := p.castCost("f.effective_cost")
	qty := p.castCost("f.pricing_quantity")
	appSK := p.applicationSKExpr()
	env := p.environmentExpr()
	joins := p.appContextJoins()
	subJoin := p.subAccountJoin()

	q := fmt.Sprintf(`
		SELECT f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s,
		  f.sku_sk, SUM(%s), SUM(%s)
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL
		  AND f.resource_sk IS NOT NULL
		  AND f.sku_sk IS NOT NULL
		  AND (%s)
		GROUP BY f.billing_period_start, a.provider, f.resource_sk, f.service_sk, %s, %s, f.sku_sk`,
		appSK, env, effective, qty, subJoin, joins, p.applicationDimJoin(), strings.Join(filters, " OR "), appSK, env)

	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var aggs []skuMonthAgg
	totals := map[string]map[int64]resourceMonthMeta{}
	for rows.Next() {
		var a skuMonthAgg
		var costStr, qtyStr string
		if err := rows.Scan(&a.month, &a.provider, &a.resourceSK, &a.serviceSK, &a.applicationSK, &a.environment, &a.skuSK, &costStr, &qtyStr); err != nil {
			return nil, nil, err
		}
		a.month = focus.DateOnly(a.month)
		a.skuCost = parseDecimal(costStr)
		a.skuQty = parseDecimal(qtyStr)
		aggs = append(aggs, a)

		if totals[a.month] == nil {
			totals[a.month] = map[int64]resourceMonthMeta{}
		}
		meta := totals[a.month][a.resourceSK]
		meta.provider = a.provider
		meta.serviceSK = a.serviceSK
		meta.applicationSK = a.applicationSK
		meta.environment = a.environment
		meta.totalCost += a.skuCost
		totals[a.month][a.resourceSK] = meta
	}
	return aggs, totals, rows.Err()
}

func dominantSkuByResourceMonth(aggs []skuMonthAgg, month string) map[int64]dominantSku {
	month = focus.DateOnly(month)
	byResource := map[int64]map[int64]skuMonthAgg{}
	for _, a := range aggs {
		if a.month != month {
			continue
		}
		if byResource[a.resourceSK] == nil {
			byResource[a.resourceSK] = map[int64]skuMonthAgg{}
		}
		prev := byResource[a.resourceSK][a.skuSK]
		prev.skuCost += a.skuCost
		prev.skuQty += a.skuQty
		byResource[a.resourceSK][a.skuSK] = prev
	}

	out := map[int64]dominantSku{}
	for resSK, skus := range byResource {
		var best dominantSku
		for skuSK, agg := range skus {
			if agg.skuCost > best.cost ||
				(agg.skuCost == best.cost && agg.skuQty > best.qty) ||
				(agg.skuCost == best.cost && agg.skuQty == best.qty && skuSK < best.skuSK) {
				best = dominantSku{
					skuSK:    skuSK,
					cost:     agg.skuCost,
					qty:      agg.skuQty,
					unitRate: unitRate(agg.skuCost, agg.skuQty),
				}
			}
		}
		if best.skuSK > 0 {
			out[resSK] = best
		}
	}
	return out
}

func (p *Processor) loadSkuNaturalKeys(ctx context.Context, tx *sql.Tx) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sku_sk, provider || '|' || sku_id FROM dim_sku`)
	if p.Dialect == "sqlserver" {
		rows, err = tx.QueryContext(ctx, `SELECT sku_sk, provider + '|' + sku_id FROM dim_sku`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var sk int64
		var nk string
		if err := rows.Scan(&sk, &nk); err != nil {
			return nil, err
		}
		out[sk] = strings.ToUpper(strings.TrimSpace(nk))
	}
	return out, rows.Err()
}

func skuNaturalKey(keys map[int64]string, skuSK int64) string {
	if keys == nil {
		return strconv.FormatInt(skuSK, 10)
	}
	if nk, ok := keys[skuSK]; ok && nk != "" {
		return nk
	}
	return strconv.FormatInt(skuSK, 10)
}

func (p *Processor) insertMoMRightsizing(
	ctx context.Context, tx *sql.Tx, month, priorMonth string,
	current, prior map[int64]dominantSku,
	resourceTotals map[string]map[int64]resourceMonthMeta,
	skuAggs []skuMonthAgg,
	rollups map[string]*serviceRightsizingRollup,
	now string,
	skuKeys map[int64]string,
) error {
	insertSQL := `INSERT INTO agg_resource_rightsizing_monthly (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		prior_month_start, prior_sku_sk, current_sku_sk,
		prior_unit_rate, current_unit_rate, post_change_quantity,
		realized_savings_unit, realized_savings_cost_delta, change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_resource_rightsizing_monthly (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		prior_month_start, prior_sku_sk, current_sku_sk,
		prior_unit_rate, current_unit_rate, post_change_quantity,
		realized_savings_unit, realized_savings_cost_delta, change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16)`
	}

	for resSK, cur := range current {
		prev, ok := prior[resSK]
		if !ok || skuNaturalKey(skuKeys, prev.skuSK) == skuNaturalKey(skuKeys, cur.skuSK) {
			continue
		}
		meta := resourceTotals[month][resSK]
		priorMeta := resourceTotals[priorMonth][resSK]
		postQty := currentSkuQty(skuAggs, month, resSK, cur.skuSK)

		unitSavings := (prev.unitRate - cur.unitRate) * postQty
		costDelta := priorMeta.totalCost - meta.totalCost
		dir := changeDirection(prev.unitRate, cur.unitRate)

		_, err := tx.ExecContext(ctx, p.q(insertSQL),
			month, meta.provider, resSK, meta.serviceSK, meta.applicationSK, meta.environment,
			priorMonth, prev.skuSK, cur.skuSK,
			formatCost(prev.unitRate), formatCost(cur.unitRate), formatCost(postQty),
			formatCost(unitSavings), formatCost(costDelta), dir, now,
		)
		if err != nil {
			return fmt.Errorf("agg_resource_rightsizing_monthly: %w", err)
		}
		addRollup(rollups, meta.provider, meta.serviceSK, unitSavings, costDelta, dir, true, false)
	}
	return nil
}

func (p *Processor) insertIntraMonthRightsizing(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceRightsizingRollup, now string, skuKeys map[int64]string) error {
	daily, metaByResource, err := p.loadDailyResourceSkuAggs(ctx, tx, month)
	if err != nil {
		return err
	}

	insertSQL := `INSERT INTO agg_resource_rightsizing_intramonth (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		change_date, prior_sku_sk, new_sku_sk, days_on_prior_sku, days_on_new_sku,
		realized_savings_unit, realized_savings_cost_delta, change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_resource_rightsizing_intramonth (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		change_date, prior_sku_sk, new_sku_sk, days_on_prior_sku, days_on_new_sku,
		realized_savings_unit, realized_savings_cost_delta, change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15)`
	}

	for resSK, days := range daily {
		events := detectIntraMonthChanges(days, skuKeys)
		if len(events) == 0 {
			continue
		}
		meta := metaByResource[resSK]
		for _, ev := range events {
			unitSavings := (ev.priorUnitRate - ev.newUnitRate) * ev.newQty
			costDelta := (ev.priorUnitRate * ev.totalQty) - ev.actualCost
			dir := changeDirection(ev.priorUnitRate, ev.newUnitRate)

			_, err := tx.ExecContext(ctx, p.q(insertSQL),
				month, meta.provider, resSK, meta.serviceSK, meta.applicationSK, meta.environment,
				ev.changeDate, ev.priorSKU, ev.newSKU, ev.daysPrior, ev.daysNew,
				formatCost(unitSavings), formatCost(costDelta), dir, now,
			)
			if err != nil {
				return fmt.Errorf("agg_resource_rightsizing_intramonth: %w", err)
			}
			addRollup(rollups, meta.provider, meta.serviceSK, unitSavings, costDelta, dir, false, true)
		}
	}
	return nil
}

type intraMonthEvent struct {
	changeDate     string
	priorSKU       int64
	newSKU         int64
	daysPrior      int
	daysNew        int
	priorUnitRate  float64
	newUnitRate    float64
	newQty         float64
	totalQty       float64
	actualCost     float64
}

func detectIntraMonthChanges(days []dailySkuAgg, skuKeys map[int64]string) []intraMonthEvent {
	if len(days) == 0 {
		return nil
	}
	byDate := map[string][]dailySkuAgg{}
	for _, d := range days {
		byDate[d.chargeDate] = append(byDate[d.chargeDate], d)
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	dailyDominant := make([]struct {
		date string
		sku  int64
	}, 0, len(dates))
	for _, d := range dates {
		sku := dominantSkuFromDaily(byDate[d])
		if sku > 0 {
			dailyDominant = append(dailyDominant, struct {
				date string
				sku  int64
			}{d, sku})
		}
	}
	if len(dailyDominant) < 2 {
		return nil
	}

	var events []intraMonthEvent
	for i := 1; i < len(dailyDominant); i++ {
		if skuNaturalKey(skuKeys, dailyDominant[i].sku) == skuNaturalKey(skuKeys, dailyDominant[i-1].sku) {
			continue
		}
		priorSKU := dailyDominant[i-1].sku
		newSKU := dailyDominant[i].sku
		changeDate := dailyDominant[i].date

		var priorCost, priorQty, newCost, newQty, totalQty, actualCost float64
		daysPrior, daysNew := 0, 0
		for _, d := range dates {
			for _, row := range byDate[d] {
				actualCost += row.cost
				totalQty += row.qty
			}
			if d < changeDate {
				daysPrior++
				for _, row := range byDate[d] {
					if row.skuSK == priorSKU {
						priorCost += row.cost
						priorQty += row.qty
					}
				}
			} else {
				daysNew++
				for _, row := range byDate[d] {
					if row.skuSK == newSKU {
						newCost += row.cost
						newQty += row.qty
					}
				}
			}
		}

		events = append(events, intraMonthEvent{
			changeDate:    changeDate,
			priorSKU:      priorSKU,
			newSKU:        newSKU,
			daysPrior:     daysPrior,
			daysNew:       daysNew,
			priorUnitRate: unitRate(priorCost, priorQty),
			newUnitRate:   unitRate(newCost, newQty),
			newQty:        newQty,
			totalQty:      totalQty,
			actualCost:    actualCost,
		})
		break // first transition only per plan v1
	}
	return events
}

func dominantSkuFromDaily(rows []dailySkuAgg) int64 {
	var best int64
	var bestCost float64
	bySku := map[int64]dailySkuAgg{}
	for _, r := range rows {
		prev := bySku[r.skuSK]
		prev.cost += r.cost
		prev.qty += r.qty
		bySku[r.skuSK] = prev
	}
	for sku, agg := range bySku {
		if agg.cost > bestCost || (agg.cost == bestCost && sku < best) {
			bestCost = agg.cost
			best = sku
		}
	}
	return best
}

func (p *Processor) loadDailyResourceSkuAggs(ctx context.Context, tx *sql.Tx, month string) (map[int64][]dailySkuAgg, map[int64]resourceMonthMeta, error) {
	effective := p.castCost("f.effective_cost")
	qty := p.castCost("f.pricing_quantity")
	appSK := p.applicationSKExpr()
	env := p.environmentExpr()
	joins := p.appContextJoins()
	subJoin := p.subAccountJoin()
	monthFilter := monthEq("f.billing_period_start", month)

	q := fmt.Sprintf(`
		SELECT f.charge_date, f.resource_sk, a.provider, f.service_sk, %s, %s,
		  f.sku_sk, SUM(%s), SUM(%s)
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL
		  AND f.resource_sk IS NOT NULL
		  AND f.sku_sk IS NOT NULL
		  AND %s
		GROUP BY f.charge_date, f.resource_sk, a.provider, f.service_sk, %s, %s, f.sku_sk`,
		appSK, env, effective, qty, subJoin, joins, p.applicationDimJoin(), monthFilter, appSK, env)

	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := map[int64][]dailySkuAgg{}
	meta := map[int64]resourceMonthMeta{}
	for rows.Next() {
		var chargeDate, provider, environment string
		var resourceSK, serviceSK, applicationSK, skuSK int64
		var costStr, qtyStr string
		if err := rows.Scan(&chargeDate, &resourceSK, &provider, &serviceSK, &applicationSK, &environment, &skuSK, &costStr, &qtyStr); err != nil {
			return nil, nil, err
		}
		chargeDate = focus.DateOnly(chargeDate)
		out[resourceSK] = append(out[resourceSK], dailySkuAgg{
			chargeDate: chargeDate,
			skuSK:      skuSK,
			cost:       parseDecimal(costStr),
			qty:        parseDecimal(qtyStr),
		})
		meta[resourceSK] = resourceMonthMeta{
			provider:      provider,
			serviceSK:     serviceSK,
			applicationSK: applicationSK,
			environment:   environment,
		}
	}
	return out, meta, rows.Err()
}

func (p *Processor) insertRightsizingSummary(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceRightsizingRollup, now string) error {
	insertSQL := `INSERT INTO agg_rightsizing_summary_monthly (
		month_start, provider, service_sk,
		total_realized_savings_unit, total_realized_savings_cost_delta,
		mom_change_count, intramonth_change_count, downsize_count, upsize_count, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_rightsizing_summary_monthly (
		month_start, provider, service_sk,
		total_realized_savings_unit, total_realized_savings_cost_delta,
		mom_change_count, intramonth_change_count, downsize_count, upsize_count, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10)`
	}

	for key, r := range rollups {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		serviceSK, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		_, err = tx.ExecContext(ctx, p.q(insertSQL),
			month, parts[0], serviceSK,
			formatCost(r.unitSavings), formatCost(r.costDelta),
			r.momCount, r.intraCount, r.downsizeCnt, r.upsizeCnt, now,
		)
		if err != nil {
			return fmt.Errorf("agg_rightsizing_summary_monthly: %w", err)
		}
	}
	return nil
}

func (p *Processor) updateSavingsSummaryRealized(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceRightsizingRollup) error {
	// Reset realized columns for the month, then apply rollup values.
	resetSQL := `UPDATE agg_savings_summary SET
		total_realized_savings_unit = 0,
		total_realized_savings_cost_delta = 0,
		rightsizing_change_count = 0
		WHERE ` + monthEq("month_start", month)
	if _, err := tx.ExecContext(ctx, resetSQL); err != nil {
		return err
	}

	updateSQL := `UPDATE agg_savings_summary SET
		total_realized_savings_unit = ?,
		total_realized_savings_cost_delta = ?,
		rightsizing_change_count = ?
		WHERE month_start = ? AND provider = ? AND service_sk = ?`
	if p.Dialect == "sqlserver" {
		updateSQL = `UPDATE agg_savings_summary SET
		total_realized_savings_unit = @p1,
		total_realized_savings_cost_delta = @p2,
		rightsizing_change_count = @p3
		WHERE month_start = @p4 AND provider = @p5 AND service_sk = @p6`
	}

	for key, r := range rollups {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		serviceSK, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		changeCount := r.momCount + r.intraCount
		res, err := tx.ExecContext(ctx, p.q(updateSQL),
			formatCost(r.unitSavings), formatCost(r.costDelta), changeCount,
			month, parts[0], serviceSK,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if p.Dialect == "sqlserver" {
				insertSQL := `INSERT INTO agg_savings_summary (
				month_start, provider, service_sk,
				total_effective_cost, total_projected_savings, recommendation_count,
				total_realized_savings_unit, total_realized_savings_cost_delta, rightsizing_change_count, refreshed_utc
			) VALUES (@p1, @p2, @p3, 0, 0, 0, @p4, @p5, @p6, SYSUTCDATETIME())`
				if _, err := tx.ExecContext(ctx, insertSQL,
					month, parts[0], serviceSK,
					formatCost(r.unitSavings), formatCost(r.costDelta), changeCount,
				); err != nil {
					return err
				}
				continue
			}
			insertSQL := `INSERT INTO agg_savings_summary (
				month_start, provider, service_sk,
				total_effective_cost, total_projected_savings, recommendation_count,
				total_realized_savings_unit, total_realized_savings_cost_delta, rightsizing_change_count, refreshed_utc
			) VALUES (?, ?, ?, 0, 0, 0, ?, ?, ?, ?)`
			if _, err := tx.ExecContext(ctx, p.q(insertSQL),
				month, parts[0], serviceSK,
				formatCost(r.unitSavings), formatCost(r.costDelta), changeCount, p.nowUTC(),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func addRollup(rollups map[string]*serviceRightsizingRollup, provider string, serviceSK int64, unitSavings, costDelta float64, dir string, mom, intra bool) {
	key := rollupKey(provider, serviceSK)
	if rollups[key] == nil {
		rollups[key] = &serviceRightsizingRollup{}
	}
	r := rollups[key]
	r.unitSavings += unitSavings
	r.costDelta += costDelta
	if mom {
		r.momCount++
	}
	if intra {
		r.intraCount++
	}
	switch dir {
	case changeDownsize:
		r.downsizeCnt++
	case changeUpsize:
		r.upsizeCnt++
	}
}

func rollupKey(provider string, serviceSK int64) string {
	return provider + "|" + strconv.FormatInt(serviceSK, 10)
}

func priorBillingMonth(month string) string {
	t, err := time.Parse("2006-01-02", focus.DateOnly(month))
	if err != nil {
		return ""
	}
	return t.AddDate(0, -1, 0).Format("2006-01-02")
}

func currentSkuQty(aggs []skuMonthAgg, month string, resourceSK, skuSK int64) float64 {
	var qty float64
	for _, a := range aggs {
		if a.month == month && a.resourceSK == resourceSK && a.skuSK == skuSK {
			qty += a.skuQty
		}
	}
	return qty
}

func unitRate(cost, qty float64) float64 {
	if qty <= 0 || math.IsNaN(qty) {
		if cost <= 0 {
			return 0
		}
		return cost
	}
	return cost / qty
}

func changeDirection(priorRate, currentRate float64) string {
	const eps = 1e-9
	switch {
	case priorRate-currentRate > eps:
		return changeDownsize
	case currentRate-priorRate > eps:
		return changeUpsize
	default:
		return changeNeutral
	}
}

func parseDecimal(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
