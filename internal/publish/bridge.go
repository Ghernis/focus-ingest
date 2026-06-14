package publish

import (
	"context"
	"database/sql"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/store"
)

type tagBridgeRow struct {
	GrainKey string
	TagSK    int64
}

func publishBridge(ctx context.Context, local, server *sql.DB, month string, maps skMaps) (int, error) {
	localPairs, err := loadLocalBridgePairs(ctx, local, month, maps)
	if err != nil {
		return 0, err
	}
	if len(localPairs) == 0 {
		return 0, nil
	}

	serverMap, err := loadServerFactGrainMap(ctx, server, month)
	if err != nil {
		return 0, err
	}

	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var batch [][]interface{}
	total := 0
	prefix := `INSERT INTO bridge_cost_tag (cost_daily_id, tag_sk) VALUES `
	for _, p := range localPairs {
		costID := serverMap[p.GrainKey]
		if costID == 0 {
			continue
		}
		tagSK := maps.remap("dim_tag", p.TagSK)
		batch = append(batch, []interface{}{costID, tagSK})
		if len(batch) >= 500 {
			if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, 2, batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := store.ExecSQLServerMultiInsert(ctx, tx, prefix, 2, batch); err != nil {
			return total, err
		}
		total += len(batch)
	}
	if err := tx.Commit(); err != nil {
		return total, err
	}
	return total, nil
}

func loadLocalBridgePairs(ctx context.Context, local *sql.DB, month string, maps skMaps) ([]tagBridgeRow, error) {
	q := `
		SELECT f.charge_date, f.billing_account_sk,
		  IFNULL(f.sub_account_sk,-1), IFNULL(f.resource_sk,-1), f.service_sk, f.charge_category_sk,
		  f.charge_description_hash, f.billing_period_start, b.tag_sk
		FROM bridge_cost_tag b
		INNER JOIN fact_focus_cost_daily f ON f.cost_daily_id = b.cost_daily_id
		WHERE f.billing_period_start = ?`
	rows, err := local.QueryContext(ctx, q, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tagBridgeRow
	for rows.Next() {
		var chargeDate, hash, billStart string
		var accSK, subSK, resSK, svcSK, catSK, tagSK int64
		if err := rows.Scan(&chargeDate, &accSK, &subSK, &resSK, &svcSK, &catSK, &hash, &billStart, &tagSK); err != nil {
			return nil, err
		}
		key := etl.GrainLookupKey(
			chargeDate,
			int64FromRemap(maps.remap("dim_account", accSK)),
			int64FromRemap(maps.remap("dim_sub_account", subSK)),
			int64FromRemap(maps.remap("dim_resource", resSK)),
			int64FromRemap(maps.remap("dim_service", svcSK)),
			catSK, hash, billStart,
		)
		out = append(out, tagBridgeRow{GrainKey: key, TagSK: tagSK})
	}
	return out, rows.Err()
}

func loadServerFactGrainMap(ctx context.Context, server *sql.DB, month string) (map[string]int64, error) {
	q := `
		SELECT cost_daily_id, charge_date, billing_account_sk,
		  ISNULL(sub_account_sk,-1), ISNULL(resource_sk,-1), service_sk, charge_category_sk,
		  charge_description_hash, billing_period_start
		FROM fact_focus_cost_daily WHERE billing_period_start = @p1`
	rows, err := server.QueryContext(ctx, q, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := map[string]int64{}
	for rows.Next() {
		var id, accSK, subSK, resSK, svcSK, catSK int64
		var chargeDate, hash, billStart string
		if err := rows.Scan(&id, &chargeDate, &accSK, &subSK, &resSK, &svcSK, &catSK, &hash, &billStart); err != nil {
			return nil, err
		}
		key := etl.GrainLookupKey(chargeDate, accSK, subSK, resSK, svcSK, catSK, hash, billStart)
		m[key] = id
	}
	return m, rows.Err()
}
