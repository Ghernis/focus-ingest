package parquet

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/deprecated"

	"github.com/ghernis/focus_dt/internal/focus"
)

// Julian day number for 1970-01-01 (Impala/Spark INT96 timestamp epoch).
const julianEpoch = 2440588

// focusParquetRow matches Azure/AWS FOCUS 1.2 parquet export columns.
type focusParquetRow struct {
	AvailabilityZone               *string  `parquet:"AvailabilityZone"`
	BilledCost                     *[16]byte `parquet:"BilledCost"`
	BillingAccountId               *string  `parquet:"BillingAccountId"`
	BillingAccountName             *string  `parquet:"BillingAccountName"`
	BillingAccountType             *string  `parquet:"BillingAccountType"`
	BillingCurrency                *string  `parquet:"BillingCurrency"`
	BillingPeriodEnd               deprecated.Int96 `parquet:"BillingPeriodEnd"`
	BillingPeriodStart             deprecated.Int96 `parquet:"BillingPeriodStart"`
	CapacityReservationId          *string  `parquet:"CapacityReservationId"`
	CapacityReservationStatus      *string  `parquet:"CapacityReservationStatus"`
	ChargeCategory                 *string  `parquet:"ChargeCategory"`
	ChargeClass                    *string  `parquet:"ChargeClass"`
	ChargeDescription              *string  `parquet:"ChargeDescription"`
	ChargeFrequency                *string  `parquet:"ChargeFrequency"`
	ChargePeriodEnd                deprecated.Int96 `parquet:"ChargePeriodEnd"`
	ChargePeriodStart              deprecated.Int96 `parquet:"ChargePeriodStart"`
	CommitmentDiscountCategory     *string  `parquet:"CommitmentDiscountCategory"`
	CommitmentDiscountId           *string  `parquet:"CommitmentDiscountId"`
	CommitmentDiscountName         *string  `parquet:"CommitmentDiscountName"`
	CommitmentDiscountQuantity     *[16]byte `parquet:"CommitmentDiscountQuantity"`
	CommitmentDiscountStatus       *string  `parquet:"CommitmentDiscountStatus"`
	CommitmentDiscountType         *string  `parquet:"CommitmentDiscountType"`
	CommitmentDiscountUnit         *string  `parquet:"CommitmentDiscountUnit"`
	ConsumedQuantity               *[16]byte `parquet:"ConsumedQuantity"`
	ConsumedUnit                   *string  `parquet:"ConsumedUnit"`
	ContractedCost                 *[16]byte `parquet:"ContractedCost"`
	ContractedUnitPrice            *[16]byte `parquet:"ContractedUnitPrice"`
	EffectiveCost                  *[16]byte `parquet:"EffectiveCost"`
	InvoiceId                      *string  `parquet:"InvoiceId"`
	InvoiceIssuerName              *string  `parquet:"InvoiceIssuerName"`
	ListCost                       *[16]byte `parquet:"ListCost"`
	ListUnitPrice                  *[16]byte `parquet:"ListUnitPrice"`
	PricingCategory                *string  `parquet:"PricingCategory"`
	PricingCurrency                *string  `parquet:"PricingCurrency"`
	PricingQuantity                *[16]byte `parquet:"PricingQuantity"`
	PricingUnit                    *string  `parquet:"PricingUnit"`
	ProviderName                   *string  `parquet:"ProviderName"`
	PublisherName                  *string  `parquet:"PublisherName"`
	RegionId                       *string  `parquet:"RegionId"`
	RegionName                     *string  `parquet:"RegionName"`
	ResourceId                     *string  `parquet:"ResourceId"`
	ResourceName                   *string  `parquet:"ResourceName"`
	ResourceType                   *string  `parquet:"ResourceType"`
	ServiceCategory                *string  `parquet:"ServiceCategory"`
	ServiceName                    *string  `parquet:"ServiceName"`
	ServiceSubcategory             *string  `parquet:"ServiceSubcategory"`
	SkuMeter                       *string  `parquet:"SkuMeter"`
	SkuId                          *string  `parquet:"SkuId"`
	SkuPriceId                     *string  `parquet:"SkuPriceId"`
	SkuPriceDetails                *string  `parquet:"SkuPriceDetails"`
	SubAccountId                   *string  `parquet:"SubAccountId"`
	SubAccountName                 *string  `parquet:"SubAccountName"`
	SubAccountType                 *string  `parquet:"SubAccountType"`
	Tags                           *string  `parquet:"Tags"`
}

func ReadFile(path string, batchSize int, fn func([]focus.StagingRow) error) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := parquet.NewGenericReader[focusParquetRow](f)
	defer reader.Close()

	buf := make([]focusParquetRow, batchSize)
	total := 0
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			out := make([]focus.StagingRow, 0, n)
			for i := 0; i < n; i++ {
				sr := mapRow(buf[i])
				if sr.ProviderCode() == "" {
					continue
				}
				if sr.BillingAccountId == nil || strings.TrimSpace(*sr.BillingAccountId) == "" {
					continue
				}
				if sr.ChargePeriodStart == nil || strings.TrimSpace(*sr.ChargePeriodStart) == "" {
					continue
				}
				out = append(out, sr)
			}
			if len(out) > 0 {
				if err2 := fn(out); err2 != nil {
					return total, err2
				}
				total += len(out)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return total, err
		}
		if n == 0 {
			break
		}
	}
	return total, nil
}

func mapRow(r focusParquetRow) focus.StagingRow {
	provider := r.ProviderName
	return focus.StagingRow{
		SourceProvider:             focus.PtrStr(provider),
		AvailabilityZone:             r.AvailabilityZone,
		BilledCost:                   decimalFlbaString(r.BilledCost),
		BillingAccountId:             r.BillingAccountId,
		BillingAccountName:           r.BillingAccountName,
		BillingAccountType:           r.BillingAccountType,
		BillingCurrency:              r.BillingCurrency,
		BillingPeriodEnd:             int96Str(r.BillingPeriodEnd),
		BillingPeriodStart:           int96Str(r.BillingPeriodStart),
		CapacityReservationId:        r.CapacityReservationId,
		CapacityReservationStatus:    r.CapacityReservationStatus,
		ChargeCategory:               r.ChargeCategory,
		ChargeClass:                  r.ChargeClass,
		ChargeDescription:            r.ChargeDescription,
		ChargeFrequency:              r.ChargeFrequency,
		ChargePeriodEnd:              int96Str(r.ChargePeriodEnd),
		ChargePeriodStart:            int96Str(r.ChargePeriodStart),
		CommitmentDiscountCategory:   r.CommitmentDiscountCategory,
		CommitmentDiscountId:         r.CommitmentDiscountId,
		CommitmentDiscountName:       r.CommitmentDiscountName,
		CommitmentDiscountQuantity:   decimalFlbaString(r.CommitmentDiscountQuantity),
		CommitmentDiscountStatus:     r.CommitmentDiscountStatus,
		CommitmentDiscountType:       r.CommitmentDiscountType,
		CommitmentDiscountUnit:       r.CommitmentDiscountUnit,
		ConsumedQuantity:             decimalFlbaString(r.ConsumedQuantity),
		ConsumedUnit:                 r.ConsumedUnit,
		ContractedCost:               decimalFlbaString(r.ContractedCost),
		ContractedUnitPrice:          decimalFlbaString(r.ContractedUnitPrice),
		EffectiveCost:                decimalFlbaString(r.EffectiveCost),
		InvoiceId:                    r.InvoiceId,
		InvoiceIssuer:                r.InvoiceIssuerName,
		ListCost:                     decimalFlbaString(r.ListCost),
		ListUnitPrice:                decimalFlbaString(r.ListUnitPrice),
		PricingCategory:              r.PricingCategory,
		PricingCurrency:              r.PricingCurrency,
		PricingQuantity:              decimalFlbaString(r.PricingQuantity),
		PricingUnit:                  r.PricingUnit,
		Provider:                     provider,
		Publisher:                    r.PublisherName,
		RegionId:                     r.RegionId,
		RegionName:                   r.RegionName,
		ResourceId:                   r.ResourceId,
		ResourceName:                 r.ResourceName,
		ResourceType:                 r.ResourceType,
		ServiceCategory:              r.ServiceCategory,
		ServiceName:                  r.ServiceName,
		ServiceSubcategory:           r.ServiceSubcategory,
		SkuId:                        r.SkuId,
		SkuMeter:                     r.SkuMeter,
		SkuPriceDetails:              r.SkuPriceDetails,
		SkuPriceId:                   r.SkuPriceId,
		SubAccountId:                 r.SubAccountId,
		SubAccountName:               r.SubAccountName,
		SubAccountType:               r.SubAccountType,
		RawTagsJSON:                  normalizeTags(r.Tags),
	}
}

func int96Str(i96 deprecated.Int96) *string {
	if int96IsZero(i96) {
		return nil
	}
	nanos := int64(i96[0]) | int64(i96[1])<<32
	julianDay := int64(i96[2])
	secs := (julianDay - julianEpoch) * 86400
	s := time.Unix(secs, nanos).UTC().Format("2006-01-02 15:04:05")
	return &s
}

func int96IsZero(i96 deprecated.Int96) bool {
	return i96[0] == 0 && i96[1] == 0 && i96[2] == 0
}

func normalizeTags(tags *string) *string {
	if tags == nil {
		return nil
	}
	raw := strings.TrimSpace(*tags)
	if raw == "" {
		return nil
	}
	return &raw
}
