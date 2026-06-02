-- =====================================================================
-- ETL Step 1: Load FOCUS export into staging
-- Target: Azure SQL Database
-- =====================================================================
-- Prerequisites:
--   1. Run focus_dw.sql
--   2. Upload CSV to Azure Blob Storage OR place on SQL Server accessible path
--   3. Set variables below
-- =====================================================================

SET NOCOUNT ON;

DECLARE @SourceFile       VARCHAR(512) = N'focus_sample_100000.csv';
DECLARE @FocusVersion     VARCHAR(16)  = N'1.0';
DECLARE @SourceProvider   VARCHAR(10)  = NULL;  -- NULL = infer per row from Provider column
DECLARE @BlobUrl          NVARCHAR(1000) = NULL; -- e.g. 'https://account.blob.core.windows.net/focus/focus_sample_100000.csv'
DECLARE @SasToken         NVARCHAR(500)  = NULL; -- '?sv=...' if using blob
DECLARE @IngestionBatchId BIGINT;

-- Register batch
INSERT INTO dbo.dim_ingestion_batch (source_provider, focus_version, source_file, status)
VALUES (COALESCE(@SourceProvider, 'MIXED'), @FocusVersion, @SourceFile, 'LOADING');

SET @IngestionBatchId = SCOPE_IDENTITY();

-- ---------------------------------------------------------------------
-- Option A: BULK INSERT from local path (on-prem / VM with file access)
-- Uncomment and set DATA_SOURCE path for your environment.
-- ---------------------------------------------------------------------
/*
BULK INSERT dbo.stg_focus_cost_line (
  ingestion_batch_id, focus_version, source_file, x_source_row_id,
  AvailabilityZone, BilledCost, BillingAccountId, BillingAccountName,
  BillingCurrency, BillingPeriodEnd, BillingPeriodStart,
  ChargeCategory, ChargeClass, ChargeDescription, ChargeFrequency,
  ChargePeriodEnd, ChargePeriodStart,
  CommitmentDiscountCategory, CommitmentDiscountId, CommitmentDiscountName,
  CommitmentDiscountStatus, CommitmentDiscountType,
  ConsumedQuantity, ConsumedUnit, ContractedCost, ContractedUnitPrice, EffectiveCost,
  InvoiceIssuer, ListCost, ListUnitPrice, PricingCategory, PricingQuantity, PricingUnit,
  Provider, Publisher, RegionId, RegionName, ResourceId, ResourceName, ResourceType,
  ServiceCategory, ServiceName, SkuId, SkuPriceId, SubAccountId, SubAccountName,
  raw_tags_json
)
FROM 'C:\data\focus_sample_100000.csv'
WITH (
  FIRSTROW = 2,
  FIELDTERMINATOR = ',',
  ROWTERMINATOR = '0x0a',
  TABLOCK,
  CODEPAGE = '65001'
);
*/

-- ---------------------------------------------------------------------
-- Option B: OPENROWSET from Azure Blob (recommended for Azure SQL)
-- Requires DATABASE SCOPED CREDENTIAL + EXTERNAL DATA SOURCE setup.
-- See: https://learn.microsoft.com/en-us/sql/t-sql/functions/openrowset-bulk-transact-sql
-- ---------------------------------------------------------------------
/*
INSERT INTO dbo.stg_focus_cost_line (
  ingestion_batch_id, focus_version, source_file,
  AvailabilityZone, BilledCost, BillingAccountId, BillingAccountName,
  BillingCurrency, BillingPeriodEnd, BillingPeriodStart,
  ChargeCategory, ChargeDescription, ChargeFrequency,
  ChargePeriodEnd, ChargePeriodStart,
  CommitmentDiscountCategory, CommitmentDiscountId, CommitmentDiscountName,
  CommitmentDiscountStatus, CommitmentDiscountType,
  ConsumedQuantity, ConsumedUnit, ContractedCost, ContractedUnitPrice, EffectiveCost,
  InvoiceIssuer, ListCost, ListUnitPrice, PricingCategory, PricingQuantity, PricingUnit,
  Provider, Publisher, RegionId, RegionName, ResourceId, ResourceName, ResourceType,
  ServiceCategory, ServiceName, SkuId, SkuPriceId, SubAccountId, SubAccountName,
  raw_tags_json
)
SELECT
  @IngestionBatchId,
  @FocusVersion,
  @SourceFile,
  AvailabilityZone, TRY_CAST(BilledCost AS DECIMAL(28,10)), BillingAccountId, BillingAccountName,
  BillingCurrency, TRY_CAST(BillingPeriodEnd AS DATETIME2(0)), TRY_CAST(BillingPeriodStart AS DATETIME2(0)),
  ChargeCategory, ChargeDescription, ChargeFrequency,
  TRY_CAST(ChargePeriodEnd AS DATETIME2(0)), TRY_CAST(ChargePeriodStart AS DATETIME2(0)),
  CommitmentDiscountCategory, CommitmentDiscountId, CommitmentDiscountName,
  CommitmentDiscountStatus, CommitmentDiscountType,
  TRY_CAST(ConsumedQuantity AS DECIMAL(28,10)), ConsumedUnit,
  TRY_CAST(ContractedCost AS DECIMAL(28,10)), TRY_CAST(ContractedUnitPrice AS DECIMAL(28,10)),
  TRY_CAST(EffectiveCost AS DECIMAL(28,10)),
  InvoiceIssuerName, TRY_CAST(ListCost AS DECIMAL(28,10)), TRY_CAST(ListUnitPrice AS DECIMAL(28,10)),
  PricingCategory, TRY_CAST(PricingQuantity AS DECIMAL(28,10)), PricingUnit,
  ProviderName, PublisherName, RegionId, RegionName, ResourceId, ResourceName, ResourceType,
  ServiceCategory, ServiceName, SkuId, SkuPriceId, SubAccountId, SubAccountName,
  Tags
FROM OPENROWSET(
  BULK @BlobUrl,
  SINGLE_CLOB,
  FORMAT = 'CSV',
  FIELDTERMINATOR = ',',
  ROWTERMINATOR = '0x0a',
  FIRSTROW = 2
) AS raw
CROSS APPLY OPENJSON(BulkColumn)
WITH (
  AvailabilityZone NVARCHAR(128) '$.AvailabilityZone',
  BilledCost NVARCHAR(50) '$.BilledCost'
  -- extend WITH clause for all columns when using JSON CSV parser
) j;
*/

-- ---------------------------------------------------------------------
-- Option C: Python/Go loader inserts directly (see etl/load_sample.py)
-- After external load, run step 2: 02_migrate_dims_and_daily.sql
-- ---------------------------------------------------------------------

-- Update batch metadata after load (adjust row_count when data is loaded)
UPDATE dbo.dim_ingestion_batch
SET
  row_count = (SELECT COUNT(*) FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @IngestionBatchId),
  billing_period_start = (SELECT MIN(CAST(BillingPeriodStart AS DATE)) FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @IngestionBatchId),
  billing_period_end   = (SELECT MAX(CAST(BillingPeriodEnd AS DATE)) FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @IngestionBatchId),
  status = CASE WHEN EXISTS (SELECT 1 FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @IngestionBatchId) THEN 'LOADED' ELSE 'EMPTY' END
WHERE ingestion_batch_id = @IngestionBatchId;

SELECT @IngestionBatchId AS ingestion_batch_id;

PRINT 'Step 1 complete. If staging is empty, use etl/load_sample.py or configure BULK INSERT / OPENROWSET above.';
