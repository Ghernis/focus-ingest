package publish

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

type pendingDim struct {
	Table      string
	NaturalKey string
	LocalSK    int64
}

var dimPublishOrder = []string{
	"dim_account", "dim_service", "dim_region", "dim_sku",
	"dim_commitment_discount", "dim_capacity_reservation", "dim_sub_account",
	"dim_resource", "dim_tag", "dim_application",
}

func dimOrderIndex(table string) int {
	for i, t := range dimPublishOrder {
		if t == table {
			return i
		}
	}
	return len(dimPublishOrder)
}

func loadPendingDims(ctx context.Context, local *sql.DB) ([]pendingDim, error) {
	rows, err := local.QueryContext(ctx, `SELECT dim_table, natural_key, local_sk FROM dim_sync_pending`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingDim
	for rows.Next() {
		var p pendingDim
		if err := rows.Scan(&p.Table, &p.NaturalKey, &p.LocalSK); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := dimOrderIndex(out[i].Table), dimOrderIndex(out[j].Table)
		if oi != oj {
			return oi < oj
		}
		return out[i].NaturalKey < out[j].NaturalKey
	})
	return out, rows.Err()
}

func mergePendingDim(ctx context.Context, local, server *sql.DB, p pendingDim, realign map[string]map[int64]int64) (int64, error) {
	switch p.Table {
	case "dim_account":
		return mergeAccount(ctx, local, server, p)
	case "dim_sub_account":
		return mergeSubAccount(ctx, local, server, p, realign)
	case "dim_service":
		return mergeService(ctx, local, server, p)
	case "dim_region":
		return mergeRegion(ctx, local, server, p)
	case "dim_sku":
		return mergeSKU(ctx, local, server, p)
	case "dim_commitment_discount":
		return mergeCommitment(ctx, local, server, p)
	case "dim_capacity_reservation":
		return mergeCapacity(ctx, local, server, p)
	case "dim_resource":
		return mergeResource(ctx, local, server, p, realign)
	case "dim_tag":
		return mergeTag(ctx, local, server, p)
	case "dim_application":
		return mergeApplication(ctx, local, server, p)
	default:
		return 0, fmt.Errorf("unknown dim table %q", p.Table)
	}
}

func mergeAccount(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var name, acctType sql.NullString
	var isActive int
	err := local.QueryRowContext(ctx, `SELECT account_name, billing_account_type, is_active FROM dim_account WHERE account_sk = ?`, p.LocalSK).
		Scan(&name, &acctType, &isActive)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_account AS t
		USING (SELECT @p1 AS provider, @p2 AS account_id, @p3 AS account_name, @p4 AS billing_account_type, @p5 AS is_active) AS s
		ON t.provider = s.provider AND t.account_id = s.account_id
		WHEN MATCHED THEN UPDATE SET
		  account_name = COALESCE(s.account_name, t.account_name),
		  billing_account_type = COALESCE(s.billing_account_type, t.billing_account_type),
		  is_active = s.is_active
		WHEN NOT MATCHED THEN INSERT (provider, account_id, account_name, billing_account_type, is_active)
		  VALUES (s.provider, s.account_id, s.account_name, s.billing_account_type, s.is_active);`,
		parts[0], parts[1], nullIface(name), nullIface(acctType), isActive)
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT account_sk FROM dim_account WHERE provider = @p1 AND account_id = @p2`, parts[0], parts[1])
}

func mergeSubAccount(ctx context.Context, local, server *sql.DB, p pendingDim, realign map[string]map[int64]int64) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var name, subType sql.NullString
	var billSK sql.NullInt64
	err := local.QueryRowContext(ctx, `SELECT sub_account_name, sub_account_type, billing_account_sk FROM dim_sub_account WHERE sub_account_sk = ?`, p.LocalSK).
		Scan(&name, &subType, &billSK)
	if err != nil {
		return 0, err
	}
	if billSK.Valid {
		billSK.Int64 = remapSK(realign, "dim_account", billSK.Int64)
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_sub_account AS t
		USING (SELECT @p1 provider, @p2 sub_account_id, @p3 sub_account_name, @p4 sub_account_type, @p5 billing_account_sk) s
		ON t.provider = s.provider AND t.sub_account_id = s.sub_account_id
		WHEN MATCHED THEN UPDATE SET
		  sub_account_name = COALESCE(s.sub_account_name, t.sub_account_name),
		  sub_account_type = COALESCE(s.sub_account_type, t.sub_account_type),
		  billing_account_sk = COALESCE(s.billing_account_sk, t.billing_account_sk)
		WHEN NOT MATCHED THEN INSERT (provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
		  VALUES (s.provider, s.sub_account_id, s.sub_account_name, s.sub_account_type, s.billing_account_sk);`,
		parts[0], parts[1], nullIface(name), nullIface(subType), nullIfaceInt(billSK))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT sub_account_sk FROM dim_sub_account WHERE provider = @p1 AND sub_account_id = @p2`, parts[0], parts[1])
}

func mergeService(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var cat, subcat sql.NullString
	var svcName string
	err := local.QueryRowContext(ctx, `SELECT service_name, service_category, service_subcategory FROM dim_service WHERE service_sk = ?`, p.LocalSK).
		Scan(&svcName, &cat, &subcat)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_service AS t
		USING (SELECT @p1 provider, @p2 service_code, @p3 service_name, @p4 service_category, @p5 service_subcategory) s
		ON t.provider = s.provider AND t.service_code = s.service_code
		WHEN MATCHED THEN UPDATE SET
		  service_name = s.service_name,
		  service_category = COALESCE(s.service_category, t.service_category),
		  service_subcategory = COALESCE(s.service_subcategory, t.service_subcategory)
		WHEN NOT MATCHED THEN INSERT (provider, service_code, service_name, service_category, service_subcategory)
		  VALUES (s.provider, s.service_code, s.service_name, s.service_category, s.service_subcategory);`,
		parts[0], parts[1], svcName, nullIface(cat), nullIface(subcat))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT service_sk FROM dim_service WHERE provider = @p1 AND service_code = @p2`, parts[0], parts[1])
}

func mergeRegion(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var name sql.NullString
	err := local.QueryRowContext(ctx, `SELECT region_name FROM dim_region WHERE region_sk = ?`, p.LocalSK).Scan(&name)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_region AS t USING (SELECT @p1 provider, @p2 region_id, @p3 region_name) s
		ON t.provider = s.provider AND t.region_id = s.region_id
		WHEN MATCHED THEN UPDATE SET region_name = COALESCE(s.region_name, t.region_name)
		WHEN NOT MATCHED THEN INSERT (provider, region_id, region_name) VALUES (s.provider, s.region_id, s.region_name);`,
		parts[0], parts[1], nullIface(name))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT region_sk FROM dim_region WHERE provider = @p1 AND region_id = @p2`, parts[0], parts[1])
}

func mergeSKU(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 3)
	var meter, details, svc sql.NullString
	err := local.QueryRowContext(ctx, `SELECT sku_meter, sku_price_details, service_name FROM dim_sku WHERE sku_sk = ?`, p.LocalSK).
		Scan(&meter, &details, &svc)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_sku AS t USING (SELECT @p1 provider, @p2 sku_id, @p3 sku_price_id, @p4 sku_meter, @p5 sku_price_details, @p6 service_name) s
		ON t.provider = s.provider AND t.sku_id = s.sku_id AND ISNULL(t.sku_price_id,'') = ISNULL(s.sku_price_id,'')
		WHEN MATCHED THEN UPDATE SET
		  sku_meter = COALESCE(s.sku_meter, t.sku_meter),
		  sku_price_details = COALESCE(s.sku_price_details, t.sku_price_details),
		  service_name = COALESCE(s.service_name, t.service_name)
		WHEN NOT MATCHED THEN INSERT (provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
		  VALUES (s.provider, s.sku_id, s.sku_price_id, s.sku_meter, s.sku_price_details, s.service_name);`,
		parts[0], parts[1], emptyToNil(parts[2]), nullIface(meter), nullIface(details), nullIface(svc))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT sku_sk FROM dim_sku WHERE provider = @p1 AND sku_id = @p2 AND ISNULL(sku_price_id,'') = ISNULL(@p3,'')`,
		parts[0], parts[1], emptyToNil(parts[2]))
}

func mergeCommitment(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var n, typ, cat, unit sql.NullString
	err := local.QueryRowContext(ctx, `SELECT commitment_discount_name, commitment_discount_type, commitment_discount_category, commitment_discount_unit
		FROM dim_commitment_discount WHERE commitment_sk = ?`, p.LocalSK).Scan(&n, &typ, &cat, &unit)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_commitment_discount AS t USING (
		  SELECT @p1 provider, @p2 commitment_discount_id, @p3 commitment_discount_name,
		         @p4 commitment_discount_type, @p5 commitment_discount_category, @p6 commitment_discount_unit) s
		ON t.provider = s.provider AND t.commitment_discount_id = s.commitment_discount_id
		WHEN MATCHED THEN UPDATE SET
		  commitment_discount_name = COALESCE(s.commitment_discount_name, t.commitment_discount_name),
		  commitment_discount_type = COALESCE(s.commitment_discount_type, t.commitment_discount_type),
		  commitment_discount_category = COALESCE(s.commitment_discount_category, t.commitment_discount_category),
		  commitment_discount_unit = COALESCE(s.commitment_discount_unit, t.commitment_discount_unit)
		WHEN NOT MATCHED THEN INSERT (provider, commitment_discount_id, commitment_discount_name,
		  commitment_discount_type, commitment_discount_category, commitment_discount_unit)
		  VALUES (s.provider, s.commitment_discount_id, s.commitment_discount_name,
		  s.commitment_discount_type, s.commitment_discount_category, s.commitment_discount_unit);`,
		parts[0], parts[1], nullIface(n), nullIface(typ), nullIface(cat), nullIface(unit))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT commitment_sk FROM dim_commitment_discount WHERE provider = @p1 AND commitment_discount_id = @p2`, parts[0], parts[1])
}

func mergeCapacity(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	parts := strings.SplitN(p.NaturalKey, "|", 2)
	var status sql.NullString
	err := local.QueryRowContext(ctx, `SELECT capacity_reservation_status FROM dim_capacity_reservation WHERE capacity_reservation_sk = ?`, p.LocalSK).Scan(&status)
	if err != nil {
		return 0, err
	}
	_, err = server.ExecContext(ctx, `
		MERGE dim_capacity_reservation AS t USING (SELECT @p1 provider, @p2 capacity_reservation_id, @p3 capacity_reservation_status) s
		ON t.provider = s.provider AND t.capacity_reservation_id = s.capacity_reservation_id
		WHEN MATCHED THEN UPDATE SET capacity_reservation_status = COALESCE(s.capacity_reservation_status, t.capacity_reservation_status)
		WHEN NOT MATCHED THEN INSERT (provider, capacity_reservation_id, capacity_reservation_status)
		  VALUES (s.provider, s.capacity_reservation_id, s.capacity_reservation_status);`,
		parts[0], parts[1], nullIface(status))
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT capacity_reservation_sk FROM dim_capacity_reservation WHERE provider = @p1 AND capacity_reservation_id = @p2`, parts[0], parts[1])
}

func mergeResource(ctx context.Context, local, server *sql.DB, p pendingDim, realign map[string]map[int64]int64) (int64, error) {
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
		return 0, err
	}
	accSK = remapSK(realign, "dim_account", accSK)
	svcSK = remapSK(realign, "dim_service", svcSK)
	if subSK.Valid {
		subSK.Int64 = remapSK(realign, "dim_sub_account", subSK.Int64)
	}
	if regionSK.Valid {
		regionSK.Int64 = remapSK(realign, "dim_region", regionSK.Int64)
	}
	_, err = server.ExecContext(ctx, `
		IF NOT EXISTS (SELECT 1 FROM dim_resource WHERE provider = @p1 AND global_resource_id = @p2 AND valid_to IS NULL)
		INSERT INTO dim_resource (provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region_sk, name, owner_email, cost_center, environment, application, business, tags_json, hourly_cost, valid_from, is_excluded)
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14, @p15, @p16, @p17)`,
		parts[0], parts[1], rtype, accSK, nullIfaceInt(subSK), svcSK,
		nullIfaceInt(regionSK), nullIface(name), nullIface(owner), nullIface(cc), nullIface(env), nullIface(app), nullIface(biz),
		nullIface(tags), nullIface(hourly), validFrom, excluded)
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT resource_sk FROM dim_resource WHERE provider = @p1 AND global_resource_id = @p2 AND valid_to IS NULL`, parts[0], parts[1])
}

func mergeTag(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	idx := strings.IndexByte(p.NaturalKey, '\x00')
	key, val := p.NaturalKey, ""
	if idx >= 0 {
		key, val = p.NaturalKey[:idx], p.NaturalKey[idx+1:]
	}
	_, err := server.ExecContext(ctx, `
		IF NOT EXISTS (SELECT 1 FROM dim_tag WHERE tag_key = @p1 AND tag_value = @p2)
		  INSERT INTO dim_tag (tag_key, tag_value) VALUES (@p1, @p2)`, key, val)
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT tag_sk FROM dim_tag WHERE tag_key = @p1 AND tag_value = @p2`, key, val)
}

func mergeApplication(ctx context.Context, local, server *sql.DB, p pendingDim) (int64, error) {
	canon := p.NaturalKey
	var aliases, firstSeen sql.NullString
	err := local.QueryRowContext(ctx, `SELECT alias_values, first_seen_date FROM dim_application WHERE application_sk = ?`, p.LocalSK).
		Scan(&aliases, &firstSeen)
	if err != nil {
		return 0, err
	}
	var serverAliases sql.NullString
	err = server.QueryRowContext(ctx, `SELECT alias_values FROM dim_application WHERE application_name = @p1`, canon).Scan(&serverAliases)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	merged := focus.MergeAliasLists(serverAliases.String, aliases.String)
	if err != sql.ErrNoRows && merged == focus.CanonicalAliasList(serverAliases.String) {
		return lookupServerSK(ctx, server, `SELECT application_sk FROM dim_application WHERE application_name = @p1`, canon)
	}
	if err == sql.ErrNoRows {
		_, err = server.ExecContext(ctx, `
			INSERT INTO dim_application (application_name, alias_values, first_seen_date, created_utc, updated_utc)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME(), SYSUTCDATETIME())`,
			canon, nullIfEmpty(merged), nullIface(firstSeen))
	} else {
		_, err = server.ExecContext(ctx, `
			UPDATE dim_application SET alias_values = @p1, updated_utc = SYSUTCDATETIME() WHERE application_name = @p2`,
			nullIfEmpty(merged), canon)
	}
	if err != nil {
		return 0, err
	}
	return lookupServerSK(ctx, server, `SELECT application_sk FROM dim_application WHERE application_name = @p1`, canon)
}

func lookupServerSK(ctx context.Context, db *sql.DB, q string, args ...interface{}) (int64, error) {
	var sk int64
	err := db.QueryRowContext(ctx, q, args...).Scan(&sk)
	return sk, err
}

func nullIface(s sql.NullString) interface{} {
	if s.Valid {
		return s.String
	}
	return nil
}

func nullIfaceInt(n sql.NullInt64) interface{} {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func emptyToNil(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
