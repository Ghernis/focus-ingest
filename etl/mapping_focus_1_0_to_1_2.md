# FOCUS 1.0 → 1.2 Column Mapping

Reference for loading exports from AWS, Azure, and GCP into `stg_focus_cost_line`.

## Provider normalization

| FOCUS export value | `source_provider` / dim value |
|--------------------|---------------------------------|
| AWS                | AWS                             |
| Microsoft          | AZURE                           |
| Google Cloud       | GCP                             |
| Oracle             | (skipped unless you extend CHECK) |

## Column renames (1.0 sample → 1.2 staging)

| FOCUS 1.0 (sample CSV) | FOCUS 1.2 staging column | Notes |
|------------------------|--------------------------|-------|
| ProviderName           | Provider                 | Required |
| InvoiceIssuerName      | InvoiceIssuer            | |
| PublisherName          | Publisher                | |
| Id                     | x_source_row_id          | Provider-specific row id |
| Tags                   | raw_tags_json            | JSON object string |
| (all other shared columns) | Same PascalCase name | Direct map |

## Columns present in 1.2 only (NULL for 1.0 files)

- BillingAccountType
- CapacityReservationId, CapacityReservationStatus
- CommitmentDiscountQuantity, CommitmentDiscountUnit
- InvoiceId
- PricingCurrency, PricingCurrencyContractedUnitPrice, PricingCurrencyEffectiveCost, PricingCurrencyListUnitPrice
- ServiceSubcategory
- SkuMeter, SkuPriceDetails
- SubAccountType

## Four-cost model (primary Power BI measures)

| Column | Use |
|--------|-----|
| ListCost | Public/on-demand list price |
| ContractedCost | After negotiated/contract pricing |
| BilledCost | Invoice-aligned amount |
| EffectiveCost | After all discounts — **primary optimization metric** |

## Commitments & reservations

Rows with commitment data use the same table:

- **CommitmentDiscountStatus**: `Used` (consumed) / `Unused` (waste)
- **CommitmentDiscountType**: e.g. Savings Plan, Reservation
- **CapacityReservationStatus** (1.2): same Used/Unused pattern

Filter `ChargeCategory = 'Purchase'` for upfront commitment purchases; filter `CommitmentDiscountStatus IS NOT NULL` for utilization analysis.

## Tag keys in sample (for `dim_tag` / `agg_cost_by_tag`)

Common keys: `application`, `environment`, `business_unit`, `CostCenter`, `Project`, `org`, `env`.

ETL maps JSON tags into `dim_tag` + `bridge_cost_tag` at daily fact grain.

## Charge & pricing lookups

Seed tables (in `focus_dw.sql`):

- **ChargeCategory**: Usage, Purchase, Tax, Credit, Adjustment
- **ChargeFrequency**: Usage-Based, Recurring, One-Time
- **PricingCategory**: Standard, Committed, Dynamic, Other (normalize `standard` → `Standard` in ETL)

## Daily rollup grain

`fact_focus_cost_daily` groups staging rows by:

`charge_date` (date of ChargePeriodStart) + billing/sub accounts + resource + service + sku + region + charge/pricing category + commitment + capacity reservation + hash(ChargeDescription) + billing period + batch.

Measures are **summed**; `line_count` tracks source row compression.
