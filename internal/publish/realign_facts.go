package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/etl"
)

// factGrainCols are UQ_fact_focus_cost_daily_grain columns except the FK column being remapped.
var factGrainCols = []string{
	"charge_date",
	"billing_account_sk",
	"sub_account_sk",
	"resource_sk",
	"service_sk",
	"sku_sk",
	"region_sk",
	"charge_category_sk",
	"charge_frequency_sk",
	"pricing_category_sk",
	"commitment_sk",
	"commitment_discount_status",
	"capacity_reservation_sk",
	"capacity_reservation_status",
	"charge_description_hash",
	"billing_period_start",
	"ingestion_batch_id",
}

var factMeasureCols = []string{
	"billed_cost", "effective_cost", "list_cost", "contracted_cost",
	"pricing_quantity", "consumed_quantity", "commitment_discount_quantity",
	"line_count",
}

type factFKCol struct {
	col string
	dim string
}

var factFKCols = []factFKCol{
	{"billing_account_sk", "dim_account"},
	{"sub_account_sk", "dim_sub_account"},
	{"resource_sk", "dim_resource"},
	{"service_sk", "dim_service"},
	{"sku_sk", "dim_sku"},
	{"region_sk", "dim_region"},
	{"commitment_sk", "dim_commitment_discount"},
	{"capacity_reservation_sk", "dim_capacity_reservation"},
}

func realignFactsAndBridge(ctx context.Context, tx *sql.Tx, maps map[string]map[int64]int64) error {
	for _, fk := range factFKCols {
		m := maps[fk.dim]
		if len(m) == 0 {
			continue
		}
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			if err := realignFactColumn(ctx, tx, fk.col, oldSK, newSK); err != nil {
				return fmt.Errorf("fact %s %d->%d: %w", fk.col, oldSK, newSK, err)
			}
		}
	}
	if m := maps["dim_tag"]; len(m) > 0 {
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			if err := realignBridgeTagSK(ctx, tx, oldSK, newSK); err != nil {
				return fmt.Errorf("bridge tag_sk %d->%d: %w", oldSK, newSK, err)
			}
		}
	}
	return nil
}

func factGrainJoin(tgt, src, remapCol string, newSK, oldSK int64) string {
	var parts []string
	for _, c := range factGrainCols {
		if c == remapCol {
			continue
		}
		switch c {
		case "commitment_discount_status", "capacity_reservation_status":
			parts = append(parts, fmt.Sprintf("IFNULL(%s.%s,'') = IFNULL(%s.%s,'')", tgt, c, src, c))
		case "sub_account_sk", "resource_sk", "sku_sk", "region_sk", "charge_frequency_sk", "pricing_category_sk", "commitment_sk", "capacity_reservation_sk":
			parts = append(parts, fmt.Sprintf("IFNULL(%s.%s,-1) = IFNULL(%s.%s,-1)", tgt, c, src, c))
		default:
			parts = append(parts, fmt.Sprintf("%s.%s = %s.%s", tgt, c, src, c))
		}
	}
	parts = append(parts, fmt.Sprintf("%s.%s = %d", tgt, remapCol, newSK))
	parts = append(parts, fmt.Sprintf("%s.%s = %d", src, remapCol, oldSK))
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += " AND " + parts[i]
	}
	return out
}

func realignFactColumn(ctx context.Context, tx *sql.Tx, col string, oldSK, newSK int64) error {
	join := factGrainJoin("tgt", "src", col, newSK, oldSK)

	// Copy tag bridge rows from source facts to target facts before deleting sources.
	bridgeSQL := fmt.Sprintf(`
		INSERT OR IGNORE INTO bridge_cost_tag (cost_daily_id, tag_sk)
		SELECT tgt.cost_daily_id, b.tag_sk
		FROM bridge_cost_tag b
		INNER JOIN fact_focus_cost_daily src ON src.cost_daily_id = b.cost_daily_id
		INNER JOIN fact_focus_cost_daily tgt ON %s
		WHERE src.%s = ?`, join, col)
	if _, err := tx.ExecContext(ctx, bridgeSQL, oldSK); err != nil {
		return fmt.Errorf("bridge copy: %w", err)
	}

	// Merge measure columns from conflicting source rows into target rows.
	for _, measure := range factMeasureCols {
		var setExpr string
		if measure == "line_count" {
			setExpr = fmt.Sprintf(`%s = %s + COALESCE((
				SELECT SUM(src.%s) FROM fact_focus_cost_daily src
				INNER JOIN fact_focus_cost_daily tgt ON %s AND tgt.cost_daily_id = fact_focus_cost_daily.cost_daily_id
				WHERE src.%s = ?
			), 0)`, measure, measure, measure, join, col)
		} else {
			setExpr = fmt.Sprintf(`%s = printf('%%f', CAST(%s AS REAL) + COALESCE((
				SELECT SUM(CAST(src.%s AS REAL)) FROM fact_focus_cost_daily src
				INNER JOIN fact_focus_cost_daily tgt ON %s AND tgt.cost_daily_id = fact_focus_cost_daily.cost_daily_id
				WHERE src.%s = ?
			), 0))`, measure, measure, measure, join, col)
		}
		upd := fmt.Sprintf(`UPDATE fact_focus_cost_daily SET %s
			WHERE %s = ? AND EXISTS (
				SELECT 1 FROM fact_focus_cost_daily src
				INNER JOIN fact_focus_cost_daily tgt ON %s AND tgt.cost_daily_id = fact_focus_cost_daily.cost_daily_id
				WHERE src.%s = ?
			)`, setExpr, col, join, col)
		if _, err := tx.ExecContext(ctx, upd, oldSK, newSK, oldSK); err != nil {
			return fmt.Errorf("merge %s: %w", measure, err)
		}
	}

	// Remove bridge rows for source facts that will be deleted.
	delBridge := fmt.Sprintf(`
		DELETE FROM bridge_cost_tag WHERE cost_daily_id IN (
			SELECT src.cost_daily_id FROM fact_focus_cost_daily src
			INNER JOIN fact_focus_cost_daily tgt ON %s
			WHERE src.%s = ?
		)`, join, col)
	if _, err := tx.ExecContext(ctx, delBridge, oldSK); err != nil {
		return fmt.Errorf("bridge delete: %w", err)
	}

	// Delete conflicting source fact rows (measures already merged into target).
	delSrc := fmt.Sprintf(`
		DELETE FROM fact_focus_cost_daily WHERE %s = ? AND cost_daily_id IN (
			SELECT src.cost_daily_id FROM fact_focus_cost_daily src
			INNER JOIN fact_focus_cost_daily tgt ON %s
			WHERE src.%s = ?
		)`, col, join, col)
	if _, err := tx.ExecContext(ctx, delSrc, oldSK, oldSK); err != nil {
		return fmt.Errorf("delete conflicting: %w", err)
	}

	// Remap remaining rows (no collision).
	upd := fmt.Sprintf(`UPDATE fact_focus_cost_daily SET %s = ? WHERE %s = ?`, col, col)
	if _, err := tx.ExecContext(ctx, upd, newSK, oldSK); err != nil {
		return err
	}
	return nil
}

func realignBridgeTagSK(ctx context.Context, tx *sql.Tx, oldSK, newSK int64) error {
	delDup := `
		DELETE FROM bridge_cost_tag WHERE rowid IN (
			SELECT b1.rowid FROM bridge_cost_tag b1
			INNER JOIN bridge_cost_tag b2 ON b1.cost_daily_id = b2.cost_daily_id AND b2.tag_sk = ?
			WHERE b1.tag_sk = ?
		)`
	if _, err := tx.ExecContext(ctx, delDup, newSK, oldSK); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE bridge_cost_tag SET tag_sk = ? WHERE tag_sk = ?`, newSK, oldSK)
	return err
}

func rebuildLocalAggregatesAfterRealign(ctx context.Context, db *sql.DB, months []string) error {
	if len(months) == 0 {
		return nil
	}
	proc := &etl.Processor{DB: db, Dialect: "sqlite"}
	for _, month := range months {
		if err := proc.RebuildAggregatesForMonth(ctx, month); err != nil {
			return fmt.Errorf("rebuild aggregates %s: %w", month, err)
		}
		fmt.Printf("  rebuilt local aggregates for %s\n", month)
	}
	return nil
}
