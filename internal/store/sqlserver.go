package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/focus"
	"github.com/ghernis/focus_dt/internal/schema"

	_ "github.com/microsoft/go-mssqldb"
)

type sqlserverStore struct {
	db             *sql.DB
	skipTags       bool
	skipAggregates bool
	useGoETL       bool
}

func OpenSQLServer(connection string, skipTags, skipAggregates, useGoETL bool) (Store, error) {
	db, err := sql.Open("sqlserver", connection)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql server ping: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return &sqlserverStore{db: db, skipTags: skipTags, skipAggregates: skipAggregates, useGoETL: useGoETL}, nil
}

const stgInsertCols = 57

const stgInsertPrefixSQL = `
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
			) VALUES `

func (s *sqlserverStore) Dialect() string { return "sqlserver" }

func (s *sqlserverStore) Close() error { return s.db.Close() }

func (s *sqlserverStore) ApplySchema(ctx context.Context) error {
	for i, batch := range splitOnGO(schema.SQLServerDDL) {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, batch); err != nil {
			snippet := batch
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			return fmt.Errorf("schema apply batch %d: %w\nbatch start: %s", i+1, err, snippet)
		}
	}
	return nil
}

func splitOnGO(script string) []string {
	var batches []string
	var b strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(strings.ToUpper(line)) == "GO" {
			batches = append(batches, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if tail := strings.TrimSpace(b.String()); tail != "" {
		batches = append(batches, tail)
	}
	return batches
}

func (s *sqlserverStore) BeginBatch(ctx context.Context, meta BatchMeta) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO dim_ingestion_batch (source_provider, focus_version, source_file, status)
		OUTPUT INSERTED.ingestion_batch_id
		VALUES (@p1, @p2, @p3, 'LOADING')`, meta.SourceProvider, meta.FocusVersion, meta.SourceFile).Scan(&id)
	return id, err
}

func (s *sqlserverStore) InsertStaging(ctx context.Context, batchID int64, focusVersion, sourceFile string, rows []focus.StagingRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	buf := make([][]interface{}, len(rows))
	for i, r := range rows {
		buf[i] = stagingRowArgs(batchID, focusVersion, sourceFile, r)
	}
	if err := execSQLServerMultiInsert(ctx, tx, stgInsertPrefixSQL, stgInsertCols, buf); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlserverStore) ProcessBatch(ctx context.Context, batchID int64, focusVersion string) error {
	p := &etl.Processor{
		DB: s.db, Dialect: "sqlserver",
		SkipTags: s.skipTags, SkipAggregates: s.skipAggregates, UseGoETL: s.useGoETL,
	}
	return p.ProcessBatch(ctx, batchID, focusVersion)
}

func (s *sqlserverStore) Validate(ctx context.Context, batchID int64) (ValidationReport, error) {
	return validateBatch(ctx, s.db, batchID)
}

func (s *sqlserverStore) FindCompletedImport(ctx context.Context, sourceFile, focusVersion string) (int64, bool, error) {
	return findCompletedImport(ctx, s.db, "sqlserver", sourceFile, focusVersion)
}

func (s *sqlserverStore) PurgeImport(ctx context.Context, batchID int64) error {
	return purgeBatch(ctx, s.db, "sqlserver", batchID)
}

func (s *sqlserverStore) MarkBatchFailed(ctx context.Context, batchID int64) error {
	return MarkBatchFailed(ctx, s.db, "sqlserver", batchID)
}

func (s *sqlserverStore) PurgeStaleLoading(ctx context.Context, sourceFile, focusVersion string) (int, error) {
	return PurgeLoadingBatchesForFile(ctx, s.db, "sqlserver", sourceFile, focusVersion)
}

func (s *sqlserverStore) RebuildAggregates(ctx context.Context) error {
	return (&etl.Processor{DB: s.db, Dialect: "sqlserver"}).RebuildAggregates(ctx)
}

func (s *sqlserverStore) RebuildTags(ctx context.Context) error {
	return (&etl.Processor{DB: s.db, Dialect: "sqlserver"}).RebuildTagsAll(ctx)
}
