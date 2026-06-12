package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/focus"
	"github.com/ghernis/focus_dt/internal/schema"
)

type sqliteStore struct {
	db             *sql.DB
	skipTags       bool
	skipAggregates bool
}

func OpenSQLite(path string, skipTags bool, skipAggregates bool) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// Prevent SQLITE_BUSY from connection pool contention.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	_, _ = db.Exec(`PRAGMA busy_timeout = 5000`)
	// Pragmas for faster local ingest. These trade durability for speed (fine for dev).
	_, _ = db.Exec(`PRAGMA synchronous = NORMAL`)
	_, _ = db.Exec(`PRAGMA temp_store = MEMORY`)
	_, _ = db.Exec(`PRAGMA cache_size = -200000`) // ~200MB
	return &sqliteStore{db: db, skipTags: skipTags, skipAggregates: skipAggregates}, nil
}

func (s *sqliteStore) Dialect() string { return "sqlite" }

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) ApplySchema(ctx context.Context) error {
	return execSQLScript(ctx, s.db, schema.SQLiteDDL)
}

func (s *sqliteStore) BeginBatch(ctx context.Context, meta BatchMeta) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO dim_ingestion_batch (source_provider, focus_version, source_file, status)
		VALUES (?, ?, ?, 'LOADING')`, meta.SourceProvider, meta.FocusVersion, meta.SourceFile)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *sqliteStore) InsertStaging(ctx context.Context, batchID int64, focusVersion, sourceFile string, rows []focus.StagingRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sqlText := `
		INSERT INTO stg_focus_cost_line (
		  ingestion_batch_id, source_provider, focus_version, source_file,
		  BilledCost, BillingAccountId, BillingAccountName, BillingAccountType, BillingCurrency,
		  BillingPeriodEnd, BillingPeriodStart, CapacityReservationId, CapacityReservationStatus,
		  ChargeCategory, ChargeClass, ChargeDescription, ChargeFrequency, ChargePeriodEnd, ChargePeriodStart,
		  CommitmentDiscountCategory, CommitmentDiscountId, CommitmentDiscountName, CommitmentDiscountQuantity,
		  CommitmentDiscountStatus, CommitmentDiscountType, CommitmentDiscountUnit,
		  ConsumedQuantity, ConsumedUnit, ContractedCost, ContractedUnitPrice, EffectiveCost,
		  InvoiceId, InvoiceIssuer, ListCost, ListUnitPrice, PricingCategory, PricingCurrency,
		  PricingQuantity, PricingUnit, Provider, Publisher, RegionId, RegionName,
		  ResourceId, ResourceName, ResourceType, ServiceCategory, ServiceName, ServiceSubcategory,
		  SkuId, SkuMeter, SkuPriceDetails, SkuPriceId, SubAccountId, SubAccountName, SubAccountType,
		  raw_tags_json
		) VALUES (` + strings.Repeat("?,", 56) + `?)`
	stmt, err := tx.PrepareContext(ctx, sqlText)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range rows {
		args := stagingRowArgs(batchID, focusVersion, sourceFile, r)
		_, err = stmt.ExecContext(ctx, args...)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ProcessBatch(ctx context.Context, batchID int64, focusVersion string) error {
	p := &etl.Processor{
		DB:               s.db,
		Dialect:          "sqlite",
		SkipTags:         s.skipTags,
		SkipAggregates:   s.skipAggregates,
		TrackPendingDims: true,
	}
	return p.ProcessBatch(ctx, batchID, focusVersion)
}

func (s *sqliteStore) GetBatchInfo(ctx context.Context, batchID int64) (BatchInfo, error) {
	return GetBatchInfo(ctx, s.db, "sqlite", batchID)
}

func (s *sqliteStore) Validate(ctx context.Context, batchID int64) (ValidationReport, error) {
	return validateBatch(ctx, s.db, batchID)
}

func (s *sqliteStore) FindCompletedImport(ctx context.Context, sourceFile, focusVersion string) (int64, bool, error) {
	return findCompletedImport(ctx, s.db, "sqlite", sourceFile, focusVersion)
}

func (s *sqliteStore) PurgeImport(ctx context.Context, batchID int64) error {
	return purgeBatch(ctx, s.db, "sqlite", batchID)
}

func (s *sqliteStore) MarkBatchFailed(ctx context.Context, batchID int64) error {
	return MarkBatchFailed(ctx, s.db, "sqlite", batchID)
}

func (s *sqliteStore) PurgeStaleLoading(ctx context.Context, sourceFile, focusVersion string) (int, error) {
	return PurgeLoadingBatchesForFile(ctx, s.db, "sqlite", sourceFile, focusVersion)
}

func (s *sqliteStore) RebuildAggregates(ctx context.Context, full bool) (int, error) {
	return (&etl.Processor{DB: s.db, Dialect: "sqlite"}).RebuildAggregates(ctx, full)
}

func (s *sqliteStore) RebuildTags(ctx context.Context) error {
	return (&etl.Processor{DB: s.db, Dialect: "sqlite"}).RebuildTagsAll(ctx)
}

func execSQLScript(ctx context.Context, db *sql.DB, script string) error {
	for _, stmt := range splitSQL(script) {
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("schema apply failed: %w\nstatement: %.120s...", err, stmt)
		}
	}
	return nil
}

// SplitSQL splits a DDL script on semicolon-terminated statements.
func SplitSQL(script string) []string {
	return splitSQL(script)
}

func splitSQL(script string) []string {
	var out []string
	var b strings.Builder
	lines := strings.Split(script, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
		if strings.HasSuffix(trim, ";") {
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		}
	}
	if tail := strings.TrimSpace(b.String()); tail != "" {
		out = append(out, tail)
	}
	return out
}

func nullStr(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
