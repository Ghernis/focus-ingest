package focus

// StagingRow maps a FOCUS export row to stg_focus_cost_line columns.
type StagingRow struct {
	SourceProvider string
	XSourceRowID   *int64

	AvailabilityZone               *string
	BilledCost                     *string
	BillingAccountId               *string
	BillingAccountName             *string
	BillingAccountType             *string
	BillingCurrency                *string
	BillingPeriodEnd               *string
	BillingPeriodStart             *string
	CapacityReservationId          *string
	CapacityReservationStatus      *string
	ChargeCategory                 *string
	ChargeClass                    *string
	ChargeDescription              *string
	ChargeFrequency                *string
	ChargePeriodEnd                *string
	ChargePeriodStart              *string
	CommitmentDiscountCategory     *string
	CommitmentDiscountId           *string
	CommitmentDiscountName         *string
	CommitmentDiscountQuantity     *string
	CommitmentDiscountStatus       *string
	CommitmentDiscountType         *string
	CommitmentDiscountUnit         *string
	ConsumedQuantity               *string
	ConsumedUnit                   *string
	ContractedCost                 *string
	ContractedUnitPrice            *string
	EffectiveCost                  *string
	InvoiceId                      *string
	InvoiceIssuer                  *string
	ListCost                       *string
	ListUnitPrice                  *string
	PricingCategory                *string
	PricingCurrency                *string
	PricingCurrencyContractedUnitPrice *string
	PricingCurrencyEffectiveCost   *string
	PricingCurrencyListUnitPrice   *string
	PricingQuantity                *string
	PricingUnit                    *string
	Provider                       *string
	Publisher                      *string
	RegionId                       *string
	RegionName                     *string
	ResourceId                     *string
	ResourceName                   *string
	ResourceType                   *string
	ServiceCategory                *string
	ServiceName                    *string
	ServiceSubcategory             *string
	SkuId                          *string
	SkuMeter                       *string
	SkuPriceDetails                *string
	SkuPriceId                     *string
	SubAccountId                   *string
	SubAccountName                 *string
	SubAccountType                 *string
	RawTagsJSON                    *string
}

// ProviderCode returns normalized provider or empty if unsupported.
func (r StagingRow) ProviderCode() string {
	raw := PtrStr(r.Provider)
	if raw == "" {
		raw = r.SourceProvider
	}
	return NormalizeProvider(raw)
}

// ChargeDate returns YYYY-MM-DD from ChargePeriodStart.
func (r StagingRow) ChargeDate() string {
	return DateOnly(PtrStr(r.ChargePeriodStart))
}
