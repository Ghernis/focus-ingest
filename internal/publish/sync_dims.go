package publish

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// SyncDimsOptions controls dimension seeding from SQL Server into local SQLite.
type SyncDimsOptions struct {
	Connection string
	SQLitePath   string
	Fresh        bool
}

// SyncDims copies warehouse dimensions from SQL Server to SQLite preserving surrogate keys.
func SyncDims(ctx context.Context, opts SyncDimsOptions) error {
	if opts.Connection == "" {
		return fmt.Errorf("SQL Server connection required (--connection or FOCUS_DATABASE_URL)")
	}
	if opts.SQLitePath == "" {
		return fmt.Errorf("sqlite path required")
	}

	if opts.Fresh {
		_ = os.Remove(opts.SQLitePath)
	}

	server, err := openSQLServer(ctx, opts.Connection)
	if err != nil {
		return err
	}
	defer server.Close()

	local, err := openSQLite(ctx, opts.SQLitePath)
	if err != nil {
		return err
	}
	defer local.Close()

	if opts.Fresh {
		if err := applySQLiteSchema(ctx, local); err != nil {
			return err
		}
	} else {
		if err := ensureSyncTables(ctx, local); err != nil {
			return err
		}
	}

	if opts.Fresh {
		if err := clearDimTables(ctx, local); err != nil {
			return err
		}
	}

	copiers := []func(context.Context, *sql.DB, *sql.DB) error{
		copyDimDate,
		copyDimAccount,
		copyDimSubAccount,
		copyDimService,
		copyDimRegion,
		copyDimSKU,
		copyDimChargeCategory,
		copyDimChargeFrequency,
		copyDimPricingCategory,
		copyDimCommitment,
		copyDimCapacity,
		copyDimResource,
		copyDimTag,
		copyDimApplication,
	}

	for _, copy := range copiers {
		if err := copy(ctx, server, local); err != nil {
			return err
		}
	}

	if err := resetSQLiteSequences(ctx, local); err != nil {
		return err
	}
	if err := recordSeededMax(ctx, local); err != nil {
		return err
	}
	if opts.Fresh {
		if _, err := local.ExecContext(ctx, `DELETE FROM dim_sync_pending`); err != nil {
			return err
		}
	}
	var accounts int
	_ = local.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_account`).Scan(&accounts)
	fmt.Printf("sync-dims complete: %d accounts copied to %s\n", accounts, opts.SQLitePath)
	return nil
}

func ensureSyncTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS dim_sync_pending (
		  dim_table TEXT NOT NULL, natural_key TEXT NOT NULL, local_sk INTEGER NOT NULL,
		  PRIMARY KEY (dim_table, natural_key))`,
		`CREATE TABLE IF NOT EXISTS dim_sync_seeded_max (
		  dim_table TEXT NOT NULL PRIMARY KEY, max_sk INTEGER NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func clearDimTables(ctx context.Context, db *sql.DB) error {
	tables := []string{
		"dim_application", "dim_tag", "dim_resource", "dim_capacity_reservation",
		"dim_commitment_discount", "dim_pricing_category", "dim_charge_frequency",
		"dim_charge_category", "dim_sku", "dim_region", "dim_service",
		"dim_sub_account", "dim_account", "dim_date",
	}
	for _, t := range tables {
		if _, err := db.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	return nil
}

func batchInsert(ctx context.Context, dst *sql.DB, insertPrefix string, colCount int, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	placeholders := "(" + strings.Repeat("?,", colCount-1) + "?)"
	const batchSize = 500
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		var b strings.Builder
		b.WriteString(insertPrefix)
		args := make([]interface{}, 0, len(chunk)*colCount)
		for i, row := range chunk {
			if len(row) != colCount {
				return fmt.Errorf("row %d: expected %d cols, got %d", start+i, colCount, len(row))
			}
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(placeholders)
			args = append(args, row...)
		}
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func copyQuery(ctx context.Context, src *sql.DB, dst *sql.DB, srcQ, insertPrefix string, colCount int, scan func(*sql.Rows) ([]interface{}, error)) error {
	rows, err := src.QueryContext(ctx, srcQ)
	if err != nil {
		return err
	}
	defer rows.Close()

	var batch [][]interface{}
	for rows.Next() {
		vals, err := scan(rows)
		if err != nil {
			return err
		}
		batch = append(batch, vals)
		if len(batch) >= 2000 {
			if err := batchInsert(ctx, dst, insertPrefix, colCount, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return batchInsert(ctx, dst, insertPrefix, colCount, batch)
}

func scanN(rows *sql.Rows, n int) ([]interface{}, error) {
	dest := make([]interface{}, n)
	ptrs := make([]interface{}, n)
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	for i := range dest {
		dest[i] = normalizeVal(dest[i])
	}
	return dest, nil
}

func normalizeVal(v interface{}) interface{} {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case bool:
		if t {
			return 1
		}
		return 0
	default:
		return v
	}
}

func copyDimDate(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT date_sk, CONVERT(VARCHAR(10), full_date, 23), year_num, quarter_num, month_num, month_name,
		  CONVERT(VARCHAR(10), month_start, 23), week_num, day_of_month, day_of_week, day_name,
		  CASE WHEN is_weekend = 1 THEN 1 ELSE 0 END, fiscal_year, fiscal_quarter FROM dim_date`,
		`INSERT OR REPLACE INTO dim_date (date_sk, full_date, year_num, quarter_num, month_num, month_name,
		  month_start, week_num, day_of_month, day_of_week, day_name, is_weekend, fiscal_year, fiscal_quarter) VALUES `,
		14, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 14) })
}

func copyDimAccount(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT account_sk, provider, account_id, account_name, billing_account_type,
		  CASE WHEN is_active = 1 THEN 1 ELSE 0 END FROM dim_account`,
		`INSERT OR REPLACE INTO dim_account (account_sk, provider, account_id, account_name, billing_account_type, is_active) VALUES `,
		6, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 6) })
}

func copyDimSubAccount(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT sub_account_sk, provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk FROM dim_sub_account`,
		`INSERT OR REPLACE INTO dim_sub_account (sub_account_sk, provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk) VALUES `,
		6, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 6) })
}

func copyDimService(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT service_sk, provider, service_code, service_name, service_category, service_subcategory FROM dim_service`,
		`INSERT OR REPLACE INTO dim_service (service_sk, provider, service_code, service_name, service_category, service_subcategory) VALUES `,
		6, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 6) })
}

func copyDimRegion(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT region_sk, provider, region_id, region_name FROM dim_region`,
		`INSERT OR REPLACE INTO dim_region (region_sk, provider, region_id, region_name) VALUES `,
		4, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 4) })
}

func copyDimSKU(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT sku_sk, provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name FROM dim_sku`,
		`INSERT OR REPLACE INTO dim_sku (sku_sk, provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name) VALUES `,
		7, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 7) })
}

func copyDimChargeCategory(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT charge_category_sk, charge_category FROM dim_charge_category`,
		`INSERT OR REPLACE INTO dim_charge_category (charge_category_sk, charge_category) VALUES `,
		2, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 2) })
}

func copyDimChargeFrequency(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT charge_frequency_sk, charge_frequency FROM dim_charge_frequency`,
		`INSERT OR REPLACE INTO dim_charge_frequency (charge_frequency_sk, charge_frequency) VALUES `,
		2, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 2) })
}

func copyDimPricingCategory(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT pricing_category_sk, pricing_category FROM dim_pricing_category`,
		`INSERT OR REPLACE INTO dim_pricing_category (pricing_category_sk, pricing_category) VALUES `,
		2, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 2) })
}

func copyDimCommitment(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT commitment_sk, provider, commitment_discount_id, commitment_discount_name,
		  commitment_discount_type, commitment_discount_category, commitment_discount_unit FROM dim_commitment_discount`,
		`INSERT OR REPLACE INTO dim_commitment_discount (commitment_sk, provider, commitment_discount_id, commitment_discount_name,
		  commitment_discount_type, commitment_discount_category, commitment_discount_unit) VALUES `,
		7, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 7) })
}

func copyDimCapacity(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT capacity_reservation_sk, provider, capacity_reservation_id, capacity_reservation_status FROM dim_capacity_reservation`,
		`INSERT OR REPLACE INTO dim_capacity_reservation (capacity_reservation_sk, provider, capacity_reservation_id, capacity_reservation_status) VALUES `,
		4, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 4) })
}

func copyDimResource(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT resource_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost,
		  CONVERT(VARCHAR(10), valid_from, 23), CONVERT(VARCHAR(10), valid_to, 23),
		  CASE WHEN is_excluded = 1 THEN 1 ELSE 0 END
		  FROM dim_resource WHERE valid_to IS NULL`,
		`INSERT OR REPLACE INTO dim_resource (resource_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost,
		  valid_from, valid_to, is_excluded) VALUES `,
		19, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 19) })
}

func copyDimTag(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT tag_sk, tag_key, tag_value FROM dim_tag`,
		`INSERT OR REPLACE INTO dim_tag (tag_sk, tag_key, tag_value) VALUES `,
		3, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 3) })
}

func copyDimApplication(ctx context.Context, src, dst *sql.DB) error {
	return copyQuery(ctx, src, dst,
		`SELECT application_sk, application_name, alias_values,
		  CONVERT(VARCHAR(10), first_seen_date, 23),
		  CONVERT(VARCHAR(30), created_utc, 126),
		  CONVERT(VARCHAR(30), updated_utc, 126) FROM dim_application`,
		`INSERT OR REPLACE INTO dim_application (application_sk, application_name, alias_values, first_seen_date, created_utc, updated_utc) VALUES `,
		6, func(r *sql.Rows) ([]interface{}, error) { return scanN(r, 6) })
}

var sequenceTables = []struct {
	table string
	pk    string
}{
	{"dim_account", "account_sk"},
	{"dim_sub_account", "sub_account_sk"},
	{"dim_service", "service_sk"},
	{"dim_region", "region_sk"},
	{"dim_sku", "sku_sk"},
	{"dim_charge_category", "charge_category_sk"},
	{"dim_charge_frequency", "charge_frequency_sk"},
	{"dim_pricing_category", "pricing_category_sk"},
	{"dim_commitment_discount", "commitment_sk"},
	{"dim_capacity_reservation", "capacity_reservation_sk"},
	{"dim_resource", "resource_sk"},
	{"dim_tag", "tag_sk"},
	{"dim_application", "application_sk"},
}

func resetSQLiteSequences(ctx context.Context, db *sql.DB) error {
	for _, t := range sequenceTables {
		q := fmt.Sprintf(`INSERT OR REPLACE INTO sqlite_sequence (name, seq)
			SELECT %q, COALESCE(MAX(%s), 0) FROM %s`, t.table, t.pk, t.table)
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("sequence %s: %w", t.table, err)
		}
	}
	return nil
}

func recordSeededMax(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM dim_sync_seeded_max`); err != nil {
		return err
	}
	for _, t := range sequenceTables {
		q := fmt.Sprintf(`INSERT INTO dim_sync_seeded_max (dim_table, max_sk)
			SELECT %q, COALESCE(MAX(%s), 0) FROM %s`, t.table, t.pk, t.table)
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// DimsSeeded reports whether local SQLite is ready for hybrid import.
// True after sync-dims (even when the server has zero entity dims), or after schema apply with lookup seeds.
func DimsSeeded(ctx context.Context, sqlitePath string) (bool, error) {
	local, err := openSQLite(ctx, sqlitePath)
	if err != nil {
		return false, err
	}
	defer local.Close()

	var n int
	// sync-dims always populates this table (max_sk may be 0 per dim on first-ever deploy).
	if err := local.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_sync_seeded_max`).Scan(&n); err == nil && n > 0 {
		return true, nil
	}

	// Local schema apply seeds charge_category / dim_date without sync-dims.
	if err := local.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_charge_category`).Scan(&n); err == nil && n > 0 {
		return true, nil
	}

	err = local.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_account`).Scan(&n)
	return n > 0, err
}
