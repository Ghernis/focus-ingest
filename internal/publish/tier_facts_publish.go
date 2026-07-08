package publish

import (
	"context"
	"database/sql"
	"fmt"
)

func publishTierFactsForMonth(ctx context.Context, local *sql.DB, serverTx *sql.Tx, month string, maps skMaps) error {
	for _, spec := range tierFactSpecs(month) {
		n, err := copyAggTableRemapped(ctx, local, serverTx, spec, maps)
		if err != nil {
			return err
		}
		fmt.Printf("  published %d rows to %s\n", n, spec.table)
	}
	return nil
}

func publishTierFactsMerge(ctx context.Context, local *sql.DB, serverTx *sql.Tx, month string, maps skMaps) error {
	for _, spec := range tierFactMergeSpecs(month) {
		serverRows, err := readAggRowsTx(ctx, serverTx, spec)
		if err != nil {
			return err
		}
		localRows, err := readAggRows(ctx, local, spec, maps, true)
		if err != nil {
			return err
		}
		merged := mergeLocalOverrideRows(serverRows, localRows)
		merged = consolidateAggRows(merged, spec)
		if err := deleteAggTableMonth(ctx, serverTx, spec); err != nil {
			return err
		}
		n, err := writeAggRows(ctx, serverTx, spec, merged)
		if err != nil {
			return err
		}
		fmt.Printf("  merged %d rows to %s\n", n, spec.table)
	}
	return nil
}

func mergeLocalOverrideRows(base, local map[string][]interface{}) map[string][]interface{} {
	out := make(map[string][]interface{}, len(base)+len(local))
	for k, v := range base {
		out[k] = append([]interface{}(nil), v...)
	}
	for k, v := range local {
		out[k] = append([]interface{}(nil), v...)
	}
	return out
}

func tierFactMergeSpecs(month string) []aggPublishSpec {
	all := tierFactSpecs(month)
	return all
}

func readAggRowsTx(ctx context.Context, tx *sql.Tx, spec aggPublishSpec) (map[string][]interface{}, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, spec.serverCols, spec.table, spec.localWhere)
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]interface{}{}
	for rows.Next() {
		vals, err := scanN(rows, spec.colCount)
		if err != nil {
			return nil, err
		}
		key := grainKeyWithNorm(vals, spec.grainCols, spec.grainNorms)
		if prev, ok := out[key]; ok {
			mergeRows(prev, vals, spec.sumDecCols, spec.sumIntCols)
		} else {
			dup := append([]interface{}(nil), vals...)
			out[key] = dup
		}
	}
	return out, rows.Err()
}

func tierFactSpecs(month string) []aggPublishSpec {
	m := month
	return []aggPublishSpec{
		{
			aggCopySpec: aggCopySpec{
				table:      "fact_resource_tier_daily",
				localWhere: fmt.Sprintf(`billing_period_start = '%s'`, m),
				serverCols: `charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
				tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc`,
				localCols: `charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
				tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty, refreshed_utc`,
				colCount: 14,
			},
			grainCols:  []int{0, 1, 2, 3, 4, 7, 8, 9, 10},
			grainNorms: []grainNorm{grainNormDate, grainNormDate, grainNormFold, grainNormInt, grainNormInt, grainNormDefault, grainNormInt, grainNormInt, grainNormDefault},
			skCols:     map[int]string{3: "dim_resource", 4: "dim_service", 5: "dim_application", 9: "dim_sku"},
			colKinds: []aggColKind{
				aggColDate, aggColDate, aggColString, aggColInt, aggColInt, aggColInt, aggColString,
				aggColString, aggColInt, aggColInt, aggColDecimal, aggColDecimal, aggColDecimal, aggColDateTime,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "fact_resource_tier_change",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `change_scope, month_start, change_date, provider, resource_sk, service_sk, application_sk, environment,
				prior_tier_code, new_tier_code, prior_tier_rank, new_tier_rank,
				prior_tier_sku_sk, new_tier_sku_sk, prior_unit_rate, new_unit_rate, post_change_quantity,
				total_qty_on_new_tier, counterfactual_cost_on_new_tier,
				days_on_prior_tier, days_on_new_tier,
				realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc`,
				localCols: `change_scope, month_start, change_date, provider, resource_sk, service_sk, application_sk, environment,
				prior_tier_code, new_tier_code, prior_tier_rank, new_tier_rank,
				prior_tier_sku_sk, new_tier_sku_sk, prior_unit_rate, new_unit_rate, post_change_quantity,
				total_qty_on_new_tier, counterfactual_cost_on_new_tier,
				days_on_prior_tier, days_on_new_tier,
				realized_savings_unit, realized_savings_cost_delta, month_realized_savings, projected_annual_savings, change_direction, refreshed_utc`,
				colCount: 27,
			},
			grainCols:  []int{0, 1, 2, 3, 4, 5, 8, 9, 12, 13},
			grainNorms: []grainNorm{grainNormFold, grainNormDate, grainNormDate, grainNormFold, grainNormInt, grainNormInt, grainNormDefault, grainNormDefault, grainNormInt, grainNormInt},
			skCols:     map[int]string{4: "dim_resource", 5: "dim_service", 6: "dim_application", 12: "dim_sku", 13: "dim_sku"},
			colKinds: []aggColKind{
				aggColString, aggColDate, aggColDate, aggColString, aggColInt, aggColInt, aggColInt, aggColString,
				aggColString, aggColString, aggColInt, aggColInt,
				aggColInt, aggColInt, aggColDecimal, aggColDecimal, aggColDecimal,
				aggColDecimal, aggColDecimal,
				aggColInt, aggColInt,
				aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal, aggColString, aggColDateTime,
			},
		},
		{
			aggCopySpec: aggCopySpec{
				table:      "fact_resource_tier_carryforward",
				localWhere: fmt.Sprintf(`month_start = '%s'`, m),
				serverCols: `month_start, provider, resource_sk, service_sk, application_sk, environment,
				baseline_change_month, baseline_change_date,
				baseline_tier_code, baseline_tier_rank, baseline_tier_sku_sk, baseline_unit_rate,
				current_tier_code, current_tier_rank, current_tier_sku_sk, current_unit_rate,
				month_quantity, month_actual_cost, month_counterfactual_cost,
				month_realized_delta, cumulative_realized_delta,
				change_direction, refreshed_utc`,
				localCols: `month_start, provider, resource_sk, service_sk, application_sk, environment,
				baseline_change_month, baseline_change_date,
				baseline_tier_code, baseline_tier_rank, baseline_tier_sku_sk, baseline_unit_rate,
				current_tier_code, current_tier_rank, current_tier_sku_sk, current_unit_rate,
				month_quantity, month_actual_cost, month_counterfactual_cost,
				month_realized_delta, cumulative_realized_delta,
				change_direction, refreshed_utc`,
				colCount: 23,
			},
			grainCols:  []int{0, 1, 2, 3, 7},
			grainNorms: []grainNorm{grainNormDate, grainNormFold, grainNormInt, grainNormInt, grainNormDate},
			skCols:     map[int]string{2: "dim_resource", 3: "dim_service", 4: "dim_application", 10: "dim_sku", 14: "dim_sku"},
			colKinds: []aggColKind{
				aggColDate, aggColString, aggColInt, aggColInt, aggColInt, aggColString,
				aggColDate, aggColDate,
				aggColString, aggColInt, aggColInt, aggColDecimal,
				aggColString, aggColInt, aggColInt, aggColDecimal,
				aggColDecimal, aggColDecimal, aggColDecimal,
				aggColDecimal, aggColDecimal,
				aggColString, aggColDateTime,
			},
		},
	}
}
