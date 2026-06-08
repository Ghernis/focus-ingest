package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

func TestApplicationNormalization_MergesAliases(t *testing.T) {
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

	importRow := func(batchID int64, appTag string) error {
		tags := `{"Application":"` + appTag + `","Environment":"prod"}`
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
			BilledCost:         strPtr("5.00"),
			EffectiveCost:      strPtr("5.00"),
			RawTagsJSON:        &tags,
		}
		return s.InsertStaging(ctx, batchID, "1.0", "test.parquet", []focus.StagingRow{row})
	}

	batch1, err := s.BeginBatch(ctx, BatchMeta{SourceProvider: "aws", FocusVersion: "1.0", SourceFile: "a.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	if err := importRow(batch1, "INS APP1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batch1, "1.0"); err != nil {
		t.Fatal(err)
	}

	batch2, err := s.BeginBatch(ctx, BatchMeta{SourceProvider: "aws", FocusVersion: "1.0", SourceFile: "b.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	if err := importRow(batch2, "ins-app1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batch2, "1.0"); err != nil {
		t.Fatal(err)
	}

	if err := s.RebuildAggregates(ctx); err != nil {
		t.Fatal(err)
	}

	db := s.(*sqliteStore).db
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_application WHERE application_name = 'INS_APP1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 normalized app, got %d", count)
	}

	var aliases string
	if err := db.QueryRowContext(ctx, `SELECT alias_values FROM dim_application WHERE application_name = 'INS_APP1'`).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if aliases != "INS APP1|ins-app1" {
		t.Fatalf("aliases=%q", aliases)
	}
}

func TestApplicationNormalization_PluralMerge(t *testing.T) {
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

	importRow := func(batchID int64, appTag string) error {
		tags := `{"Application":"` + appTag + `","Environment":"prod"}`
		row := focus.StagingRow{
			SourceProvider:     "aws",
			Provider:           strPtr("Amazon Web Services"),
			BillingAccountId:   strPtr("123456789012"),
			SubAccountId:       strPtr("123456789012"),
			ChargePeriodStart:  strPtr("2024-02-10T00:00:00Z"),
			ChargePeriodEnd:    strPtr("2024-02-11T00:00:00Z"),
			BillingPeriodStart: strPtr("2024-02-01"),
			BillingPeriodEnd:   strPtr("2024-02-29"),
			ChargeCategory:     strPtr("Usage"),
			ServiceName:        strPtr("Amazon Virtual Private Cloud"),
			BilledCost:         strPtr("3.00"),
			EffectiveCost:      strPtr("3.00"),
			RawTagsJSON:        &tags,
		}
		return s.InsertStaging(ctx, batchID, "1.0", "test.parquet", []focus.StagingRow{row})
	}

	b1, _ := s.BeginBatch(ctx, BatchMeta{SourceProvider: "aws", FocusVersion: "1.0", SourceFile: "p1.parquet"})
	if err := importRow(b1, "Networking Services"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, b1, "1.0"); err != nil {
		t.Fatal(err)
	}

	b2, _ := s.BeginBatch(ctx, BatchMeta{SourceProvider: "aws", FocusVersion: "1.0", SourceFile: "p2.parquet"})
	if err := importRow(b2, "Networking Service"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, b2, "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.RebuildAggregates(ctx); err != nil {
		t.Fatal(err)
	}

	db := s.(*sqliteStore).db
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_application WHERE application_name = 'NETWORKING_SERVICE'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 canonical app, got %d", count)
	}

	var aliases string
	if err := db.QueryRowContext(ctx, `SELECT alias_values FROM dim_application WHERE application_name = 'NETWORKING_SERVICE'`).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if aliases != "Networking Services|Networking Service" {
		t.Fatalf("aliases=%q", aliases)
	}
}
