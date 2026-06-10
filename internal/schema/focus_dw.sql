-- =====================================================================
-- FOCUS Data Warehouse — Unified Schema (Azure SQL)
-- Merges schema.sql + improve_reco.sql + FOCUS 1.2 cost model
-- Idempotent: safe to re-run
-- =====================================================================

SET NOCOUNT ON;
GO

-- =====================================================================
-- SECTION 1: SHARED DIMENSIONS
-- =====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_date') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_date (
    date_sk          INT         NOT NULL PRIMARY KEY,  -- YYYYMMDD
    full_date        DATE        NOT NULL UNIQUE,
    year_num         SMALLINT    NOT NULL,
    quarter_num      TINYINT     NOT NULL,
    month_num        TINYINT     NOT NULL,
    month_name       VARCHAR(16) NOT NULL,
    month_start      DATE        NOT NULL,
    week_num         TINYINT     NOT NULL,
    day_of_month     TINYINT     NOT NULL,
    day_of_week      TINYINT     NOT NULL,
    day_name         VARCHAR(16) NOT NULL,
    is_weekend       BIT         NOT NULL,
    fiscal_year      SMALLINT    NULL,
    fiscal_quarter   TINYINT     NULL
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_account') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_account (
    account_sk           INT IDENTITY(1,1) PRIMARY KEY,
    provider             VARCHAR(10)  NOT NULL CHECK (provider IN ('AWS','AZURE','GCP')),
    account_id           VARCHAR(64)  NOT NULL,
    account_name         VARCHAR(256) NULL,
    billing_account_type VARCHAR(64)  NULL,
    is_active            BIT NOT NULL DEFAULT 1,
    UNIQUE (provider, account_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_sub_account') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_sub_account (
    sub_account_sk    INT IDENTITY(1,1) PRIMARY KEY,
    provider          VARCHAR(10)  NOT NULL CHECK (provider IN ('AWS','AZURE','GCP')),
    sub_account_id    VARCHAR(128) NOT NULL,
    sub_account_name  VARCHAR(256) NULL,
    sub_account_type  VARCHAR(64)  NULL,
    billing_account_sk INT         NULL FOREIGN KEY REFERENCES dbo.dim_account(account_sk),
    UNIQUE (provider, sub_account_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_service') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_service (
    service_sk           INT IDENTITY(1,1) PRIMARY KEY,
    provider             VARCHAR(10)  NOT NULL,
    service_code         VARCHAR(128) NOT NULL,
    service_name         VARCHAR(256) NOT NULL,
    service_category     VARCHAR(128) NULL,
    service_subcategory  VARCHAR(128) NULL,
    UNIQUE (provider, service_code)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_region') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_region (
    region_sk    INT IDENTITY(1,1) PRIMARY KEY,
    provider     VARCHAR(10)  NOT NULL,
    region_id    VARCHAR(128) NOT NULL,
    region_name  VARCHAR(256) NULL,
    UNIQUE (provider, region_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_sku') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_sku (
    sku_sk           INT IDENTITY(1,1) PRIMARY KEY,
    provider         VARCHAR(10)  NOT NULL,
    sku_id           VARCHAR(256) NOT NULL,
    sku_price_id     VARCHAR(512) NULL,
    sku_meter        VARCHAR(256) NULL,
    sku_price_details NVARCHAR(512) NULL,
    service_name     VARCHAR(256) NULL,
    UNIQUE (provider, sku_id, sku_price_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_charge_category') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_charge_category (
    charge_category_sk TINYINT IDENTITY(1,1) PRIMARY KEY,
    charge_category    VARCHAR(32) NOT NULL UNIQUE
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_charge_frequency') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_charge_frequency (
    charge_frequency_sk TINYINT IDENTITY(1,1) PRIMARY KEY,
    charge_frequency    VARCHAR(32) NOT NULL UNIQUE
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_pricing_category') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_pricing_category (
    pricing_category_sk TINYINT IDENTITY(1,1) PRIMARY KEY,
    pricing_category    VARCHAR(32) NOT NULL UNIQUE
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_commitment_discount') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_commitment_discount (
    commitment_sk              INT IDENTITY(1,1) PRIMARY KEY,
    provider                   VARCHAR(10)  NOT NULL,
    commitment_discount_id     VARCHAR(512) NOT NULL,
    commitment_discount_name   VARCHAR(256) NULL,
    commitment_discount_type   VARCHAR(128) NULL,
    commitment_discount_category VARCHAR(64) NULL,
    commitment_discount_unit   VARCHAR(64)  NULL,
    UNIQUE (provider, commitment_discount_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_capacity_reservation') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_capacity_reservation (
    capacity_reservation_sk INT IDENTITY(1,1) PRIMARY KEY,
    provider                VARCHAR(10)  NOT NULL,
    capacity_reservation_id VARCHAR(512) NOT NULL,
    capacity_reservation_status VARCHAR(32) NULL,
    UNIQUE (provider, capacity_reservation_id)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_resource') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_resource (
    resource_sk          INT IDENTITY(1,1) PRIMARY KEY,
    provider             VARCHAR(10)  NOT NULL,
    global_resource_id   VARCHAR(512) NOT NULL,
    resource_type        VARCHAR(128) NOT NULL,
    account_sk           INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_account(account_sk),
    sub_account_sk       INT NULL FOREIGN KEY REFERENCES dbo.dim_sub_account(sub_account_sk),
    service_sk           INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_service(service_sk),
    region               VARCHAR(64)  NULL,
    name                 VARCHAR(256) NULL,
    owner_email          VARCHAR(320) NULL,
    cost_center          VARCHAR(64)  NULL,
    environment          VARCHAR(32)  NULL,
    application          VARCHAR(128) NULL,
    business             VARCHAR(128) NULL,
    tags_json            NVARCHAR(MAX) NULL,
    hourly_cost          DECIMAL(18,6) NULL,
    valid_from           DATE NOT NULL,
    valid_to             DATE NULL,
    is_current           AS CASE WHEN valid_to IS NULL THEN 1 ELSE 0 END PERSISTED,
    is_excluded          BIT NOT NULL DEFAULT 0,
    UNIQUE (provider, global_resource_id, valid_from)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_dim_resource_current' AND object_id = OBJECT_ID(N'dbo.dim_resource'))
BEGIN
  CREATE INDEX IX_dim_resource_current
    ON dbo.dim_resource (provider, global_resource_id)
    WHERE valid_to IS NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_dim_resource_sub_account_current' AND object_id = OBJECT_ID(N'dbo.dim_resource'))
BEGIN
  CREATE INDEX IX_dim_resource_sub_account_current
    ON dbo.dim_resource (sub_account_sk)
    WHERE valid_to IS NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_tag') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_tag (
    tag_sk     INT IDENTITY(1,1) PRIMARY KEY,
    tag_key    VARCHAR(256) NOT NULL,
    tag_value  NVARCHAR(512) NOT NULL,
    UNIQUE (tag_key, tag_value)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_application') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_application (
    application_sk   INT IDENTITY(1,1) PRIMARY KEY,
    application_name VARCHAR(256) NOT NULL,
    alias_values     NVARCHAR(MAX) NULL,
    first_seen_date  DATE         NULL,
    created_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (application_name)
  );
END
GO

IF COL_LENGTH('dbo.dim_application', 'first_seen_date') IS NULL
BEGIN
  ALTER TABLE dbo.dim_application ADD first_seen_date DATE NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_dim_application_name' AND object_id = OBJECT_ID(N'dbo.dim_application'))
BEGIN
  CREATE INDEX IX_dim_application_name ON dbo.dim_application (application_name);
END
GO

MERGE dbo.dim_application AS t
USING (SELECT '(UNASSIGNED)' AS application_name, '(Unassigned)' AS alias_values) AS s
ON t.application_name = s.application_name
WHEN NOT MATCHED THEN INSERT (application_name, alias_values) VALUES (s.application_name, s.alias_values);
GO

-- Seed FOCUS lookup values
MERGE dbo.dim_charge_category AS t
USING (VALUES ('Usage'),('Purchase'),('Tax'),('Credit'),('Adjustment')) AS s(charge_category)
ON t.charge_category = s.charge_category
WHEN NOT MATCHED THEN INSERT (charge_category) VALUES (s.charge_category);
GO

MERGE dbo.dim_charge_frequency AS t
USING (VALUES ('Usage-Based'),('Recurring'),('One-Time')) AS s(charge_frequency)
ON t.charge_frequency = s.charge_frequency
WHEN NOT MATCHED THEN INSERT (charge_frequency) VALUES (s.charge_frequency);
GO

MERGE dbo.dim_pricing_category AS t
USING (VALUES ('Standard'),('Committed'),('Dynamic'),('Other')) AS s(pricing_category)
ON t.pricing_category = s.pricing_category
WHEN NOT MATCHED THEN INSERT (pricing_category) VALUES (s.pricing_category);
GO

-- Populate dim_date (2020-01-01 through 2035-12-31)
IF NOT EXISTS (SELECT 1 FROM dbo.dim_date)
BEGIN
  ;WITH n AS (
    SELECT TOP (5844) ROW_NUMBER() OVER (ORDER BY (SELECT NULL)) - 1 AS n
    FROM sys.all_objects a CROSS JOIN sys.all_objects b
  )
  INSERT INTO dbo.dim_date (
    date_sk, full_date, year_num, quarter_num, month_num, month_name,
    month_start, week_num, day_of_month, day_of_week, day_name, is_weekend
  )
  SELECT
    CONVERT(INT, FORMAT(d, 'yyyyMMdd')),
    d,
    YEAR(d),
    DATEPART(QUARTER, d),
    MONTH(d),
    DATENAME(MONTH, d),
    DATEFROMPARTS(YEAR(d), MONTH(d), 1),
    DATEPART(ISO_WEEK, d),
    DAY(d),
    DATEPART(WEEKDAY, d),
    DATENAME(WEEKDAY, d),
    CASE WHEN DATEPART(WEEKDAY, d) IN (1, 7) THEN 1 ELSE 0 END
  FROM (SELECT DATEADD(DAY, n, '2020-01-01') AS d FROM n) x;
END
GO

-- =====================================================================
-- SECTION 2: FOCUS COST — INGESTION & FACTS
-- =====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_ingestion_batch') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_ingestion_batch (
    ingestion_batch_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    source_provider    VARCHAR(10)  NOT NULL,
    focus_version      VARCHAR(16)  NOT NULL,
    source_file        VARCHAR(512) NULL,
    billing_period_start DATE       NULL,
    billing_period_end   DATE       NULL,
    row_count          BIGINT       NULL,
    loaded_utc         DATETIME2    NOT NULL DEFAULT SYSUTCDATETIME(),
    status             VARCHAR(32)  NOT NULL DEFAULT 'LOADED'
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.stg_focus_cost_line') AND type = N'U')
BEGIN
  CREATE TABLE dbo.stg_focus_cost_line (
    stg_line_id                    BIGINT IDENTITY(1,1) PRIMARY KEY,
    ingestion_batch_id             BIGINT       NOT NULL,
    source_provider                VARCHAR(10)  NULL,
    focus_version                  VARCHAR(16)  NULL,
    source_file                    VARCHAR(512) NULL,
    loaded_utc                     DATETIME2    NOT NULL DEFAULT SYSUTCDATETIME(),
    x_source_row_id                BIGINT       NULL,

    AvailabilityZone               VARCHAR(128) NULL,
    BilledCost                     DECIMAL(28,10) NULL,
    BillingAccountId               VARCHAR(128) NULL,
    BillingAccountName             VARCHAR(256) NULL,
    BillingAccountType             VARCHAR(64)  NULL,
    BillingCurrency                VARCHAR(16)  NULL,
    BillingPeriodEnd               DATETIME2(0) NULL,
    BillingPeriodStart             DATETIME2(0) NULL,
    CapacityReservationId          VARCHAR(512) NULL,
    CapacityReservationStatus      VARCHAR(32)  NULL,
    ChargeCategory                 VARCHAR(32)  NULL,
    ChargeClass                    VARCHAR(32)  NULL,
    ChargeDescription              NVARCHAR(1024) NULL,
    ChargeFrequency                VARCHAR(32)  NULL,
    ChargePeriodEnd                DATETIME2(0) NULL,
    ChargePeriodStart              DATETIME2(0) NULL,
    CommitmentDiscountCategory     VARCHAR(64)  NULL,
    CommitmentDiscountId           VARCHAR(512) NULL,
    CommitmentDiscountName         VARCHAR(256) NULL,
    CommitmentDiscountQuantity     DECIMAL(28,10) NULL,
    CommitmentDiscountStatus       VARCHAR(32)  NULL,
    CommitmentDiscountType           VARCHAR(128) NULL,
    CommitmentDiscountUnit         VARCHAR(64)  NULL,
    ConsumedQuantity               DECIMAL(28,10) NULL,
    ConsumedUnit                   VARCHAR(64)  NULL,
    ContractedCost                 DECIMAL(28,10) NULL,
    ContractedUnitPrice            DECIMAL(28,10) NULL,
    EffectiveCost                  DECIMAL(28,10) NULL,
    InvoiceId                      VARCHAR(128) NULL,
    InvoiceIssuer                  VARCHAR(256) NULL,
    ListCost                       DECIMAL(28,10) NULL,
    ListUnitPrice                  DECIMAL(28,10) NULL,
    PricingCategory                VARCHAR(32)  NULL,
    PricingCurrency                VARCHAR(16)  NULL,
    PricingCurrencyContractedUnitPrice DECIMAL(28,10) NULL,
    PricingCurrencyEffectiveCost   DECIMAL(28,10) NULL,
    PricingCurrencyListUnitPrice   DECIMAL(28,10) NULL,
    PricingQuantity                DECIMAL(28,10) NULL,
    PricingUnit                    VARCHAR(64)  NULL,
    Provider                       VARCHAR(64)  NULL,
    Publisher                      VARCHAR(256) NULL,
    RegionId                       VARCHAR(128) NULL,
    RegionName                     VARCHAR(256) NULL,
    ResourceId                     VARCHAR(512) NULL,
    ResourceName                   VARCHAR(256) NULL,
    ResourceType                   VARCHAR(128) NULL,
    ServiceCategory                VARCHAR(128) NULL,
    ServiceName                    VARCHAR(256) NULL,
    ServiceSubcategory             VARCHAR(128) NULL,
    SkuId                          VARCHAR(256) NULL,
    SkuMeter                       VARCHAR(256) NULL,
    SkuPriceDetails                NVARCHAR(512) NULL,
    SkuPriceId                     VARCHAR(512) NULL,
    SubAccountId                   VARCHAR(128) NULL,
    SubAccountName                 VARCHAR(256) NULL,
    SubAccountType                 VARCHAR(64)  NULL,
    raw_tags_json                  NVARCHAR(MAX) NULL
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_stg_focus_batch' AND object_id = OBJECT_ID(N'dbo.stg_focus_cost_line'))
BEGIN
  CREATE INDEX IX_stg_focus_batch ON dbo.stg_focus_cost_line (ingestion_batch_id);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_focus_cost_daily (
    cost_daily_id              BIGINT IDENTITY(1,1) NOT NULL,
    charge_date                DATE         NOT NULL,
    billing_account_sk         INT          NOT NULL FOREIGN KEY REFERENCES dbo.dim_account(account_sk),
    sub_account_sk             INT          NULL FOREIGN KEY REFERENCES dbo.dim_sub_account(sub_account_sk),
    resource_sk                INT          NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    service_sk                 INT          NOT NULL FOREIGN KEY REFERENCES dbo.dim_service(service_sk),
    sku_sk                     INT          NULL FOREIGN KEY REFERENCES dbo.dim_sku(sku_sk),
    region_sk                  INT          NULL FOREIGN KEY REFERENCES dbo.dim_region(region_sk),
    charge_category_sk         TINYINT      NOT NULL FOREIGN KEY REFERENCES dbo.dim_charge_category(charge_category_sk),
    charge_frequency_sk        TINYINT      NULL FOREIGN KEY REFERENCES dbo.dim_charge_frequency(charge_frequency_sk),
    pricing_category_sk        TINYINT      NULL FOREIGN KEY REFERENCES dbo.dim_pricing_category(pricing_category_sk),
    commitment_sk              INT          NULL FOREIGN KEY REFERENCES dbo.dim_commitment_discount(commitment_sk),
    commitment_discount_status VARCHAR(32)  NULL,
    capacity_reservation_sk    INT          NULL FOREIGN KEY REFERENCES dbo.dim_capacity_reservation(capacity_reservation_sk),
    capacity_reservation_status VARCHAR(32) NULL,
    charge_description_hash    CHAR(64)     NOT NULL,
    billing_period_start       DATE         NOT NULL,
    billing_period_end         DATE         NOT NULL,
    billed_cost                DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost             DECIMAL(28,10) NOT NULL DEFAULT 0,
    list_cost                  DECIMAL(28,10) NOT NULL DEFAULT 0,
    contracted_cost            DECIMAL(28,10) NOT NULL DEFAULT 0,
    pricing_quantity           DECIMAL(28,10) NOT NULL DEFAULT 0,
    consumed_quantity          DECIMAL(28,10) NOT NULL DEFAULT 0,
    commitment_discount_quantity DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count                 INT          NOT NULL DEFAULT 0,
    first_charge_period_start  DATETIME2(0) NULL,
    last_charge_period_end     DATETIME2(0) NULL,
    ingestion_batch_id         BIGINT       NOT NULL FOREIGN KEY REFERENCES dbo.dim_ingestion_batch(ingestion_batch_id),
    focus_version              VARCHAR(16)  NOT NULL,
    created_utc                DATETIME2    NOT NULL DEFAULT SYSUTCDATETIME(),

    CONSTRAINT PK_fact_focus_cost_daily PRIMARY KEY NONCLUSTERED (cost_daily_id),
    CONSTRAINT UQ_fact_focus_cost_daily_grain UNIQUE CLUSTERED (
      charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk,
      sku_sk, region_sk, charge_category_sk, pricing_category_sk,
      commitment_sk, commitment_discount_status, capacity_reservation_sk,
      capacity_reservation_status, charge_description_hash,
      billing_period_start, ingestion_batch_id
    )
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.bridge_cost_tag') AND type = N'U')
BEGIN
  CREATE TABLE dbo.bridge_cost_tag (
    cost_daily_id BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.fact_focus_cost_daily(cost_daily_id),
    tag_sk        INT    NOT NULL FOREIGN KEY REFERENCES dbo.dim_tag(tag_sk),
    PRIMARY KEY (cost_daily_id, tag_sk)
  );
END
GO

-- =====================================================================
-- SECTION 3: RECOMMENDATION V2
-- =====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_recommendation_snapshot_v2 (
    rec_snapshot_id            BIGINT IDENTITY(1,1) PRIMARY KEY,
    snapshot_month             DATE NOT NULL,
    resource_sk                INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    recommendation_type        VARCHAR(64) NOT NULL,
    current_utilization_status VARCHAR(64) NULL,
    terminate_recommendation   BIT NOT NULL DEFAULT 0,
    current_instance_type      VARCHAR(128) NULL,
    current_vcpu               INT NULL,
    current_memory_gb          DECIMAL(10,2) NULL,
    current_storage_gb         DECIMAL(10,2) NULL,
    current_iops               INT NULL,
    current_throughput_mbps    INT NULL,
    current_dtu                INT NULL,
    current_service_tier       VARCHAR(64) NULL,
    recommended_instance_type  VARCHAR(128) NULL,
    recommended_vcpu           INT NULL,
    recommended_memory_gb      DECIMAL(10,2) NULL,
    recommended_storage_gb     DECIMAL(10,2) NULL,
    recommended_iops           INT NULL,
    recommended_throughput_mbps INT NULL,
    recommended_dtu            INT NULL,
    recommended_service_tier   VARCHAR(64) NULL,
    current_monthly_price      DECIMAL(18,2) NULL,
    projected_monthly_price    DECIMAL(18,2) NULL,
    projected_monthly_savings  DECIMAL(18,2) NULL,
    current_cost_mtd           DECIMAL(18,2) NULL,
    projected_cost_mtd         DECIMAL(18,2) NULL,
    projected_savings_mtd      DECIMAL(18,2) NULL,
    expected_perf_impact       VARCHAR(64) NULL,
    number_of_options          INT NULL DEFAULT 1,
    engine_run_id              VARCHAR(64) NULL,
    provider_resource_id       VARCHAR(512) NULL,
    region                     VARCHAR(64) NULL,
    raw_payload_json           NVARCHAR(MAX) NULL,
    created_utc                DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_utc                DATETIME2 NULL,
    UNIQUE (snapshot_month, resource_sk, recommendation_type)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_recommendation_metrics') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_recommendation_metrics (
    metric_id       BIGINT IDENTITY(1,1) PRIMARY KEY,
    rec_snapshot_id BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.fact_recommendation_snapshot_v2(rec_snapshot_id) ON DELETE CASCADE,
    metric_name     VARCHAR(128) NOT NULL,
    aggregation     VARCHAR(16) NOT NULL,
    metric_value    DECIMAL(18,6) NULL,
    metric_unit     VARCHAR(32) NULL,
    metric_status   VARCHAR(32) NULL,
    metric_type     VARCHAR(32) NULL,
    created_utc     DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (rec_snapshot_id, metric_name, aggregation)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_recommendation_options') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_recommendation_options (
    option_id                     BIGINT IDENTITY(1,1) PRIMARY KEY,
    rec_snapshot_id               BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.fact_recommendation_snapshot_v2(rec_snapshot_id) ON DELETE CASCADE,
    option_rank                   INT NOT NULL DEFAULT 1,
    is_best_fit                   BIT NOT NULL DEFAULT 0,
    option_instance_type          VARCHAR(128) NULL,
    option_vcpu                   INT NULL,
    option_memory_gb              DECIMAL(10,2) NULL,
    option_storage_gb             DECIMAL(10,2) NULL,
    option_storage_type           VARCHAR(64) NULL,
    option_iops                   INT NULL,
    option_throughput_mbps        INT NULL,
    option_dtu                    INT NULL,
    option_service_tier           VARCHAR(64) NULL,
    option_network_bandwidth_gbps DECIMAL(10,4) NULL,
    option_monthly_price          DECIMAL(18,2) NULL,
    option_monthly_savings        DECIMAL(18,2) NULL,
    option_hourly_rate            DECIMAL(18,6) NULL,
    created_utc                   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (rec_snapshot_id, option_rank)
  );
END
GO

-- Recommendation indexes
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_v2_snapshot_month' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2'))
BEGIN
  CREATE INDEX IX_fact_rec_v2_snapshot_month ON dbo.fact_recommendation_snapshot_v2 (snapshot_month);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_v2_resource' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2'))
BEGIN
  CREATE INDEX IX_fact_rec_v2_resource ON dbo.fact_recommendation_snapshot_v2 (resource_sk, recommendation_type);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_v2_utilization' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2'))
BEGIN
  CREATE INDEX IX_fact_rec_v2_utilization ON dbo.fact_recommendation_snapshot_v2 (current_utilization_status, terminate_recommendation);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_v2_savings' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2'))
BEGIN
  CREATE INDEX IX_fact_rec_v2_savings ON dbo.fact_recommendation_snapshot_v2 (projected_monthly_savings DESC) WHERE projected_monthly_savings > 0;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_v2_month_resource_savings' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_snapshot_v2'))
BEGIN
  CREATE INDEX IX_fact_rec_v2_month_resource_savings
    ON dbo.fact_recommendation_snapshot_v2 (snapshot_month, resource_sk)
    INCLUDE (projected_monthly_savings);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_metrics_snapshot' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_metrics'))
BEGIN
  CREATE INDEX IX_fact_rec_metrics_snapshot ON dbo.fact_recommendation_metrics (rec_snapshot_id);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_metrics_name' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_metrics'))
BEGIN
  CREATE INDEX IX_fact_rec_metrics_name ON dbo.fact_recommendation_metrics (metric_name, metric_status);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_options_snapshot' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_options'))
BEGIN
  CREATE INDEX IX_fact_rec_options_snapshot ON dbo.fact_recommendation_options (rec_snapshot_id);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_rec_options_bestfit' AND object_id = OBJECT_ID(N'dbo.fact_recommendation_options'))
BEGIN
  CREATE INDEX IX_fact_rec_options_bestfit ON dbo.fact_recommendation_options (is_best_fit) WHERE is_best_fit = 1;
END
GO

-- =====================================================================
-- SECTION 4: OPERATIONAL TABLES
-- =====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_resource_daily') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_resource_daily (
    resource_daily_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    as_of_date        DATE NOT NULL,
    resource_sk       INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    power_state       VARCHAR(16) NULL,
    instance_size     VARCHAR(128) NULL,
    storage_type      VARCHAR(64) NULL,
    provisioned_gb    INT NULL,
    avg_cpu_pct_7d    DECIMAL(5,2) NULL,
    avg_mem_pct_7d    DECIMAL(5,2) NULL,
    avg_net_in_out_7d DECIMAL(18,4) NULL,
    tag_hash          CHAR(64) NULL,
    raw_state_json    NVARCHAR(MAX) NULL,
    created_utc       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (as_of_date, resource_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_resource_daily_res_date' AND object_id = OBJECT_ID(N'dbo.fact_resource_daily'))
BEGIN
  CREATE INDEX IX_fact_resource_daily_res_date ON dbo.fact_resource_daily (resource_sk, as_of_date);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.fact_alert_snapshot') AND type = N'U')
BEGIN
  CREATE TABLE dbo.fact_alert_snapshot (
    alert_id                   BIGINT IDENTITY(1,1) PRIMARY KEY,
    alert_date                 DATETIME2 NOT NULL,
    resource_sk                INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    alert_type                 VARCHAR(64) NOT NULL,
    severity                   VARCHAR(16) NULL,
    rule_name                  VARCHAR(256) NULL,
    threshold                  DECIMAL(10,2) NULL,
    raw_payload_json           NVARCHAR(MAX) NULL,
    projected_cost_for_month   DECIMAL(18,2) NULL,
    created_utc                DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (alert_date, resource_sk, alert_type)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_alert_type_date' AND object_id = OBJECT_ID(N'dbo.fact_alert_snapshot'))
BEGIN
  CREATE INDEX IX_alert_type_date ON dbo.fact_alert_snapshot (alert_type, alert_date);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.dim_team_group') AND type = N'U')
BEGIN
  CREATE TABLE dbo.dim_team_group (
    group_sk      BIGINT IDENTITY(1,1) PRIMARY KEY,
    group_name    VARCHAR(256) NOT NULL,
    group_email   VARCHAR(320) NULL,
    group_sn_name VARCHAR(256) NULL,
    group_sn_id   VARCHAR(128) NULL,
    created_utc   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_utc   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME()
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_dim_team_group_email' AND object_id = OBJECT_ID(N'dbo.dim_team_group'))
BEGIN
  CREATE UNIQUE INDEX UX_dim_team_group_email ON dbo.dim_team_group (group_email) WHERE group_email IS NOT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_dim_team_group_sn_id' AND object_id = OBJECT_ID(N'dbo.dim_team_group'))
BEGIN
  CREATE UNIQUE INDEX UX_dim_team_group_sn_id ON dbo.dim_team_group (group_sn_id) WHERE group_sn_id IS NOT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.ritm_header') AND type = N'U')
BEGIN
  CREATE TABLE dbo.ritm_header (
    ritm_sk           BIGINT IDENTITY(1,1) PRIMARY KEY,
    ritm_number       VARCHAR(32) NOT NULL,
    opened_utc        DATETIME2 NOT NULL,
    closed_utc        DATETIME2 NULL,
    state             VARCHAR(32) NOT NULL,
    group_sk          BIGINT NULL FOREIGN KEY REFERENCES dbo.dim_team_group(group_sk),
    assignee_email    VARCHAR(320) NULL,
    requester_email   VARCHAR(320) NULL,
    short_description NVARCHAR(400) NULL,
    catalog_item      VARCHAR(128) NULL,
    raw_payload_json  NVARCHAR(MAX) NULL,
    UNIQUE (ritm_number)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.ritm_by_resource_header') AND type = N'U')
BEGIN
  CREATE TABLE dbo.ritm_by_resource_header (
    ritm_sk           BIGINT IDENTITY(1,1) PRIMARY KEY,
    ritm_number       VARCHAR(32) NOT NULL,
    opened_utc        DATETIME2 NOT NULL,
    closed_utc        DATETIME2 NULL,
    state             VARCHAR(32) NOT NULL,
    resource_sk       INT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    assignee_email    VARCHAR(320) NULL,
    requester_email   VARCHAR(320) NULL,
    short_description NVARCHAR(400) NULL,
    catalog_item      VARCHAR(128) NULL,
    raw_payload_json  NVARCHAR(MAX) NULL,
    UNIQUE (ritm_number)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.bridge_recommendation_ritm') AND type = N'U')
BEGIN
  CREATE TABLE dbo.bridge_recommendation_ritm (
    rec_snapshot_id BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.fact_recommendation_snapshot_v2(rec_snapshot_id),
    ritm_sk         BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.ritm_header(ritm_sk),
    linked_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    PRIMARY KEY (rec_snapshot_id, ritm_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.ritm_comment') AND type = N'U')
BEGIN
  CREATE TABLE dbo.ritm_comment (
    ritm_comment_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    ritm_sk         BIGINT NOT NULL FOREIGN KEY REFERENCES dbo.ritm_header(ritm_sk),
    commented_utc   DATETIME2 NOT NULL,
    author_email    VARCHAR(320) NULL,
    comment_text    NVARCHAR(MAX) NOT NULL
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.exceptions') AND type = N'U')
BEGIN
  CREATE TABLE dbo.exceptions (
    exception_id  BIGINT IDENTITY(1,1) PRIMARY KEY,
    resource_sk   INT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    exception_utc DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    commented     VARCHAR(320) NOT NULL,
    reason        VARCHAR(320) NOT NULL,
    end_exception DATETIME2 NULL
  );
END
GO

-- =====================================================================
-- SECTION 5: POWER BI AGGREGATE TABLES
-- =====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_cost_daily') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_cost_daily (
    agg_cost_daily_id  BIGINT IDENTITY(1,1) PRIMARY KEY,
    charge_date        DATE NOT NULL,
    billing_period_start DATE NOT NULL,
    provider           VARCHAR(10) NOT NULL,
    sub_account_sk     INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_sub_account(sub_account_sk),
    service_sk         INT NOT NULL,
    region_sk          INT NULL,
    billed_cost        DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost     DECIMAL(28,10) NOT NULL DEFAULT 0,
    list_cost          DECIMAL(28,10) NOT NULL DEFAULT 0,
    contracted_cost    DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count         INT NOT NULL DEFAULT 0,
    refreshed_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_daily_charge' AND object_id = OBJECT_ID(N'dbo.agg_cost_daily'))
BEGIN
  CREATE INDEX IX_agg_cost_daily_charge ON dbo.agg_cost_daily (charge_date, billing_period_start, provider, sub_account_sk);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_cost_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_cost_monthly (
    agg_cost_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start         DATE NOT NULL,
    provider            VARCHAR(10) NOT NULL,
    sub_account_sk      INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_sub_account(sub_account_sk),
    service_category    VARCHAR(128) NULL,
    charge_category_sk  TINYINT NOT NULL,
    billed_cost         DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost      DECIMAL(28,10) NOT NULL DEFAULT 0,
    list_cost           DECIMAL(28,10) NOT NULL DEFAULT 0,
    contracted_cost     DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count          INT NOT NULL DEFAULT 0,
    refreshed_utc       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, sub_account_sk, service_category, charge_category_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_monthly_month' AND object_id = OBJECT_ID(N'dbo.agg_cost_monthly'))
BEGIN
  CREATE INDEX IX_agg_cost_monthly_month ON dbo.agg_cost_monthly (month_start, provider, sub_account_sk);
END
GO

-- Migrate legacy agg tables: billing_account_sk → sub_account_sk (rebuild aggregates after applying)
IF COL_LENGTH('dbo.agg_cost_daily', 'billing_account_sk') IS NOT NULL
   AND COL_LENGTH('dbo.agg_cost_daily', 'sub_account_sk') IS NULL
BEGIN
  IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_daily_charge' AND object_id = OBJECT_ID(N'dbo.agg_cost_daily'))
    DROP INDEX IX_agg_cost_daily_charge ON dbo.agg_cost_daily;
  DECLARE @uq_daily SYSNAME;
  DECLARE @sql NVARCHAR(MAX);
  SELECT @uq_daily = kc.name
  FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_cost_daily') AND kc.type = 'UQ';
  IF @uq_daily IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_cost_daily DROP CONSTRAINT ' + QUOTENAME(@uq_daily);
    EXEC (@sql);
  END
  EXEC sp_rename 'dbo.agg_cost_daily.billing_account_sk', 'sub_account_sk', 'COLUMN';
  CREATE INDEX IX_agg_cost_daily_charge ON dbo.agg_cost_daily (charge_date, provider, sub_account_sk);
  ALTER TABLE dbo.agg_cost_daily ADD CONSTRAINT UQ_agg_cost_daily_grain
    UNIQUE (charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk);
END
GO

IF COL_LENGTH('dbo.agg_cost_daily', 'billing_period_start') IS NULL
BEGIN
  IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_daily_charge' AND object_id = OBJECT_ID(N'dbo.agg_cost_daily'))
    DROP INDEX IX_agg_cost_daily_charge ON dbo.agg_cost_daily;
  DECLARE @uq_daily_billing SYSNAME;
  DECLARE @sql NVARCHAR(MAX);
  SELECT @uq_daily_billing = kc.name
  FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_cost_daily') AND kc.type = 'UQ';
  IF @uq_daily_billing IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_cost_daily DROP CONSTRAINT ' + QUOTENAME(@uq_daily_billing);
    EXEC (@sql);
  END
  ALTER TABLE dbo.agg_cost_daily ADD billing_period_start DATE NOT NULL
    CONSTRAINT DF_agg_cost_daily_billing_period DEFAULT '1900-01-01';
  ALTER TABLE dbo.agg_cost_daily DROP CONSTRAINT DF_agg_cost_daily_billing_period;
  CREATE INDEX IX_agg_cost_daily_charge ON dbo.agg_cost_daily (charge_date, billing_period_start, provider, sub_account_sk);
  ALTER TABLE dbo.agg_cost_daily ADD CONSTRAINT UQ_agg_cost_daily_grain
    UNIQUE (charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk);
END
GO

IF COL_LENGTH('dbo.agg_cost_monthly', 'billing_account_sk') IS NOT NULL
   AND COL_LENGTH('dbo.agg_cost_monthly', 'sub_account_sk') IS NULL
BEGIN
  IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_monthly_month' AND object_id = OBJECT_ID(N'dbo.agg_cost_monthly'))
    DROP INDEX IX_agg_cost_monthly_month ON dbo.agg_cost_monthly;
  DECLARE @uq_monthly SYSNAME;
  DECLARE @sql NVARCHAR(MAX);
  SELECT @uq_monthly = kc.name
  FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_cost_monthly') AND kc.type = 'UQ';
  IF @uq_monthly IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_cost_monthly DROP CONSTRAINT ' + QUOTENAME(@uq_monthly);
    EXEC (@sql);
  END
  EXEC sp_rename 'dbo.agg_cost_monthly.billing_account_sk', 'sub_account_sk', 'COLUMN';
  CREATE INDEX IX_agg_cost_monthly_month ON dbo.agg_cost_monthly (month_start, provider, sub_account_sk);
  ALTER TABLE dbo.agg_cost_monthly ADD CONSTRAINT UQ_agg_cost_monthly_grain
    UNIQUE (month_start, provider, sub_account_sk, service_category, charge_category_sk);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_cost_by_tag') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_cost_by_tag (
    agg_cost_by_tag_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start        DATE NOT NULL,
    provider           VARCHAR(10) NOT NULL,
    tag_key            VARCHAR(256) NOT NULL,
    tag_value          NVARCHAR(512) NOT NULL,
    effective_cost     DECIMAL(28,10) NOT NULL DEFAULT 0,
    billed_cost        DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count         INT NOT NULL DEFAULT 0,
    refreshed_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, tag_key, tag_value)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_commitment_utilization') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_commitment_utilization (
    agg_commitment_util_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start            DATE NOT NULL,
    provider               VARCHAR(10) NOT NULL,
    commitment_sk          INT NOT NULL,
    commitment_status      VARCHAR(32) NOT NULL,
    effective_cost         DECIMAL(28,10) NOT NULL DEFAULT 0,
    commitment_quantity    DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count             INT NOT NULL DEFAULT 0,
    refreshed_utc          DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, commitment_sk, commitment_status)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_commitment_utilization_daily') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_commitment_utilization_daily (
    agg_commitment_daily_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    charge_date             DATE NOT NULL,
    provider                VARCHAR(10) NOT NULL,
    commitment_sk           INT NOT NULL,
    commitment_status       VARCHAR(32) NOT NULL,
    effective_cost          DECIMAL(28,10) NOT NULL DEFAULT 0,
    commitment_quantity     DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count              INT NOT NULL DEFAULT 0,
    refreshed_utc           DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (charge_date, provider, commitment_sk, commitment_status)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_savings_summary') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_savings_summary (
    agg_savings_summary_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start            DATE NOT NULL,
    provider               VARCHAR(10) NOT NULL,
    service_sk             INT NOT NULL,
    total_effective_cost   DECIMAL(28,10) NOT NULL DEFAULT 0,
    total_projected_savings DECIMAL(18,2) NOT NULL DEFAULT 0,
    recommendation_count   INT NOT NULL DEFAULT 0,
    refreshed_utc          DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, service_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_app_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_app_monthly (
    agg_app_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start        DATE NOT NULL,
    provider           VARCHAR(10) NOT NULL,
    application_sk     INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_application(application_sk),
    environment        NVARCHAR(128) NOT NULL DEFAULT '(Unknown)',
    billed_cost        DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost     DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count         INT NOT NULL DEFAULT 0,
    refreshed_utc      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, application_sk, environment)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_app_service_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_app_service_monthly (
    agg_app_service_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start                DATE NOT NULL,
    provider                   VARCHAR(10) NOT NULL,
    application_sk             INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_application(application_sk),
    environment                NVARCHAR(128) NOT NULL DEFAULT '(Unknown)',
    service_sk                 INT NOT NULL,
    billed_cost                DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost             DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count                 INT NOT NULL DEFAULT 0,
    refreshed_utc              DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, application_sk, environment, service_sk)
  );
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_app_service_resource_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_app_service_resource_monthly (
    agg_app_service_resource_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start                         DATE NOT NULL,
    provider                            VARCHAR(10) NOT NULL,
    application_sk                      INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_application(application_sk),
    environment                         NVARCHAR(128) NOT NULL DEFAULT '(Unknown)',
    service_sk                          INT NOT NULL,
    resource_sk                         INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_resource(resource_sk),
    billed_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    effective_cost                      DECIMAL(28,10) NOT NULL DEFAULT 0,
    line_count                          INT NOT NULL DEFAULT 0,
    refreshed_utc                       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, application_sk, environment, service_sk, resource_sk)
  );
END
GO

-- Migrate legacy app aggs: application (name) → application_sk (rebuild aggregates after applying)
IF COL_LENGTH('dbo.agg_app_monthly', 'application') IS NOT NULL
   AND COL_LENGTH('dbo.agg_app_monthly', 'application_sk') IS NULL
BEGIN
  TRUNCATE TABLE dbo.agg_app_monthly;
  TRUNCATE TABLE dbo.agg_app_service_monthly;
  TRUNCATE TABLE dbo.agg_app_service_resource_monthly;
  TRUNCATE TABLE dbo.agg_cost_distribution_monthly;

  DECLARE @uq_app SYSNAME;
  DECLARE @sql NVARCHAR(MAX);
  SELECT @uq_app = kc.name FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_app_monthly') AND kc.type = 'UQ';
  IF @uq_app IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_app_monthly DROP CONSTRAINT ' + QUOTENAME(@uq_app);
    EXEC (@sql);
  END
  ALTER TABLE dbo.agg_app_monthly DROP COLUMN application;
  ALTER TABLE dbo.agg_app_monthly ADD application_sk INT NOT NULL
    CONSTRAINT DF_agg_app_monthly_app_sk DEFAULT 1;
  ALTER TABLE dbo.agg_app_monthly DROP CONSTRAINT DF_agg_app_monthly_app_sk;
  ALTER TABLE dbo.agg_app_monthly ADD CONSTRAINT FK_agg_app_monthly_app
    FOREIGN KEY (application_sk) REFERENCES dbo.dim_application(application_sk);
  ALTER TABLE dbo.agg_app_monthly ADD CONSTRAINT UQ_agg_app_monthly_grain
    UNIQUE (month_start, provider, application_sk, environment);

  SELECT @uq_app = kc.name FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_app_service_monthly') AND kc.type = 'UQ';
  IF @uq_app IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_app_service_monthly DROP CONSTRAINT ' + QUOTENAME(@uq_app);
    EXEC (@sql);
  END
  ALTER TABLE dbo.agg_app_service_monthly DROP COLUMN application;
  ALTER TABLE dbo.agg_app_service_monthly ADD application_sk INT NOT NULL
    CONSTRAINT DF_agg_app_svc_monthly_app_sk DEFAULT 1;
  ALTER TABLE dbo.agg_app_service_monthly DROP CONSTRAINT DF_agg_app_svc_monthly_app_sk;
  ALTER TABLE dbo.agg_app_service_monthly ADD CONSTRAINT FK_agg_app_service_monthly_app
    FOREIGN KEY (application_sk) REFERENCES dbo.dim_application(application_sk);
  ALTER TABLE dbo.agg_app_service_monthly ADD CONSTRAINT UQ_agg_app_service_monthly_grain
    UNIQUE (month_start, provider, application_sk, environment, service_sk);

  SELECT @uq_app = kc.name FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_app_service_resource_monthly') AND kc.type = 'UQ';
  IF @uq_app IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_app_service_resource_monthly DROP CONSTRAINT ' + QUOTENAME(@uq_app);
    EXEC (@sql);
  END
  ALTER TABLE dbo.agg_app_service_resource_monthly DROP COLUMN application;
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD application_sk INT NOT NULL
    CONSTRAINT DF_agg_app_res_monthly_app_sk DEFAULT 1;
  ALTER TABLE dbo.agg_app_service_resource_monthly DROP CONSTRAINT DF_agg_app_res_monthly_app_sk;
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD CONSTRAINT FK_agg_app_service_resource_monthly_app
    FOREIGN KEY (application_sk) REFERENCES dbo.dim_application(application_sk);
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD CONSTRAINT UQ_agg_app_service_resource_monthly_grain
    UNIQUE (month_start, provider, application_sk, environment, service_sk, resource_sk);
END
GO

-- Migrate resource_sk VARCHAR → INT FK (rebuild aggregates after applying)
IF EXISTS (
  SELECT 1 FROM sys.columns c
  INNER JOIN sys.types t ON c.user_type_id = t.user_type_id
  WHERE c.object_id = OBJECT_ID(N'dbo.agg_app_service_resource_monthly')
    AND c.name = 'resource_sk'
    AND t.name IN ('varchar', 'nvarchar')
)
BEGIN
  TRUNCATE TABLE dbo.agg_app_service_resource_monthly;

  DECLARE @uq_res SYSNAME;
  DECLARE @sql NVARCHAR(MAX);
  SELECT @uq_res = kc.name FROM sys.key_constraints kc
  WHERE kc.parent_object_id = OBJECT_ID(N'dbo.agg_app_service_resource_monthly') AND kc.type = 'UQ';
  IF @uq_res IS NOT NULL
  BEGIN
    SET @sql = N'ALTER TABLE dbo.agg_app_service_resource_monthly DROP CONSTRAINT ' + QUOTENAME(@uq_res);
    EXEC (@sql);
  END

  ALTER TABLE dbo.agg_app_service_resource_monthly DROP COLUMN resource_sk;
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD resource_sk INT NOT NULL;
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD CONSTRAINT FK_agg_app_service_resource_monthly_resource
    FOREIGN KEY (resource_sk) REFERENCES dbo.dim_resource(resource_sk);
  ALTER TABLE dbo.agg_app_service_resource_monthly ADD CONSTRAINT UQ_agg_app_service_resource_monthly_grain
    UNIQUE (month_start, provider, application_sk, environment, service_sk, resource_sk);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_cost_distribution_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_cost_distribution_monthly (
    agg_cost_distribution_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start                      DATE NOT NULL,
    provider                         VARCHAR(10) NOT NULL,
    level_name                       VARCHAR(32) NOT NULL,
    parent_key                       NVARCHAR(512) NULL,
    entity_count                     INT NOT NULL DEFAULT 0,
    total_cost                       DECIMAL(28,10) NOT NULL DEFAULT 0,
    min_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    p50_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    p75_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    p90_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    p95_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    p99_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    max_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    avg_cost                         DECIMAL(28,10) NOT NULL DEFAULT 0,
    stddev_cost                      DECIMAL(28,10) NOT NULL DEFAULT 0,
    gini                             DECIMAL(18,8) NOT NULL DEFAULT 0,
    cr5                              DECIMAL(18,8) NOT NULL DEFAULT 0,
    cr10                             DECIMAL(18,8) NOT NULL DEFAULT 0,
    cr20                             DECIMAL(18,8) NOT NULL DEFAULT 0,
    top_10_cost_pct                  DECIMAL(18,8) NOT NULL DEFAULT 0,
    tail_80_cost_pct                 DECIMAL(18,8) NOT NULL DEFAULT 0,
    refreshed_utc                    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, level_name, parent_key)
  );
END
GO

IF COL_LENGTH('dbo.agg_cost_distribution_monthly', 'stddev_cost') IS NULL
BEGIN
  ALTER TABLE dbo.agg_cost_distribution_monthly
    ADD stddev_cost DECIMAL(28,10) NOT NULL CONSTRAINT DF_agg_cost_dist_stddev DEFAULT 0;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE object_id = OBJECT_ID(N'dbo.agg_cost_anomaly_monthly') AND type = N'U')
BEGIN
  CREATE TABLE dbo.agg_cost_anomaly_monthly (
    agg_cost_anomaly_monthly_id BIGINT IDENTITY(1,1) PRIMARY KEY,
    month_start                 DATE NOT NULL,
    provider                    VARCHAR(10) NOT NULL,
    entity_level                VARCHAR(16) NOT NULL,
    application_sk              INT NOT NULL FOREIGN KEY REFERENCES dbo.dim_application(application_sk),
    service_sk                  INT NULL,
    billed_cost_current         DECIMAL(28,10) NOT NULL DEFAULT 0,
    billed_cost_avg_3m          DECIMAL(28,10) NOT NULL DEFAULT 0,
    billed_cost_stddev_3m       DECIMAL(28,10) NOT NULL DEFAULT 0,
    z_score                     DECIMAL(18,8) NOT NULL DEFAULT 0,
    pct_change_vs_avg           DECIMAL(18,8) NOT NULL DEFAULT 0,
    history_months              TINYINT NOT NULL DEFAULT 0,
    anomaly_flag                BIT NOT NULL DEFAULT 0,
    anomaly_type                VARCHAR(32) NOT NULL DEFAULT 'NORMAL',
    refreshed_utc               DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    UNIQUE (month_start, provider, entity_level, application_sk, service_sk)
  );
END
GO

IF COL_LENGTH('dbo.agg_cost_anomaly_monthly', 'anomaly_type') IS NULL
BEGIN
  ALTER TABLE dbo.agg_cost_anomaly_monthly
    ADD anomaly_type VARCHAR(32) NOT NULL CONSTRAINT DF_agg_cost_anomaly_type DEFAULT 'NORMAL';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_cost_anomaly_month' AND object_id = OBJECT_ID(N'dbo.agg_cost_anomaly_monthly'))
BEGIN
  CREATE INDEX IX_agg_cost_anomaly_month ON dbo.agg_cost_anomaly_monthly (month_start, provider, anomaly_flag)
    INCLUDE (entity_level, application_sk, service_sk, z_score);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_agg_app_monthly_month' AND object_id = OBJECT_ID(N'dbo.agg_app_monthly'))
BEGIN
  CREATE INDEX IX_agg_app_monthly_month ON dbo.agg_app_monthly (month_start, provider, application_sk, environment);
END
GO

-- =====================================================================
-- SECTION 6: FACT INDEXES (columnstore + reporting)
-- =====================================================================

-- Columnstore requires Azure SQL S3+ / Premium / vCore; skip on Basic and S0–S2 tiers.
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'CCI_fact_focus_cost_daily' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  IF EXISTS (
    SELECT 1 FROM sys.dm_db_persisted_sku_features
    WHERE feature_name = N'COLUMNSTORE'
  )
  BEGIN
    CREATE NONCLUSTERED COLUMNSTORE INDEX CCI_fact_focus_cost_daily
      ON dbo.fact_focus_cost_daily (
        charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk,
        sku_sk, region_sk, charge_category_sk, pricing_category_sk, commitment_sk,
        billed_cost, effective_cost, list_cost, contracted_cost, line_count
      );
  END
  ELSE
  BEGIN
    BEGIN TRY
      CREATE NONCLUSTERED COLUMNSTORE INDEX CCI_fact_focus_cost_daily
        ON dbo.fact_focus_cost_daily (
          charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk,
          sku_sk, region_sk, charge_category_sk, pricing_category_sk, commitment_sk,
          billed_cost, effective_cost, list_cost, contracted_cost, line_count
        );
    END TRY
    BEGIN CATCH
      IF ERROR_MESSAGE() LIKE N'%COLUMNSTORE%'
        PRINT N'Skipping CCI_fact_focus_cost_daily: columnstore not available on this service tier.';
      ELSE
        THROW;
    END CATCH
  END
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_ingestion_batch_source' AND object_id = OBJECT_ID(N'dbo.dim_ingestion_batch'))
BEGIN
  CREATE INDEX IX_ingestion_batch_source
    ON dbo.dim_ingestion_batch (source_file, focus_version, status);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_batch' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_batch
    ON dbo.fact_focus_cost_daily (ingestion_batch_id);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_date_account' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_date_account
    ON dbo.fact_focus_cost_daily (charge_date, billing_account_sk)
    INCLUDE (billed_cost, effective_cost);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_billing_period' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_billing_period
    ON dbo.fact_focus_cost_daily (billing_period_start, billing_account_sk);
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_resource' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_resource
    ON dbo.fact_focus_cost_daily (resource_sk, charge_date)
    WHERE resource_sk IS NOT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_commitment' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_commitment
    ON dbo.fact_focus_cost_daily (commitment_sk, charge_date)
    WHERE commitment_sk IS NOT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_fact_cost_daily_service' AND object_id = OBJECT_ID(N'dbo.fact_focus_cost_daily'))
BEGIN
  CREATE INDEX IX_fact_cost_daily_service
    ON dbo.fact_focus_cost_daily (service_sk, charge_date)
    INCLUDE (billed_cost, effective_cost);
END
GO

-- =====================================================================
-- SECTION 7: REPORTING VIEWS
-- =====================================================================

CREATE OR ALTER VIEW dbo.vw_recommendations_summary AS
SELECT
    r.rec_snapshot_id,
    r.snapshot_month,
    r.recommendation_type,
    r.current_utilization_status,
    r.terminate_recommendation,
    res.name AS resource_name,
    res.resource_type,
    res.region AS resource_region,
    res.owner_email,
    res.environment,
    acc.provider,
    sa.sub_account_name AS account_name,
    svc.service_name,
    r.current_instance_type,
    r.current_vcpu,
    r.current_memory_gb,
    r.recommended_instance_type,
    r.recommended_vcpu,
    r.recommended_memory_gb,
    r.current_monthly_price,
    r.projected_monthly_price,
    r.projected_monthly_savings,
    CASE
        WHEN r.current_monthly_price > 0
        THEN (r.projected_monthly_savings / r.current_monthly_price) * 100
        ELSE 0
    END AS savings_percentage,
    MAX(CASE WHEN m.metric_name = 'avg_cpu' THEN m.metric_value END) AS avg_cpu,
    MAX(CASE WHEN m.metric_name = 'avg_mem' THEN m.metric_value END) AS avg_mem,
    MAX(CASE WHEN m.metric_name = 'avg_disk' THEN m.metric_value END) AS avg_disk,
    MAX(CASE WHEN m.metric_name = 'avg_dtu' THEN m.metric_value END) AS avg_dtu,
    MAX(CASE WHEN m.metric_name = 'avg_iops' THEN m.metric_value END) AS avg_iops,
    r.number_of_options,
    r.created_utc
FROM dbo.fact_recommendation_snapshot_v2 r
INNER JOIN dbo.dim_resource res ON r.resource_sk = res.resource_sk
INNER JOIN dbo.dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account acc ON sa.billing_account_sk = acc.account_sk
INNER JOIN dbo.dim_service svc ON res.service_sk = svc.service_sk
LEFT JOIN dbo.fact_recommendation_metrics m ON r.rec_snapshot_id = m.rec_snapshot_id
GROUP BY
    r.rec_snapshot_id, r.snapshot_month, r.recommendation_type, r.current_utilization_status,
    r.terminate_recommendation, res.name, res.resource_type, res.region, res.owner_email,
    res.environment, acc.provider, sa.sub_account_name AS account_name, svc.service_name,
    r.current_instance_type, r.current_vcpu, r.current_memory_gb,
    r.recommended_instance_type, r.recommended_vcpu, r.recommended_memory_gb,
    r.current_monthly_price, r.projected_monthly_price, r.projected_monthly_savings,
    r.number_of_options, r.created_utc;
GO

CREATE OR ALTER VIEW dbo.vw_top_savings_opportunities AS
SELECT TOP 100
    r.snapshot_month,
    res.name AS resource_name,
    acc.provider,
    sa.sub_account_name AS account_name,
    svc.service_name,
    r.recommendation_type,
    r.current_instance_type,
    r.recommended_instance_type,
    r.current_monthly_price,
    r.projected_monthly_price,
    r.projected_monthly_savings,
    CAST((r.projected_monthly_savings / NULLIF(r.current_monthly_price, 0)) * 100 AS DECIMAL(5,2)) AS savings_percentage,
    r.current_utilization_status,
    r.terminate_recommendation,
    res.owner_email,
    res.environment
FROM dbo.fact_recommendation_snapshot_v2 r
INNER JOIN dbo.dim_resource res ON r.resource_sk = res.resource_sk
INNER JOIN dbo.dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account acc ON sa.billing_account_sk = acc.account_sk
INNER JOIN dbo.dim_service svc ON res.service_sk = svc.service_sk
WHERE r.projected_monthly_savings > 0
  AND res.is_excluded = 0
ORDER BY r.projected_monthly_savings DESC;
GO

CREATE OR ALTER VIEW dbo.vw_utilization_summary AS
SELECT
    r.snapshot_month,
    acc.provider,
    svc.service_code,
    r.current_utilization_status,
    COUNT(*) AS resource_count,
    SUM(r.current_monthly_price) AS total_current_monthly_cost,
    SUM(r.projected_monthly_savings) AS total_potential_savings,
    AVG(r.projected_monthly_savings) AS avg_savings_per_resource
FROM dbo.fact_recommendation_snapshot_v2 r
INNER JOIN dbo.dim_resource res ON r.resource_sk = res.resource_sk
INNER JOIN dbo.dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_account acc ON sa.billing_account_sk = acc.account_sk
INNER JOIN dbo.dim_service svc ON res.service_sk = svc.service_sk
GROUP BY r.snapshot_month, acc.provider, svc.service_code, r.current_utilization_status;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_cost_monthly AS
SELECT
    a.month_start,
    a.month_start AS billing_period_start,
    a.provider,
    sa.sub_account_name AS account_name,
    a.service_category,
    cc.charge_category,
    a.billed_cost,
    a.effective_cost,
    a.billed_cost - a.effective_cost AS discount_amount,
    a.list_cost,
    a.contracted_cost,
    a.line_count,
    a.refreshed_utc
FROM dbo.agg_cost_monthly a
INNER JOIN dbo.dim_sub_account sa ON a.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_charge_category cc ON a.charge_category_sk = cc.charge_category_sk;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_cost_daily AS
SELECT
    a.charge_date,
    a.billing_period_start,
    a.billing_period_start AS month_start,
    a.provider,
    sa.sub_account_name AS account_name,
    svc.service_name,
    reg.region_name,
    a.billed_cost,
    a.effective_cost,
    a.billed_cost - a.effective_cost AS discount_amount,
    a.list_cost,
    a.contracted_cost,
    a.line_count,
    a.refreshed_utc
FROM dbo.agg_cost_daily a
INNER JOIN dbo.dim_sub_account sa ON a.sub_account_sk = sa.sub_account_sk
INNER JOIN dbo.dim_service svc ON a.service_sk = svc.service_sk
LEFT JOIN dbo.dim_region reg ON a.region_sk = reg.region_sk;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_cost_by_tag AS
SELECT
    month_start,
    provider,
    tag_key,
    tag_value,
    billed_cost,
    effective_cost,
    line_count,
    refreshed_utc
FROM dbo.agg_cost_by_tag;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_app_monthly AS
SELECT
    a.month_start,
    a.provider,
    app.application_sk,
    app.application_name,
    app.alias_values,
    app.first_seen_date,
    CONCAT(app.application_sk, ';', app.application_name, ';', COALESCE(app.alias_values, '')) AS application_summary,
    a.environment,
    a.billed_cost,
    a.effective_cost,
    a.billed_cost - a.effective_cost AS discount_amount,
    a.line_count,
    a.refreshed_utc
FROM dbo.agg_app_monthly a
INNER JOIN dbo.dim_application app ON a.application_sk = app.application_sk;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_app_service_monthly AS
SELECT
    a.month_start,
    a.provider,
    app.application_sk,
    app.application_name,
    app.alias_values,
    a.environment,
    svc.service_name,
    svc.service_category,
    a.billed_cost,
    a.effective_cost,
    a.billed_cost - a.effective_cost AS discount_amount,
    a.line_count,
    a.refreshed_utc
FROM dbo.agg_app_service_monthly a
INNER JOIN dbo.dim_application app ON a.application_sk = app.application_sk
INNER JOIN dbo.dim_service svc ON a.service_sk = svc.service_sk;
GO

CREATE OR ALTER VIEW dbo.vw_dim_application AS
SELECT
    application_sk,
    application_name,
    alias_values,
    first_seen_date,
    CONCAT(application_sk, ';', application_name, ';', COALESCE(alias_values, '')) AS application_summary,
    created_utc,
    updated_utc
FROM dbo.dim_application;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_cost_distribution_monthly AS
SELECT
    month_start,
    provider,
    level_name,
    parent_key,
    entity_count,
    total_cost,
    min_cost,
    p50_cost,
    p75_cost,
    p90_cost,
    p95_cost,
    p99_cost,
    max_cost,
    avg_cost,
    stddev_cost,
    gini,
    cr5,
    cr10,
    cr20,
    top_10_cost_pct,
    tail_80_cost_pct,
    refreshed_utc
FROM dbo.agg_cost_distribution_monthly;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_cost_anomaly_monthly AS
SELECT
    a.month_start,
    a.month_start AS billing_period_start,
    a.provider,
    a.entity_level,
    a.application_sk,
    app.application_name,
    app.alias_values,
    a.service_sk,
    svc.service_name,
    a.billed_cost_current,
    a.billed_cost_avg_3m,
    a.billed_cost_stddev_3m,
    a.z_score,
    a.pct_change_vs_avg,
    a.history_months,
    a.anomaly_flag,
    a.anomaly_type,
    a.refreshed_utc
FROM dbo.agg_cost_anomaly_monthly a
INNER JOIN dbo.dim_application app ON a.application_sk = app.application_sk
LEFT JOIN dbo.dim_service svc ON a.service_sk = svc.service_sk;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_commitment_utilization AS
SELECT
    a.month_start,
    a.provider,
    c.commitment_discount_name,
    c.commitment_discount_type,
    a.commitment_status,
    a.effective_cost,
    a.commitment_quantity,
    a.line_count,
    a.refreshed_utc
FROM dbo.agg_commitment_utilization a
INNER JOIN dbo.dim_commitment_discount c ON a.commitment_sk = c.commitment_sk;
GO

CREATE OR ALTER VIEW dbo.vw_pbi_savings_summary AS
SELECT
    a.month_start,
    a.provider,
    svc.service_name,
    a.total_effective_cost,
    a.total_projected_savings,
    a.recommendation_count,
    a.refreshed_utc
FROM dbo.agg_savings_summary a
INNER JOIN dbo.dim_service svc ON a.service_sk = svc.service_sk;
GO

-- Optional migration from fact_recommendation_snapshot v1 (if legacy table exists)
/*
INSERT INTO dbo.fact_recommendation_snapshot_v2 (
    snapshot_month, resource_sk, recommendation_type, current_utilization_status,
    terminate_recommendation, current_instance_type, recommended_instance_type,
    projected_monthly_savings, expected_perf_impact, engine_run_id, raw_payload_json, created_utc
)
SELECT
    snapshot_month, resource_sk, recommendation_type, recommendation_status,
    terminate_recommendation, current_instance, recommended_instance,
    expected_monthly_savings_usd, expected_perf_impact, engine_run_id, raw_payload_json, created_utc
FROM dbo.fact_recommendation_snapshot old
WHERE NOT EXISTS (
    SELECT 1 FROM dbo.fact_recommendation_snapshot_v2 v2
    WHERE v2.snapshot_month = old.snapshot_month
      AND v2.resource_sk = old.resource_sk
      AND v2.recommendation_type = old.recommendation_type
);
GO
*/

PRINT 'focus_dw.sql completed successfully.';
GO
