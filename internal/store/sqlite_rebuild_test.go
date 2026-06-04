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

func strPtr(s string) *string { return &s }
