-- =====================================================================
-- ETL Step 2: Staging → Dimensions → Daily Fact → Aggregates
-- Parameter: @IngestionBatchId (from step 1 or load_sample.py)
-- =====================================================================

SET NOCOUNT ON;

DECLARE @IngestionBatchId BIGINT = 1;  -- SET THIS before running
DECLARE @FocusVersion     VARCHAR(16);

SELECT @FocusVersion = focus_version
FROM dbo.dim_ingestion_batch
WHERE ingestion_batch_id = @IngestionBatchId;

IF @FocusVersion IS NULL
BEGIN
  RAISERROR('Invalid ingestion_batch_id: %d', 16, 1, @IngestionBatchId);
  RETURN;
END

-- ---------------------------------------------------------------------
-- Helper: normalize FOCUS provider to dim provider code
-- ---------------------------------------------------------------------
IF OBJECT_ID('tempdb..#stg_norm') IS NOT NULL DROP TABLE #stg_norm;

SELECT
  s.*,
  CAST(CASE
    WHEN COALESCE(s.Provider, s.source_provider) IN ('AWS', 'Amazon Web Services') THEN 'AWS'
    WHEN COALESCE(s.Provider, s.source_provider) IN ('Microsoft', 'Azure') THEN 'AZURE'
    WHEN COALESCE(s.Provider, s.source_provider) IN ('Google Cloud', 'GCP', 'Google') THEN 'GCP'
    ELSE NULL
  END AS VARCHAR(10)) COLLATE DATABASE_DEFAULT AS provider_code,
  CAST(s.ChargePeriodStart AS DATE) AS charge_date,
  CAST(s.BillingPeriodStart AS DATE) AS billing_period_start_date,
  CAST(s.BillingPeriodEnd AS DATE) AS billing_period_end_date,
  CAST(CASE
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'usage' THEN 'Usage'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'purchase' THEN 'Purchase'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'tax' THEN 'Tax'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'credit' THEN 'Credit'
    WHEN LOWER(LTRIM(RTRIM(s.ChargeCategory))) = 'adjustment' THEN 'Adjustment'
    ELSE s.ChargeCategory
  END AS VARCHAR(32)) COLLATE DATABASE_DEFAULT AS charge_category_norm,
  CAST(CASE WHEN NULLIF(LTRIM(RTRIM(s.PricingCategory)), '') IS NULL THEN NULL
       WHEN LOWER(s.PricingCategory) = 'standard' THEN 'Standard'
       WHEN LOWER(s.PricingCategory) = 'committed' THEN 'Committed'
       WHEN LOWER(s.PricingCategory) = 'dynamic' THEN 'Dynamic'
       ELSE 'Other'
  END AS VARCHAR(32)) COLLATE DATABASE_DEFAULT AS pricing_category_norm,
  CONVERT(CHAR(64), HASHBYTES('SHA2_256', COALESCE(s.ChargeDescription, N'')), 2) COLLATE DATABASE_DEFAULT AS charge_description_hash,
  CAST(NULLIF(LTRIM(RTRIM(s.SkuId)), '') AS VARCHAR(128)) COLLATE DATABASE_DEFAULT AS sku_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.SkuPriceId)), '') AS VARCHAR(256)) COLLATE DATABASE_DEFAULT AS sku_price_id_norm,
  CAST(COALESCE(NULLIF(LTRIM(RTRIM(s.ServiceName)), ''), 'UNKNOWN') AS VARCHAR(256)) COLLATE DATABASE_DEFAULT AS service_code_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.SubAccountId)), '') AS VARCHAR(512)) COLLATE DATABASE_DEFAULT AS sub_account_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.RegionId)), '') AS VARCHAR(128)) COLLATE DATABASE_DEFAULT AS region_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.ResourceId)), '') AS VARCHAR(512)) COLLATE DATABASE_DEFAULT AS resource_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.CommitmentDiscountId)), '') AS VARCHAR(512)) COLLATE DATABASE_DEFAULT AS commitment_discount_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.CapacityReservationId)), '') AS VARCHAR(512)) COLLATE DATABASE_DEFAULT AS capacity_reservation_id_norm,
  CAST(NULLIF(LTRIM(RTRIM(s.ChargeFrequency)), '') AS VARCHAR(32)) COLLATE DATABASE_DEFAULT AS charge_frequency_norm
INTO #stg_norm
FROM dbo.stg_focus_cost_line s
WHERE s.ingestion_batch_id = @IngestionBatchId
  AND s.ChargePeriodStart IS NOT NULL
  AND s.BillingAccountId IS NOT NULL;

-- Force expression columns onto the user-database collation (tempdb may differ).
ALTER TABLE #stg_norm ALTER COLUMN provider_code VARCHAR(10) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN charge_category_norm VARCHAR(32) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN pricing_category_norm VARCHAR(32) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN charge_description_hash CHAR(64) COLLATE DATABASE_DEFAULT NOT NULL;
ALTER TABLE #stg_norm ALTER COLUMN sku_id_norm VARCHAR(256) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN sku_price_id_norm VARCHAR(512) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN service_code_norm VARCHAR(256) COLLATE DATABASE_DEFAULT NOT NULL;
ALTER TABLE #stg_norm ALTER COLUMN sub_account_id_norm VARCHAR(512) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN region_id_norm VARCHAR(128) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN resource_id_norm VARCHAR(512) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN commitment_discount_id_norm VARCHAR(512) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN capacity_reservation_id_norm VARCHAR(512) COLLATE DATABASE_DEFAULT NULL;
ALTER TABLE #stg_norm ALTER COLUMN charge_frequency_norm VARCHAR(32) COLLATE DATABASE_DEFAULT NULL;

DELETE FROM #stg_norm WHERE provider_code IS NULL;

-- ---------------------------------------------------------------------
-- 1. Upsert billing accounts
-- ---------------------------------------------------------------------
MERGE dbo.dim_account AS t
USING (
  SELECT
    provider_code AS provider,
    BillingAccountId AS account_id,
    MAX(BillingAccountName) AS account_name,
    MAX(BillingAccountType) AS billing_account_type
  FROM #stg_norm
  GROUP BY provider_code, BillingAccountId
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
  SELECT
    n.provider_code AS provider,
    n.sub_account_id_norm AS sub_account_id,
    MAX(n.SubAccountName) AS sub_account_name,
    MAX(n.SubAccountType) AS sub_account_type,
    MAX(a.account_sk) AS billing_account_sk
  FROM #stg_norm n
  INNER JOIN dbo.dim_account a
    ON a.provider = n.provider_code AND a.account_id = n.BillingAccountId
  WHERE n.sub_account_id_norm IS NOT NULL
  GROUP BY n.provider_code, n.sub_account_id_norm
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
  SELECT
    provider_code AS provider,
    service_code_norm AS service_code,
    service_code_norm AS service_name,
    MAX(ServiceCategory) AS service_category,
    MAX(ServiceSubcategory) AS service_subcategory
  FROM #stg_norm
  GROUP BY provider_code, service_code_norm
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
  SELECT provider_code AS provider, region_id_norm AS region_id, MAX(RegionName) AS region_name
  FROM #stg_norm
  WHERE region_id_norm IS NOT NULL
  GROUP BY provider_code, region_id_norm
) AS s
ON t.provider = s.provider AND t.region_id = s.region_id
WHEN MATCHED THEN UPDATE SET region_name = COALESCE(s.region_name, t.region_name)
WHEN NOT MATCHED THEN INSERT (provider, region_id, region_name)
  VALUES (s.provider, s.region_id, s.region_name);

MERGE dbo.dim_sku AS t
USING (
  SELECT
    provider_code AS provider,
    sku_id_norm AS sku_id,
    sku_price_id_norm AS sku_price_id,
    MAX(SkuMeter) AS sku_meter,
    MAX(SkuPriceDetails) AS sku_price_details,
    MAX(ServiceName) AS service_name
  FROM #stg_norm
  WHERE sku_id_norm IS NOT NULL
  GROUP BY provider_code, sku_id_norm, sku_price_id_norm
) AS s
ON t.provider = s.provider AND t.sku_id = s.sku_id
   AND ISNULL(NULLIF(LTRIM(RTRIM(t.sku_price_id)), ''), '~') = ISNULL(s.sku_price_id, '~')
WHEN MATCHED THEN UPDATE SET
  sku_meter = COALESCE(s.sku_meter, t.sku_meter),
  sku_price_details = COALESCE(s.sku_price_details, t.sku_price_details),
  service_name = COALESCE(s.service_name, t.service_name)
WHEN NOT MATCHED THEN INSERT (provider, sku_id, sku_price_id, sku_meter, sku_price_details, service_name)
  VALUES (s.provider, s.sku_id, s.sku_price_id, s.sku_meter, s.sku_price_details, s.service_name);

MERGE dbo.dim_commitment_discount AS t
USING (
  SELECT
    provider_code AS provider,
    commitment_discount_id_norm AS commitment_discount_id,
    MAX(CommitmentDiscountName) AS commitment_discount_name,
    MAX(CommitmentDiscountType) AS commitment_discount_type,
    MAX(CommitmentDiscountCategory) AS commitment_discount_category,
    MAX(CommitmentDiscountUnit) AS commitment_discount_unit
  FROM #stg_norm
  WHERE commitment_discount_id_norm IS NOT NULL
  GROUP BY provider_code, commitment_discount_id_norm
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
  SELECT
    provider_code AS provider,
    capacity_reservation_id_norm AS capacity_reservation_id,
    MAX(CapacityReservationStatus) AS capacity_reservation_status
  FROM #stg_norm
  WHERE capacity_reservation_id_norm IS NOT NULL
  GROUP BY provider_code, capacity_reservation_id_norm
) AS s
ON t.provider = s.provider AND t.capacity_reservation_id = s.capacity_reservation_id
WHEN MATCHED THEN UPDATE SET capacity_reservation_status = COALESCE(s.capacity_reservation_status, t.capacity_reservation_status)
WHEN NOT MATCHED THEN INSERT (provider, capacity_reservation_id, capacity_reservation_status)
  VALUES (s.provider, s.capacity_reservation_id, s.capacity_reservation_status);

-- ---------------------------------------------------------------------
-- 4. Upsert resources (SCD2-lite: insert new version when tags/name change)
-- ---------------------------------------------------------------------
;WITH src AS (
  SELECT
    n.provider_code AS provider,
    n.resource_id_norm AS global_resource_id,
    COALESCE(NULLIF(MAX(NULLIF(LTRIM(RTRIM(n.ResourceType)), '')), ''), 'UNKNOWN') AS resource_type,
    MAX(a.account_sk) AS account_sk,
    MAX(sa.sub_account_sk) AS sub_account_sk,
    MAX(svc.service_sk) AS service_sk,
    MAX(reg.region_sk) AS region_sk,
    MAX(n.ResourceName) AS name,
    MAX(JSON_VALUE(n.raw_tags_json, '$.application')) AS application,
    MAX(JSON_VALUE(n.raw_tags_json, '$.environment')) AS environment,
    MAX(JSON_VALUE(n.raw_tags_json, '$.business_unit')) AS business,
    MAX(JSON_VALUE(n.raw_tags_json, '$.CostCenter')) AS cost_center,
    MAX(n.raw_tags_json) AS tags_json,
    MIN(n.charge_date) AS valid_from
  FROM #stg_norm n
  INNER JOIN dbo.dim_account a ON a.provider = n.provider_code AND a.account_id = n.BillingAccountId
  LEFT JOIN dbo.dim_sub_account sa ON sa.provider = n.provider_code AND sa.sub_account_id = n.sub_account_id_norm
  INNER JOIN dbo.dim_service svc ON svc.provider = n.provider_code
    AND svc.service_code = n.service_code_norm
  LEFT JOIN dbo.dim_region reg ON reg.provider = n.provider_code AND reg.region_id = n.region_id_norm
  WHERE n.resource_id_norm IS NOT NULL
  GROUP BY n.provider_code, n.resource_id_norm
)
INSERT INTO dbo.dim_resource (
  provider, global_resource_id, resource_type, account_sk, sub_account_sk, service_sk,
  region_sk, name, application, environment, business, cost_center, tags_json, valid_from
)
SELECT
  s.provider, s.global_resource_id, s.resource_type, s.account_sk, s.sub_account_sk, s.service_sk,
  s.region_sk, s.name, s.application, s.environment, s.business, s.cost_center, s.tags_json, s.valid_from
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
  AND svc.service_code = n.service_code_norm
INNER JOIN dbo.dim_charge_category cc ON cc.charge_category = n.charge_category_norm
LEFT JOIN dbo.dim_sub_account sa ON sa.provider = n.provider_code AND sa.sub_account_id = n.sub_account_id_norm
LEFT JOIN dbo.dim_resource res ON res.provider = n.provider_code
  AND res.global_resource_id = n.resource_id_norm AND res.valid_to IS NULL
LEFT JOIN dbo.dim_sku sku ON sku.provider = n.provider_code
  AND sku.sku_id = n.sku_id_norm
  AND ISNULL(NULLIF(LTRIM(RTRIM(sku.sku_price_id)), ''), '~') = ISNULL(n.sku_price_id_norm, '~')
LEFT JOIN dbo.dim_region reg ON reg.provider = n.provider_code AND reg.region_id = n.region_id_norm
LEFT JOIN dbo.dim_charge_frequency cf ON cf.charge_frequency = n.charge_frequency_norm
LEFT JOIN dbo.dim_pricing_category pc ON pc.pricing_category = n.pricing_category_norm
LEFT JOIN dbo.dim_commitment_discount cmt ON cmt.provider = n.provider_code
  AND cmt.commitment_discount_id = n.commitment_discount_id_norm
LEFT JOIN dbo.dim_capacity_reservation cap ON cap.provider = n.provider_code
  AND cap.capacity_reservation_id = n.capacity_reservation_id_norm
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

-- ---------------------------------------------------------------------
-- 6. Tag bridge (from staging rows linked to daily facts via resource + date)
-- ---------------------------------------------------------------------
IF OBJECT_ID('tempdb..#tag_pairs') IS NOT NULL DROP TABLE #tag_pairs;

SELECT DISTINCT
  f.cost_daily_id,
  j.[key] AS tag_key,
  LEFT(j.[value], 512) AS tag_value
INTO #tag_pairs
FROM dbo.fact_focus_cost_daily f
INNER JOIN #stg_norm n
  ON n.charge_date = f.charge_date
 AND n.charge_description_hash = f.charge_description_hash
INNER JOIN dbo.dim_account a ON a.account_sk = f.billing_account_sk
  AND a.account_id = n.BillingAccountId AND a.provider = n.provider_code
CROSS APPLY OPENJSON(n.raw_tags_json) j
WHERE f.ingestion_batch_id = @IngestionBatchId
  AND n.raw_tags_json IS NOT NULL
  AND ISJSON(n.raw_tags_json) = 1
  AND j.[type] = 1;

INSERT INTO dbo.dim_tag (tag_key, tag_value)
SELECT DISTINCT tag_key, tag_value
FROM #tag_pairs tp
WHERE NOT EXISTS (
  SELECT 1 FROM dbo.dim_tag t WHERE t.tag_key = tp.tag_key AND t.tag_value = tp.tag_value
);

INSERT INTO dbo.bridge_cost_tag (cost_daily_id, tag_sk)
SELECT DISTINCT tp.cost_daily_id, t.tag_sk
FROM #tag_pairs tp
INNER JOIN dbo.dim_tag t ON t.tag_key = tp.tag_key AND t.tag_value = tp.tag_value
WHERE NOT EXISTS (
  SELECT 1 FROM dbo.bridge_cost_tag b
  WHERE b.cost_daily_id = tp.cost_daily_id AND b.tag_sk = t.tag_sk
);

-- ---------------------------------------------------------------------
-- 7. Rebuild aggregate tables (full refresh; swap to incremental by batch if needed)
-- ---------------------------------------------------------------------

TRUNCATE TABLE dbo.agg_cost_daily;

INSERT INTO dbo.agg_cost_daily (
  charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk,
  billed_cost, effective_cost, list_cost, contracted_cost, line_count
)
SELECT
  f.charge_date,
  f.billing_period_start,
  a.provider,
  f.sub_account_sk,
  f.service_sk,
  f.region_sk,
  SUM(f.billed_cost),
  SUM(f.effective_cost),
  SUM(f.list_cost),
  SUM(f.contracted_cost),
  SUM(f.line_count)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
WHERE f.sub_account_sk IS NOT NULL
GROUP BY f.charge_date, f.billing_period_start, a.provider, f.sub_account_sk, f.service_sk, f.region_sk;

TRUNCATE TABLE dbo.agg_cost_monthly;

INSERT INTO dbo.agg_cost_monthly (
  month_start, provider, sub_account_sk, service_category, charge_category_sk,
  billed_cost, effective_cost, list_cost, contracted_cost, line_count
)
SELECT
  f.billing_period_start,
  a.provider,
  f.sub_account_sk,
  svc.service_category,
  f.charge_category_sk,
  SUM(f.billed_cost),
  SUM(f.effective_cost),
  SUM(f.list_cost),
  SUM(f.contracted_cost),
  SUM(f.line_count)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
INNER JOIN dbo.dim_service svc ON f.service_sk = svc.service_sk
WHERE f.sub_account_sk IS NOT NULL
GROUP BY
  f.billing_period_start,
  a.provider, f.sub_account_sk, svc.service_category, f.charge_category_sk;

TRUNCATE TABLE dbo.agg_cost_by_tag;

INSERT INTO dbo.agg_cost_by_tag (month_start, provider, tag_key, tag_value, effective_cost, billed_cost, line_count)
SELECT
  f.billing_period_start,
  a.provider,
  t.tag_key,
  t.tag_value,
  SUM(f.effective_cost),
  SUM(f.billed_cost),
  SUM(f.line_count)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
INNER JOIN dbo.bridge_cost_tag b ON b.cost_daily_id = f.cost_daily_id
INNER JOIN dbo.dim_tag t ON t.tag_sk = b.tag_sk
WHERE f.sub_account_sk IS NOT NULL
GROUP BY f.billing_period_start, a.provider, t.tag_key, t.tag_value;

TRUNCATE TABLE dbo.agg_commitment_utilization;

INSERT INTO dbo.agg_commitment_utilization (
  month_start, provider, commitment_sk, commitment_status,
  effective_cost, commitment_quantity, line_count
)
SELECT
  f.billing_period_start,
  a.provider,
  f.commitment_sk,
  COALESCE(f.commitment_discount_status, 'Unknown'),
  SUM(f.effective_cost),
  SUM(f.commitment_discount_quantity),
  SUM(f.line_count)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
WHERE f.commitment_sk IS NOT NULL
  AND f.sub_account_sk IS NOT NULL
GROUP BY
  f.billing_period_start,
  a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status, 'Unknown');

TRUNCATE TABLE dbo.agg_commitment_utilization_daily;

INSERT INTO dbo.agg_commitment_utilization_daily (
  charge_date, provider, commitment_sk, commitment_status,
  effective_cost, commitment_quantity, line_count
)
SELECT
  f.charge_date,
  a.provider,
  f.commitment_sk,
  COALESCE(f.commitment_discount_status, 'Unknown'),
  SUM(f.effective_cost),
  SUM(f.commitment_discount_quantity),
  SUM(f.line_count)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
WHERE f.commitment_sk IS NOT NULL
  AND f.sub_account_sk IS NOT NULL
GROUP BY f.charge_date, a.provider, f.commitment_sk, COALESCE(f.commitment_discount_status, 'Unknown');

TRUNCATE TABLE dbo.agg_savings_summary;

INSERT INTO dbo.agg_savings_summary (
  month_start, provider, service_sk, total_effective_cost, total_projected_savings, recommendation_count
)
SELECT
  f.billing_period_start,
  a.provider,
  f.service_sk,
  SUM(f.effective_cost),
  COALESCE(r.total_savings, 0),
  COALESCE(r.rec_count, 0)
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account a ON sa.billing_account_sk = a.account_sk
LEFT JOIN (
  SELECT
    rec.snapshot_month,
    res.service_sk,
    acc.provider,
    SUM(rec.projected_monthly_savings) AS total_savings,
    COUNT(*) AS rec_count
  FROM dbo.fact_recommendation_snapshot_v2 rec
  INNER JOIN dbo.dim_resource res ON rec.resource_sk = res.resource_sk
  INNER JOIN dbo.dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
  INNER JOIN dbo.dim_account acc ON sa.billing_account_sk = acc.account_sk
  GROUP BY rec.snapshot_month, res.service_sk, acc.provider
) r ON r.snapshot_month = f.billing_period_start
   AND r.service_sk = f.service_sk AND r.provider = a.provider
WHERE f.sub_account_sk IS NOT NULL
GROUP BY
  f.billing_period_start,
  a.provider, f.service_sk, r.total_savings, r.rec_count;

UPDATE dbo.dim_ingestion_batch SET status = 'PROCESSED' WHERE ingestion_batch_id = @IngestionBatchId;

PRINT 'ETL step 2 complete for batch ' + CAST(@IngestionBatchId AS VARCHAR(20));
