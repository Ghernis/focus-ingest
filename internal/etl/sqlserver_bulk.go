package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
	"github.com/ghernis/focus_dt/internal/sqlserver"
)

const factInsertCols = 29

const factInsertPrefix = `INSERT INTO fact_focus_cost_daily (
		charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk, sku_sk, region_sk,
		charge_category_sk, charge_frequency_sk, pricing_category_sk, commitment_sk,
		commitment_discount_status, capacity_reservation_sk, capacity_reservation_status,
		charge_description_hash, billing_period_start, billing_period_end,
		billed_cost, effective_cost, list_cost, contracted_cost,
		pricing_quantity, consumed_quantity, commitment_discount_quantity, line_count,
		first_charge_period_start, last_charge_period_end, ingestion_batch_id, focus_version
	) VALUES `

func (p *Processor) insertDailyGrains(ctx context.Context, tx *sql.Tx, grains map[string]*dailyGrain, batchID int64, focusVersion string) error {
	if len(grains) == 0 {
		return nil
	}
	list := make([]*dailyGrain, 0, len(grains))
	for _, g := range grains {
		list = append(list, g)
	}
	chunk := sqlserver.ChunkRows(factInsertCols)
	for start := 0; start < len(list); start += chunk {
		end := start + chunk
		if end > len(list) {
			end = len(list)
		}
		if err := p.insertDailyGrainChunk(ctx, tx, list[start:end], batchID, focusVersion); err != nil {
			return fmt.Errorf("insert daily fact: %w", err)
		}
	}
	return nil
}

func (p *Processor) insertDailyGrainChunk(ctx context.Context, tx *sql.Tx, grains []*dailyGrain, batchID int64, focusVersion string) error {
	if len(grains) == 0 {
		return nil
	}
	if p.Dialect == "sqlite" {
		ins := `INSERT INTO fact_focus_cost_daily (
		charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk, sku_sk, region_sk,
		charge_category_sk, charge_frequency_sk, pricing_category_sk, commitment_sk,
		commitment_discount_status, capacity_reservation_sk, capacity_reservation_status,
		charge_description_hash, billing_period_start, billing_period_end,
		billed_cost, effective_cost, list_cost, contracted_cost,
		pricing_quantity, consumed_quantity, commitment_discount_quantity, line_count,
		first_charge_period_start, last_charge_period_end, ingestion_batch_id, focus_version
	) VALUES (` + strings.Repeat("?,", factInsertCols-1) + `?)`
		stmt, err := tx.PrepareContext(ctx, ins)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, g := range grains {
			if _, err := stmt.ExecContext(ctx, dailyGrainArgs(g, batchID, focusVersion)...); err != nil {
				return err
			}
		}
		return nil
	}

	var b strings.Builder
	b.WriteString(factInsertPrefix)
	args := make([]interface{}, 0, len(grains)*factInsertCols)
	n := 1
	for i, g := range grains {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < factInsertCols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "@p%d", n)
			n++
		}
		b.WriteByte(')')
		args = append(args, dailyGrainArgs(g, batchID, focusVersion)...)
	}
	if err := sqlserver.CheckParamCount(len(args)); err != nil {
		return fmt.Errorf("daily fact insert chunk (%d rows): %w", len(grains), err)
	}
	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func dailyGrainArgs(g *dailyGrain, batchID int64, focusVersion string) []interface{} {
	return []interface{}{
		g.ChargeDate, g.BillingAccountSK, g.SubAccountSK, g.ResourceSK, g.ServiceSK, g.SkuSK, g.RegionSK,
		g.ChargeCategorySK, g.ChargeFrequencySK, g.PricingCategorySK, g.CommitmentSK,
		g.CommitmentDiscountStatus, g.CapacitySK, g.CapacityStatus,
		g.ChargeDescriptionHash, g.BillingPeriodStart, g.BillingPeriodEnd,
		g.Billed.String(), g.Effective.String(), g.List.String(), g.Contracted.String(),
		g.PricingQty.String(), g.ConsumedQty.String(), g.CommitmentQty.String(), g.LineCount,
		nullIfEmpty(g.FirstCharge), nullIfEmpty(g.LastCharge), batchID, focusVersion,
	}
}

type resourceStagingRow struct {
	provider   string
	resourceID string
	rtype      string
	accountSK  int64
	subSK      interface{}
	serviceSK  int64
	region     interface{}
	name       interface{}
	application interface{}
	environment interface{}
	business   interface{}
	costCenter interface{}
	ownerEmail interface{}
	tagsJSON   interface{}
	validFrom  string
}

func (p *Processor) upsertResourcesBulkSQLServer(ctx context.Context, tx *sql.Tx, resources map[string]normRow, cache *dimCache) error {
	pending := make([]resourceStagingRow, 0, len(resources))
	for resKey, r := range resources {
		if cache.resource[resKey] != 0 {
			continue
		}
		accSK := cache.account[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)]
		svcSK := cache.service[r.ProviderCode+"|"+r.ServiceCode]
		if accSK == 0 || svcSK == 0 {
			continue
		}
		rtype := focus.PtrStr(r.ResourceType)
		if rtype == "" {
			rtype = "UNKNOWN"
		}
		var subSK interface{}
		if r.SubAccountId != nil && strings.TrimSpace(*r.SubAccountId) != "" {
			if sk := cache.sub[r.ProviderCode+"|"+focus.PtrStr(r.SubAccountId)]; sk != 0 {
				subSK = sk
			}
		}
		pending = append(pending, resourceStagingRow{
			provider:    r.ProviderCode,
			resourceID:  focus.PtrStr(r.ResourceId),
			rtype:       rtype,
			accountSK:   accSK,
			subSK:       subSK,
			serviceSK:   svcSK,
			region:      nullStr(r.RegionId),
			name:        nullStr(r.ResourceName),
			application: tagFromJSON(r.RawTagsJSON, "Application"),
			environment: tagFromJSON(r.RawTagsJSON, "Environment"),
			business:    tagFromJSON(r.RawTagsJSON, "Business"),
			costCenter:  tagFromJSON(r.RawTagsJSON, "CostCenter"),
			ownerEmail:  tagFromJSON(r.RawTagsJSON, "info:support-team-email"),
			tagsJSON:    nullStr(r.RawTagsJSON),
			validFrom:   r.ChargeDate,
		})
	}
	if len(pending) == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE #res_stg (
			provider VARCHAR(10) NOT NULL,
			global_resource_id VARCHAR(512) NOT NULL,
			resource_type VARCHAR(128) NOT NULL,
			account_sk INT NOT NULL,
			sub_account_sk INT NULL,
			service_sk INT NOT NULL,
			region VARCHAR(64) NULL,
			name VARCHAR(256) NULL,
			application VARCHAR(128) NULL,
			environment VARCHAR(32) NULL,
			business VARCHAR(128) NULL,
			cost_center VARCHAR(64) NULL,
			owner_email VARCHAR(320) NULL,
			tags_json NVARCHAR(MAX) NULL,
			valid_from DATE NOT NULL
		)`); err != nil {
		return err
	}

	const resCols = 15
	chunk := sqlserver.ChunkRows(resCols)
	prefix := `INSERT INTO #res_stg (
			provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
			region, name, application, environment, business, cost_center, owner_email, tags_json, valid_from
		) VALUES `
	for start := 0; start < len(pending); start += chunk {
		end := start + chunk
		if end > len(pending) {
			end = len(pending)
		}
		if err := p.insertResourceStagingChunk(ctx, tx, prefix, pending[start:end]); err != nil {
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `
		MERGE dbo.dim_resource AS t
		USING #res_stg AS s
		ON t.provider = s.provider AND t.global_resource_id = s.global_resource_id AND t.valid_to IS NULL
		WHEN NOT MATCHED THEN INSERT (
		  provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
		  region, name, application, environment, business, cost_center, owner_email, tags_json, valid_from
		) VALUES (
		  s.provider, s.global_resource_id, s.resource_type, s.account_sk, s.sub_account_sk, s.service_sk,
		  s.region, s.name, s.application, s.environment, s.business, s.cost_center, s.owner_email, s.tags_json, s.valid_from
		)
		OUTPUT INSERTED.provider, INSERTED.global_resource_id, INSERTED.resource_sk`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var provider, globalID string
		var sk int64
		if err := rows.Scan(&provider, &globalID, &sk); err != nil {
			return err
		}
		cache.resource[provider+"|"+globalID] = sk
	}
	return rows.Err()
}

func (p *Processor) insertResourceStagingChunk(ctx context.Context, tx *sql.Tx, prefix string, rows []resourceStagingRow) error {
	var b strings.Builder
	b.WriteString(prefix)
	args := make([]interface{}, 0, len(rows)*15)
	n := 1
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < 15; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "@p%d", n)
			n++
		}
		b.WriteByte(')')
		args = append(args,
			r.provider, r.resourceID, r.rtype, r.accountSK, r.subSK, r.serviceSK,
			r.region, r.name, r.application, r.environment, r.business, r.costCenter, r.ownerEmail, r.tagsJSON, r.validFrom,
		)
	}
	if err := sqlserver.CheckParamCount(len(args)); err != nil {
		return fmt.Errorf("resource staging insert chunk (%d rows): %w", len(rows), err)
	}
	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}
