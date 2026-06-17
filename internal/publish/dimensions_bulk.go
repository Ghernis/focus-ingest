package publish

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/store"
)

func publishPendingDims(ctx context.Context, local, server *sql.DB) (map[string]map[int64]int64, error) {
	pending, err := loadPendingDims(ctx, local)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return map[string]map[int64]int64{}, nil
	}

	byTable := map[string][]pendingDim{}
	for _, p := range pending {
		byTable[p.Table] = append(byTable[p.Table], p)
	}

	realign := map[string]map[int64]int64{}
	for _, table := range dimPublishOrder {
		items := byTable[table]
		if len(items) == 0 {
			continue
		}
		var err error
		switch table {
		case "dim_tag":
			err = bulkPublishTags(ctx, server, items, realign)
		case "dim_resource":
			err = bulkPublishResources(ctx, local, server, items, realign)
		default:
			err = publishDimsSequential(ctx, local, server, items, realign)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", table, err)
		}
	}
	return realign, nil
}

func publishDimsSequential(ctx context.Context, local, server *sql.DB, pending []pendingDim, realign map[string]map[int64]int64) error {
	total := len(pending)
	for i, p := range pending {
		serverSK, err := mergePendingDim(ctx, local, server, p, realign)
		if err != nil {
			return fmt.Errorf("%s %s: %w", p.Table, p.NaturalKey, err)
		}
		recordRealign(realign, p, serverSK)
		if (i+1)%500 == 0 || i+1 == total {
			fmt.Printf("  dimensions: %d / %d (%s)\n", i+1, total, p.Table)
		}
	}
	return nil
}

func recordRealign(realign map[string]map[int64]int64, p pendingDim, serverSK int64) {
	if serverSK != 0 && serverSK != p.LocalSK {
		if realign[p.Table] == nil {
			realign[p.Table] = map[int64]int64{}
		}
		realign[p.Table][p.LocalSK] = serverSK
	}
}

func bulkPublishTags(ctx context.Context, server *sql.DB, items []pendingDim, realign map[string]map[int64]int64) error {
	fmt.Printf("  bulk publishing %d dim_tag rows\n", len(items))
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE #tag_stg (
		local_sk BIGINT NOT NULL,
		tag_key VARCHAR(256) NOT NULL,
		tag_value NVARCHAR(512) NOT NULL
	)`); err != nil {
		return err
	}

	var batch [][]interface{}
	for _, p := range items {
		key, val := splitTagNaturalKey(p.NaturalKey)
		batch = append(batch, []interface{}{p.LocalSK, truncateRunes(strings.TrimSpace(key), 256), truncateRunes(strings.TrimSpace(val), 512)})
		if len(batch) >= 500 {
			if err := insertTagStaging(ctx, tx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := insertTagStaging(ctx, tx, batch); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		SET IDENTITY_INSERT dim_tag ON;
		INSERT INTO dim_tag (tag_sk, tag_key, tag_value)
		SELECT s.local_sk, s.tag_key, s.tag_value
		FROM #tag_stg s
		WHERE NOT EXISTS (
			SELECT 1 FROM dim_tag t WHERE t.tag_key = s.tag_key AND t.tag_value = s.tag_value
		)
		AND NOT EXISTS (
			SELECT 1 FROM dim_tag t WHERE t.tag_sk = s.local_sk
		);
		SET IDENTITY_INSERT dim_tag OFF;

		INSERT INTO dim_tag (tag_key, tag_value)
		SELECT s.tag_key, s.tag_value
		FROM #tag_stg s
		WHERE NOT EXISTS (
			SELECT 1 FROM dim_tag t WHERE t.tag_key = s.tag_key AND t.tag_value = s.tag_value
		)
		AND EXISTS (
			SELECT 1 FROM dim_tag t WHERE t.tag_sk = s.local_sk
		);`); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT s.local_sk, t.tag_sk
		FROM #tag_stg s
		INNER JOIN dim_tag t ON t.tag_key = s.tag_key AND t.tag_value = s.tag_value
		WHERE s.local_sk <> t.tag_sk`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var localSK, serverSK int64
		if err := rows.Scan(&localSK, &serverSK); err != nil {
			return err
		}
		if realign["dim_tag"] == nil {
			realign["dim_tag"] = map[int64]int64{}
		}
		realign["dim_tag"][localSK] = serverSK
	}
	return tx.Commit()
}

func insertTagStaging(ctx context.Context, tx *sql.Tx, batch [][]interface{}) error {
	prefix := `INSERT INTO #tag_stg (local_sk, tag_key, tag_value) VALUES `
	return store.ExecSQLServerMultiInsert(ctx, tx, prefix, 3, batch)
}

func bulkPublishResources(ctx context.Context, local, server *sql.DB, items []pendingDim, realign map[string]map[int64]int64) error {
	fmt.Printf("  bulk publishing %d dim_resource rows\n", len(items))
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE #res_stg (
		local_sk BIGINT NOT NULL,
		provider VARCHAR(10) NOT NULL,
		global_resource_id VARCHAR(512) NOT NULL,
		resource_type VARCHAR(128) NOT NULL,
		account_sk INT NOT NULL,
		sub_account_sk INT NULL,
		service_sk INT NOT NULL,
		region_sk INT NULL,
		name VARCHAR(256) NULL,
		owner_email VARCHAR(320) NULL,
		cost_center VARCHAR(64) NULL,
		environment VARCHAR(32) NULL,
		application VARCHAR(128) NULL,
		business VARCHAR(128) NULL,
		tags_json NVARCHAR(MAX) NULL,
		hourly_cost DECIMAL(18,6) NULL,
		valid_from DATE NOT NULL,
		is_excluded BIT NOT NULL
	)`); err != nil {
		return err
	}

	var batch [][]interface{}
	for _, p := range items {
		row, err := loadResourceStagingRow(ctx, local, p, realign)
		if err != nil {
			return err
		}
		batch = append(batch, row)
		if len(batch) >= 100 {
			if err := insertResourceStaging(ctx, tx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := insertResourceStaging(ctx, tx, batch); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		SET IDENTITY_INSERT dim_resource ON;
		INSERT INTO dim_resource (resource_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region_sk, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded)
		SELECT s.local_sk, s.provider, s.global_resource_id, s.resource_type, s.account_sk, s.sub_account_sk, s.service_sk,
		  s.region_sk, s.name, s.owner_email, s.cost_center, s.environment, s.application, s.business, s.tags_json, s.hourly_cost, s.valid_from, s.is_excluded
		FROM #res_stg s
		WHERE NOT EXISTS (
			SELECT 1 FROM dim_resource r
			WHERE r.provider = s.provider AND r.global_resource_id = s.global_resource_id AND r.valid_to IS NULL
		)
		AND NOT EXISTS (
			SELECT 1 FROM dim_resource r WHERE r.resource_sk = s.local_sk
		);
		SET IDENTITY_INSERT dim_resource OFF;

		INSERT INTO dim_resource (provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region_sk, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded)
		SELECT s.provider, s.global_resource_id, s.resource_type, s.account_sk, s.sub_account_sk, s.service_sk,
		  s.region_sk, s.name, s.owner_email, s.cost_center, s.environment, s.application, s.business, s.tags_json, s.hourly_cost, s.valid_from, s.is_excluded
		FROM #res_stg s
		WHERE NOT EXISTS (
			SELECT 1 FROM dim_resource r
			WHERE r.provider = s.provider AND r.global_resource_id = s.global_resource_id AND r.valid_to IS NULL
		)
		AND EXISTS (
			SELECT 1 FROM dim_resource r WHERE r.resource_sk = s.local_sk
		);`); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT s.local_sk, r.resource_sk
		FROM #res_stg s
		INNER JOIN dim_resource r ON r.provider = s.provider AND r.global_resource_id = s.global_resource_id AND r.valid_to IS NULL
		WHERE s.local_sk <> r.resource_sk`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var localSK, serverSK int64
		if err := rows.Scan(&localSK, &serverSK); err != nil {
			return err
		}
		if realign["dim_resource"] == nil {
			realign["dim_resource"] = map[int64]int64{}
		}
		realign["dim_resource"][localSK] = serverSK
	}
	return tx.Commit()
}

func loadResourceStagingRow(ctx context.Context, local *sql.DB, p pendingDim, realign map[string]map[int64]int64) ([]interface{}, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var rtype string
	var accSK, svcSK int64
	var subSK, regionSK sql.NullInt64
	var name, owner, cc, env, app, biz, tags, hourly sql.NullString
	var validFrom string
	var excluded int
	err := local.QueryRowContext(ctx, `
		SELECT resource_type, account_sk, sub_account_sk, service_sk, region_sk, name, owner_email, cost_center,
		  environment, application, business, tags_json, hourly_cost, valid_from, is_excluded
		FROM dim_resource WHERE resource_sk = ?`, p.LocalSK).Scan(
		&rtype, &accSK, &subSK, &svcSK, &regionSK, &name, &owner, &cc, &env, &app, &biz, &tags, &hourly, &validFrom, &excluded)
	if err != nil {
		return nil, err
	}
	accSK = remapSK(realign, "dim_account", accSK)
	svcSK = remapSK(realign, "dim_service", svcSK)
	if subSK.Valid {
		subSK.Int64 = remapSK(realign, "dim_sub_account", subSK.Int64)
	}
	if regionSK.Valid {
		regionSK.Int64 = remapSK(realign, "dim_region", regionSK.Int64)
	}
	return []interface{}{
		p.LocalSK, parts[0], parts[1], rtype, accSK, nullIfaceInt(subSK), svcSK,
		nullIfaceInt(regionSK), nullIface(name), nullIface(owner), nullIface(cc), nullIface(env),
		nullIface(app), nullIface(biz), nullIface(tags), nullIface(hourly), validFrom, excluded,
	}, nil
}

func insertResourceStaging(ctx context.Context, tx *sql.Tx, batch [][]interface{}) error {
	prefix := `INSERT INTO #res_stg (
		local_sk, provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		region_sk, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded
	) VALUES `
	return store.ExecSQLServerMultiInsert(ctx, tx, prefix, 18, batch)
}

func splitTagNaturalKey(nk string) (key, val string) {
	idx := strings.IndexByte(nk, '\x00')
	if idx >= 0 {
		return nk[:idx], nk[idx+1:]
	}
	return nk, ""
}
