-- Tag bridge (requires #stg_norm from process_batch_sqlserver_core.sql in same transaction)
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
