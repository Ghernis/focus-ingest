# E2E fixtures — focus-ingest

Known test inputs for this repo. Files marked **gitignored** must be supplied locally or documented here after first use.

## Inventory

| Fixture | Path | Status | Used for |
|---------|------|--------|----------|
| VM staging sample | `stg_example_vm.csv` | gitignored | Azure VM month (`slpazrusadm03`, B2ms Compute Hour lines, billing month `2026-01-01`) |
| SKU catalog | `ms_skus.json` | gitignored | SKU/tier rule validation against real Azure meter shapes |
| Sample cost CSV | `focus_sample_*.csv` | gitignored | General ingest smoke tests |
| Query exports | `query_results.csv` | gitignored | Ad-hoc validation exports |

## How to obtain

### `stg_example_vm.csv`

Export from staging warehouse for one VM and one billing month:

```sql
SELECT * FROM stg_focus_cost_line
WHERE ResourceId LIKE '%slpazrusadm03%'
  AND BillingPeriodStart >= '2026-01-01'
  AND BillingPeriodStart < '2026-02-01';
```

Save as CSV in repo root. Expected: ~156 lines, ~40 `Compute Hour` / `B2ms` rows for tier E2E.

### `ms_skus.json`

Azure retail/sku reference export used to validate `dim_sku` / tier regex rules. Keep local; do not commit if large or licensed.

### Minimal synthetic alternative

When fixtures are unavailable, use programmatic rows like `internal/etl/tier_integration_test.go` (`importAzureVMRow`, `importAzureAppServiceRow`). Sufficient for tier logic; **not** sufficient for parquet ingest path or SQL Server bulk ETL.

## Per-scenario checklist

When adding a new feature, extend this table:

| Scenario | Minimum fixture | Key assertion |
|----------|-----------------|---------------|
| VM tier daily | B2ms + Compute Hour lines | `fact_resource_tier_daily.tier_code = 'B2ms'` |
| VM tier MoM | Same `sku_id`, different `sku_meter` across months | `agg_resource_tier_change_monthly` count = 1 |
| VM tier intramonth | Two meters same month, same resource | `agg_resource_tier_change_intramonth` count = 1 |
| App Service tier | App Service Hour lines only (no data transfer noise) | intramonth change ignores non-tier meters |
| Rebuild performance | One billing month on `--local` | rebuild completes in < 30s |

## Run target

| Target | Command | When |
|--------|---------|------|
| Local SQLite (default) | `focus-ingest --local ...` | Fast E2E, tier/aggs validation |
| Azure SQL | `focus-ingest --connection ...` | SQL Server dialect, bulk ETL, publish path |

Document which target was used in the E2E report.
