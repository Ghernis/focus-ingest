package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/store"
)

var factSKCols = map[int]string{
	1:  "dim_account",
	2:  "dim_sub_account",
	3:  "dim_resource",
	4:  "dim_service",
	5:  "dim_sku",
	6:  "dim_region",
	9:  "dim_commitment_discount",
	11: "dim_capacity_reservation",
}

var publishFactGrainCols = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 13, 14, 15, 16, 29, 30}
var factSumDecCols = []int{17, 18, 19, 20, 21, 22, 23}
var factSumIntCols = []int{24}

var factColKinds = []aggColKind{
	aggColString,
	aggColInt, aggColIntNull, aggColIntNull, aggColInt, aggColIntNull, aggColIntNull,
	aggColInt, aggColInt, aggColInt, aggColIntNull, aggColString,
	aggColIntNull, aggColString, aggColString, aggColString, aggColString,
	aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
	aggColInt, aggColString, aggColString,
	aggColInt, aggColString, aggColString,
}

func publishFacts(ctx context.Context, local, server *sql.DB, month string, batchID int64, maps skMaps) (int, error) {
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE b FROM bridge_cost_tag b
		INNER JOIN fact_focus_cost_daily f ON f.cost_daily_id = b.cost_daily_id
		WHERE f.billing_period_start = @p1`, month); err != nil {
		return 0, fmt.Errorf("delete bridge: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM fact_focus_cost_daily WHERE billing_period_start = @p1`, month); err != nil {
		return 0, fmt.Errorf("delete facts: %w", err)
	}

	cols := `charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk, sku_sk, region_sk,
		charge_category_sk, charge_frequency_sk, pricing_category_sk, commitment_sk, commitment_discount_status,
		capacity_reservation_sk, capacity_reservation_status, charge_description_hash, billing_period_start,
		billing_period_end, billed_cost, effective_cost, list_cost, contracted_cost, pricing_quantity,
		consumed_quantity, commitment_discount_quantity, line_count, first_charge_period_start, last_charge_period_end,
		ingestion_batch_id, focus_version, created_utc`
	colCount := 31

	q := fmt.Sprintf(`SELECT %s FROM fact_focus_cost_daily WHERE billing_period_start = ?`, cols)
	rows, err := local.QueryContext(ctx, q, month)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	merged := map[string][]interface{}{}
	for rows.Next() {
		vals, err := scanN(rows, colCount)
		if err != nil {
			return 0, err
		}
		applySKRemap(vals, factSKCols, maps)
		vals[28] = batchID
		key := grainKey(vals, publishFactGrainCols)
		if prev, ok := merged[key]; ok {
			mergeRows(prev, vals, factSumDecCols, factSumIntCols)
		} else {
			merged[key] = append([]interface{}(nil), vals...)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	prefix := fmt.Sprintf(`INSERT INTO fact_focus_cost_daily (%s) VALUES `, cols)
	var batch [][]interface{}
	total := 0
	for _, vals := range merged {
		coerceAggVals(vals, factColKinds)
		batch = append(batch, vals)
		if len(batch) >= 100 {
			if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, colCount, batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, colCount, batch); err != nil {
			return total, err
		}
		total += len(batch)
	}
	if err := tx.Commit(); err != nil {
		return total, err
	}
	return total, nil
}

func ensurePublishBatch(ctx context.Context, server *sql.DB, month, sourceFile string) (int64, error) {
	var id int64
	err := server.QueryRowContext(ctx, `
		SELECT ingestion_batch_id FROM dim_ingestion_batch
		WHERE billing_period_start = @p1 AND status = 'PROCESSED' AND source_file = @p2`,
		month, sourceFile).Scan(&id)
	if err == nil {
		_, _ = server.ExecContext(ctx, `UPDATE dim_ingestion_batch SET aggregates_status = 'COMPLETE' WHERE ingestion_batch_id = @p1`, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := server.ExecContext(ctx, `
		INSERT INTO dim_ingestion_batch (source_provider, focus_version, source_file, billing_period_start, status, aggregates_status)
		VALUES ('MIXED', '1.2', @p1, @p2, 'PROCESSED', 'COMPLETE')`, sourceFile, month)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
