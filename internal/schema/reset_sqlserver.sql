-- Reset FOCUS warehouse on Azure SQL (drops all dbo views/tables).
-- Run once, then: focus-ingest schema apply --connection "<conn>"
--
-- SQL Server has no DROP SCHEMA CASCADE. Drop views, foreign keys, then tables.

DECLARE @sql NVARCHAR(MAX) = N'';

SELECT @sql = @sql + N'DROP VIEW ' + QUOTENAME(SCHEMA_NAME(v.schema_id)) + N'.' + QUOTENAME(v.name) + N';'
FROM sys.views v
WHERE SCHEMA_NAME(v.schema_id) = N'dbo';

IF @sql <> N'' EXEC sp_executesql @sql;
SET @sql = N'';

SELECT @sql = @sql + N'ALTER TABLE ' + QUOTENAME(OBJECT_SCHEMA_NAME(f.parent_object_id)) + N'.' + QUOTENAME(OBJECT_NAME(f.parent_object_id))
    + N' DROP CONSTRAINT ' + QUOTENAME(f.name) + N';'
FROM sys.foreign_keys f
WHERE OBJECT_SCHEMA_NAME(f.parent_object_id) = N'dbo';

IF @sql <> N'' EXEC sp_executesql @sql;
SET @sql = N'';

SELECT @sql = @sql + N'DROP TABLE IF EXISTS ' + QUOTENAME(s.name) + N'.' + QUOTENAME(t.name) + N';'
FROM sys.tables t
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE s.name = N'dbo';

IF @sql <> N'' EXEC sp_executesql @sql;

PRINT 'All dbo tables dropped. Run: focus-ingest schema apply --connection "<conn>"';
