```powershell
go build -o focus-ingest.exe ./cmd/focus-ingest

# Hybrid local ETL + SQL Server publish (recommended for Azure DTU limits)

# Historical backfill (aggregates + dims only; no detailed facts on server):
foreach ($month in $months) {
  .\focus-ingest.exe sync-dims --local --connection "<conn>" --sqlite-path "focus_$month.db" --fresh
  .\focus-ingest.exe import --local --sqlite-path "focus_$month.db" --skip-tags --skip-aggregates $file
  .\focus-ingest.exe rebuild --local --sqlite-path "focus_$month.db" --tags --aggregates --full
  .\focus-ingest.exe publish --connection "<conn>" --sqlite-path "focus_$month.db" --billing-period $month
}

# Daily / current month (include detailed facts on server):
.\focus-ingest.exe sync-dims --local --connection "<conn>" --sqlite-path focus_current.db
.\focus-ingest.exe import --local --sqlite-path focus_current.db --skip-tags --skip-aggregates daily.parquet
.\focus-ingest.exe rebuild --local --sqlite-path focus_current.db --tags --aggregates
.\focus-ingest.exe publish --connection "<conn>" --sqlite-path focus_current.db --billing-period 2026-06-01 --facts

# Direct SQL Server import (legacy; heavy on DTU):
.\focus-ingest.exe schema apply --connection "<conn>"
.\focus-ingest.exe import --connection "<conn>" --skip-tags --skip-aggregates daily.parquet
.\focus-ingest.exe rebuild --tags --connection "<conn>"
.\focus-ingest.exe rebuild --aggregates --connection "<conn>"
```

`import --local` auto-runs `sync-dims --fresh` when the local database has no dimensions (requires `--connection` or `FOCUS_DATABASE_URL`).
