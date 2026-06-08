# Power BI Data Model — FOCUS Data Warehouse

Connect Power BI Desktop to Azure SQL using **Import** mode on aggregate views (recommended) or **DirectQuery** on `vw_pbi_*` views for larger datasets.

## Tables to import

| Table / View | Role | Notes |
|--------------|------|-------|
| `dim_date` | Dimension | Mark as date table on `full_date` |
| `vw_pbi_cost_monthly` | Fact (agg) | Executive spend by month |
| `vw_pbi_cost_daily` | Fact (agg) | Trend / drill-down |
| `vw_pbi_cost_by_tag` | Fact (agg) | Allocation by application, env, BU |
| `vw_pbi_commitment_utilization` | Fact (agg) | RI / Savings Plan Used vs Unused |
| `vw_pbi_savings_summary` | Fact (agg) | Spend vs recommendation savings |
| `vw_recommendations_summary` | Fact | Rightsizing / optimization detail |
| `vw_top_savings_opportunities` | Fact | Top 100 savings (or query underlying table) |
| `dim_application` / `vw_dim_application` | Dimension | Normalized applications + comma-separated aliases |
| `dim_account` | Dimension | Billing accounts |
| `dim_service` | Dimension | Service names and categories |
| `dim_region` | Dimension | Regions |
| `dim_commitment_discount` | Dimension | Commitment names and types |

Optional detail: `fact_focus_cost_daily` for resource-level drill-through (large; use DirectQuery or aggregated import).

## Relationships (many → one)

Set **cross-filter direction: single** from fact → dimension.

```
vw_pbi_app_monthly[application_sk]        → dim_application[application_sk]
vw_pbi_app_service_monthly[application_sk] → dim_application[application_sk]
vw_pbi_cost_monthly[month_start]          → dim_date[full_date]
vw_pbi_cost_daily[charge_date]            → dim_date[full_date] (usage date)
vw_pbi_cost_daily[billing_period_start] → dim_date[full_date] (billing period; use second dim_date copy)
vw_pbi_cost_monthly[account via join]     → dim_account[account_name]  (or use billing_account_sk if importing base agg table)
vw_pbi_cost_daily[service_name]           → dim_service[service_name]
vw_pbi_commitment_utilization[...]        → dim_commitment_discount[commitment_discount_name]
vw_recommendations_summary[resource]      → dim_resource (via resource_name / resource_sk if imported)
fact_focus_cost_daily[resource_sk]        → dim_resource[resource_sk]
dim_resource[account_sk]                  → dim_account[account_sk]
dim_resource[service_sk]                  → dim_service[service_sk]
bridge_cost_tag[cost_daily_id]            → fact_focus_cost_daily[cost_daily_id]
bridge_cost_tag[tag_sk]                   → dim_tag[tag_sk]
```

### Role-playing dates

- **Charge date** — when usage occurred (`charge_date` / `ChargePeriodStart` day)
- **Billing period** — invoice window (`billing_period_start` / `month_start` on monthly aggs)

Monthly aggregate `month_start` stores the actual `billing_period_start` from FOCUS (not normalized to the 1st). Filter in Power BI when you only want calendar-month billings, e.g. `DAY(billing_period_start) = 1`.

Use two copies of `dim_date` in Power BI (e.g. `dim_date_charge`, `dim_date_billing`) or inactive relationships.

## Recommended measures (DAX)

```dax
Total Effective Cost = SUM ( vw_pbi_cost_monthly[effective_cost] )
Total Billed Cost    = SUM ( vw_pbi_cost_monthly[billed_cost] )
Cost Delta List vs Effective = [Total List Cost] - [Total Effective Cost]
Total Projected Savings = SUM ( vw_pbi_savings_summary[total_projected_savings] )
Commitment Waste = CALCULATE (
    SUM ( vw_pbi_commitment_utilization[effective_cost] ),
    vw_pbi_commitment_utilization[commitment_status] = "Unused"
)
```

## Dashboard pages

1. **Multi-cloud spend** — `vw_pbi_cost_monthly` by provider, service_category, charge_category
2. **Effective vs billed** — compare four cost columns from monthly or daily agg
3. **Commitment utilization** — stacked bar Used vs Unused from `vw_pbi_commitment_utilization`
4. **Savings opportunities** — `vw_top_savings_opportunities` + spend by `dim_resource`
5. **Tag allocation** — `vw_pbi_cost_by_tag` slicers on tag_key (application, environment, business_unit)

## Refresh order (Azure Data Factory or SQL Agent)

1. Load FOCUS export → `stg_focus_cost_line` (`01_load_staging.sql` or `load_sample.py`)
2. Run `02_migrate_dims_and_daily.sql`
3. Run `03_validate.sql`
4. Refresh Power BI dataset

## Joining recommendations to cost

Both share **`dim_resource`**. In Power BI:

- Import `dim_resource` with `global_resource_id`, `application`, `environment`
- Relate `vw_recommendations_summary` and cost tables through `resource_sk` or resource name
- Compare `projected_monthly_savings` to monthly `effective_cost` by resource/account
