package publish

import (
	"context"
	"database/sql"
	"fmt"
)

// realignLocalSKs updates local SQLite FK columns when server assigned different surrogate keys.
func realignLocalSKs(ctx context.Context, local *sql.DB, maps map[string]map[int64]int64) error {
	if len(maps) == 0 {
		return nil
	}
	tx, err := local.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Child rows are updated before parent PKs exist at the new SK; disable FK checks for this pass.
	if _, err := tx.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}

	for dim, m := range maps {
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			if err := ensureDimTargetRow(ctx, tx, dim, oldSK, newSK); err != nil {
				return fmt.Errorf("ensure %s %d->%d: %w", dim, oldSK, newSK, err)
			}
		}
	}

	type colMap struct {
		table string
		col   string
		dim   string
	}
	updates := []colMap{
		{"fact_focus_cost_daily", "billing_account_sk", "dim_account"},
		{"fact_focus_cost_daily", "sub_account_sk", "dim_sub_account"},
		{"fact_focus_cost_daily", "resource_sk", "dim_resource"},
		{"fact_focus_cost_daily", "service_sk", "dim_service"},
		{"fact_focus_cost_daily", "sku_sk", "dim_sku"},
		{"fact_focus_cost_daily", "region_sk", "dim_region"},
		{"fact_focus_cost_daily", "commitment_sk", "dim_commitment_discount"},
		{"fact_focus_cost_daily", "capacity_reservation_sk", "dim_capacity_reservation"},
		{"bridge_cost_tag", "tag_sk", "dim_tag"},
		{"agg_cost_daily", "sub_account_sk", "dim_sub_account"},
		{"agg_cost_daily", "service_sk", "dim_service"},
		{"agg_cost_daily", "region_sk", "dim_region"},
		{"agg_cost_monthly", "sub_account_sk", "dim_sub_account"},
		{"agg_commitment_utilization", "commitment_sk", "dim_commitment_discount"},
		{"agg_commitment_utilization_daily", "commitment_sk", "dim_commitment_discount"},
		{"agg_savings_summary", "service_sk", "dim_service"},
		{"agg_app_monthly", "application_sk", "dim_application"},
		{"agg_app_service_monthly", "application_sk", "dim_application"},
		{"agg_app_service_monthly", "service_sk", "dim_service"},
		{"agg_app_service_resource_monthly", "application_sk", "dim_application"},
		{"agg_app_service_resource_monthly", "service_sk", "dim_service"},
		{"agg_app_service_resource_monthly", "resource_sk", "dim_resource"},
		{"agg_cost_anomaly_monthly", "application_sk", "dim_application"},
		{"agg_cost_anomaly_monthly", "service_sk", "dim_service"},
	}

	for _, u := range updates {
		m := maps[u.dim]
		if len(m) == 0 {
			continue
		}
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			q := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, u.table, u.col, u.col)
			if _, err := tx.ExecContext(ctx, q, newSK, oldSK); err != nil {
				return fmt.Errorf("realign %s.%s %d->%d: %w", u.table, u.col, oldSK, newSK, err)
			}
		}
	}

	for dim, m := range maps {
		pk := dimPK(dim)
		if pk == "" {
			continue
		}
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, dim, pk), oldSK); err != nil {
				return fmt.Errorf("realign delete %s %d: %w", dim, oldSK, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE dim_sync_pending SET local_sk = ? WHERE dim_table = ? AND local_sk = ?`, newSK, dim, oldSK); err != nil {
				return err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureDimTargetRow(ctx context.Context, tx *sql.Tx, table string, oldSK, newSK int64) error {
	q, ok := dimCopySQL[table]
	if !ok {
		return fmt.Errorf("no copy SQL for %s", table)
	}
	pk := dimPK(table)
	var one int
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT 1 FROM %s WHERE %s = ? LIMIT 1`, table, pk), newSK).Scan(&one)
	if err == nil {
		return nil // target SK already present (from sync-dims)
	}
	if err != sql.ErrNoRows {
		return err
	}
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT 1 FROM %s WHERE %s = ? LIMIT 1`, table, pk), oldSK).Scan(&one)
	if err == sql.ErrNoRows {
		return nil // old row gone; target must come from server seed
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, q, newSK, oldSK)
	return err
}

// dimCopySQL copies a dimension row to a new surrogate key (INSERT OR IGNORE).
var dimCopySQL = map[string]string{
	"dim_account": `INSERT OR IGNORE INTO dim_account (account_sk, provider, account_id, account_name, billing_account_type, is_active)
		SELECT ?, provider, account_id, account_name, billing_account_type, is_active FROM dim_account WHERE account_sk = ?`,
	"dim_sub_account": `INSERT OR IGNORE INTO dim_sub_account (sub_account_sk, provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
		SELECT ?, provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk FROM dim_sub_account WHERE sub_account_sk = ?`,
	"dim_service": `INSERT OR IGNORE INTO dim_service (service_sk, provider, service_code, service_name, service_category, service_subcategory)
		SELECT ?, provider, service_code, service_name, service_category, service_subcategory FROM dim_service WHERE service_sk = ?`,
	"dim_region": `INSERT OR IGNORE INTO dim_region (region_sk, provider, region_id, region_name)
		SELECT ?, provider, region_id, region_name FROM dim_region WHERE region_sk = ?`,
	"dim_sku": `INSERT OR IGNORE INTO dim_sku (sku_sk, provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
		SELECT ?, provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name FROM dim_sku WHERE sku_sk = ?`,
	"dim_commitment_discount": `INSERT OR IGNORE INTO dim_commitment_discount (commitment_sk, provider, commitment_discount_id, commitment_discount_name,
		commitment_discount_type, commitment_discount_category, commitment_discount_unit)
		SELECT ?, provider, commitment_discount_id, commitment_discount_name, commitment_discount_type, commitment_discount_category, commitment_discount_unit
		FROM dim_commitment_discount WHERE commitment_sk = ?`,
	"dim_capacity_reservation": `INSERT OR IGNORE INTO dim_capacity_reservation (capacity_reservation_sk, provider, capacity_reservation_id, capacity_reservation_status)
		SELECT ?, provider, capacity_reservation_id, capacity_reservation_status FROM dim_capacity_reservation WHERE capacity_reservation_sk = ?`,
	"dim_resource": `INSERT OR IGNORE INTO dim_resource (resource_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, valid_to, is_excluded)
		SELECT ?, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, valid_to, is_excluded
		FROM dim_resource WHERE resource_sk = ?`,
	"dim_tag": `INSERT OR IGNORE INTO dim_tag (tag_sk, tag_key, tag_value)
		SELECT ?, tag_key, tag_value FROM dim_tag WHERE tag_sk = ?`,
	"dim_application": `INSERT OR IGNORE INTO dim_application (application_sk, application_name, alias_values, first_seen_date, created_utc, updated_utc)
		SELECT ?, application_name, alias_values, first_seen_date, created_utc, updated_utc FROM dim_application WHERE application_sk = ?`,
}

func dimPK(table string) string {
	for _, t := range sequenceTables {
		if t.table == table {
			return t.pk
		}
	}
	return ""
}
