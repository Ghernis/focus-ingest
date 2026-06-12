package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/ghernis/focus_dt/internal/focus"
)

type dailyGrain struct {
	ChargeDate               string
	BillingAccountSK         int64
	SubAccountSK             *int64
	ResourceSK               *int64
	ServiceSK                int64
	SkuSK                    *int64
	RegionSK                 *int64
	ChargeCategorySK         int64
	ChargeFrequencySK        *int64
	PricingCategorySK        *int64
	CommitmentSK             *int64
	CommitmentDiscountStatus *string
	CapacitySK               *int64
	CapacityStatus           *string
	ChargeDescriptionHash    string
	BillingPeriodStart       string
	BillingPeriodEnd         string
	Billed                   decimal.Decimal
	Effective                decimal.Decimal
	List                     decimal.Decimal
	Contracted               decimal.Decimal
	PricingQty               decimal.Decimal
	ConsumedQty              decimal.Decimal
	CommitmentQty            decimal.Decimal
	LineCount                int
	FirstCharge              string
	LastCharge               string
}

type dimCache struct {
	account   map[string]int64
	sub       map[string]int64
	service   map[string]int64
	region    map[string]int64
	sku       map[string]int64
	chargeCat map[string]int64
	chargeFr  map[string]int64
	pricing   map[string]int64
	commit    map[string]int64
	capacity  map[string]int64
	resource  map[string]int64
}

func (p *Processor) loadDimCache(ctx context.Context, tx *sql.Tx) (*dimCache, error) {
	c := &dimCache{
		account:   map[string]int64{},
		sub:       map[string]int64{},
		service:   map[string]int64{},
		region:    map[string]int64{},
		sku:       map[string]int64{},
		chargeCat: map[string]int64{},
		chargeFr:  map[string]int64{},
		pricing:   map[string]int64{},
		commit:    map[string]int64{},
		capacity:  map[string]int64{},
		resource:  map[string]int64{},
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||account_id, account_sk FROM dim_account`, c.account); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||sub_account_id, sub_account_sk FROM dim_sub_account`, c.sub); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||service_code, service_sk FROM dim_service`, c.service); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||region_id, region_sk FROM dim_region`, c.region); err != nil {
		return nil, err
	}
	skuQ := `SELECT provider||'|'||sku_id||'|'||IFNULL(sku_price_id,''), sku_sk FROM dim_sku`
	if p.Dialect == "sqlserver" {
		skuQ = `SELECT provider+'|'+sku_id+'|'+ISNULL(sku_price_id,''), sku_sk FROM dim_sku`
	}
	if err := p.scanPairs(ctx, tx, skuQ, c.sku); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT charge_category, charge_category_sk FROM dim_charge_category`, c.chargeCat); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT charge_frequency, charge_frequency_sk FROM dim_charge_frequency`, c.chargeFr); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT pricing_category, pricing_category_sk FROM dim_pricing_category`, c.pricing); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||commitment_discount_id, commitment_sk FROM dim_commitment_discount`, c.commit); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||capacity_reservation_id, capacity_reservation_sk FROM dim_capacity_reservation`, c.capacity); err != nil {
		return nil, err
	}
	if err := p.scanPairs(ctx, tx, `SELECT provider||'|'||global_resource_id, resource_sk FROM dim_resource WHERE valid_to IS NULL`, c.resource); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *Processor) scanPairs(ctx context.Context, tx *sql.Tx, q string, m map[string]int64) error {
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		m[k] = v
	}
	return rows.Err()
}

func (p *Processor) rollupDaily(ctx context.Context, tx *sql.Tx, batchID int64, focusVersion string, rows []normRow) error {
	if err := p.deleteBatchFacts(ctx, tx, batchID); err != nil {
		return err
	}
	cache, err := p.loadDimCache(ctx, tx)
	if err != nil {
		return err
	}

	grains := map[string]*dailyGrain{}
	for _, r := range rows {
		accSK := cache.account[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)]
		svcSK := cache.service[r.ProviderCode+"|"+r.ServiceCode]
		catSK := cache.chargeCat[r.ChargeCategoryNorm]
		if accSK == 0 || svcSK == 0 || catSK == 0 {
			continue
		}
		var subSK, resSK, skuSK, regSK, cfSK, pcSK, cmtSK, capSK *int64
		if id := cache.sub[r.ProviderCode+"|"+focus.PtrStr(r.SubAccountId)]; id != 0 {
			subSK = &id
		}
		if id := cache.resource[r.ProviderCode+"|"+focus.PtrStr(r.ResourceId)]; id != 0 {
			resSK = &id
		}
		if id := cache.sku[r.ProviderCode+"|"+focus.PtrStr(r.SkuId)+"|"+focus.PtrStr(r.SkuPriceId)]; id != 0 {
			skuSK = &id
		}
		if id := cache.region[r.ProviderCode+"|"+focus.PtrStr(r.RegionId)]; id != 0 {
			regSK = &id
		}
		if id := cache.chargeFr[focus.PtrStr(r.ChargeFrequency)]; id != 0 {
			cfSK = &id
		}
		if r.PricingCategoryNorm != "" {
			if id := cache.pricing[r.PricingCategoryNorm]; id != 0 {
				pcSK = &id
			}
		}
		if id := cache.commit[r.ProviderCode+"|"+focus.PtrStr(r.CommitmentDiscountId)]; id != 0 {
			cmtSK = &id
		}
		if id := cache.capacity[r.ProviderCode+"|"+focus.PtrStr(r.CapacityReservationId)]; id != 0 {
			capSK = &id
		}

		key := dailyGrainKey(
			r.ChargeDate, accSK, subSK, resSK, svcSK, skuSK, regSK, catSK,
			cfSK, pcSK, cmtSK, capSK,
			r.CommitmentDiscountStatus, r.CapacityReservationStatus,
			r.ChargeDescriptionHash, r.BillingPeriodStart,
		)

		g, ok := grains[key]
		if !ok {
			g = &dailyGrain{
				ChargeDate:               r.ChargeDate,
				BillingAccountSK:         accSK,
				SubAccountSK:             subSK,
				ResourceSK:               resSK,
				ServiceSK:                svcSK,
				SkuSK:                    skuSK,
				RegionSK:                 regSK,
				ChargeCategorySK:         catSK,
				ChargeFrequencySK:        cfSK,
				PricingCategorySK:        pcSK,
				CommitmentSK:             cmtSK,
				CommitmentDiscountStatus: r.CommitmentDiscountStatus,
				CapacitySK:               capSK,
				CapacityStatus:           r.CapacityReservationStatus,
				ChargeDescriptionHash:    r.ChargeDescriptionHash,
				BillingPeriodStart:       r.BillingPeriodStart,
				BillingPeriodEnd:         r.BillingPeriodEnd,
				FirstCharge:              focus.PtrStr(r.ChargePeriodStart),
				LastCharge:               focus.PtrStr(r.ChargePeriodEnd),
			}
			grains[key] = g
		}
		g.Billed = g.Billed.Add(decVal(r.BilledCost))
		g.Effective = g.Effective.Add(decVal(r.EffectiveCost))
		g.List = g.List.Add(decVal(r.ListCost))
		g.Contracted = g.Contracted.Add(decVal(r.ContractedCost))
		g.PricingQty = g.PricingQty.Add(decVal(r.PricingQuantity))
		g.ConsumedQty = g.ConsumedQty.Add(decVal(r.ConsumedQuantity))
		g.CommitmentQty = g.CommitmentQty.Add(decVal(r.CommitmentDiscountQuantity))
		g.LineCount++
		cps := focus.PtrStr(r.ChargePeriodStart)
		cpe := focus.PtrStr(r.ChargePeriodEnd)
		if g.FirstCharge == "" || cps < g.FirstCharge {
			g.FirstCharge = cps
		}
		if cpe > g.LastCharge {
			g.LastCharge = cpe
		}
	}

	return p.insertDailyGrains(ctx, tx, grains, batchID, focusVersion)
}

// dailyGrainKey matches UQ_fact_focus_cost_daily_grain (excluding ingestion_batch_id).
// Uses scalar values — never pointer addresses (%v on *int64 prints distinct addresses).
func dailyGrainKey(
	chargeDate string,
	billingAccountSK int64,
	subAccountSK, resourceSK *int64,
	serviceSK int64,
	skuSK, regionSK *int64,
	chargeCategorySK int64,
	chargeFrequencySK, pricingCategorySK, commitmentSK, capacitySK *int64,
	commitmentDiscountStatus, capacityReservationStatus *string,
	chargeDescriptionHash, billingPeriodStart string,
) string {
	return strings.Join([]string{
		chargeDate,
		strconv.FormatInt(billingAccountSK, 10),
		ptrInt64Key(subAccountSK),
		ptrInt64Key(resourceSK),
		strconv.FormatInt(serviceSK, 10),
		ptrInt64Key(skuSK),
		ptrInt64Key(regionSK),
		strconv.FormatInt(chargeCategorySK, 10),
		ptrInt64Key(chargeFrequencySK),
		ptrInt64Key(pricingCategorySK),
		ptrInt64Key(commitmentSK),
		ptrStrKey(commitmentDiscountStatus),
		ptrInt64Key(capacitySK),
		ptrStrKey(capacityReservationStatus),
		chargeDescriptionHash,
		billingPeriodStart,
	}, "|")
}

func ptrInt64Key(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

func ptrStrKey(p *string) string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return ""
	}
	return *p
}

func (p *Processor) deleteBatchFacts(ctx context.Context, tx *sql.Tx, batchID int64) error {
	if _, err := tx.ExecContext(ctx, p.q(`
		DELETE FROM bridge_cost_tag WHERE cost_daily_id IN (
		  SELECT cost_daily_id FROM fact_focus_cost_daily WHERE ingestion_batch_id = ?)`), batchID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, p.q(`DELETE FROM fact_focus_cost_daily WHERE ingestion_batch_id = ?`), batchID)
	return err
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (p *Processor) q(sqlite string) string {
	if p.Dialect == "sqlserver" {
		n := 1
		var b strings.Builder
		for _, ch := range sqlite {
			if ch == '?' {
				fmt.Fprintf(&b, "@p%d", n)
				n++
			} else {
				b.WriteRune(ch)
			}
		}
		return b.String()
	}
	return sqlite
}

func (p *Processor) updateBatchStatus(ctx context.Context, tx *sql.Tx, batchID, rowCount int64) error {
	aggStatus := AggregatesStatusComplete
	if p.SkipAggregates {
		aggStatus = AggregatesStatusPending
	}
	_, err := tx.ExecContext(ctx, p.q(`
		UPDATE dim_ingestion_batch SET row_count = ?, status = 'PROCESSED', aggregates_status = ? WHERE ingestion_batch_id = ?`),
		rowCount, aggStatus, batchID)
	return err
}
