-- =====================================================================
-- ETL Step 3: Post-load validation queries
-- Run after 02_migrate_dims_and_daily.sql
-- =====================================================================

DECLARE @IngestionBatchId BIGINT = 1;  -- SET THIS

PRINT '--- Batch status ---';
SELECT * FROM dbo.dim_ingestion_batch WHERE ingestion_batch_id = @IngestionBatchId;

PRINT '--- Row counts ---';
SELECT 'stg_focus_cost_line' AS tbl, COUNT(*) AS cnt FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @IngestionBatchId
UNION ALL SELECT 'fact_focus_cost_daily', COUNT(*) FROM dbo.fact_focus_cost_daily WHERE ingestion_batch_id = @IngestionBatchId
UNION ALL SELECT 'bridge_cost_tag', COUNT(*) FROM dbo.bridge_cost_tag b
  INNER JOIN dbo.fact_focus_cost_daily f ON b.cost_daily_id = f.cost_daily_id WHERE f.ingestion_batch_id = @IngestionBatchId
UNION ALL SELECT 'dim_commitment_discount', COUNT(*) FROM dbo.dim_commitment_discount
UNION ALL SELECT 'agg_cost_monthly', COUNT(*) FROM dbo.agg_cost_monthly
UNION ALL SELECT 'agg_commitment_utilization', COUNT(*) FROM dbo.agg_commitment_utilization;

PRINT '--- Provider spend (effective cost) ---';
SELECT a.provider, SUM(f.effective_cost) AS total_effective_cost, SUM(f.line_count) AS source_lines
FROM dbo.fact_focus_cost_daily f
INNER JOIN dbo.dim_account a ON f.billing_account_sk = a.account_sk
WHERE f.ingestion_batch_id = @IngestionBatchId
GROUP BY a.provider
ORDER BY total_effective_cost DESC;

PRINT '--- Commitment utilization (expect Used/Unused) ---';
SELECT commitment_status, SUM(effective_cost) AS effective_cost, SUM(line_count) AS lines
FROM dbo.agg_commitment_utilization
GROUP BY commitment_status;

PRINT '--- Top tags by effective cost ---';
SELECT TOP 20 tag_key, tag_value, effective_cost, line_count
FROM dbo.agg_cost_by_tag
ORDER BY effective_cost DESC;

PRINT '--- FK integrity ---';
SELECT fk.name, OBJECT_NAME(fk.parent_object_id) AS child_table, OBJECT_NAME(fk.referenced_object_id) AS parent_table
FROM sys.foreign_keys fk
WHERE OBJECT_NAME(fk.parent_object_id) IN (
  'fact_focus_cost_daily', 'fact_recommendation_snapshot_v2', 'bridge_recommendation_ritm',
  'bridge_cost_tag', 'fact_recommendation_metrics', 'fact_recommendation_options'
)
ORDER BY child_table;

PRINT '--- Orphan check: daily facts without account ---';
SELECT COUNT(*) AS orphan_count
FROM dbo.fact_focus_cost_daily f
LEFT JOIN dbo.dim_account a ON f.billing_account_sk = a.account_sk
WHERE f.ingestion_batch_id = @IngestionBatchId AND a.account_sk IS NULL;

-- Sample expects ~243 commitment staging rows → non-zero agg_commitment_utilization
-- Sample expects tag keys application, environment, business_unit in agg_cost_by_tag
