package etl

import (
	"context"
	"database/sql"
)

func (p *Processor) recordPendingDim(ctx context.Context, tx *sql.Tx, table, naturalKey string, sk int64) error {
	if !p.TrackPendingDims || p.Dialect != "sqlite" || sk == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO dim_sync_pending (dim_table, natural_key, local_sk)
		VALUES (?, ?, ?)`, table, naturalKey, sk)
	return err
}

func (p *Processor) dimExists(ctx context.Context, tx *sql.Tx, table, naturalKey string) (bool, error) {
	var q string
	switch table {
	case "dim_account":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_account WHERE provider = ? AND account_id = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_sub_account":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_sub_account WHERE provider = ? AND sub_account_id = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_service":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_service WHERE provider = ? AND service_code = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_region":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_region WHERE provider = ? AND region_id = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_sku":
		parts := splitNK(naturalKey, 3)
		q = `SELECT 1 FROM dim_sku WHERE provider = ? AND sku_id = ? AND IFNULL(sku_price_id,'') = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1], parts[2])
	case "dim_commitment_discount":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_commitment_discount WHERE provider = ? AND commitment_discount_id = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_capacity_reservation":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_capacity_reservation WHERE provider = ? AND capacity_reservation_id = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_resource":
		parts := splitNK(naturalKey, 2)
		q = `SELECT 1 FROM dim_resource WHERE provider = ? AND global_resource_id = ? AND valid_to IS NULL`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_tag":
		parts := splitTagNK(naturalKey)
		q = `SELECT 1 FROM dim_tag WHERE tag_key = ? AND tag_value = ?`
		return scanExists(ctx, tx, q, parts[0], parts[1])
	case "dim_application":
		q = `SELECT 1 FROM dim_application WHERE application_name = ?`
		return scanExists(ctx, tx, q, naturalKey)
	default:
		return false, nil
	}
}

func splitTagNK(key string) []string {
	idx := indexByte(key, '\x00', 0)
	if idx < 0 {
		return []string{key, ""}
	}
	return []string{key[:idx], key[idx+1:]}
}

func splitNK(key string, n int) []string {
	out := make([]string, n)
	start := 0
	for i := 0; i < n-1; i++ {
		idx := indexByte(key, '|', start)
		if idx < 0 {
			return out
		}
		out[i] = key[start:idx]
		start = idx + 1
	}
	if start <= len(key) {
		out[n-1] = key[start:]
	}
	return out
}

func indexByte(s string, b byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func scanExists(ctx context.Context, tx *sql.Tx, q string, args ...interface{}) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, q, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (p *Processor) trackDimInsert(ctx context.Context, tx *sql.Tx, table, naturalKey string, lookup func() (int64, error)) error {
	if !p.TrackPendingDims || p.Dialect != "sqlite" {
		return nil
	}
	existed, err := p.dimExists(ctx, tx, table, naturalKey)
	if err != nil {
		return err
	}
	if existed {
		return nil
	}
	sk, err := lookup()
	if err != nil {
		return err
	}
	return p.recordPendingDim(ctx, tx, table, naturalKey, sk)
}
