```go
go build -o focus-ingest.exe ./cmd/focus-ingest
.\focus-ingest.exe schema apply --connection "<conn>"   # adds aggregates_status column

# One-time after upgrade (or if aggs are inconsistent):
.\focus-ingest.exe rebuild --aggregates --full --connection "<conn>"
.\focus-ingest.exe rebuild --tags --connection "<conn>"

# Daily export (new month file):
.\focus-ingest.exe import --connection "<conn>" --skip-tags --skip-aggregates daily.parquet
.\focus-ingest.exe rebuild --tags --connection "<conn>"      # set-based on SQL Server
.\focus-ingest.exe rebuild --aggregates --connection "<conn>"  # only the new month(s)
```