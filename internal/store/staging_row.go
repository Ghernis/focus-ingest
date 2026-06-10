package store

import "github.com/ghernis/focus_dt/internal/focus"

func stagingRowArgs(batchID int64, focusVersion, sourceFile string, r focus.StagingRow) []interface{} {
	return []interface{}{
		batchID, r.SourceProvider, focusVersion, sourceFile,
		nullStr(r.BilledCost), nullStr(r.BillingAccountId), nullStr(r.BillingAccountName), nullStr(r.BillingAccountType), nullStr(r.BillingCurrency),
		nullStr(r.BillingPeriodEnd), nullStr(r.BillingPeriodStart), nullStr(r.CapacityReservationId), nullStr(r.CapacityReservationStatus),
		nullStr(r.ChargeCategory), nullStr(r.ChargeClass), nullStr(r.ChargeDescription), nullStr(r.ChargeFrequency), nullStr(r.ChargePeriodEnd), nullStr(r.ChargePeriodStart),
		nullStr(r.CommitmentDiscountCategory), nullStr(r.CommitmentDiscountId), nullStr(r.CommitmentDiscountName), nullStr(r.CommitmentDiscountQuantity),
		nullStr(r.CommitmentDiscountStatus), nullStr(r.CommitmentDiscountType), nullStr(r.CommitmentDiscountUnit),
		nullStr(r.ConsumedQuantity), nullStr(r.ConsumedUnit), nullStr(r.ContractedCost), nullStr(r.ContractedUnitPrice), nullStr(r.EffectiveCost),
		nullStr(r.InvoiceId), nullStr(r.InvoiceIssuer), nullStr(r.ListCost), nullStr(r.ListUnitPrice), nullStr(r.PricingCategory), nullStr(r.PricingCurrency),
		nullStr(r.PricingQuantity), nullStr(r.PricingUnit), nullStr(r.Provider), nullStr(r.Publisher), nullStr(r.RegionId), nullStr(r.RegionName),
		nullStr(r.ResourceId), nullStr(r.ResourceName), nullStr(r.ResourceType), nullStr(r.ServiceCategory), nullStr(r.ServiceName), nullStr(r.ServiceSubcategory),
		nullStr(r.SkuId), nullStr(r.SkuMeter), nullStr(r.SkuPriceDetails), nullStr(r.SkuPriceId), nullStr(r.SubAccountId), nullStr(r.SubAccountName), nullStr(r.SubAccountType),
		nullStr(r.RawTagsJSON),
	}
}
