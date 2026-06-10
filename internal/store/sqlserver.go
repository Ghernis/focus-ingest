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
}

func OpenSQLServer(connection string, skipTags bool, skipAggregates bool) (Store, error) {
	db, err := sql.Open("sqlserver", connection)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql server ping: %w", err)
	}
	return &sqlserverStore{db: db, skipTags: skipTags, skipAggregates: skipAggregates}, nil
}

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

	for _, r := range rows {
		_, err = tx.ExecContext(ctx, `
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
			) VALUES (
			  @p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,
			  @p20,@p21,@p22,@p23,@p24,@p25,@p26,@p27,@p28,@p29,@p30,@p31,@p32,@p33,@p34,@p35,@p36,
			  @p37,@p38,@p39,@p40,@p41,@p42,@p43,@p44,@p45,@p46,@p47,@p48,@p49,@p50,@p51,@p52,@p53,@p54,@p55,@p56,@p57
			)`,
			batchID, r.SourceProvider, focusVersion, sourceFile,
			nullStr(r.BilledCost), nullStr(r.BillingAccountId), nullStr(r.BillingAccountName), nullStr(r.BillingAccountType), nullStr(r.BillingCurrency),
			nullStr(r.BillingPeriodEnd), nullStr(r.BillingPeriodStart), nullStr(r.CapacityReservationId), nullStr(r.CapacityReservationStatus),
			nullStr(r.ChargeCategory), nullStr(r.ChargeClass), nullStr(r.ChargeDescription), nullStr(r.ChargeFrequency), nullStr(r.ChargePeriodEnd), nullStr(r.ChargePeriodStart),
			nullStr(r.CommitmentDiscountCategory), nullStr(r.CommitmentDiscountId), nullStr(r.CommitmentDiscountName), nullStr(r.CommitmentDiscountQuantity),
			nullStr(r.CommitmentDiscountStatus), nullStr(r.CommitmentDiscountType), nullStr(r.CommitmentDiscountUnit),
			nullStr(r.ConsumedQuantity), nullStr(r.ConsumedUnit), nullStr(r.ContractedCost), nullStr(r.ContractedUnitPrice), nullStr(r.EffectiveCost),
			nullStr(r.InvoiceId), nullStr(r.InvoiceIssuer), nullStr(r.ListCost), nullStr(r.ListUnitPrice), nullStr(r.PricingCategory), nullStr(r.PricingCurrency),
			nullStr(r.PricingQuantity), nullStr(r.PricingUnit), nullStr(r.Provider), nullStr(r.Publisher), nullStr(r.RegionId), nullStr(r.RegionName),
			nullStr(r.ResourceId), nullStr(r.ResourceName), nullStr(r.ResourceType), nullStr(r.ServiceCategory), nullStr(r.ServiceName), nullStr(r.ServiceSubcategory),
			nullStr(r.SkuId), nullStr(r.SkuMeter), nullStr(r.SkuPriceDetails), nullStr(r.SkuPriceId), nullStr(r.SubAccountId), nullStr(r.SubAccountName), nullStr(r.SubAccountType),
			nullStr(r.RawTagsJSON),
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlserverStore) ProcessBatch(ctx context.Context, batchID int64, focusVersion string) error {
	p := &etl.Processor{DB: s.db, Dialect: "sqlserver", SkipTags: s.skipTags, SkipAggregates: s.skipAggregates}
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
