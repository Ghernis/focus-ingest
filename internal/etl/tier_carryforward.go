package etl

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/focus"
)

type carryforwardBaseline struct {
	monthStart   string
	changeDate   string
	provider     string
	resourceSK   int64
	serviceSK    int64
	priorTierCode string
	priorTierRank int
	priorSkuSK    int64
	priorUnitRate float64
}

type carryforwardPrevKey struct {
	provider   string
	resourceSK int64
	serviceSK  int64
	changeDate string
}

func (p *Processor) buildFactResourceTierCarryforward(ctx context.Context, tx *sql.Tx, month string, daily []tierDailyRow, refreshed interface{}) error {
	month = p.normDate(month)
	if month == "" {
		return nil
	}
	current := dominantTierByMonth(daily, month)
	if len(current) == 0 {
		return nil
	}

	baselines, err := p.loadCarryforwardBaselines(ctx, tx, month)
	if err != nil {
		return err
	}
	if len(baselines) == 0 {
		return nil
	}
	prevCumulative, err := p.loadCarryforwardPrevCumulative(ctx, tx, month)
	if err != nil {
		return err
	}

	insertSQL := `INSERT INTO fact_resource_tier_carryforward (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		baseline_change_month, baseline_change_date,
		baseline_tier_code, baseline_tier_rank, baseline_tier_sku_sk, baseline_unit_rate,
		current_tier_code, current_tier_rank, current_tier_sku_sk, current_unit_rate,
		month_quantity, month_actual_cost, month_counterfactual_cost,
		month_realized_delta, cumulative_realized_delta,
		change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO fact_resource_tier_carryforward (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		baseline_change_month, baseline_change_date,
		baseline_tier_code, baseline_tier_rank, baseline_tier_sku_sk, baseline_unit_rate,
		current_tier_code, current_tier_rank, current_tier_sku_sk, current_unit_rate,
		month_quantity, month_actual_cost, month_counterfactual_cost,
		month_realized_delta, cumulative_realized_delta,
		change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,@p21,@p22,@p23)`
	}

	for k, cur := range current {
		base, ok := baselines[k]
		if !ok {
			continue
		}
		counterfactual, monthDelta := computeCarryForwardMonthDelta(base.priorUnitRate, cur.tierQty, cur.tierCost)
		prevKey := carryforwardPrevKey{provider: cur.provider, resourceSK: cur.resourceSK, serviceSK: cur.serviceSK, changeDate: base.changeDate}
		cumulative := prevCumulative[prevKey] + monthDelta
		dir := changeDirection(base.priorUnitRate, cur.tierUnitRate)

		if _, err := tx.ExecContext(ctx, p.q(insertSQL),
			month, cur.provider, cur.resourceSK, cur.serviceSK, cur.applicationSK, cur.environment,
			base.monthStart, base.changeDate,
			base.priorTierCode, base.priorTierRank, base.priorSkuSK, formatCost(base.priorUnitRate),
			cur.tierCode, cur.tierRank, cur.tierSkuSK, formatCost(cur.tierUnitRate),
			formatCost(cur.tierQty), formatCost(cur.tierCost), formatCost(counterfactual),
			formatCost(monthDelta), formatCost(cumulative),
			dir, refreshed,
		); err != nil {
			return fmt.Errorf("fact_resource_tier_carryforward: %w", err)
		}
	}

	return nil
}

func (p *Processor) loadCarryforwardBaselines(ctx context.Context, tx *sql.Tx, month string) (map[resourceServiceKey]carryforwardBaseline, error) {
	where := "substr(month_start,1,10) <= ?"
	if p.Dialect == "sqlserver" {
		where = "CAST(month_start AS DATE) <= @p1"
	}
	q := fmt.Sprintf(`SELECT %s, %s, provider, resource_sk, service_sk,
		prior_tier_code, prior_tier_rank, prior_tier_sku_sk, prior_unit_rate
		FROM fact_resource_tier_change
		WHERE %s
		ORDER BY month_start, change_date, fact_resource_tier_change_id`,
		p.dateOnlySelectExpr("month_start"), p.dateOnlySelectExpr("change_date"), where)
	rows, err := tx.QueryContext(ctx, p.q(q), month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[resourceServiceKey]carryforwardBaseline{}
	for rows.Next() {
		var b carryforwardBaseline
		var rateStr string
		if err := rows.Scan(&b.monthStart, &b.changeDate, &b.provider, &b.resourceSK, &b.serviceSK,
			&b.priorTierCode, &b.priorTierRank, &b.priorSkuSK, &rateStr); err != nil {
			return nil, err
		}
		k := resourceServiceKey{resourceSK: b.resourceSK, serviceSK: b.serviceSK}
		if _, exists := out[k]; exists {
			continue
		}
		b.monthStart = p.normDate(b.monthStart)
		b.changeDate = p.normDate(b.changeDate)
		b.priorUnitRate = parseDecimal(rateStr)
		out[k] = b
	}
	return out, rows.Err()
}

func (p *Processor) loadCarryforwardPrevCumulative(ctx context.Context, tx *sql.Tx, month string) (map[carryforwardPrevKey]float64, error) {
	where := "substr(month_start,1,10) < ?"
	if p.Dialect == "sqlserver" {
		where = "CAST(month_start AS DATE) < @p1"
	}
	dateExpr := p.dateOnlySelectExpr("baseline_change_date")
	q := fmt.Sprintf(`SELECT provider, resource_sk, service_sk, %s,
		SUM(CAST(month_realized_delta AS DECIMAL(28,10)))
		FROM fact_resource_tier_carryforward
		WHERE %s
		GROUP BY provider, resource_sk, service_sk, %s`, dateExpr, where, dateExpr)
	if p.Dialect != "sqlserver" {
		q = fmt.Sprintf(`SELECT provider, resource_sk, service_sk, %s,
		SUM(month_realized_delta)
		FROM fact_resource_tier_carryforward
		WHERE %s
		GROUP BY provider, resource_sk, service_sk, %s`, dateExpr, where, dateExpr)
	}
	rows, err := tx.QueryContext(ctx, p.q(q), month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[carryforwardPrevKey]float64{}
	for rows.Next() {
		var key carryforwardPrevKey
		var sumStr string
		if err := rows.Scan(&key.provider, &key.resourceSK, &key.serviceSK, &key.changeDate, &sumStr); err != nil {
			return nil, err
		}
		key.changeDate = p.normDate(key.changeDate)
		out[key] = parseDecimal(sumStr)
	}
	return out, rows.Err()
}

func (p *Processor) normDate(v string) string {
	return focus.DateOnly(v)
}
