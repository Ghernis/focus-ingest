package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func (p *Processor) buildTierAggregates(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceTierRollup, refreshed interface{}) error {
	if err := p.insertTierChangeMonthlyAggs(ctx, tx, month, refreshed); err != nil {
		return err
	}
	if err := p.insertTierChangeIntramonthAggs(ctx, tx, month, refreshed); err != nil {
		return err
	}
	return p.insertTierChangeSummary(ctx, tx, month, rollups, refreshed)
}

func (p *Processor) insertTierChangeMonthlyAggs(ctx context.Context, tx *sql.Tx, month string, refreshed interface{}) error {
	q := `SELECT month_start, provider, resource_sk, service_sk, application_sk, environment,
		prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		prior_unit_rate, new_unit_rate, post_change_quantity, total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction
		FROM fact_resource_tier_change WHERE change_scope = 'MOM' AND ` + p.monthEq("month_start", month)
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	insertSQL := `INSERT INTO agg_resource_tier_change_monthly (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		prior_month_start, prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		prior_unit_rate, new_unit_rate, post_change_quantity, total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_resource_tier_change_monthly (
		month_start, provider, resource_sk, service_sk, application_sk, environment,
		prior_month_start, prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		prior_unit_rate, new_unit_rate, post_change_quantity, total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,@p21,@p22)`
	}
	priorMonth := priorBillingMonth(month)
	for rows.Next() {
		var monthStart, provider, priorTier, newTier, environment, direction string
		var resourceSK, serviceSK, appSK, priorSku, newSku int64
		var priorRate, newRate, postQty, totalQty, counterfactual, unitSav, costDelta, monthSav, projected string
		if err := rows.Scan(&monthStart, &provider, &resourceSK, &serviceSK, &appSK, &environment,
			&priorTier, &newTier, &priorSku, &newSku, &priorRate, &newRate, &postQty, &totalQty, &counterfactual,
			&unitSav, &costDelta, &monthSav, &projected, &direction); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, p.q(insertSQL),
			monthStart, provider, resourceSK, serviceSK, appSK, environment,
			priorMonth, priorTier, newTier, priorSku, newSku,
			priorRate, newRate, postQty, totalQty, counterfactual,
			unitSav, costDelta, monthSav, projected, direction, refreshed,
		)
		if err != nil {
			return fmt.Errorf("agg_resource_tier_change_monthly: %w", err)
		}
	}
	return rows.Err()
}

func (p *Processor) insertTierChangeIntramonthAggs(ctx context.Context, tx *sql.Tx, month string, refreshed interface{}) error {
	q := `SELECT month_start, provider, resource_sk, service_sk, application_sk, environment, change_date,
		prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		days_on_prior_tier, days_on_new_tier, prior_unit_rate, new_unit_rate, post_change_quantity,
		total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction
		FROM fact_resource_tier_change WHERE change_scope = 'INTRAMONTH' AND ` + p.monthEq("month_start", month)
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	insertSQL := `INSERT INTO agg_resource_tier_change_intramonth (
		month_start, provider, resource_sk, service_sk, application_sk, environment, change_date,
		prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		days_on_prior_tier, days_on_new_tier, prior_unit_rate, new_unit_rate, post_change_quantity,
		total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_resource_tier_change_intramonth (
		month_start, provider, resource_sk, service_sk, application_sk, environment, change_date,
		prior_tier_code, new_tier_code, prior_tier_sku_sk, new_tier_sku_sk,
		days_on_prior_tier, days_on_new_tier, prior_unit_rate, new_unit_rate, post_change_quantity,
		total_qty_on_new_tier, counterfactual_cost_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,@p21,@p22,@p23,@p24)`
	}
	for rows.Next() {
		var monthStart, provider, priorTier, newTier, environment, changeDate, direction string
		var resourceSK, serviceSK, appSK, priorSku, newSku int64
		var daysPrior, daysNew int
		var priorRate, newRate, postQty, totalQty, counterfactual, unitSav, costDelta, monthSav, projected string
		if err := rows.Scan(&monthStart, &provider, &resourceSK, &serviceSK, &appSK, &environment, &changeDate,
			&priorTier, &newTier, &priorSku, &newSku, &daysPrior, &daysNew, &priorRate, &newRate, &postQty, &totalQty, &counterfactual,
			&unitSav, &costDelta, &monthSav, &projected, &direction); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, p.q(insertSQL),
			monthStart, provider, resourceSK, serviceSK, appSK, environment, changeDate,
			priorTier, newTier, priorSku, newSku, daysPrior, daysNew, priorRate, newRate, postQty, totalQty, counterfactual,
			unitSav, costDelta, monthSav, projected, direction, refreshed,
		)
		if err != nil {
			return fmt.Errorf("agg_resource_tier_change_intramonth: %w", err)
		}
	}
	return rows.Err()
}

func (p *Processor) insertTierChangeSummary(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceTierRollup, refreshed interface{}) error {
	insertSQL := `INSERT INTO agg_tier_change_summary_monthly (
		month_start, provider, service_sk,
		total_realized_savings_unit, total_realized_savings_cost_delta,
		mom_change_count, intramonth_change_count, downsize_count, upsize_count, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO agg_tier_change_summary_monthly (
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
			formatCost(r.monthSavings), formatCost(r.monthSavings),
			r.momCount, r.intraCount, r.downsizeCnt, r.upsizeCnt, refreshed,
		)
		if err != nil {
			return fmt.Errorf("agg_tier_change_summary_monthly: %w", err)
		}
	}
	return nil
}

func (p *Processor) updateSavingsSummaryFromTierRollups(ctx context.Context, tx *sql.Tx, month string, rollups map[string]*serviceTierRollup) error {
	resetSQL := `UPDATE agg_savings_summary SET
		total_realized_savings_unit = 0,
		total_realized_savings_cost_delta = 0,
		rightsizing_change_count = 0
		WHERE ` + p.monthEq("month_start", month)
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
			formatCost(r.monthSavings), formatCost(r.monthSavings), changeCount,
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
					formatCost(r.monthSavings), formatCost(r.monthSavings), changeCount,
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
				formatCost(r.monthSavings), formatCost(r.monthSavings), changeCount, p.refreshedUTCParam(),
			); err != nil {
				return err
			}
		}
	}
	return nil
}
