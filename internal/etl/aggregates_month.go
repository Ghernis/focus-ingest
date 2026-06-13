package etl

import (
	"context"
	"database/sql"
	"fmt"
)

func (p *Processor) rebuildAggregatesForMonths(ctx context.Context, months []string) error {
	for _, month := range months {
		tx, err := p.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := p.rebuildAggregatesForMonth(ctx, tx, month); err != nil {
			tx.Rollback()
			return fmt.Errorf("aggregates %s: %w", month, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// RebuildAggregatesForMonth rebuilds all aggregate tables for one billing month.
func (p *Processor) RebuildAggregatesForMonth(ctx context.Context, month string) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := p.rebuildAggregatesForMonth(ctx, tx, month); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (p *Processor) rebuildAggregatesForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	if err := p.deleteAggregatesForMonth(ctx, tx, month); err != nil {
		return err
	}
	if err := p.insertCoreAggregatesForMonth(ctx, tx, month); err != nil {
		return err
	}
	if err := p.insertAppAggregatesForMonth(ctx, tx, month); err != nil {
		return err
	}
	if err := p.rebuildCostDistributionForMonth(ctx, tx, month); err != nil {
		return err
	}
	return p.RebuildCostAnomaliesForMonth(ctx, tx, month)
}

// RebuildCostAnomaliesForMonth recomputes anomaly rows for one billing month using agg_app_* history on the DB.
func (p *Processor) RebuildCostAnomaliesForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	return p.rebuildCostAnomaliesForMonth(ctx, tx, month)
}

// DeleteAggregatesForMonth removes aggregate rows scoped to one billing month (excludes anomaly if skipAnomaly).
func (p *Processor) DeleteAggregatesForMonth(ctx context.Context, tx *sql.Tx, month string, skipAnomaly bool) error {
	return p.deleteAggregatesForMonth(ctx, tx, month, skipAnomaly)
}

func (p *Processor) deleteAggregatesForMonth(ctx context.Context, tx *sql.Tx, month string, skipAnomaly ...bool) error {
	skip := false
	if len(skipAnomaly) > 0 {
		skip = skipAnomaly[0]
	}
	m := monthEq("month_start", month)
	bm := monthEq("billing_period_start", month)
	tables := []struct {
		table string
		where string
	}{
		{"agg_cost_monthly", m},
		{"agg_cost_by_tag", m},
		{"agg_commitment_utilization", m},
		{"agg_savings_summary", m},
		{"agg_app_monthly", m},
		{"agg_app_service_monthly", m},
		{"agg_app_service_resource_monthly", m},
		{"agg_cost_distribution_monthly", m},
		{"agg_cost_daily", bm},
	}
	if !skip {
		tables = append(tables, struct {
			table string
			where string
		}{"agg_cost_anomaly_monthly", m})
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t.table+" WHERE "+t.where); err != nil {
			return fmt.Errorf("delete %s: %w", t.table, err)
		}
	}

	commitDaily := fmt.Sprintf(`DELETE FROM agg_commitment_utilization_daily
		WHERE charge_date IN (
		  SELECT DISTINCT charge_date FROM fact_focus_cost_daily WHERE %s
		)`, bm)
	if _, err := tx.ExecContext(ctx, commitDaily); err != nil {
		return fmt.Errorf("delete agg_commitment_utilization_daily: %w", err)
	}
	return nil
}

func (p *Processor) insertCoreAggregatesForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	subJoin := p.subAccountJoin()
	billingPeriod := p.billingMonthStartExpr()
	billed := p.castCost("f.billed_cost")
	effective := p.castCost("f.effective_cost")
	list := p.castCost("f.list_cost")
	contracted := p.castCost("f.contracted_cost")
	now := p.nowUTC()
	monthFilter := monthEq("f.billing_period_start", month)

	dailySQL := fmt.Sprintf(`
		INSERT INTO agg_cost_daily (
		  charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
		  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
		SELECT f.charge_date, %s, a.provider, f.sub_account_sk, f.service_sk, f.region_sk,
		  SUM(%s), SUM(%s), SUM(%s), SUM(%s),
		  SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY f.charge_date, %s, a.provider, f.sub_account_sk, f.service_sk, f.region_sk`,
		billingPeriod, billed, effective, list, contracted, now, subJoin, monthFilter, billingPeriod)
	if _, err := tx.ExecContext(ctx, dailySQL); err != nil {
		return fmt.Errorf("agg_cost_daily: %w", err)
	}

	monthlySQL := fmt.Sprintf(`
		INSERT INTO agg_cost_monthly (
		  month_start, provider, sub_account_sk, service_category, charge_category_sk,
		  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, f.sub_account_sk, svc.service_category, f.charge_category_sk,
		  SUM(%s), SUM(%s), SUM(%s), SUM(%s),
		  SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, f.sub_account_sk, svc.service_category, f.charge_category_sk`,
		billingPeriod, billed, effective, list, contracted, now, subJoin, monthFilter, billingPeriod)
	if _, err := tx.ExecContext(ctx, monthlySQL); err != nil {
		return fmt.Errorf("agg_cost_monthly: %w", err)
	}

	tagSQL := fmt.Sprintf(`
		INSERT INTO agg_cost_by_tag (month_start, provider, tag_key, tag_value, billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, t.tag_key, t.tag_value,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		INNER JOIN bridge_cost_tag b ON b.cost_daily_id = f.cost_daily_id
		INNER JOIN dim_tag t ON b.tag_sk = t.tag_sk
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, t.tag_key, t.tag_value`,
		billingPeriod, billed, effective, now, subJoin, monthFilter, billingPeriod)
	if _, err := tx.ExecContext(ctx, tagSQL); err != nil {
		return fmt.Errorf("agg_cost_by_tag: %w", err)
	}

	commitQty := p.castCost("f.commitment_discount_quantity")
	commitMonthlySQL := fmt.Sprintf(`
		INSERT INTO agg_commitment_utilization (month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
		SELECT %s, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.commitment_sk IS NOT NULL AND f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`,
		billingPeriod, effective, commitQty, now, subJoin, monthFilter, billingPeriod)
	if _, err := tx.ExecContext(ctx, commitMonthlySQL); err != nil {
		return fmt.Errorf("agg_commitment_utilization: %w", err)
	}

	commitDailySQL := fmt.Sprintf(`
		INSERT INTO agg_commitment_utilization_daily (charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
		SELECT f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.commitment_sk IS NOT NULL AND f.sub_account_sk IS NOT NULL AND %s
		GROUP BY f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`,
		effective, commitQty, now, subJoin, monthFilter)
	if _, err := tx.ExecContext(ctx, commitDailySQL); err != nil {
		return fmt.Errorf("agg_commitment_utilization_daily: %w", err)
	}

	savingsSQL := fmt.Sprintf(`
		INSERT INTO agg_savings_summary (month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc)
		SELECT %s, a.provider, f.service_sk,
		  SUM(%s), 0, 0, %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, f.service_sk`,
		billingPeriod, effective, now, subJoin, monthFilter, billingPeriod)
	if _, err := tx.ExecContext(ctx, savingsSQL); err != nil {
		return fmt.Errorf("agg_savings_summary: %w", err)
	}
	return nil
}

func (p *Processor) insertAppAggregatesForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	if err := p.syncApplicationsFromFactsForMonth(ctx, tx, month); err != nil {
		return fmt.Errorf("sync applications: %w", err)
	}

	monthExpr := p.billingMonthStartExpr()
	env := p.environmentExpr()
	billed := p.castCost("f.billed_cost")
	effective := p.castCost("f.effective_cost")
	now := p.nowUTC()
	joins := p.appContextJoins()
	appJoin := p.applicationDimJoin()
	subJoin := p.subAccountJoin()
	monthFilter := monthEq("f.billing_period_start", month)

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_monthly (
		  month_start, provider, application_sk, environment,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, da.application_sk, %s,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, da.application_sk, %s`,
		monthExpr, env, billed, effective, now, subJoin, joins, appJoin, monthFilter, monthExpr, env)); err != nil {
		return fmt.Errorf("agg_app_monthly: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_monthly (
		  month_start, provider, application_sk, environment, service_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, da.application_sk, %s, f.service_sk,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, da.application_sk, %s, f.service_sk`,
		monthExpr, env, billed, effective, now, subJoin, joins, appJoin, monthFilter, monthExpr, env)); err != nil {
		return fmt.Errorf("agg_app_service_monthly: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_resource_monthly (
		  month_start, provider, application_sk, environment, service_sk, resource_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, da.application_sk, %s, f.service_sk, f.resource_sk,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL AND f.resource_sk IS NOT NULL AND %s
		GROUP BY %s, a.provider, da.application_sk, %s, f.service_sk, f.resource_sk`,
		monthExpr, env, billed, effective, now, subJoin, joins, appJoin, monthFilter, monthExpr, env)); err != nil {
		return fmt.Errorf("agg_app_service_resource_monthly: %w", err)
	}
	return nil
}
