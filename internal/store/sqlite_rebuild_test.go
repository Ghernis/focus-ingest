package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

func TestSQLiteRebuildTags_NoDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "dw.db")
	s, err := OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	batchID, err := s.BeginBatch(ctx, BatchMeta{
		SourceProvider: "aws",
		FocusVersion:   "1.0",
		SourceFile:     "test.parquet",
	})
	if err != nil {
		t.Fatal(err)
	}

	tags := `{"Application":"billing","Environment":"prod"}`
	row := focus.StagingRow{
		SourceProvider:     "aws",
		Provider:           strPtr("Amazon Web Services"),
		BillingAccountId:   strPtr("123456789012"),
		BillingAccountName: strPtr("main"),
		SubAccountId:       strPtr("123456789012"),
		ChargePeriodStart:  strPtr("2024-01-15T00:00:00Z"),
		ChargePeriodEnd:    strPtr("2024-01-16T00:00:00Z"),
		BillingPeriodStart: strPtr("2024-01-01"),
		BillingPeriodEnd:   strPtr("2024-01-31"),
		ChargeCategory:     strPtr("Usage"),
		PricingCategory:    strPtr("OnDemand"),
		ChargeDescription:  strPtr("EC2 instance"),
		ServiceName:        strPtr("Amazon Elastic Compute Cloud"),
		BilledCost:         strPtr("1.00"),
		EffectiveCost:      strPtr("1.00"),
		RawTagsJSON:        &tags,
	}
	if err := s.InsertStaging(ctx, batchID, "1.0", "test.parquet", []focus.StagingRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batchID, "1.0"); err != nil {
		t.Fatal(err)
	}

	if err := s.RebuildTags(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteRebuildAggregates_DistributionInsert(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "dw.db")
	s, err := OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	batchID, err := s.BeginBatch(ctx, BatchMeta{
		SourceProvider: "aws",
		FocusVersion:   "1.0",
		SourceFile:     "test.parquet",
	})
	if err != nil {
		t.Fatal(err)
	}

	tags := `{"Application":"INS_AZ_OPENIA PRO","Environment":"prod"}`
	row := focus.StagingRow{
		SourceProvider:     "aws",
		Provider:           strPtr("Amazon Web Services"),
		BillingAccountId:   strPtr("123456789012"),
		SubAccountId:       strPtr("123456789012"),
		ChargePeriodStart:  strPtr("2024-01-15T00:00:00Z"),
		ChargePeriodEnd:    strPtr("2024-01-16T00:00:00Z"),
		BillingPeriodStart: strPtr("2024-01-01"),
		BillingPeriodEnd:   strPtr("2024-01-31"),
		ChargeCategory:     strPtr("Usage"),
		ServiceName:        strPtr("Amazon Elastic Compute Cloud"),
		BilledCost:         strPtr("10.00"),
		EffectiveCost:      strPtr("10.00"),
		RawTagsJSON:        &tags,
	}
	if err := s.InsertStaging(ctx, batchID, "1.0", "test.parquet", []focus.StagingRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batchID, "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.RebuildTags(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.RebuildAggregates(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteForceReimport_NoDuplicateFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "dw.db")
	s, err := OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	sourceFile := "monthly_export.parquet"
	row := focus.StagingRow{
		SourceProvider:     "aws",
		Provider:           strPtr("Amazon Web Services"),
		BillingAccountId:   strPtr("123456789012"),
		SubAccountId:       strPtr("123456789012"),
		ChargePeriodStart:  strPtr("2024-01-15T00:00:00Z"),
		ChargePeriodEnd:    strPtr("2024-01-16T00:00:00Z"),
		BillingPeriodStart: strPtr("2024-01-01"),
		BillingPeriodEnd:   strPtr("2024-01-31"),
		ChargeCategory:     strPtr("Usage"),
		ServiceName:        strPtr("Amazon Elastic Compute Cloud"),
		BilledCost:         strPtr("42.00"),
		EffectiveCost:      strPtr("40.00"),
	}

	importOnce := func() int64 {
		t.Helper()
		batchID, err := s.BeginBatch(ctx, BatchMeta{
			SourceProvider: "aws",
			FocusVersion:   "1.0",
			SourceFile:     sourceFile,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.InsertStaging(ctx, batchID, "1.0", sourceFile, []focus.StagingRow{row}); err != nil {
			t.Fatal(err)
		}
		if err := s.ProcessBatch(ctx, batchID, "1.0"); err != nil {
			t.Fatal(err)
		}
		return batchID
	}

	firstID := importOnce()
	prevID, found, err := s.FindCompletedImport(ctx, sourceFile, "1.0")
	if err != nil || !found || prevID != firstID {
		t.Fatalf("FindCompletedImport: id=%d found=%v err=%v", prevID, found, err)
	}

	if err := s.PurgeImport(ctx, prevID); err != nil {
		t.Fatal(err)
	}
	secondID := importOnce()
	if secondID == firstID {
		t.Fatalf("expected new batch id after purge, got %d", secondID)
	}

	var factCount int
	if err := s.(*sqliteStore).db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_focus_cost_daily`).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 1 {
		t.Fatalf("fact rows after force re-import = %d, want 1", factCount)
	}

	var billed string
	if err := s.(*sqliteStore).db.QueryRowContext(ctx, `SELECT billed_cost FROM fact_focus_cost_daily`).Scan(&billed); err != nil {
		t.Fatal(err)
	}
	if billed != "42" && billed != "42.00" && billed != "42.0000000000" {
		t.Fatalf("billed_cost=%q", billed)
	}
}

func strPtr(s string) *string { return &s }
