package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/store"
)

type publishMonthMode int

const (
	publishReplace publishMonthMode = iota
	publishMerge
)

type publishMonthPlan struct {
	month string
	mode  publishMonthMode
}

// planPublishMonths decides replace vs merge per billing period. Merge applies when
// SQL Server already has a fuller agg_app_monthly snapshot and this export only
// carries a thinner overlap (late charges/credits for a prior billing period).
func planPublishMonths(ctx context.Context, local, server *sql.DB, months []string, force bool) ([]publishMonthPlan, error) {
	var plans []publishMonthPlan
	for _, m := range months {
		localN, err := countAggAppMonthly(ctx, local, m, false)
		if err != nil {
			return nil, err
		}
		if localN == 0 {
			fmt.Printf("  skipping %s: no agg_app_monthly rows in local database\n", m)
			continue
		}
		mode := publishReplace
		if !force {
			serverN, err := countAggAppMonthly(ctx, server, m, true)
			if err != nil {
				return nil, err
			}
			if serverN > localN {
				mode = publishMerge
				fmt.Printf("  merging %s: server %d agg_app_monthly rows + local %d overlap rows\n", m, serverN, localN)
			}
		}
		plans = append(plans, publishMonthPlan{month: m, mode: mode})
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("no billing periods to publish")
	}
	return plans, nil
}

func countAggAppMonthly(ctx context.Context, db *sql.DB, month string, sqlServer bool) (int, error) {
	q := `SELECT COUNT(*) FROM agg_app_monthly WHERE month_start = ?`
	if sqlServer {
		q = `SELECT COUNT(*) FROM agg_app_monthly WHERE month_start = @p1`
	}
	var n int
	if err := db.QueryRowContext(ctx, q, month).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func publishAggregatesMerge(ctx context.Context, local, server *sql.DB, month string, maps skMaps) error {
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	proc := &etl.Processor{DB: server, Dialect: "sqlserver"}

	for _, spec := range aggSpecs(month) {
		if spec.table == "agg_cost_distribution_monthly" {
			continue
		}
		serverRows, err := readAggRows(ctx, server, spec, nil, false)
		if err != nil {
			return err
		}
		localRows, err := readAggRows(ctx, local, spec, maps, true)
		if err != nil {
			return err
		}
		merged := mergeAggRowMaps(serverRows, localRows, spec)
		if err := deleteAggTableMonth(ctx, tx, spec); err != nil {
			return err
		}
		n, err := writeAggRows(ctx, tx, spec, merged)
		if err != nil {
			return err
		}
		fmt.Printf("  merged %d rows to %s\n", n, spec.table)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM agg_cost_distribution_monthly WHERE month_start = @p1`, month); err != nil {
		return fmt.Errorf("delete distribution: %w", err)
	}
	if err := proc.RebuildCostDistributionForMonth(ctx, tx, month); err != nil {
		return fmt.Errorf("rebuild distribution: %w", err)
	}

	return tx.Commit()
}

func readAggRows(ctx context.Context, db *sql.DB, spec aggPublishSpec, maps skMaps, remap bool) (map[string][]interface{}, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`, spec.serverCols, spec.table, spec.localWhere)
	rows, err := db.QueryContext(ctx, q)
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
		if remap && len(spec.skCols) > 0 {
			applySKRemap(vals, spec.skCols, maps)
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

func mergeAggRowMaps(base, delta map[string][]interface{}, spec aggPublishSpec) map[string][]interface{} {
	out := make(map[string][]interface{}, len(base)+len(delta))
	for k, v := range base {
		dup := append([]interface{}(nil), v...)
		out[k] = dup
	}
	for k, v := range delta {
		if prev, ok := out[k]; ok {
			mergeRows(prev, v, spec.sumDecCols, spec.sumIntCols)
		} else {
			dup := append([]interface{}(nil), v...)
			out[k] = dup
		}
	}
	return out
}

func deleteAggTableMonth(ctx context.Context, tx *sql.Tx, spec aggPublishSpec) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", spec.table, spec.localWhere))
	return err
}

func writeAggRows(ctx context.Context, tx *sql.Tx, spec aggPublishSpec, rows map[string][]interface{}) (int, error) {
	var batch [][]interface{}
	total := 0
	prefix := fmt.Sprintf(`INSERT INTO %s (%s) VALUES `, spec.table, spec.serverCols)
	for _, vals := range rows {
		if err := coerceAggVals(vals, spec.colKinds); err != nil {
			return total, fmt.Errorf("%s: %w; row={%s}", spec.table, err, formatAggRow(vals, spec.colKinds))
		}
		batch = append(batch, vals)
		if len(batch) >= 200 {
			if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, spec.colCount, batch); err != nil {
				return total, fmt.Errorf("%s: %w", spec.table, err)
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, spec.colCount, batch); err != nil {
			return total, fmt.Errorf("%s: %w", spec.table, err)
		}
		total += len(batch)
	}
	return total, nil
}
