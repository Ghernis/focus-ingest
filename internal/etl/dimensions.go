package etl

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

func (p *Processor) upsertDimensions(ctx context.Context, tx *sql.Tx, rows []normRow) error {
	accounts := map[string]normRow{}
	subs := map[string]normRow{}
	services := map[string]normRow{}
	regions := map[string]normRow{}
	skus := map[string]normRow{}
	commitments := map[string]normRow{}
	capacities := map[string]normRow{}
	resources := map[string]normRow{}

	for _, r := range rows {
		accounts[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)] = r
		if r.SubAccountId != nil && strings.TrimSpace(*r.SubAccountId) != "" {
			subs[r.ProviderCode+"|"+focus.PtrStr(r.SubAccountId)] = r
		}
		services[r.ProviderCode+"|"+r.ServiceCode] = r
		if r.RegionId != nil && strings.TrimSpace(*r.RegionId) != "" {
			regions[r.ProviderCode+"|"+focus.PtrStr(r.RegionId)] = r
		}
		if r.SkuId != nil && strings.TrimSpace(*r.SkuId) != "" {
			skus[r.ProviderCode+"|"+focus.PtrStr(r.SkuId)+"|"+focus.PtrStr(r.SkuPriceId)] = r
		}
		if r.CommitmentDiscountId != nil && strings.TrimSpace(*r.CommitmentDiscountId) != "" {
			commitments[r.ProviderCode+"|"+focus.PtrStr(r.CommitmentDiscountId)] = r
		}
		if r.CapacityReservationId != nil && strings.TrimSpace(*r.CapacityReservationId) != "" {
			capacities[r.ProviderCode+"|"+focus.PtrStr(r.CapacityReservationId)] = r
		}
		if r.ResourceId != nil && strings.TrimSpace(*r.ResourceId) != "" {
			resources[r.ProviderCode+"|"+focus.PtrStr(r.ResourceId)] = r
		}
	}

	for _, r := range accounts {
		if err := p.upsertAccount(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, r := range services {
		if err := p.upsertService(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, r := range regions {
		if err := p.upsertRegion(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, r := range skus {
		if err := p.upsertSKU(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, r := range commitments {
		if err := p.upsertCommitment(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, r := range capacities {
		if err := p.upsertCapacity(ctx, tx, r); err != nil {
			return err
		}
	}
	cache, err := p.loadDimCache(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range subs {
		if err := p.upsertSubAccount(ctx, tx, r, cache); err != nil {
			return err
		}
	}
	if err := p.refreshDimCache(ctx, tx, cache); err != nil {
		return err
	}
	for _, r := range resources {
		if err := p.upsertResource(ctx, tx, r, cache); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) upsertAccount(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_account (provider, account_id, account_name, billing_account_type)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(provider, account_id) DO UPDATE SET
			  account_name = COALESCE(excluded.account_name, dim_account.account_name),
			  billing_account_type = COALESCE(excluded.billing_account_type, dim_account.billing_account_type)`,
			r.ProviderCode, focus.PtrStr(r.BillingAccountId), nullStr(r.BillingAccountName), nullStr(r.BillingAccountType))
		return err
	}
	_, err := tx.ExecContext(ctx, `
		MERGE dim_account AS t
		USING (SELECT @p1 AS provider, @p2 AS account_id, @p3 AS account_name, @p4 AS billing_account_type) AS s
		ON t.provider = s.provider AND t.account_id = s.account_id
		WHEN MATCHED THEN UPDATE SET
		  account_name = COALESCE(s.account_name, t.account_name),
		  billing_account_type = COALESCE(s.billing_account_type, t.billing_account_type)
		WHEN NOT MATCHED THEN INSERT (provider, account_id, account_name, billing_account_type)
		  VALUES (s.provider, s.account_id, s.account_name, s.billing_account_type);`,
		r.ProviderCode, focus.PtrStr(r.BillingAccountId), nullStr(r.BillingAccountName), nullStr(r.BillingAccountType))
	return err
}

func (p *Processor) upsertSubAccount(ctx context.Context, tx *sql.Tx, r normRow, cache *dimCache) error {
	accSK := cache.account[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)]
	if accSK == 0 {
		var err error
		accSK, err = p.lookupAccountSK(ctx, tx, r.ProviderCode, focus.PtrStr(r.BillingAccountId))
		if err != nil {
			return err
		}
	}
	var err error
	if p.Dialect == "sqlite" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO dim_sub_account (provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(provider, sub_account_id) DO UPDATE SET
			  sub_account_name = COALESCE(excluded.sub_account_name, dim_sub_account.sub_account_name),
			  sub_account_type = COALESCE(excluded.sub_account_type, dim_sub_account.sub_account_type),
			  billing_account_sk = COALESCE(excluded.billing_account_sk, dim_sub_account.billing_account_sk)`,
			r.ProviderCode, focus.PtrStr(r.SubAccountId), nullStr(r.SubAccountName), nullStr(r.SubAccountType), accSK)
		return err
	}
	_, err = tx.ExecContext(ctx, `
		MERGE dim_sub_account AS t
		USING (SELECT @p1 provider, @p2 sub_account_id, @p3 sub_account_name, @p4 sub_account_type, @p5 billing_account_sk) s
		ON t.provider = s.provider AND t.sub_account_id = s.sub_account_id
		WHEN MATCHED THEN UPDATE SET
		  sub_account_name = COALESCE(s.sub_account_name, t.sub_account_name),
		  sub_account_type = COALESCE(s.sub_account_type, t.sub_account_type),
		  billing_account_sk = COALESCE(s.billing_account_sk, t.billing_account_sk)
		WHEN NOT MATCHED THEN INSERT (provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
		  VALUES (s.provider, s.sub_account_id, s.sub_account_name, s.sub_account_type, s.billing_account_sk);`,
		r.ProviderCode, focus.PtrStr(r.SubAccountId), nullStr(r.SubAccountName), nullStr(r.SubAccountType), accSK)
	return err
}

func (p *Processor) upsertService(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_service (provider, service_code, service_name, service_category, service_subcategory)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(provider, service_code) DO UPDATE SET
			  service_name = excluded.service_name,
			  service_category = COALESCE(excluded.service_category, dim_service.service_category),
			  service_subcategory = COALESCE(excluded.service_subcategory, dim_service.service_subcategory)`,
			r.ProviderCode, r.ServiceCode, r.ServiceCode, nullStr(r.ServiceCategory), nullStr(r.ServiceSubcategory))
		return err
	}
	_, err := tx.ExecContext(ctx, `
		MERGE dim_service AS t
		USING (SELECT @p1 provider, @p2 service_code, @p3 service_name, @p4 service_category, @p5 service_subcategory) s
		ON t.provider = s.provider AND t.service_code = s.service_code
		WHEN MATCHED THEN UPDATE SET
		  service_name = s.service_name,
		  service_category = COALESCE(s.service_category, t.service_category),
		  service_subcategory = COALESCE(s.service_subcategory, t.service_subcategory)
		WHEN NOT MATCHED THEN INSERT (provider, service_code, service_name, service_category, service_subcategory)
		  VALUES (s.provider, s.service_code, s.service_name, s.service_category, s.service_subcategory);`,
		r.ProviderCode, r.ServiceCode, r.ServiceCode, nullStr(r.ServiceCategory), nullStr(r.ServiceSubcategory))
	return err
}

func (p *Processor) upsertRegion(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_region (provider, region_id, region_name) VALUES (?, ?, ?)
			ON CONFLICT(provider, region_id) DO UPDATE SET region_name = COALESCE(excluded.region_name, dim_region.region_name)`,
			r.ProviderCode, focus.PtrStr(r.RegionId), nullStr(r.RegionName))
		return err
	}
	_, err := tx.ExecContext(ctx, `
		MERGE dim_region AS t USING (SELECT @p1 provider, @p2 region_id, @p3 region_name) s
		ON t.provider = s.provider AND t.region_id = s.region_id
		WHEN MATCHED THEN UPDATE SET region_name = COALESCE(s.region_name, t.region_name)
		WHEN NOT MATCHED THEN INSERT (provider, region_id, region_name) VALUES (s.provider, s.region_id, s.region_name);`,
		r.ProviderCode, focus.PtrStr(r.RegionId), nullStr(r.RegionName))
	return err
}

func (p *Processor) upsertSKU(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_sku (provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider, sku_id, sku_price_id) DO UPDATE SET
			  sku_meter = COALESCE(excluded.sku_meter, dim_sku.sku_meter),
			  sku_price_details = COALESCE(excluded.sku_price_details, dim_sku.sku_price_details),
			  service_name = COALESCE(excluded.service_name, dim_sku.service_name)`,
			r.ProviderCode, focus.PtrStr(r.SkuId), nullStr(r.SkuPriceId), nullStr(r.SkuMeter), nullStr(r.SkuPriceDetails), nullStr(r.ServiceName))
		return err
	}
	_, err := tx.ExecContext(ctx, `
		MERGE dim_sku AS t USING (SELECT @p1 provider, @p2 sku_id, @p3 sku_price_id, @p4 sku_meter, @p5 sku_price_details, @p6 service_name) s
		ON t.provider = s.provider AND t.sku_id = s.sku_id AND ISNULL(t.sku_price_id,'') = ISNULL(s.sku_price_id,'')
		WHEN MATCHED THEN UPDATE SET
		  sku_meter = COALESCE(s.sku_meter, t.sku_meter),
		  sku_price_details = COALESCE(s.sku_price_details, t.sku_price_details),
		  service_name = COALESCE(s.service_name, t.service_name)
		WHEN NOT MATCHED THEN INSERT (provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
		  VALUES (s.provider, s.sku_id, s.sku_price_id, s.sku_meter, s.sku_price_details, s.service_name);`,
		r.ProviderCode, focus.PtrStr(r.SkuId), focus.PtrStr(r.SkuPriceId), nullStr(r.SkuMeter), nullStr(r.SkuPriceDetails), nullStr(r.ServiceName))
	return err
}

func (p *Processor) upsertCommitment(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_commitment_discount (provider, commitment_discount_id, commitment_discount_name,
			  commitment_discount_type, commitment_discount_category, commitment_discount_unit)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider, commitment_discount_id) DO UPDATE SET
			  commitment_discount_name = COALESCE(excluded.commitment_discount_name, dim_commitment_discount.commitment_discount_name),
			  commitment_discount_type = COALESCE(excluded.commitment_discount_type, dim_commitment_discount.commitment_discount_type),
			  commitment_discount_category = COALESCE(excluded.commitment_discount_category, dim_commitment_discount.commitment_discount_category),
			  commitment_discount_unit = COALESCE(excluded.commitment_discount_unit, dim_commitment_discount.commitment_discount_unit)`,
			r.ProviderCode, focus.PtrStr(r.CommitmentDiscountId), nullStr(r.CommitmentDiscountName),
			nullStr(r.CommitmentDiscountType), nullStr(r.CommitmentDiscountCategory), nullStr(r.CommitmentDiscountUnit))
		return err
	}
	_, err := tx.ExecContext(ctx, `
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
		r.ProviderCode, focus.PtrStr(r.CommitmentDiscountId), nullStr(r.CommitmentDiscountName),
		nullStr(r.CommitmentDiscountType), nullStr(r.CommitmentDiscountCategory), nullStr(r.CommitmentDiscountUnit))
	return err
}

func (p *Processor) upsertCapacity(ctx context.Context, tx *sql.Tx, r normRow) error {
	if p.Dialect == "sqlite" {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dim_capacity_reservation (provider, capacity_reservation_id, capacity_reservation_status)
			VALUES (?, ?, ?)
			ON CONFLICT(provider, capacity_reservation_id) DO UPDATE SET
			  capacity_reservation_status = COALESCE(excluded.capacity_reservation_status, dim_capacity_reservation.capacity_reservation_status)`,
			r.ProviderCode, focus.PtrStr(r.CapacityReservationId), nullStr(r.CapacityReservationStatus))
		return err
	}
	_, err := tx.ExecContext(ctx, `
		MERGE dim_capacity_reservation AS t USING (SELECT @p1 provider, @p2 capacity_reservation_id, @p3 capacity_reservation_status) s
		ON t.provider = s.provider AND t.capacity_reservation_id = s.capacity_reservation_id
		WHEN MATCHED THEN UPDATE SET capacity_reservation_status = COALESCE(s.capacity_reservation_status, t.capacity_reservation_status)
		WHEN NOT MATCHED THEN INSERT (provider, capacity_reservation_id, capacity_reservation_status)
		  VALUES (s.provider, s.capacity_reservation_id, s.capacity_reservation_status);`,
		r.ProviderCode, focus.PtrStr(r.CapacityReservationId), nullStr(r.CapacityReservationStatus))
	return err
}

func (p *Processor) refreshDimCache(ctx context.Context, tx *sql.Tx, cache *dimCache) error {
	return p.scanPairs(ctx, tx, `SELECT provider||'|'||sub_account_id, sub_account_sk FROM dim_sub_account`, cache.sub)
}

func (p *Processor) upsertResource(ctx context.Context, tx *sql.Tx, r normRow, cache *dimCache) error {
	resKey := r.ProviderCode + "|" + focus.PtrStr(r.ResourceId)
	if cache.resource[resKey] != 0 {
		return nil
	}
	accSK := cache.account[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)]
	if accSK == 0 {
		return nil
	}
	var subSK interface{}
	if r.SubAccountId != nil && strings.TrimSpace(*r.SubAccountId) != "" {
		if sk := cache.sub[r.ProviderCode+"|"+focus.PtrStr(r.SubAccountId)]; sk != 0 {
			subSK = sk
		}
	}
	svcSK := cache.service[r.ProviderCode+"|"+r.ServiceCode]
	if svcSK == 0 {
		return nil
	}
	rtype := focus.PtrStr(r.ResourceType)
	if rtype == "" {
		rtype = "UNKNOWN"
	}
	if _, err := tx.ExecContext(ctx, p.q(`
		INSERT INTO dim_resource (provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region, name, application, environment, business, cost_center,owner_email, tags_json, valid_from)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		r.ProviderCode, focus.PtrStr(r.ResourceId), rtype, accSK, subSK, svcSK,
		nullStr(r.RegionId), nullStr(r.ResourceName),
		tagFromJSON(r.RawTagsJSON, "Application"), tagFromJSON(r.RawTagsJSON, "Environment"),
		tagFromJSON(r.RawTagsJSON, "Business"), tagFromJSON(r.RawTagsJSON, "CostCenter"),
		tagFromJSON(r.RawTagsJSON, "info:support-team-email"),
		nullStr(r.RawTagsJSON), r.ChargeDate); err != nil {
		return err
	}
	var sk int64
	if err := tx.QueryRowContext(ctx, p.q(`
		SELECT resource_sk FROM dim_resource
		WHERE provider = ? AND global_resource_id = ? AND valid_to IS NULL`),
		r.ProviderCode, focus.PtrStr(r.ResourceId)).Scan(&sk); err == nil {
		cache.resource[resKey] = sk
	}
	return nil
}

func (p *Processor) lookupSubAccountSK(ctx context.Context, tx *sql.Tx, provider, subAccountID string) (int64, error) {
	var sk int64
	err := tx.QueryRowContext(ctx, p.q(`SELECT sub_account_sk FROM dim_sub_account WHERE provider = ? AND sub_account_id = ?`), provider, subAccountID).Scan(&sk)
	return sk, err
}

func (p *Processor) lookupAccountSK(ctx context.Context, tx *sql.Tx, provider, accountID string) (int64, error) {
	var sk int64
	err := tx.QueryRowContext(ctx, p.q(`SELECT account_sk FROM dim_account WHERE provider = ? AND account_id = ?`), provider, accountID).Scan(&sk)
	return sk, err
}

func (p *Processor) lookupServiceSK(ctx context.Context, tx *sql.Tx, provider, code string) (int64, error) {
	var sk int64
	err := tx.QueryRowContext(ctx, p.q(`SELECT service_sk FROM dim_service WHERE provider = ? AND service_code = ?`), provider, code).Scan(&sk)
	return sk, err
}

func (p *Processor) lookupChargeCategorySK(ctx context.Context, tx *sql.Tx, cat string) (int64, error) {
	var sk int64
	err := tx.QueryRowContext(ctx, p.q(`SELECT charge_category_sk FROM dim_charge_category WHERE charge_category = ?`), cat).Scan(&sk)
	return sk, err
}

func (p *Processor) lookupOptionalSK(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) (*int64, error) {
	var sk sql.NullInt64
	err := tx.QueryRowContext(ctx, query, args...).Scan(&sk)
	if err == sql.ErrNoRows || !sk.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := sk.Int64
	return &v, nil
}
