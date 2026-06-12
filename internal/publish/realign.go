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

	// PRAGMA must be set on the connection before the transaction (not reliable inside tx).
	if _, err := local.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() { _, _ = local.ExecContext(ctx, `PRAGMA foreign_keys=ON`) }()

	tx, err := local.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for dim, m := range maps {
		for oldSK, newSK := range m {
			if oldSK == newSK {
				continue
			}
			if err := ensureDimTargetRow(ctx, tx, maps, dim, oldSK, newSK); err != nil {
				return fmt.Errorf("ensure %s %d->%d: %w", dim, oldSK, newSK, err)
			}
		}
	}

	if err := realignDimParentFKs(ctx, tx, maps); err != nil {
		return fmt.Errorf("dim parent fks: %w", err)
	}

	for _, u := range realignFKUpdates {
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

	return tx.Commit()
}

type fkUpdate struct {
	table string
	col   string
	dim   string
}

var realignFKUpdates = []fkUpdate{
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

// realignDimParentFKs updates FK columns inside dimension tables before old PK rows are deleted.
func realignDimParentFKs(ctx context.Context, tx *sql.Tx, maps map[string]map[int64]int64) error {
	type dimFK struct {
		table  string
		col    string
		parent string
	}
	links := []dimFK{
		{"dim_sub_account", "billing_account_sk", "dim_account"},
		{"dim_resource", "account_sk", "dim_account"},
		{"dim_resource", "sub_account_sk", "dim_sub_account"},
		{"dim_resource", "service_sk", "dim_service"},
	}
	for _, l := range links {
		pm := maps[l.parent]
		if len(pm) == 0 {
			continue
		}
		for oldSK, newSK := range pm {
			if oldSK == newSK {
				continue
			}
			q := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, l.table, l.col, l.col)
			if _, err := tx.ExecContext(ctx, q, newSK, oldSK); err != nil {
				return fmt.Errorf("%s.%s %d->%d: %w", l.table, l.col, oldSK, newSK, err)
			}
		}
	}
	return nil
}

func ensureDimTargetRow(ctx context.Context, tx *sql.Tx, maps map[string]map[int64]int64, table string, oldSK, newSK int64) error {
	pk := dimPK(table)
	var one int
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT 1 FROM %s WHERE %s = ? LIMIT 1`, table, pk), newSK).Scan(&one)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT 1 FROM %s WHERE %s = ? LIMIT 1`, table, pk), oldSK).Scan(&one)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	switch table {
	case "dim_sub_account":
		var provider, subID string
		var name, subType sql.NullString
		var billSK sql.NullInt64
		if err := tx.QueryRowContext(ctx, `
			SELECT provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk
			FROM dim_sub_account WHERE sub_account_sk = ?`, oldSK).Scan(&provider, &subID, &name, &subType, &billSK); err != nil {
			return err
		}
		if billSK.Valid {
			billSK.Int64 = remapSK(maps, "dim_account", billSK.Int64)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO dim_sub_account (sub_account_sk, provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
			VALUES (?, ?, ?, ?, ?, ?)`, newSK, provider, subID, nullIface(name), nullIface(subType), nullIfaceInt(billSK))
		return err

	case "dim_resource":
		var provider, grid, rtype, validFrom string
		var accSK, svcSK int64
		var subSK sql.NullInt64
		var region, name, owner, cc, env, app, biz, tags, hourly sql.NullString
		var excluded int
		if err := tx.QueryRowContext(ctx, `
			SELECT provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
			  region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded
			FROM dim_resource WHERE resource_sk = ?`, oldSK).Scan(
			&provider, &grid, &rtype, &accSK, &subSK, &svcSK,
			&region, &name, &owner, &cc, &env, &app, &biz, &tags, &hourly, &validFrom, &excluded); err != nil {
			return err
		}
		accSK = remapSK(maps, "dim_account", accSK)
		svcSK = remapSK(maps, "dim_service", svcSK)
		if subSK.Valid {
			subSK.Int64 = remapSK(maps, "dim_sub_account", subSK.Int64)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO dim_resource (resource_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
			  region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newSK, provider, grid, rtype, accSK, nullIfaceInt(subSK), svcSK,
			nullIface(region), nullIface(name), nullIface(owner), nullIface(cc), nullIface(env), nullIface(app), nullIface(biz),
			nullIface(tags), nullIface(hourly), validFrom, excluded)
		return err

	default:
		q, ok := dimCopySQL[table]
		if !ok {
			return fmt.Errorf("no copy SQL for %s", table)
		}
		_, err = tx.ExecContext(ctx, q, newSK, oldSK)
		return err
	}
}

func remapSK(maps map[string]map[int64]int64, table string, sk int64) int64 {
	if sk == 0 {
		return 0
	}
	if maps == nil {
		return sk
	}
	if m := maps[table]; m != nil {
		if v, ok := m[sk]; ok {
			return v
		}
	}
	return sk
}

// dimCopySQL copies a dimension row to a new surrogate key (INSERT OR IGNORE).
var dimCopySQL = map[string]string{
	"dim_account": `INSERT OR IGNORE INTO dim_account (account_sk, provider, account_id, account_name, billing_account_type, is_active)
		SELECT ?, provider, account_id, account_name, billing_account_type, is_active FROM dim_account WHERE account_sk = ?`,
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
