package etl

import (
	"context"
	"database/sql"
)

func (p *Processor) rebuildAggregates(ctx context.Context, tx *sql.Tx) error {
	tables := []string{
		"agg_cost_daily", "agg_cost_monthly", "agg_cost_by_tag",
		"agg_commitment_utilization", "agg_commitment_utilization_daily", "agg_savings_summary",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agg_cost_daily (
		  charge_date, provider, billing_account_sk, service_sk, region_sk,
		  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
		SELECT f.charge_date, a.provider, f.billing_account_sk, f.service_sk, f.region_sk,
		  SUM(CAST(f.billed_cost AS REAL)), SUM(CAST(f.effective_cost AS REAL)),
		  SUM(CAST(f.list_cost AS REAL)), SUM(CAST(f.contracted_cost AS REAL)),
		  SUM(f.line_count), datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		GROUP BY f.charge_date, a.provider, f.billing_account_sk, f.service_sk, f.region_sk`); err != nil {
		if p.Dialect == "sqlserver" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agg_cost_daily (
				  charge_date, provider, billing_account_sk, service_sk, region_sk,
				  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
				SELECT f.charge_date, a.provider, f.billing_account_sk, f.service_sk, f.region_sk,
				  SUM(CAST(f.billed_cost AS DECIMAL(28,10))), SUM(CAST(f.effective_cost AS DECIMAL(28,10))),
				  SUM(CAST(f.list_cost AS DECIMAL(28,10))), SUM(CAST(f.contracted_cost AS DECIMAL(28,10))),
				  SUM(f.line_count), SYSUTCDATETIME()
				FROM fact_focus_cost_daily f
				INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
				GROUP BY f.charge_date, a.provider, f.billing_account_sk, f.service_sk, f.region_sk`)
		}
		if err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agg_cost_monthly (
		  month_start, provider, billing_account_sk, service_category, charge_category_sk,
		  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
		SELECT substr(f.charge_date,1,7)||'-01', a.provider, f.billing_account_sk, svc.service_category, f.charge_category_sk,
		  SUM(CAST(f.billed_cost AS REAL)), SUM(CAST(f.effective_cost AS REAL)),
		  SUM(CAST(f.list_cost AS REAL)), SUM(CAST(f.contracted_cost AS REAL)),
		  SUM(f.line_count), datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
		GROUP BY substr(f.charge_date,1,7)||'-01', a.provider, f.billing_account_sk, svc.service_category, f.charge_category_sk`); err != nil {
		if p.Dialect == "sqlserver" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agg_cost_monthly (
				  month_start, provider, billing_account_sk, service_category, charge_category_sk,
				  billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc)
				SELECT DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.billing_account_sk, svc.service_category, f.charge_category_sk,
				  SUM(CAST(f.billed_cost AS DECIMAL(28,10))), SUM(CAST(f.effective_cost AS DECIMAL(28,10))),
				  SUM(CAST(f.list_cost AS DECIMAL(28,10))), SUM(CAST(f.contracted_cost AS DECIMAL(28,10))),
				  SUM(f.line_count), SYSUTCDATETIME()
				FROM fact_focus_cost_daily f
				INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
				INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
				GROUP BY DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.billing_account_sk, svc.service_category, f.charge_category_sk`)
		}
		if err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agg_cost_by_tag (month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc)
		SELECT substr(f.charge_date,1,7)||'-01', a.provider, t.tag_key, t.tag_value,
		  SUM(CAST(f.effective_cost AS REAL)), SUM(CAST(f.billed_cost AS REAL)), SUM(f.line_count), datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		INNER JOIN bridge_cost_tag b ON b.cost_daily_id = f.cost_daily_id
		INNER JOIN dim_tag t ON b.tag_sk = t.tag_sk
		GROUP BY substr(f.charge_date,1,7)||'-01', a.provider, t.tag_key, t.tag_value`); err != nil {
		if p.Dialect == "sqlserver" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agg_cost_by_tag (month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc)
				SELECT DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, t.tag_key, t.tag_value,
				  SUM(CAST(f.effective_cost AS DECIMAL(28,10))), SUM(CAST(f.billed_cost AS DECIMAL(28,10))), SUM(f.line_count), SYSUTCDATETIME()
				FROM fact_focus_cost_daily f
				INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
				INNER JOIN bridge_cost_tag b ON b.cost_daily_id = f.cost_daily_id
				INNER JOIN dim_tag t ON b.tag_sk = t.tag_sk
				GROUP BY DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, t.tag_key, t.tag_value`)
		}
		if err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agg_commitment_utilization (month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
		SELECT substr(f.charge_date,1,7)||'-01', a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
		  SUM(CAST(f.effective_cost AS REAL)), SUM(CAST(f.commitment_discount_quantity AS REAL)), SUM(f.line_count), datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		WHERE f.commitment_sk IS NOT NULL
		GROUP BY substr(f.charge_date,1,7)||'-01', a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`); err != nil {
		if p.Dialect == "sqlserver" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agg_commitment_utilization (month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
				SELECT DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
				  SUM(CAST(f.effective_cost AS DECIMAL(28,10))), SUM(CAST(f.commitment_discount_quantity AS DECIMAL(28,10))), SUM(f.line_count), SYSUTCDATETIME()
				FROM fact_focus_cost_daily f
				INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
				WHERE f.commitment_sk IS NOT NULL
				GROUP BY DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`)
		}
		if err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agg_commitment_utilization_daily (charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
		SELECT f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
		  SUM(CAST(f.effective_cost AS REAL)), SUM(CAST(f.commitment_discount_quantity AS REAL)), SUM(f.line_count), datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		WHERE f.commitment_sk IS NOT NULL
		GROUP BY f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`); err != nil {
		if p.Dialect == "sqlserver" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agg_commitment_utilization_daily (charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc)
				SELECT f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown'),
				  SUM(CAST(f.effective_cost AS DECIMAL(28,10))), SUM(CAST(f.commitment_discount_quantity AS DECIMAL(28,10))), SUM(f.line_count), SYSUTCDATETIME()
				FROM fact_focus_cost_daily f
				INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
				WHERE f.commitment_sk IS NOT NULL
				GROUP BY f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status,'Unknown')`)
		}
		if err != nil {
			return err
		}
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO agg_savings_summary (month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc)
		SELECT substr(f.charge_date,1,7)||'-01', a.provider, f.service_sk,
		  SUM(CAST(f.effective_cost AS REAL)), 0, 0, datetime('now')
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		GROUP BY substr(f.charge_date,1,7)||'-01', a.provider, f.service_sk`)
	if p.Dialect == "sqlserver" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO agg_savings_summary (month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc)
			SELECT DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.service_sk,
			  SUM(CAST(f.effective_cost AS DECIMAL(28,10))), 0, 0, SYSUTCDATETIME()
			FROM fact_focus_cost_daily f
			INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
			GROUP BY DATEFROMPARTS(YEAR(f.charge_date), MONTH(f.charge_date), 1), a.provider, f.service_sk`)
	}
	return err
}

// satisfy unused import when aggregates only use ctx
var _ = sql.ErrNoRows
