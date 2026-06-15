package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/store"
)

type aggPublishSpec struct {
	aggCopySpec
	grainCols  []int
	sumDecCols []int
	sumIntCols []int
	skCols     map[int]string // column index -> dim table
	colKinds   []aggColKind
}

func aggSpecs(month string) []aggPublishSpec {
	m := month
	return []aggPublishSpec{
		{
			aggCopySpec: aggCopySpec{
				table: "agg_cost_daily", localWhere: fmt.Sprintf(`billing_period_start = '%s'`, m),
				serverCols: `charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
				localCols: `charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
				colCount: 12,
			},
			grainCols:  []int{0, 1, 2, 3, 4, 5},
			sumDecCols: []int{6, 7, 8, 9},
			sumIntCols: []int{10},
			skCols:     map[int]string{3: "dim_sub_account", 4: "dim_service", 5: "dim_region"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColString,
				aggColInt, aggColInt, aggColIntNull,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table: "agg_cost_monthly", localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, sub_account_sk, service_category, charge_category_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
				localCols: `month_start, provider, sub_account_sk, service_category, charge_category_sk,
				billed_cost, effective_cost, list_cost, contracted_cost, line_count, refreshed_utc`,
				colCount: 11,
			},
			grainCols:  []int{0, 1, 2, 3, 4},
			sumDecCols: []int{5, 6, 7, 8},
			sumIntCols: []int{9},
			skCols:     map[int]string{2: "dim_sub_account"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt, aggColString, aggColInt,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_cost_by_tag",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc`,
				localCols:  `month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count, refreshed_utc`,
				colCount:   8,
			},
			grainCols:  []int{0, 1, 2, 3},
			sumDecCols: []int{4, 5},
			sumIntCols: []int{6},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColString, aggColString,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_commitment_utilization",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
				localCols:  `month_start, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
				colCount:   8,
			},
			grainCols:  []int{0, 1, 2, 3},
			sumDecCols: []int{4, 5},
			sumIntCols: []int{6},
			skCols:     map[int]string{2: "dim_commitment_discount"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt, aggColString,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_savings_summary",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc`,
				localCols:  `month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count, refreshed_utc`,
				colCount:   7,
			},
			grainCols:  []int{0, 1, 2},
			sumDecCols: []int{3, 4},
			sumIntCols: []int{5},
			skCols:     map[int]string{2: "dim_service"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_app_monthly",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, application_sk, environment, billed_cost, effective_cost, line_count, refreshed_utc`,
				localCols:  `month_start, provider, application_sk, environment, billed_cost, effective_cost, line_count, refreshed_utc`,
				colCount:   8,
			},
			grainCols:  []int{0, 1, 2, 3},
			sumDecCols: []int{4, 5},
			sumIntCols: []int{6},
			skCols:     map[int]string{2: "dim_application"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt, aggColString,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_app_service_monthly",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, application_sk, environment, service_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
				localCols:  `month_start, provider, application_sk, environment, service_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
				colCount:   9,
			},
			grainCols:  []int{0, 1, 2, 3, 4},
			sumDecCols: []int{5, 6},
			sumIntCols: []int{7},
			skCols:     map[int]string{2: "dim_application", 4: "dim_service"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt, aggColString, aggColInt,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_app_service_resource_monthly",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				colCount:   10,
				serverCols: `month_start, provider, application_sk, environment, service_sk, resource_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
				localCols:  `month_start, provider, application_sk, environment, service_sk, resource_sk, billed_cost, effective_cost, line_count, refreshed_utc`,
			},
			grainCols:  []int{0, 1, 2, 3, 4, 5},
			sumDecCols: []int{6, 7},
			sumIntCols: []int{8},
			skCols:     map[int]string{2: "dim_application", 4: "dim_service", 5: "dim_resource"},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColInt, aggColString, aggColInt, aggColInt,
				aggColDecimal, aggColDecimal, aggColInt, aggColString,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "agg_cost_distribution_monthly",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				colCount:   20,
				serverCols: `month_start, provider, level_name, parent_key, entity_count, total_cost, min_cost, p50_cost, p75_cost,
				p90_cost, p95_cost, p99_cost, max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, refreshed_utc`,
				localCols: `month_start, provider, level_name, parent_key, entity_count, total_cost, min_cost, p50_cost, p75_cost,
				p90_cost, p95_cost, p99_cost, max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, refreshed_utc`,
			},
			grainCols: []int{0, 1, 2, 3},
			colKinds: []aggColKind{
				aggColString, aggColString, aggColString, aggColString,
				aggColInt,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColDecimal, aggColDecimal, aggColString,
			},
		},
	}
}

func applySKRemap(vals []interface{}, skCols map[int]string, maps skMaps) {
	for idx, dim := range skCols {
		vals[idx] = maps.remap(dim, vals[idx])
	}
}

func copyAggTableRemapped(ctx context.Context, local *sql.DB, serverTx *sql.Tx, spec aggPublishSpec, maps skMaps) (int, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, spec.localCols, spec.table, spec.localWhere)
	rows, err := local.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	merged := map[string][]interface{}{}
	for rows.Next() {
		vals, err := scanN(rows, spec.colCount)
		if err != nil {
			return 0, err
		}
		if len(spec.skCols) > 0 {
			applySKRemap(vals, spec.skCols, maps)
		}
		key := grainKey(vals, spec.grainCols)
		if prev, ok := merged[key]; ok {
			mergeRows(prev, vals, spec.sumDecCols, spec.sumIntCols)
		} else {
			dup := append([]interface{}(nil), vals...)
			merged[key] = dup
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var batch [][]interface{}
	total := 0
	prefix := fmt.Sprintf(`INSERT INTO %s (%s) VALUES `, spec.table, spec.serverCols)
	for _, vals := range merged {
		coerceAggVals(vals, spec.colKinds)
		batch = append(batch, vals)
		if len(batch) >= 200 {
			if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
				return total, fmt.Errorf("%s: %w", spec.table, err)
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
			return total, fmt.Errorf("%s: %w", spec.table, err)
		}
		total += len(batch)
	}
	return total, nil
}

func copyCommitmentDailyRemapped(ctx context.Context, local *sql.DB, serverTx *sql.Tx, month string, maps skMaps) (int, error) {
	q := fmt.Sprintf(`
		SELECT charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc
		FROM agg_commitment_utilization_daily
		WHERE charge_date IN (SELECT DISTINCT charge_date FROM fact_focus_cost_daily WHERE billing_period_start = '%s')`, month)
	rows, err := local.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	spec := aggPublishSpec{
		grainCols:  []int{0, 1, 2, 3},
		sumDecCols: []int{4, 5},
		sumIntCols: []int{6},
		skCols:     map[int]string{2: "dim_commitment_discount"},
		colKinds: []aggColKind{
			aggColString, aggColString, aggColInt, aggColString,
			aggColDecimal, aggColDecimal, aggColInt, aggColString,
		},
		aggCopySpec: aggCopySpec{
			table:      "agg_commitment_utilization_daily",
			serverCols: `charge_date, provider, commitment_sk, commitment_status, effective_cost, commitment_quantity, line_count, refreshed_utc`,
			colCount:   8,
		},
	}

	merged := map[string][]interface{}{}
	for rows.Next() {
		vals, err := scanN(rows, 8)
		if err != nil {
			return 0, err
		}
		applySKRemap(vals, spec.skCols, maps)
		key := grainKey(vals, spec.grainCols)
		if prev, ok := merged[key]; ok {
			mergeRows(prev, vals, spec.sumDecCols, spec.sumIntCols)
		} else {
			merged[key] = append([]interface{}(nil), vals...)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var batch [][]interface{}
	total := 0
	prefix := fmt.Sprintf(`INSERT INTO %s (%s) VALUES `, spec.table, spec.serverCols)
	for _, vals := range merged {
		coerceAggVals(vals, spec.colKinds)
		batch = append(batch, vals)
		if len(batch) >= 200 {
			if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
				return total, fmt.Errorf("%s: %w", spec.table, err)
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, serverTx, prefix, spec.colCount, batch); err != nil {
			return total, fmt.Errorf("%s: %w", spec.table, err)
		}
		total += len(batch)
	}
	return total, nil
}
