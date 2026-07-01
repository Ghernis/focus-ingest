-- Reset FOCUS warehouse on Azure SQL (drops all dbo tables + views).
-- Run once, then: focus-ingest schema apply --connection "<conn>"
--
-- SQL Server has no DROP SCHEMA CASCADE; this disables FKs and drops objects.

IF OBJECT_ID(N'dbo.vw_pbi_cost_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_cost_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_cost_daily', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_cost_daily;
IF OBJECT_ID(N'dbo.vw_pbi_cost_by_tag', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_cost_by_tag;
IF OBJECT_ID(N'dbo.vw_pbi_app_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_app_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_app_service_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_app_service_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_cost_distribution_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_cost_distribution_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_cost_anomaly_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_cost_anomaly_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_commitment_utilization', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_commitment_utilization;
IF OBJECT_ID(N'dbo.vw_pbi_savings_summary', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_savings_summary;
IF OBJECT_ID(N'dbo.vw_pbi_tier_change_resource_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_tier_change_resource_monthly;
IF OBJECT_ID(N'dbo.vw_pbi_tier_change_intramonth', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_tier_change_intramonth;
IF OBJECT_ID(N'dbo.vw_pbi_tier_change_summary_monthly', N'V') IS NOT NULL DROP VIEW dbo.vw_pbi_tier_change_summary_monthly;

DECLARE @sql NVARCHAR(MAX) = N'';

SELECT @sql = @sql + N'ALTER TABLE ' + QUOTENAME(s.name) + N'.' + QUOTENAME(t.name) + N' NOCHECK CONSTRAINT ALL;'
FROM sys.tables t
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE s.name = N'dbo';

EXEC sp_executesql @sql;
SET @sql = N'';

SELECT @sql = @sql + N'DROP TABLE IF EXISTS ' + QUOTENAME(s.name) + N'.' + QUOTENAME(t.name) + N';'
FROM sys.tables t
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE s.name = N'dbo'
ORDER BY t.name;

EXEC sp_executesql @sql;

PRINT 'All dbo tables dropped. Run: focus-ingest schema apply --connection "<conn>"';
