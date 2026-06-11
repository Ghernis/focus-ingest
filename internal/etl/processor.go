package etl

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/ghernis/focus_dt/internal/focus"
)

type Processor struct {
	DB             *sql.DB
	Dialect        string // "sqlite" or "sqlserver"
	SkipTags       bool
	SkipAggregates bool
	UseGoETL       bool // SQL Server only: force row-by-row Go ETL instead of set-based SQL
}

type normRow struct {
	focus.StagingRow
	ProviderCode         string
	ChargeDate           string
	BillingPeriodStart   string
	BillingPeriodEnd     string
	ChargeCategoryNorm   string
	PricingCategoryNorm  string
	ChargeDescriptionHash string
	ServiceCode          string
}

type rowQuerier interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func (p *Processor) loadNormalized(ctx context.Context, batchID int64) ([]normRow, error) {
	return p.loadNormalizedWith(ctx, p.DB, batchID)
}

func (p *Processor) loadNormalizedWith(ctx context.Context, q rowQuerier, batchID int64) ([]normRow, error) {
	qry := p.q(`SELECT
		source_provider, BillingAccountId, BillingAccountName, BillingAccountType,
		SubAccountId, SubAccountName, SubAccountType,
		Provider, ServiceName, ServiceCategory, ServiceSubcategory,
		RegionId, RegionName, SkuId, SkuPriceId, SkuMeter, SkuPriceDetails,
		ChargeCategory, ChargeFrequency, PricingCategory, ChargeDescription,
		ChargePeriodStart, ChargePeriodEnd, BillingPeriodStart, BillingPeriodEnd,
		BilledCost, EffectiveCost, ListCost, ContractedCost,
		PricingQuantity, ConsumedQuantity, CommitmentDiscountQuantity,
		ResourceId, ResourceName, ResourceType, raw_tags_json,
		CommitmentDiscountId, CommitmentDiscountName, CommitmentDiscountType,
		CommitmentDiscountCategory, CommitmentDiscountUnit, CommitmentDiscountStatus,
		CapacityReservationId, CapacityReservationStatus
	FROM stg_focus_cost_line WHERE ingestion_batch_id = ?`)

	rs, err := q.QueryContext(ctx, qry, batchID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var out []normRow
	for rs.Next() {
		var sr focus.StagingRow
		var srcProv sql.NullString
		err := rs.Scan(
			&srcProv,
			&sr.BillingAccountId, &sr.BillingAccountName, &sr.BillingAccountType,
			&sr.SubAccountId, &sr.SubAccountName, &sr.SubAccountType,
			&sr.Provider, &sr.ServiceName, &sr.ServiceCategory, &sr.ServiceSubcategory,
			&sr.RegionId, &sr.RegionName, &sr.SkuId, &sr.SkuPriceId, &sr.SkuMeter, &sr.SkuPriceDetails,
			&sr.ChargeCategory, &sr.ChargeFrequency, &sr.PricingCategory, &sr.ChargeDescription,
			&sr.ChargePeriodStart, &sr.ChargePeriodEnd, &sr.BillingPeriodStart, &sr.BillingPeriodEnd,
			&sr.BilledCost, &sr.EffectiveCost, &sr.ListCost, &sr.ContractedCost,
			&sr.PricingQuantity, &sr.ConsumedQuantity, &sr.CommitmentDiscountQuantity,
			&sr.ResourceId, &sr.ResourceName, &sr.ResourceType, &sr.RawTagsJSON,
			&sr.CommitmentDiscountId, &sr.CommitmentDiscountName, &sr.CommitmentDiscountType,
			&sr.CommitmentDiscountCategory, &sr.CommitmentDiscountUnit, &sr.CommitmentDiscountStatus,
			&sr.CapacityReservationId, &sr.CapacityReservationStatus,
		)
		if err != nil {
			return nil, err
		}
		if srcProv.Valid {
			sr.SourceProvider = srcProv.String
		}
		pc := sr.ProviderCode()
		if pc == "" {
			continue
		}
		chargeDate := sr.ChargeDate()
		if chargeDate == "" || sr.BillingAccountId == nil {
			continue
		}
		out = append(out, normRow{
			StagingRow:            sr,
			ProviderCode:          pc,
			ChargeDate:            chargeDate,
			BillingPeriodStart:    focus.DateOnly(focus.PtrStr(sr.BillingPeriodStart)),
			BillingPeriodEnd:      focus.DateOnly(focus.PtrStr(sr.BillingPeriodEnd)),
			ChargeCategoryNorm:    focus.NormalizeChargeCategory(focus.PtrStr(sr.ChargeCategory)),
			PricingCategoryNorm:   focus.NormalizePricingCategory(focus.PtrStr(sr.PricingCategory)),
			ChargeDescriptionHash: focus.ChargeDescriptionHash(focus.PtrStr(sr.ChargeDescription)),
			ServiceCode:           focus.CoalesceServiceName(focus.PtrStr(sr.ServiceName)),
		})
	}
	return out, rs.Err()
}

func nullStr(p *string) interface{} {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	return *p
}

func decVal(p *string) decimal.Decimal {
	if p == nil || strings.TrimSpace(*p) == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(strings.TrimSpace(*p))
	if err != nil {
		return decimal.Zero
	}
	return d
}

func tagFromJSON(raw *string, key string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return ""
	}
	// check if key is lowercase or uppercase
	v, ok := m[key]
	if !ok {
		v, ok = m[strings.ToLower(key)]
		if !ok {
			return ""
		}
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
