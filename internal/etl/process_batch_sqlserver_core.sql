-- Set-based ETL: staging → dimensions → fact_focus_cost_daily
-- @IngestionBatchId and @FocusVersion are supplied by focus-ingest (see process_batch_sqlserver.go)

-- ---------------------------------------------------------------------
-- Helper: normalize FOCUS provider to dim provider code
-- ---------------------------------------------------------------------
IF OBJECT_ID('tempdb..#stg_norm') IS NOT NULL DROP TABLE #stg_norm;

SELECT
  s.*,
  CASE
    WHEN COALESCE(s.Provider, s.source_provider) IN ('AWS', 'Amazon Web Services') THEN 'AWS'
    WHEN COALESCE(s.Provider, s.source_provider) IN ('Microsoft', 'Azure') THEN 'AZURE'
    WHEN COALESCE(s.Provider, s.source_provider) IN ('Google Cloud', 'GCP', 'Google') THEN 'GCP'
    ELSE NULL
  END AS provider_code,
  CAST(s.ChargePeriodStart AS DATE) AS charge_date,
  CAST(s.BillingPeriodStart AS DATE) AS billing_period_start_date,
  CAST(s.BillingPeriodEnd AS DATE) AS billing_period_end_date,
  CASE
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'usage' THEN 'Usage'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'purchase' THEN 'Purchase'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'tax' THEN 'Tax'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'credit' THEN 'Credit'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'adjustment' THEN 'Adjustment'
    ELSE s.ChargeCategory
  END AS charge_category_norm,
  CASE WHEN NULLIF(LTRIM(RTRIM(s.PricingCategory)), '') IS NULL THEN NULL
       WHEN LOWER(s.PricingCategory) = 'standard' THEN 'Standard'
       WHEN LOWER(s.PricingCategory) = 'committed' THEN 'Committed'
       WHEN LOWER(s.PricingCategory) = 'dynamic' THEN 'Dynamic'
       ELSE 'Other'
  END AS pricing_category_norm,
  CONVERT(CHAR(64), HASHBYTES('SHA2_256', COALESCE(s.ChargeDescription, N'')), 2) AS charge_description_hash,
  NULLIF(LTRIM(RTRIM(s.SkuPriceId)), '') AS sku_price_id_norm
INTO #stg_norm
FROM dbo.stg_focus_cost_line s
WHERE s.ingestion_batch_id = @IngestionBatchId
  AND s.ChargePeriodStart IS NOT NULL
  AND s.BillingAccountId IS NOT NULL;

DELETE FROM #stg_norm WHERE provider_code IS NULL;

-- ---------------------------------------------------------------------
-- 1. Upsert billing accounts
-- ---------------------------------------------------------------------
MERGE dbo.dim_account AS t
USING (
  SELECT DISTINCT
    provider_code AS provider,
    BillingAccountId AS account_id,
    BillingAccountName AS account_name,
    BillingAccountType AS billing_account_type
  FROM #stg_norm
) AS s
ON t.provider = s.provider AND t.account_id = s.account_id
WHEN MATCHED THEN UPDATE SET
  account_name = COALESCE(s.account_name, t.account_name),
  billing_account_type = COALESCE(s.billing_account_type, t.billing_account_type)
WHEN NOT MATCHED THEN INSERT (provider, account_id, account_name, billing_account_type)
  VALUES (s.provider, s.account_id, s.account_name, s.billing_account_type);

-- ---------------------------------------------------------------------
-- 2. Upsert sub-accounts
-- ---------------------------------------------------------------------
MERGE dbo.dim_sub_account AS t
USING (
  SELECT DISTINCT
    n.provider_code AS provider,
    n.SubAccountId AS sub_account_id,
    n.SubAccountName AS sub_account_name,
    n.SubAccountType AS sub_account_type,
    a.account_sk AS billing_account_sk
  FROM #stg_norm n
  INNER JOIN dbo.dim_account a
    ON a.provider = n.provider_code AND a.account_id = n.BillingAccountId
  WHERE n.SubAccountId IS NOT NULL
) AS s
ON t.provider = s.provider AND t.sub_account_id = s.sub_account_id
WHEN MATCHED THEN UPDATE SET
  sub_account_name = COALESCE(s.sub_account_name, t.sub_account_name),
  sub_account_type = COALESCE(s.sub_account_type, t.sub_account_type),
  billing_account_sk = COALESCE(s.billing_account_sk, t.billing_account_sk)
WHEN NOT MATCHED THEN INSERT (provider, sub_account_id, sub_account_name, sub_account_type, billing_account_sk)
  VALUES (s.provider, s.sub_account_id, s.sub_account_name, s.sub_account_type, s.billing_account_sk);

-- ---------------------------------------------------------------------
-- 3. Upsert services, regions, skus, commitments, capacity reservations
-- ---------------------------------------------------------------------
MERGE dbo.dim_service AS t
USING (
  SELECT DISTINCT
    provider_code AS provider,
    COALESCE(NULLIF(ServiceName, ''), 'UNKNOWN') AS service_code,
    COALESCE(NULLIF(ServiceName, ''), 'Unknown Service') AS service_name,
    ServiceCategory AS service_category,
    ServiceSubcategory AS service_subcategory
  FROM #stg_norm
) AS s
ON t.provider = s.provider AND t.service_code = s.service_code
WHEN MATCHED THEN UPDATE SET
  service_name = s.service_name,
  service_category = COALESCE(s.service_category, t.service_category),
  service_subcategory = COALESCE(s.service_subcategory, t.service_subcategory)
WHEN NOT MATCHED THEN INSERT (provider, service_code, service_name, service_category, service_subcategory)
  VALUES (s.provider, s.service_code, s.service_name, s.service_category, s.service_subcategory);

MERGE dbo.dim_region AS t
USING (
  SELECT DISTINCT provider_code AS provider, RegionId AS region_id, RegionName AS region_name
  FROM #stg_norm
  WHERE RegionId IS NOT NULL
) AS s
ON t.provider = s.provider AND t.region_id = s.region_id
WHEN MATCHED THEN UPDATE SET region_name = COALESCE(s.region_name, t.region_name)
WHEN NOT MATCHED THEN INSERT (provider, region_id, region_name)
  VALUES (s.provider, s.region_id, s.region_name);

MERGE dbo.dim_sku AS t
USING (
  SELECT DISTINCT
    provider_code AS provider,
    SkuId AS sku_id,
    sku_price_id_norm AS sku_price_id,
    SkuMeter AS sku_meter,
    SkuPriceDetails AS sku_price_details,
    ServiceName AS service_name
  FROM #stg_norm
  WHERE SkuId IS NOT NULL
) AS s
ON t.provider = s.provider AND t.sku_id = s.sku_id
   AND (t.sku_price_id = s.sku_price_id OR (t.sku_price_id IS NULL AND s.sku_price_id IS NULL))
WHEN MATCHED THEN UPDATE SET
  sku_meter = COALESCE(s.sku_meter, t.sku_meter),
  sku_price_details = COALESCE(s.sku_price_details, t.sku_price_details),
  service_name = COALESCE(s.service_name, t.service_name)
WHEN NOT MATCHED THEN INSERT (provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
  VALUES (s.provider, s.sku_id, s.sku_price_id, s.sku_meter, s.sku_price_details, s.service_name);

MERGE dbo.dim_commitment_discount AS t
USING (
  SELECT DISTINCT
    provider_code AS provider,
    CommitmentDiscountId AS commitment_discount_id,
    CommitmentDiscountName AS commitment_discount_name,
    CommitmentDiscountType AS commitment_discount_type,
    CommitmentDiscountCategory AS commitment_discount_category,
    CommitmentDiscountUnit AS commitment_discount_unit
  FROM #stg_norm
  WHERE CommitmentDiscountId IS NOT NULL
) AS s
ON t.provider = s.provider AND t.commitment_discount_id = s.commitment_discount_id
WHEN MATCHED THEN UPDATE SET
  commitment_discount_name = COALESCE(s.commitment_discount_name, t.commitment_discount_name),
  commitment_discount_type = COALESCE(s.commitment_discount_type, t.commitment_discount_type),
  commitment_discount_category = COALESCE(s.commitment_discount_category, t.commitment_discount_category),
  commitment_discount_unit = COALESCE(s.commitment_discount_unit, t.commitment_discount_unit)
WHEN NOT MATCHED THEN INSERT (provider, commitment_discount_id, commitment_discount_name,
  commitment_discount_type, commitment_discount_category, commitment_discount_unit)
  VALUES (s.provider, s.commitment_discount_id, s.commitment_discount_name,
    s.commitment_discount_type, s.commitment_discount_category, s.commitment_discount_unit);

MERGE dbo.dim_capacity_reservation AS t
USING (
  SELECT DISTINCT
    provider_code AS provider,
    CapacityReservationId AS capacity_reservation_id,
    CapacityReservationStatus AS capacity_reservation_status
  FROM #stg_norm
  WHERE CapacityReservationId IS NOT NULL
) AS s
ON t.provider = s.provider AND t.capacity_reservation_id = s.capacity_reservation_id
WHEN MATCHED THEN UPDATE SET capacity_reservation_status = COALESCE(s.capacity_reservation_status, t.capacity_reservation_status)
WHEN NOT MATCHED THEN INSERT (provider, capacity_reservation_id, capacity_reservation_status)
  VALUES (s.provider, s.capacity_reservation_id, s.capacity_reservation_status);

-- ---------------------------------------------------------------------
-- 4. Upsert resources (SCD2-lite: insert new version when tags/name change)
-- ---------------------------------------------------------------------
;WITH src AS (
  SELECT DISTINCT
    n.provider_code AS provider,
    n.ResourceId AS global_resource_id,
    COALESCE(NULLIF(n.ResourceType, ''), 'UNKNOWN') AS resource_type,
    a.account_sk,
    sa.sub_account_sk,
    svc.service_sk,
    n.RegionId AS region,
    n.ResourceName AS name,
    JSON_VALUE(n.raw_tags_json, '$.application') AS application,
    JSON_VALUE(n.raw_tags_json, '$.environment') AS environment,
    JSON_VALUE(n.raw_tags_json, '$.business_unit') AS business,
    JSON_VALUE(n.raw_tags_json, '$.CostCenter') AS cost_center,
    JSON_VALUE(n.raw_tags_json, '$."info:support-team-email"') AS owner_email,
    n.raw_tags_json AS tags_json,
    n.charge_date AS valid_from
  FROM #stg_norm n
  INNER JOIN dbo.dim_account a ON a.provider = n.provider_code AND a.account_id = n.BillingAccountId
  LEFT JOIN dbo.dim_sub_account sa ON sa.provider = n.provider_code AND sa.sub_account_id = n.SubAccountId
  INNER JOIN dbo.dim_service svc ON svc.provider = n.provider_code
    AND svc.service_code = COALESCE(NULLIF(n.ServiceName, ''), 'UNKNOWN')
  WHERE n.ResourceId IS NOT NULL
)
INSERT INTO dbo.dim_resource (
  provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
  region, name, application, environment, business, cost_center, owner_email, tags_json, valid_from
)
SELECT
  s.provider, s.global_resource_id, s.resource_type, s.account_sk, s.sub_account_sk, s.service_sk,
  s.region, s.name, s.application, s.environment, s.business, s.cost_center, s.owner_email, s.tags_json, s.valid_from
FROM src s
WHERE NOT EXISTS (
  SELECT 1 FROM dbo.dim_resource r
  WHERE r.provider = s.provider
    AND r.global_resource_id = s.global_resource_id
    AND r.valid_to IS NULL
);

-- ---------------------------------------------------------------------
-- 5. Rollup to fact_focus_cost_daily
-- ---------------------------------------------------------------------
IF OBJECT_ID('tempdb..#daily_rollup') IS NOT NULL DROP TABLE #daily_rollup;

SELECT
  n.charge_date,
  a.account_sk AS billing_account_sk,
  sa.sub_account_sk,
  res.resource_sk,
  svc.service_sk,
  sku.sku_sk,
  reg.region_sk,
  cc.charge_category_sk,
  cf.charge_frequency_sk,
  pc.pricing_category_sk,
  cmt.commitment_sk,
  n.CommitmentDiscountStatus AS commitment_discount_status,
  cap.capacity_reservation_sk,
  n.CapacityReservationStatus AS capacity_reservation_status,
  n.charge_description_hash,
  n.billing_period_start_date AS billing_period_start,
  n.billing_period_end_date AS billing_period_end,
  SUM(COALESCE(n.BilledCost, 0)) AS billed_cost,
  SUM(COALESCE(n.EffectiveCost, 0)) AS effective_cost,
  SUM(COALESCE(n.ListCost, 0)) AS list_cost,
  SUM(COALESCE(n.ContractedCost, 0)) AS contracted_cost,
  SUM(COALESCE(n.PricingQuantity, 0)) AS pricing_quantity,
  SUM(COALESCE(n.ConsumedQuantity, 0)) AS consumed_quantity,
  SUM(COALESCE(n.CommitmentDiscountQuantity, 0)) AS commitment_discount_quantity,
  COUNT(*) AS line_count,
  MIN(n.ChargePeriodStart) AS first_charge_period_start,
  MAX(n.ChargePeriodEnd) AS last_charge_period_end
INTO #daily_rollup
FROM #stg_norm n
INNER JOIN dbo.dim_account a ON a.provider = n.provider_code AND a.account_id = n.BillingAccountId
INNER JOIN dbo.dim_service svc ON svc.provider = n.provider_code
  AND svc.service_code = COALESCE(NULLIF(n.ServiceName, ''), 'UNKNOWN')
INNER JOIN dbo.dim_charge_category cc ON cc.charge_category = n.charge_category_norm
LEFT JOIN dbo.dim_sub_account sa ON sa.provider = n.provider_code AND sa.sub_account_id = n.SubAccountId
LEFT JOIN dbo.dim_resource res ON res.provider = n.provider_code
  AND res.global_resource_id = n.ResourceId AND res.valid_to IS NULL
LEFT JOIN dbo.dim_sku sku ON sku.provider = n.provider_code
  AND sku.sku_id = n.SkuId
  AND (sku.sku_price_id = n.sku_price_id_norm OR (sku.sku_price_id IS NULL AND n.sku_price_id_norm IS NULL))
LEFT JOIN dbo.dim_region reg ON reg.provider = n.provider_code AND reg.region_id = n.RegionId
LEFT JOIN dbo.dim_charge_frequency cf ON cf.charge_frequency = n.ChargeFrequency
LEFT JOIN dbo.dim_pricing_category pc ON pc.pricing_category = n.pricing_category_norm
LEFT JOIN dbo.dim_commitment_discount cmt ON cmt.provider = n.provider_code
  AND cmt.commitment_discount_id = n.CommitmentDiscountId
LEFT JOIN dbo.dim_capacity_reservation cap ON cap.provider = n.provider_code
  AND cap.capacity_reservation_id = n.CapacityReservationId
GROUP BY
  n.charge_date, a.account_sk, sa.sub_account_sk, res.resource_sk, svc.service_sk,
  sku.sku_sk, reg.region_sk, cc.charge_category_sk, cf.charge_frequency_sk,
  pc.pricing_category_sk, cmt.commitment_sk, n.CommitmentDiscountStatus,
  cap.capacity_reservation_sk, n.CapacityReservationStatus,
  n.charge_description_hash, n.billing_period_start_date, n.billing_period_end_date;

-- Remove prior rows for this batch (idempotent reload)
DELETE FROM dbo.bridge_cost_tag
WHERE cost_daily_id IN (
  SELECT cost_daily_id FROM dbo.fact_focus_cost_daily WHERE ingestion_batch_id = @IngestionBatchId
);

DELETE FROM dbo.fact_focus_cost_daily WHERE ingestion_batch_id = @IngestionBatchId;

INSERT INTO dbo.fact_focus_cost_daily (
  charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk, sku_sk,
  region_sk, charge_category_sk, charge_frequency_sk, pricing_category_sk,
  commitment_sk, commitment_discount_status, capacity_reservation_sk, capacity_reservation_status,
  charge_description_hash,
  billing_period_start, billing_period_end,
  billed_cost, effective_cost, list_cost, contracted_cost,
  pricing_quantity, consumed_quantity, commitment_discount_quantity, line_count,
  first_charge_period_start, last_charge_period_end,
  ingestion_batch_id, focus_version
)
SELECT
  charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk, sku_sk,
  region_sk, charge_category_sk, charge_frequency_sk, pricing_category_sk,
  commitment_sk, commitment_discount_status, capacity_reservation_sk, capacity_reservation_status,
  charge_description_hash,
  billing_period_start, billing_period_end,
  billed_cost, effective_cost, list_cost, contracted_cost,
  pricing_quantity, consumed_quantity, commitment_discount_quantity, line_count,
  first_charge_period_start, last_charge_period_end,
  @IngestionBatchId, @FocusVersion
FROM #daily_rollup;
