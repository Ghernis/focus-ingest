-- FOCUS Data Warehouse — SQLite (local dev)
-- Mirrors focus_dw.sql for local testing with focus-ingest --local

PRAGMA foreign_keys = ON;

-- SECTION 1: DIMENSIONS

CREATE TABLE IF NOT EXISTS dim_date (
  date_sk        INTEGER NOT NULL PRIMARY KEY,
  full_date      TEXT NOT NULL UNIQUE,
  year_num       INTEGER NOT NULL,
  quarter_num    INTEGER NOT NULL,
  month_num      INTEGER NOT NULL,
  month_name     TEXT NOT NULL,
  month_start    TEXT NOT NULL,
  week_num       INTEGER NOT NULL,
  day_of_month   INTEGER NOT NULL,
  day_of_week    INTEGER NOT NULL,
  day_name       TEXT NOT NULL,
  is_weekend     INTEGER NOT NULL,
  fiscal_year    INTEGER NULL,
  fiscal_quarter INTEGER NULL
);

CREATE TABLE IF NOT EXISTS dim_account (
  account_sk           INTEGER PRIMARY KEY AUTOINCREMENT,
  provider             TEXT NOT NULL CHECK (provider IN ('AWS','AZURE','GCP')),
  account_id           TEXT NOT NULL,
  account_name         TEXT NULL,
  billing_account_type TEXT NULL,
  is_active            INTEGER NOT NULL DEFAULT 1,
  UNIQUE (provider, account_id)
);

CREATE TABLE IF NOT EXISTS dim_sub_account (
  sub_account_sk     INTEGER PRIMARY KEY AUTOINCREMENT,
  provider           TEXT NOT NULL CHECK (provider IN ('AWS','AZURE','GCP')),
  sub_account_id     TEXT NOT NULL,
  sub_account_name   TEXT NULL,
  sub_account_type   TEXT NULL,
  billing_account_sk INTEGER NULL REFERENCES dim_account(account_sk),
  UNIQUE (provider, sub_account_id)
);

CREATE TABLE IF NOT EXISTS dim_service (
  service_sk          INTEGER PRIMARY KEY AUTOINCREMENT,
  provider            TEXT NOT NULL,
  service_code        TEXT NOT NULL,
  service_name        TEXT NOT NULL,
  service_category    TEXT NULL,
  service_subcategory TEXT NULL,
  UNIQUE (provider, service_code)
);

CREATE TABLE IF NOT EXISTS dim_region (
  region_sk   INTEGER PRIMARY KEY AUTOINCREMENT,
  provider    TEXT NOT NULL,
  region_id   TEXT NOT NULL,
  region_name TEXT NULL,
  UNIQUE (provider, region_id)
);

CREATE TABLE IF NOT EXISTS dim_sku (
  sku_sk            INTEGER PRIMARY KEY AUTOINCREMENT,
  provider          TEXT NOT NULL,
  sku_id            TEXT NOT NULL,
  sku_price_id      TEXT NULL,
  sku_meter         TEXT NULL,
  sku_price_details TEXT NULL,
  service_name      TEXT NULL,
  UNIQUE (provider, sku_id, sku_price_id)
);

CREATE TABLE IF NOT EXISTS dim_charge_category (
  charge_category_sk INTEGER PRIMARY KEY AUTOINCREMENT,
  charge_category    TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS dim_charge_frequency (
  charge_frequency_sk INTEGER PRIMARY KEY AUTOINCREMENT,
  charge_frequency    TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS dim_pricing_category (
  pricing_category_sk INTEGER PRIMARY KEY AUTOINCREMENT,
  pricing_category    TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS dim_commitment_discount (
  commitment_sk                INTEGER PRIMARY KEY AUTOINCREMENT,
  provider                     TEXT NOT NULL,
  commitment_discount_id       TEXT NOT NULL,
  commitment_discount_name     TEXT NULL,
  commitment_discount_type     TEXT NULL,
  commitment_discount_category TEXT NULL,
  commitment_discount_unit     TEXT NULL,
  UNIQUE (provider, commitment_discount_id)
);

CREATE TABLE IF NOT EXISTS dim_capacity_reservation (
  capacity_reservation_sk     INTEGER PRIMARY KEY AUTOINCREMENT,
  provider                    TEXT NOT NULL,
  capacity_reservation_id     TEXT NOT NULL,
  capacity_reservation_status TEXT NULL,
  UNIQUE (provider, capacity_reservation_id)
);

CREATE TABLE IF NOT EXISTS dim_resource (
  resource_sk        INTEGER PRIMARY KEY AUTOINCREMENT,
  provider           TEXT NOT NULL,
  global_resource_id TEXT NOT NULL,
  resource_type      TEXT NOT NULL,
  account_sk         INTEGER NOT NULL REFERENCES dim_account(account_sk),
  sub_account_sk     INTEGER NULL REFERENCES dim_sub_account(sub_account_sk),
  service_sk         INTEGER NOT NULL REFERENCES dim_service(service_sk),
  region             TEXT NULL,
  name               TEXT NULL,
  owner_email        TEXT NULL,
  cost_center        TEXT NULL,
  environment        TEXT NULL,
  application        TEXT NULL,
  business           TEXT NULL,
  tags_json          TEXT NULL,
  hourly_cost        TEXT NULL,
  valid_from         TEXT NOT NULL,
  valid_to           TEXT NULL,
  is_current         INTEGER GENERATED ALWAYS AS (CASE WHEN valid_to IS NULL THEN 1 ELSE 0 END) STORED,
  is_excluded        INTEGER NOT NULL DEFAULT 0,
  UNIQUE (provider, global_resource_id, valid_from)
);

CREATE INDEX IF NOT EXISTS IX_dim_resource_current ON dim_resource (provider, global_resource_id) WHERE valid_to IS NULL;
CREATE INDEX IF NOT EXISTS IX_dim_resource_sub_account_current ON dim_resource (sub_account_sk, is_current) WHERE is_current = 1;

CREATE TABLE IF NOT EXISTS dim_tag (
  tag_sk    INTEGER PRIMARY KEY AUTOINCREMENT,
  tag_key   TEXT NOT NULL,
  tag_value TEXT NOT NULL,
  UNIQUE (tag_key, tag_value)
);

CREATE TABLE IF NOT EXISTS dim_application (
  application_sk   INTEGER PRIMARY KEY AUTOINCREMENT,
  application_name TEXT NOT NULL UNIQUE,
  alias_values     TEXT NULL,
  first_seen_date  TEXT NULL,
  created_utc      TEXT NOT NULL DEFAULT (datetime('now')),
  updated_utc      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS IX_dim_application_name ON dim_application (application_name);

INSERT OR IGNORE INTO dim_application (application_name, alias_values)
VALUES ('(UNASSIGNED)', '(Unassigned)');

INSERT OR IGNORE INTO dim_charge_category (charge_category) VALUES ('Usage'),('Purchase'),('Tax'),('Credit'),('Adjustment');
INSERT OR IGNORE INTO dim_charge_frequency (charge_frequency) VALUES ('Usage-Based'),('Recurring'),('One-Time');
INSERT OR IGNORE INTO dim_pricing_category (pricing_category) VALUES ('Standard'),('Committed'),('Dynamic'),('Other');

-- dim_date seed 2020-01-01 .. 2035-12-31
INSERT OR IGNORE INTO dim_date (
  date_sk, full_date, year_num, quarter_num, month_num, month_name,
  month_start, week_num, day_of_month, day_of_week, day_name, is_weekend
)
WITH RECURSIVE dates(d) AS (
  SELECT date('2020-01-01')
  UNION ALL
  SELECT date(d, '+1 day') FROM dates WHERE d < date('2035-12-31')
)
SELECT
  CAST(strftime('%Y%m%d', d) AS INTEGER),
  d,
  CAST(strftime('%Y', d) AS INTEGER),
  CAST(((CAST(strftime('%m', d) AS INTEGER) - 1) / 3) + 1 AS INTEGER),
  CAST(strftime('%m', d) AS INTEGER),
  CASE CAST(strftime('%m', d) AS INTEGER)
    WHEN 1 THEN 'January' WHEN 2 THEN 'February' WHEN 3 THEN 'March' WHEN 4 THEN 'April'
    WHEN 5 THEN 'May' WHEN 6 THEN 'June' WHEN 7 THEN 'July' WHEN 8 THEN 'August'
    WHEN 9 THEN 'September' WHEN 10 THEN 'October' WHEN 11 THEN 'November' WHEN 12 THEN 'December'
  END,
  date(d, 'start of month'),
  CAST(strftime('%W', d) AS INTEGER),
  CAST(strftime('%d', d) AS INTEGER),
  CAST(strftime('%w', d) AS INTEGER) + 1,
  CASE CAST(strftime('%w', d) AS INTEGER)
    WHEN 0 THEN 'Sunday' WHEN 1 THEN 'Monday' WHEN 2 THEN 'Tuesday' WHEN 3 THEN 'Wednesday'
    WHEN 4 THEN 'Thursday' WHEN 5 THEN 'Friday' WHEN 6 THEN 'Saturday'
  END,
  CASE WHEN CAST(strftime('%w', d) AS INTEGER) IN (0, 6) THEN 1 ELSE 0 END
FROM dates;

-- SECTION 2: FOCUS COST

CREATE TABLE IF NOT EXISTS dim_ingestion_batch (
  ingestion_batch_id   INTEGER PRIMARY KEY AUTOINCREMENT,
  source_provider      TEXT NOT NULL,
  focus_version        TEXT NOT NULL,
  source_file          TEXT NULL,
  billing_period_start TEXT NULL,
  billing_period_end   TEXT NULL,
  row_count            INTEGER NULL,
  loaded_utc           TEXT NOT NULL DEFAULT (datetime('now')),
  status               TEXT NOT NULL DEFAULT 'LOADED'
);

CREATE TABLE IF NOT EXISTS stg_focus_cost_line (
  stg_line_id        INTEGER PRIMARY KEY AUTOINCREMENT,
  ingestion_batch_id INTEGER NOT NULL,
  source_provider    TEXT NULL,
  focus_version      TEXT NULL,
  source_file        TEXT NULL,
  loaded_utc         TEXT NOT NULL DEFAULT (datetime('now')),
  x_source_row_id    INTEGER NULL,
  AvailabilityZone   TEXT NULL,
  BilledCost         TEXT NULL,
  BillingAccountId   TEXT NULL,
  BillingAccountName TEXT NULL,
  BillingAccountType TEXT NULL,
  BillingCurrency    TEXT NULL,
  BillingPeriodEnd   TEXT NULL,
  BillingPeriodStart TEXT NULL,
  CapacityReservationId     TEXT NULL,
  CapacityReservationStatus TEXT NULL,
  ChargeCategory     TEXT NULL,
  ChargeClass        TEXT NULL,
  ChargeDescription  TEXT NULL,
  ChargeFrequency    TEXT NULL,
  ChargePeriodEnd    TEXT NULL,
  ChargePeriodStart  TEXT NULL,
  CommitmentDiscountCategory TEXT NULL,
  CommitmentDiscountId       TEXT NULL,
  CommitmentDiscountName     TEXT NULL,
  CommitmentDiscountQuantity TEXT NULL,
  CommitmentDiscountStatus   TEXT NULL,
  CommitmentDiscountType     TEXT NULL,
  CommitmentDiscountUnit     TEXT NULL,
  ConsumedQuantity   TEXT NULL,
  ConsumedUnit       TEXT NULL,
  ContractedCost     TEXT NULL,
  ContractedUnitPrice TEXT NULL,
  EffectiveCost      TEXT NULL,
  InvoiceId          TEXT NULL,
  InvoiceIssuer      TEXT NULL,
  ListCost           TEXT NULL,
  ListUnitPrice      TEXT NULL,
  PricingCategory    TEXT NULL,
  PricingCurrency    TEXT NULL,
  PricingCurrencyContractedUnitPrice TEXT NULL,
  PricingCurrencyEffectiveCost       TEXT NULL,
  PricingCurrencyListUnitPrice       TEXT NULL,
  PricingQuantity    TEXT NULL,
  PricingUnit        TEXT NULL,
  Provider           TEXT NULL,
  Publisher          TEXT NULL,
  RegionId           TEXT NULL,
  RegionName         TEXT NULL,
  ResourceId         TEXT NULL,
  ResourceName       TEXT NULL,
  ResourceType       TEXT NULL,
  ServiceCategory    TEXT NULL,
  ServiceName        TEXT NULL,
  ServiceSubcategory TEXT NULL,
  SkuId              TEXT NULL,
  SkuMeter           TEXT NULL,
  SkuPriceDetails    TEXT NULL,
  SkuPriceId         TEXT NULL,
  SubAccountId       TEXT NULL,
  SubAccountName     TEXT NULL,
  SubAccountType     TEXT NULL,
  raw_tags_json      TEXT NULL
);

CREATE INDEX IF NOT EXISTS IX_stg_focus_batch ON stg_focus_cost_line (ingestion_batch_id);

CREATE TABLE IF NOT EXISTS fact_focus_cost_daily (
  cost_daily_id              INTEGER PRIMARY KEY AUTOINCREMENT,
  charge_date                TEXT NOT NULL,
  billing_account_sk         INTEGER NOT NULL REFERENCES dim_account(account_sk),
  sub_account_sk             INTEGER NULL REFERENCES dim_sub_account(sub_account_sk),
  resource_sk                INTEGER NULL REFERENCES dim_resource(resource_sk),
  service_sk                 INTEGER NOT NULL REFERENCES dim_service(service_sk),
  sku_sk                     INTEGER NULL REFERENCES dim_sku(sku_sk),
  region_sk                  INTEGER NULL REFERENCES dim_region(region_sk),
  charge_category_sk         INTEGER NOT NULL REFERENCES dim_charge_category(charge_category_sk),
  charge_frequency_sk        INTEGER NULL REFERENCES dim_charge_frequency(charge_frequency_sk),
  pricing_category_sk        INTEGER NULL REFERENCES dim_pricing_category(pricing_category_sk),
  commitment_sk              INTEGER NULL REFERENCES dim_commitment_discount(commitment_sk),
  commitment_discount_status TEXT NULL,
  capacity_reservation_sk    INTEGER NULL REFERENCES dim_capacity_reservation(capacity_reservation_sk),
  capacity_reservation_status TEXT NULL,
  charge_description_hash    TEXT NOT NULL,
  billing_period_start       TEXT NOT NULL,
  billing_period_end         TEXT NOT NULL,
  billed_cost                TEXT NOT NULL DEFAULT '0',
  effective_cost             TEXT NOT NULL DEFAULT '0',
  list_cost                  TEXT NOT NULL DEFAULT '0',
  contracted_cost            TEXT NOT NULL DEFAULT '0',
  pricing_quantity           TEXT NOT NULL DEFAULT '0',
  consumed_quantity          TEXT NOT NULL DEFAULT '0',
  commitment_discount_quantity TEXT NOT NULL DEFAULT '0',
  line_count                 INTEGER NOT NULL DEFAULT 0,
  first_charge_period_start  TEXT NULL,
  last_charge_period_end     TEXT NULL,
  ingestion_batch_id         INTEGER NOT NULL REFERENCES dim_ingestion_batch(ingestion_batch_id),
  focus_version              TEXT NOT NULL,
  created_utc                TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (
    charge_date, billing_account_sk, sub_account_sk, resource_sk, service_sk,
    sku_sk, region_sk, charge_category_sk, pricing_category_sk,
    commitment_sk, commitment_discount_status, capacity_reservation_sk,
    capacity_reservation_status, charge_description_hash,
    billing_period_start, ingestion_batch_id
  )
);

CREATE TABLE IF NOT EXISTS bridge_cost_tag (
  cost_daily_id INTEGER NOT NULL REFERENCES fact_focus_cost_daily(cost_daily_id),
  tag_sk        INTEGER NOT NULL REFERENCES dim_tag(tag_sk),
  PRIMARY KEY (cost_daily_id, tag_sk)
);

-- SECTION 3: RECOMMENDATIONS V2

CREATE TABLE IF NOT EXISTS fact_recommendation_snapshot_v2 (
  rec_snapshot_id            INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_month             TEXT NOT NULL,
  resource_sk                INTEGER NOT NULL REFERENCES dim_resource(resource_sk),
  recommendation_type        TEXT NOT NULL,
  current_utilization_status TEXT NULL,
  terminate_recommendation   INTEGER NOT NULL DEFAULT 0,
  current_instance_type      TEXT NULL,
  current_vcpu               INTEGER NULL,
  current_memory_gb          TEXT NULL,
  current_storage_gb         TEXT NULL,
  current_iops               INTEGER NULL,
  current_throughput_mbps    INTEGER NULL,
  current_dtu                INTEGER NULL,
  current_service_tier       TEXT NULL,
  recommended_instance_type  TEXT NULL,
  recommended_vcpu           INTEGER NULL,
  recommended_memory_gb      TEXT NULL,
  recommended_storage_gb     TEXT NULL,
  recommended_iops           INTEGER NULL,
  recommended_throughput_mbps INTEGER NULL,
  recommended_dtu            INTEGER NULL,
  recommended_service_tier   TEXT NULL,
  current_monthly_price      TEXT NULL,
  projected_monthly_price    TEXT NULL,
  projected_monthly_savings  TEXT NULL,
  current_cost_mtd           TEXT NULL,
  projected_cost_mtd         TEXT NULL,
  projected_savings_mtd      TEXT NULL,
  expected_perf_impact       TEXT NULL,
  number_of_options          INTEGER NULL DEFAULT 1,
  engine_run_id              TEXT NULL,
  provider_resource_id       TEXT NULL,
  region                     TEXT NULL,
  raw_payload_json           TEXT NULL,
  created_utc                TEXT NOT NULL DEFAULT (datetime('now')),
  updated_utc                TEXT NULL,
  UNIQUE (snapshot_month, resource_sk, recommendation_type)
);

CREATE TABLE IF NOT EXISTS fact_recommendation_metrics (
  metric_id       INTEGER PRIMARY KEY AUTOINCREMENT,
  rec_snapshot_id INTEGER NOT NULL REFERENCES fact_recommendation_snapshot_v2(rec_snapshot_id) ON DELETE CASCADE,
  metric_name     TEXT NOT NULL,
  aggregation     TEXT NOT NULL,
  metric_value    TEXT NULL,
  metric_unit     TEXT NULL,
  metric_status   TEXT NULL,
  metric_type     TEXT NULL,
  created_utc     TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (rec_snapshot_id, metric_name, aggregation)
);

CREATE TABLE IF NOT EXISTS fact_recommendation_options (
  option_id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  rec_snapshot_id               INTEGER NOT NULL REFERENCES fact_recommendation_snapshot_v2(rec_snapshot_id) ON DELETE CASCADE,
  option_rank                   INTEGER NOT NULL DEFAULT 1,
  is_best_fit                   INTEGER NOT NULL DEFAULT 0,
  option_instance_type          TEXT NULL,
  option_vcpu                   INTEGER NULL,
  option_memory_gb              TEXT NULL,
  option_storage_gb             TEXT NULL,
  option_storage_type           TEXT NULL,
  option_iops                   INTEGER NULL,
  option_throughput_mbps        INTEGER NULL,
  option_dtu                    INTEGER NULL,
  option_service_tier           TEXT NULL,
  option_network_bandwidth_gbps TEXT NULL,
  option_monthly_price          TEXT NULL,
  option_monthly_savings        TEXT NULL,
  option_hourly_rate            TEXT NULL,
  created_utc                   TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (rec_snapshot_id, option_rank)
);

CREATE INDEX IF NOT EXISTS IX_fact_rec_v2_snapshot_month ON fact_recommendation_snapshot_v2 (snapshot_month);
CREATE INDEX IF NOT EXISTS IX_fact_rec_v2_resource ON fact_recommendation_snapshot_v2 (resource_sk, recommendation_type);
CREATE INDEX IF NOT EXISTS IX_fact_rec_v2_savings ON fact_recommendation_snapshot_v2 (projected_monthly_savings) WHERE projected_monthly_savings IS NOT NULL;

-- SECTION 4: OPERATIONAL

CREATE TABLE IF NOT EXISTS fact_resource_daily (
  resource_daily_id INTEGER PRIMARY KEY AUTOINCREMENT,
  as_of_date        TEXT NOT NULL,
  resource_sk       INTEGER NOT NULL REFERENCES dim_resource(resource_sk),
  power_state       TEXT NULL,
  instance_size     TEXT NULL,
  storage_type      TEXT NULL,
  provisioned_gb    INTEGER NULL,
  avg_cpu_pct_7d    TEXT NULL,
  avg_mem_pct_7d    TEXT NULL,
  avg_net_in_out_7d TEXT NULL,
  tag_hash          TEXT NULL,
  raw_state_json    TEXT NULL,
  created_utc       TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (as_of_date, resource_sk)
);

CREATE TABLE IF NOT EXISTS fact_alert_snapshot (
  alert_id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  alert_date               TEXT NOT NULL,
  resource_sk              INTEGER NOT NULL REFERENCES dim_resource(resource_sk),
  alert_type               TEXT NOT NULL,
  severity                 TEXT NULL,
  rule_name                TEXT NULL,
  threshold                TEXT NULL,
  raw_payload_json         TEXT NULL,
  projected_cost_for_month TEXT NULL,
  created_utc              TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (alert_date, resource_sk, alert_type)
);

CREATE TABLE IF NOT EXISTS dim_team_group (
  group_sk      INTEGER PRIMARY KEY AUTOINCREMENT,
  group_name    TEXT NOT NULL,
  group_email   TEXT NULL,
  group_sn_name TEXT NULL,
  group_sn_id   TEXT NULL,
  created_utc   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_utc   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS ritm_header (
  ritm_sk           INTEGER PRIMARY KEY AUTOINCREMENT,
  ritm_number       TEXT NOT NULL UNIQUE,
  opened_utc        TEXT NOT NULL,
  closed_utc        TEXT NULL,
  state             TEXT NOT NULL,
  group_sk          INTEGER NULL REFERENCES dim_team_group(group_sk),
  assignee_email    TEXT NULL,
  requester_email   TEXT NULL,
  short_description TEXT NULL,
  catalog_item      TEXT NULL,
  raw_payload_json  TEXT NULL
);

CREATE TABLE IF NOT EXISTS ritm_by_resource_header (
  ritm_sk           INTEGER PRIMARY KEY AUTOINCREMENT,
  ritm_number       TEXT NOT NULL UNIQUE,
  opened_utc        TEXT NOT NULL,
  closed_utc        TEXT NULL,
  state             TEXT NOT NULL,
  resource_sk       INTEGER NULL REFERENCES dim_resource(resource_sk),
  assignee_email    TEXT NULL,
  requester_email   TEXT NULL,
  short_description TEXT NULL,
  catalog_item      TEXT NULL,
  raw_payload_json  TEXT NULL
);

CREATE TABLE IF NOT EXISTS bridge_recommendation_ritm (
  rec_snapshot_id INTEGER NOT NULL REFERENCES fact_recommendation_snapshot_v2(rec_snapshot_id),
  ritm_sk         INTEGER NOT NULL REFERENCES ritm_header(ritm_sk),
  linked_utc      TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (rec_snapshot_id, ritm_sk)
);

CREATE TABLE IF NOT EXISTS ritm_comment (
  ritm_comment_id INTEGER PRIMARY KEY AUTOINCREMENT,
  ritm_sk         INTEGER NOT NULL REFERENCES ritm_header(ritm_sk),
  commented_utc   TEXT NOT NULL,
  author_email    TEXT NULL,
  comment_text    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS exceptions (
  exception_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  resource_sk   INTEGER NULL REFERENCES dim_resource(resource_sk),
  exception_utc TEXT NOT NULL DEFAULT (datetime('now')),
  commented     TEXT NOT NULL,
  reason        TEXT NOT NULL,
  end_exception TEXT NULL
);

-- SECTION 5: AGGREGATES

CREATE TABLE IF NOT EXISTS agg_cost_daily (
  agg_cost_daily_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  charge_date        TEXT NOT NULL,
  billing_period_start TEXT NOT NULL,
  provider           TEXT NOT NULL,
  sub_account_sk     INTEGER NOT NULL REFERENCES dim_sub_account(sub_account_sk),
  service_sk         INTEGER NOT NULL,
  region_sk          INTEGER NULL,
  billed_cost        TEXT NOT NULL DEFAULT '0',
  effective_cost     TEXT NOT NULL DEFAULT '0',
  list_cost          TEXT NOT NULL DEFAULT '0',
  contracted_cost    TEXT NOT NULL DEFAULT '0',
  line_count         INTEGER NOT NULL DEFAULT 0,
  refreshed_utc      TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (charge_date, billing_period_start, provider, sub_account_sk, service_sk, region_sk)
);

CREATE TABLE IF NOT EXISTS agg_cost_monthly (
  agg_cost_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start         TEXT NOT NULL,
  provider            TEXT NOT NULL,
  sub_account_sk      INTEGER NOT NULL REFERENCES dim_sub_account(sub_account_sk),
  service_category    TEXT NULL,
  charge_category_sk  INTEGER NOT NULL,
  billed_cost         TEXT NOT NULL DEFAULT '0',
  effective_cost      TEXT NOT NULL DEFAULT '0',
  list_cost           TEXT NOT NULL DEFAULT '0',
  contracted_cost     TEXT NOT NULL DEFAULT '0',
  line_count          INTEGER NOT NULL DEFAULT 0,
  refreshed_utc       TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, sub_account_sk, service_category, charge_category_sk)
);

CREATE TABLE IF NOT EXISTS agg_cost_by_tag (
  agg_cost_by_tag_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start        TEXT NOT NULL,
  provider           TEXT NOT NULL,
  tag_key            TEXT NOT NULL,
  tag_value          TEXT NOT NULL,
  effective_cost     TEXT NOT NULL DEFAULT '0',
  billed_cost        TEXT NOT NULL DEFAULT '0',
  line_count         INTEGER NOT NULL DEFAULT 0,
  refreshed_utc      TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, tag_key, tag_value)
);

CREATE TABLE IF NOT EXISTS agg_commitment_utilization (
  agg_commitment_util_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start            TEXT NOT NULL,
  provider               TEXT NOT NULL,
  commitment_sk          INTEGER NOT NULL,
  commitment_status      TEXT NOT NULL,
  effective_cost         TEXT NOT NULL DEFAULT '0',
  commitment_quantity    TEXT NOT NULL DEFAULT '0',
  line_count             INTEGER NOT NULL DEFAULT 0,
  refreshed_utc          TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, commitment_sk, commitment_status)
);

CREATE TABLE IF NOT EXISTS agg_commitment_utilization_daily (
  agg_commitment_daily_id INTEGER PRIMARY KEY AUTOINCREMENT,
  charge_date             TEXT NOT NULL,
  provider                TEXT NOT NULL,
  commitment_sk           INTEGER NOT NULL,
  commitment_status       TEXT NOT NULL,
  effective_cost          TEXT NOT NULL DEFAULT '0',
  commitment_quantity     TEXT NOT NULL DEFAULT '0',
  line_count              INTEGER NOT NULL DEFAULT 0,
  refreshed_utc           TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (charge_date, provider, commitment_sk, commitment_status)
);

CREATE TABLE IF NOT EXISTS agg_savings_summary (
  agg_savings_summary_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start             TEXT NOT NULL,
  provider                TEXT NOT NULL,
  service_sk              INTEGER NOT NULL,
  total_effective_cost    TEXT NOT NULL DEFAULT '0',
  total_projected_savings TEXT NOT NULL DEFAULT '0',
  recommendation_count    INTEGER NOT NULL DEFAULT 0,
  refreshed_utc           TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, service_sk)
);

-- Application spend (primary metric: billed_cost = actual paid after discounts/credits)
CREATE TABLE IF NOT EXISTS agg_app_monthly (
  agg_app_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start        TEXT NOT NULL,
  provider           TEXT NOT NULL,
  application_sk     INTEGER NOT NULL REFERENCES dim_application(application_sk),
  environment        TEXT NOT NULL DEFAULT '(Unknown)',
  billed_cost        TEXT NOT NULL DEFAULT '0',
  effective_cost     TEXT NOT NULL DEFAULT '0',
  line_count         INTEGER NOT NULL DEFAULT 0,
  refreshed_utc      TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, application_sk, environment)
);

CREATE TABLE IF NOT EXISTS agg_app_service_monthly (
  agg_app_service_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start                TEXT NOT NULL,
  provider                   TEXT NOT NULL,
  application_sk             INTEGER NOT NULL REFERENCES dim_application(application_sk),
  environment                TEXT NOT NULL DEFAULT '(Unknown)',
  service_sk                 INTEGER NOT NULL,
  billed_cost                TEXT NOT NULL DEFAULT '0',
  effective_cost             TEXT NOT NULL DEFAULT '0',
  line_count                 INTEGER NOT NULL DEFAULT 0,
  refreshed_utc              TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, application_sk, environment, service_sk)
);

CREATE TABLE IF NOT EXISTS agg_app_service_resource_monthly (
  agg_app_service_resource_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start                       TEXT NOT NULL,
  provider                          TEXT NOT NULL,
  application_sk                    INTEGER NOT NULL REFERENCES dim_application(application_sk),
  environment                       TEXT NOT NULL DEFAULT '(Unknown)',
  service_sk                        INTEGER NOT NULL,
  resource_sk                       INTEGER NOT NULL REFERENCES dim_resource(resource_sk),
  billed_cost                       TEXT NOT NULL DEFAULT '0',
  effective_cost                    TEXT NOT NULL DEFAULT '0',
  line_count                        INTEGER NOT NULL DEFAULT 0,
  refreshed_utc                     TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, application_sk, environment, service_sk, resource_sk)
);

CREATE TABLE IF NOT EXISTS agg_cost_distribution_monthly (
  agg_cost_distribution_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start                      TEXT NOT NULL,
  provider                         TEXT NOT NULL,
  level_name                       TEXT NOT NULL,
  parent_key                       TEXT NULL,
  entity_count                     INTEGER NOT NULL DEFAULT 0,
  total_cost                       TEXT NOT NULL DEFAULT '0',
  min_cost                         TEXT NOT NULL DEFAULT '0',
  p50_cost                         TEXT NOT NULL DEFAULT '0',
  p75_cost                         TEXT NOT NULL DEFAULT '0',
  p90_cost                         TEXT NOT NULL DEFAULT '0',
  p95_cost                         TEXT NOT NULL DEFAULT '0',
  p99_cost                         TEXT NOT NULL DEFAULT '0',
  max_cost                         TEXT NOT NULL DEFAULT '0',
  avg_cost                         TEXT NOT NULL DEFAULT '0',
  stddev_cost                      TEXT NOT NULL DEFAULT '0',
  gini                             TEXT NOT NULL DEFAULT '0',
  cr5                              TEXT NOT NULL DEFAULT '0',
  cr10                             TEXT NOT NULL DEFAULT '0',
  cr20                             TEXT NOT NULL DEFAULT '0',
  top_10_cost_pct                  TEXT NOT NULL DEFAULT '0',
  tail_80_cost_pct                 TEXT NOT NULL DEFAULT '0',
  refreshed_utc                    TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, level_name, parent_key)
);

CREATE TABLE IF NOT EXISTS agg_cost_anomaly_monthly (
  agg_cost_anomaly_monthly_id INTEGER PRIMARY KEY AUTOINCREMENT,
  month_start                 TEXT NOT NULL,
  provider                    TEXT NOT NULL,
  entity_level                TEXT NOT NULL,
  application_sk              INTEGER NOT NULL REFERENCES dim_application(application_sk),
  service_sk                  INTEGER NULL,
  billed_cost_current         TEXT NOT NULL DEFAULT '0',
  billed_cost_avg_3m          TEXT NOT NULL DEFAULT '0',
  billed_cost_stddev_3m       TEXT NOT NULL DEFAULT '0',
  z_score                     TEXT NOT NULL DEFAULT '0',
  pct_change_vs_avg           TEXT NOT NULL DEFAULT '0',
  history_months              INTEGER NOT NULL DEFAULT 0,
  anomaly_flag                INTEGER NOT NULL DEFAULT 0,
  anomaly_type                TEXT NOT NULL DEFAULT 'NORMAL',
  refreshed_utc               TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (month_start, provider, entity_level, application_sk, service_sk)
);

CREATE INDEX IF NOT EXISTS IX_agg_cost_anomaly_month ON agg_cost_anomaly_monthly (month_start, provider, anomaly_flag);

CREATE INDEX IF NOT EXISTS IX_ingestion_batch_source ON dim_ingestion_batch (source_file, focus_version, status);
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_batch ON fact_focus_cost_daily (ingestion_batch_id);
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_date_account ON fact_focus_cost_daily (charge_date, billing_account_sk);
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_billing_period ON fact_focus_cost_daily (billing_period_start, billing_account_sk);
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_resource ON fact_focus_cost_daily (resource_sk, charge_date) WHERE resource_sk IS NOT NULL;
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_commitment ON fact_focus_cost_daily (commitment_sk, charge_date) WHERE commitment_sk IS NOT NULL;
CREATE INDEX IF NOT EXISTS IX_fact_cost_daily_service ON fact_focus_cost_daily (service_sk, charge_date);
CREATE INDEX IF NOT EXISTS IX_agg_cost_daily_charge ON agg_cost_daily (charge_date, billing_period_start, provider, sub_account_sk);
CREATE INDEX IF NOT EXISTS IX_agg_cost_monthly_month ON agg_cost_monthly (month_start, provider, sub_account_sk);
CREATE INDEX IF NOT EXISTS IX_agg_app_monthly_month ON agg_app_monthly (month_start, provider, application_sk, environment);
CREATE INDEX IF NOT EXISTS IX_agg_app_service_monthly_month ON agg_app_service_monthly (month_start, provider, application_sk, environment);

-- SECTION 6: VIEWS

DROP VIEW IF EXISTS vw_recommendations_summary;
CREATE VIEW vw_recommendations_summary AS
SELECT
  r.rec_snapshot_id, r.snapshot_month, r.recommendation_type, r.current_utilization_status,
  r.terminate_recommendation, res.name AS resource_name, res.resource_type, res.region AS resource_region,
  res.owner_email, res.environment, acc.provider, sa.sub_account_name AS account_name, svc.service_name,
  r.current_instance_type, r.current_vcpu, r.current_memory_gb,
  r.recommended_instance_type, r.recommended_vcpu, r.recommended_memory_gb,
  r.current_monthly_price, r.projected_monthly_price, r.projected_monthly_savings,
  CASE WHEN CAST(r.current_monthly_price AS REAL) > 0
    THEN (CAST(r.projected_monthly_savings AS REAL) / CAST(r.current_monthly_price AS REAL)) * 100
    ELSE 0 END AS savings_percentage,
  r.number_of_options, r.created_utc
FROM fact_recommendation_snapshot_v2 r
INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
INNER JOIN dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
INNER JOIN dim_account acc ON sa.billing_account_sk = acc.account_sk
INNER JOIN dim_service svc ON res.service_sk = svc.service_sk;

DROP VIEW IF EXISTS vw_top_savings_opportunities;
CREATE VIEW vw_top_savings_opportunities AS
SELECT
  r.snapshot_month, res.name AS resource_name, acc.provider, sa.sub_account_name AS account_name, svc.service_name,
  r.recommendation_type, r.current_instance_type, r.recommended_instance_type,
  r.current_monthly_price, r.projected_monthly_price, r.projected_monthly_savings,
  r.current_utilization_status, r.terminate_recommendation, res.owner_email, res.environment
FROM fact_recommendation_snapshot_v2 r
INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
INNER JOIN dim_sub_account sa ON res.sub_account_sk = sa.sub_account_sk
INNER JOIN dim_account acc ON sa.billing_account_sk = acc.account_sk
INNER JOIN dim_service svc ON res.service_sk = svc.service_sk
WHERE CAST(r.projected_monthly_savings AS REAL) > 0 AND res.is_excluded = 0
ORDER BY CAST(r.projected_monthly_savings AS REAL) DESC
LIMIT 100;

DROP VIEW IF EXISTS vw_pbi_cost_monthly;
CREATE VIEW vw_pbi_cost_monthly AS
SELECT a.month_start, a.month_start AS billing_period_start, a.provider, sa.sub_account_name AS account_name, a.service_category, cc.charge_category,
  a.billed_cost, a.effective_cost,
  CAST(a.billed_cost AS REAL) - CAST(a.effective_cost AS REAL) AS discount_amount,
  a.list_cost, a.contracted_cost, a.line_count, a.refreshed_utc
FROM agg_cost_monthly a
INNER JOIN dim_sub_account sa ON a.sub_account_sk = sa.sub_account_sk
INNER JOIN dim_charge_category cc ON a.charge_category_sk = cc.charge_category_sk;

DROP VIEW IF EXISTS vw_pbi_cost_daily;
CREATE VIEW vw_pbi_cost_daily AS
SELECT a.charge_date, a.billing_period_start, a.billing_period_start AS month_start,
  a.provider, sa.sub_account_name AS account_name, svc.service_name, reg.region_name,
  a.billed_cost, a.effective_cost,
  CAST(a.billed_cost AS REAL) - CAST(a.effective_cost AS REAL) AS discount_amount,
  a.list_cost, a.contracted_cost, a.line_count, a.refreshed_utc
FROM agg_cost_daily a
INNER JOIN dim_sub_account sa ON a.sub_account_sk = sa.sub_account_sk
INNER JOIN dim_service svc ON a.service_sk = svc.service_sk
LEFT JOIN dim_region reg ON a.region_sk = reg.region_sk;

DROP VIEW IF EXISTS vw_pbi_cost_by_tag;
CREATE VIEW vw_pbi_cost_by_tag AS
SELECT month_start, provider, tag_key, tag_value, billed_cost, effective_cost, line_count, refreshed_utc
FROM agg_cost_by_tag;

DROP VIEW IF EXISTS vw_dim_application;
CREATE VIEW vw_dim_application AS
SELECT application_sk, application_name, alias_values, first_seen_date,
  application_sk || ';' || application_name || ';' || COALESCE(alias_values, '') AS application_summary,
  created_utc, updated_utc
FROM dim_application;

DROP VIEW IF EXISTS vw_pbi_app_monthly;
CREATE VIEW vw_pbi_app_monthly AS
SELECT a.month_start, a.provider, app.application_sk, app.application_name, app.alias_values, app.first_seen_date,
  app.application_sk || ';' || app.application_name || ';' || COALESCE(app.alias_values, '') AS application_summary,
  a.environment, a.billed_cost, a.effective_cost,
  CAST(a.billed_cost AS REAL) - CAST(a.effective_cost AS REAL) AS discount_amount,
  a.line_count, a.refreshed_utc
FROM agg_app_monthly a
INNER JOIN dim_application app ON a.application_sk = app.application_sk;

DROP VIEW IF EXISTS vw_pbi_app_service_monthly;
CREATE VIEW vw_pbi_app_service_monthly AS
SELECT a.month_start, a.provider, app.application_sk, app.application_name, app.alias_values,
  a.environment, svc.service_name, svc.service_category,
  a.billed_cost, a.effective_cost,
  CAST(a.billed_cost AS REAL) - CAST(a.effective_cost AS REAL) AS discount_amount,
  a.line_count, a.refreshed_utc
FROM agg_app_service_monthly a
INNER JOIN dim_application app ON a.application_sk = app.application_sk
INNER JOIN dim_service svc ON a.service_sk = svc.service_sk;

DROP VIEW IF EXISTS vw_pbi_cost_anomaly_monthly;
CREATE VIEW vw_pbi_cost_anomaly_monthly AS
SELECT a.month_start, a.month_start AS billing_period_start, a.provider, a.entity_level,
  a.application_sk, app.application_name, app.alias_values,
  a.service_sk, svc.service_name,
  a.billed_cost_current, a.billed_cost_avg_3m, a.billed_cost_stddev_3m,
  a.z_score, a.pct_change_vs_avg, a.history_months, a.anomaly_flag, a.anomaly_type, a.refreshed_utc
FROM agg_cost_anomaly_monthly a
INNER JOIN dim_application app ON a.application_sk = app.application_sk
LEFT JOIN dim_service svc ON a.service_sk = svc.service_sk;

DROP VIEW IF EXISTS vw_pbi_cost_distribution_monthly;
CREATE VIEW vw_pbi_cost_distribution_monthly AS
SELECT month_start, month_start AS billing_period_start, provider, level_name, parent_key,
  entity_count, total_cost, min_cost, p50_cost, p75_cost, p90_cost, p95_cost, p99_cost,
  max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, top_10_cost_pct, tail_80_cost_pct, refreshed_utc
FROM agg_cost_distribution_monthly;

DROP VIEW IF EXISTS vw_pbi_commitment_utilization;
CREATE VIEW vw_pbi_commitment_utilization AS
SELECT a.month_start, a.provider, c.commitment_discount_name, c.commitment_discount_type,
  a.commitment_status, a.effective_cost, a.commitment_quantity, a.line_count, a.refreshed_utc
FROM agg_commitment_utilization a
INNER JOIN dim_commitment_discount c ON a.commitment_sk = c.commitment_sk;

DROP VIEW IF EXISTS vw_pbi_savings_summary;
CREATE VIEW vw_pbi_savings_summary AS
SELECT a.month_start, a.provider, svc.service_name, a.total_effective_cost,
  a.total_projected_savings, a.recommendation_count, a.refreshed_utc
FROM agg_savings_summary a
INNER JOIN dim_service svc ON a.service_sk = svc.service_sk;
