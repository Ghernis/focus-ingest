package etl

import (
	"context"
	"database/sql"
	"fmt"
)

func (p *Processor) rebuildAggregates(ctx context.Context, tx *sql.Tx) error {
	tables := []string{
		"agg_cost_daily", "agg_cost_monthly", "agg_cost_by_tag",
		"agg_commitment_utilization", "agg_commitment_utilization_daily", "agg_savings_summary",
		"agg_app_monthly", "agg_app_service_monthly", "agg_app_service_resource_monthly",
		"agg_cost_distribution_monthly",
		"agg_cost_anomaly_monthly",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}

	subJoin := p.subAccountJoin()
	billingPeriod := p.billingMonthStartExpr()
	billed := p.castCost("f.billed_cost")
	effective := p.castCost("f.effective_cost")
	list := p.castCost("f.list_cost")
	contracted := p.castCost("f.contracted_cost")
	now := p.nowUTC()

	dailySQL := fmt.Sprintf(`
		INSERT INTO agg_cost_daily (
		  charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
		  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
		SELECT f.charge_date, %s, a.provider, f.sub_account_sk, f.service_sk, f.region_sk,
		  SUM(%s), SUM(%s), SUM(%s), SUM(%s),
		  SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY f.charge_date, %s, a.provider, f.sub_account_sk, f.service_sk, f.region_sk`,
		billingPeriod, billed, effective, list, contracted, now, subJoin, billingPeriod)
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
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, f.sub_account_sk, svc.service_category, f.charge_category_sk`,
		billingPeriod, billed, effective, list, contracted, now, subJoin, billingPeriod)
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
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, t.tag_key, t.tag_value`,
		billingPeriod, billed, effective, now, subJoin, billingPeriod)
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
		WHERE f.commitment_sk IS NOT NULL AND f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`,
		billingPeriod, effective, commitQty, now, subJoin, billingPeriod)
	if _, err := tx.ExecContext(ctx, commitMonthlySQL); err != nil {
		return fmt.Errorf("agg_commitment_utilization: %w", err)
	}

	commitDailySQL := fmt.Sprintf(`
		INSERT INTO agg_commitment_utilization_daily (charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
		SELECT f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.commitment_sk IS NOT NULL AND f.sub_account_sk IS NOT NULL
		GROUP BY f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`,
		effective, commitQty, now, subJoin)
	if _, err := tx.ExecContext(ctx, commitDailySQL); err != nil {
		return fmt.Errorf("agg_commitment_utilization_daily: %w", err)
	}

	savingsSQL := fmt.Sprintf(`
		INSERT INTO agg_savings_summary (month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc)
		SELECT %s, a.provider, f.service_sk,
		  SUM(%s), 0, 0, %s
		FROM fact_focus_cost_daily f
		%s
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, f.service_sk`,
		billingPeriod, effective, now, subJoin, billingPeriod)
	if _, err := tx.ExecContext(ctx, savingsSQL); err != nil {
		return fmt.Errorf("agg_savings_summary: %w", err)
	}

	return p.rebuildAppAggregates(ctx, tx)
}

// satisfy unused import when aggregates only use ctx
var _ = sql.ErrNoRows
