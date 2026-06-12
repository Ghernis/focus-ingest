package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/store"
)

type aggCopySpec struct {
	table       string
	localWhere  string
	serverCols  string
	localCols   string
	colCount    int
}

func publishAggregates(ctx context.Context, local, server *sql.DB, month string) error {
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	proc := &etl.Processor{DB: server, Dialect: "sqlserver"}
	if err := proc.DeleteAggregatesForMonth(ctx, tx, month, true); err != nil {
		return fmt.Errorf("delete server aggs: %w", err)
	}

	m := month
	specs := []aggCopySpec{
		{
			table: "agg_cost_daily",
			localWhere: fmt.Sprintf(`billing_period_start = '%s'`, m),
			serverCols: `charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
			localCols: `charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
			colCount: 12,
		},
		{
			table: "agg_cost_monthly",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, sub_account_sk, service_category, charge_category_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
			localCols: `month_start, provider, sub_account_sk, service_category, charge_category_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
			colCount: 11,
		},
		{
			table: "agg_cost_by_tag",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc`,
			localCols: `month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc`,
			colCount: 8,
		},
		{
			table: "agg_commitment_utilization",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
			localCols: `month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
			colCount: 8,
		},
		{
			table: "agg_savings_summary",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc`,
			localCols: `month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc`,
			colCount: 7,
		},
		{
			table: "agg_app_monthly",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, application_sk, environment, billed_cost, effective_cost, line_count, refreshed_utc`,
			localCols: `month_start, provider, application_sk, environment, billed_cost, effective_cost, line_count, refreshed_utc`,
			colCount: 8,
		},
		{
			table: "agg_app_service_monthly",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, application_sk, environment, service_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
			localCols: `month_start, provider, application_sk, environment, service_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
			colCount: 9,
		},
		{
			table: "agg_app_service_resource_monthly",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, application_sk, environment, service_sk, resource_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
			localCols: `month_start, provider, application_sk, environment, service_sk, resource_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
			colCount: 10,
		},
		{
			table: "agg_cost_distribution_monthly",
			localWhere: fmt.Sprintf(`month_start = '%s'`, m),
			serverCols: `month_start, provider, level_name, parent_key, entity_count, total_cost, min_cost, p50_cost, p75_cost,
				p90_cost, p95_cost, p99_cost, max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, refreshed_utc`,
			localCols: `month_start, provider, level_name, parent_key, entity_count, total_cost, min_cost, p50_cost, p75_cost,
				p90_cost, p95_cost, p99_cost, max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, refreshed_utc`,
			colCount: 20,
		},
	}

	for _, spec := range specs {
		n, err := copyAggTable(ctx, local, tx, spec)
		if err != nil {
			return err
		}
		fmt.Printf("  published %d rows to %s\n", n, spec.table)
	}

	commitDailyN, err := copyCommitmentDaily(ctx, local, tx, month)
	if err != nil {
		return err
	}
	fmt.Printf("  published %d rows to agg_commitment_utilization_daily\n", commitDailyN)

	if err := proc.RebuildCostAnomaliesForMonth(ctx, tx, month); err != nil {
		return fmt.Errorf("anomaly rebuild: %w", err)
	}

	return tx.Commit()
}

func copyAggTable(ctx context.Context, local *sql.DB, serverTx *sql.Tx, spec aggCopySpec) (int, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, spec.localCols, spec.table, spec.localWhere)
	rows, err := local.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var batch [][]interface{}
	total := 0
	prefix := fmt.Sprintf(`INSERT INTO %s (%s) VALUES `, spec.table, spec.serverCols)
	for rows.Next() {
		vals, err := scanN(rows, spec.colCount)
		if err != nil {
			return total, err
		}
		batch = append(batch, vals)
		if len(batch) >= 200 {
			if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
			return total, err
		}
		total += len(batch)
	}
	return total, nil
}

func copyCommitmentDaily(ctx context.Context, local *sql.DB, serverTx *sql.Tx, month string) (int, error) {
	q := fmt.Sprintf(`
		SELECT charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc
		FROM agg_commitment_utilization_daily
		WHERE charge_date IN (SELECT DISTINCT charge_date FROM fact_focus_cost_daily WHERE billing_period_start = '%s')`, month)
	rows, err := local.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	spec := aggCopySpec{
		table: "agg_commitment_utilization_daily",
		serverCols: `charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
		colCount: 8,
	}
	var batch [][]interface{}
	total := 0
	prefix := fmt.Sprintf(`INSERT INTO %s (%s) VALUES `, spec.table, spec.serverCols)
	for rows.Next() {
		vals, err := scanN(rows, spec.colCount)
		if err != nil {
			return total, err
		}
		batch = append(batch, vals)
		if len(batch) >= 200 {
			if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
			return total, err
		}
		total += len(batch)
	}
	return total, rows.Err()
}
