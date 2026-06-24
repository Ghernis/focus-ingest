package etl_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
	"github.com/ghernis/focus_dt/internal/store"

	_ "modernc.org/sqlite"
)

func TestRightsizing_MoMDownsizeAndUpsize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "rightsizing.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importRightsizingRow(ctx, t, s, "jan.parquet", "2024-01-15", "2024-01-01", "i-down", "sku-large", "10", "100")
	importRightsizingRow(ctx, t, s, "feb.parquet", "2024-02-15", "2024-02-01", "i-down", "sku-small", "10", "40")

	importRightsizingRow(ctx, t, s, "jan-up.parquet", "2024-01-15", "2024-01-01", "i-up", "sku-small", "5", "25")
	importRightsizingRow(ctx, t, s, "feb-up.parquet", "2024-02-15", "2024-02-01", "i-up", "sku-large", "5", "75")

	importRightsizingRow(ctx, t, s, "jan-same.parquet", "2024-01-15", "2024-01-01", "i-same", "sku-stable", "8", "80")
	importRightsizingRow(ctx, t, s, "feb-same.parquet", "2024-02-15", "2024-02-01", "i-same", "sku-stable", "8", "72")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openRightsizingTestDB(t, path)
	defer db.Close()

	var momCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_rightsizing_monthly WHERE month_start = '2024-02-01'`).Scan(&momCount); err != nil {
		t.Fatal(err)
	}
	if momCount != 2 {
		t.Fatalf("expected 2 MoM changes in Feb, got %d", momCount)
	}

	var direction string
	var unitSavings float64
	if err := db.QueryRowContext(ctx, `
		SELECT change_direction, realized_savings_unit
		FROM agg_resource_rightsizing_monthly r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE month_start = '2024-02-01' AND res.global_resource_id = 'i-down'`).Scan(&direction, &unitSavings); err != nil {
		t.Fatal(err)
	}
	if direction != "DOWNSIZE" {
		t.Fatalf("i-down direction=%s", direction)
	}
	if unitSavings < 59.9 || unitSavings > 60.1 {
		t.Fatalf("i-down unit savings=%v want ~60", unitSavings)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT change_direction, realized_savings_unit
		FROM agg_resource_rightsizing_monthly r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE month_start = '2024-02-01' AND res.global_resource_id = 'i-up'`).Scan(&direction, &unitSavings); err != nil {
		t.Fatal(err)
	}
	if direction != "UPSIZE" {
		t.Fatalf("i-up direction=%s", direction)
	}
	if unitSavings > -0.1 {
		t.Fatalf("i-up unit savings=%v want negative", unitSavings)
	}

	var sameCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agg_resource_rightsizing_monthly r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE res.global_resource_id = 'i-same'`).Scan(&sameCount); err != nil {
		t.Fatal(err)
	}
	if sameCount != 0 {
		t.Fatalf("unchanged resource should have 0 rows, got %d", sameCount)
	}
}

func TestRightsizing_IntraMonthTransition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "rightsizing_intra.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importRightsizingRow(ctx, t, s, "m1.parquet", "2024-03-10", "2024-03-01", "i-mid", "sku-large", "10", "100")
	importRightsizingRow(ctx, t, s, "m2.parquet", "2024-03-20", "2024-03-01", "i-mid", "sku-small", "10", "40")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openRightsizingTestDB(t, path)
	defer db.Close()

	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_rightsizing_intramonth WHERE month_start = '2024-03-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth event, got %d", intraCount)
	}

	var changeDate, direction string
	if err := db.QueryRowContext(ctx, `
		SELECT change_date, change_direction
		FROM agg_resource_rightsizing_intramonth r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE month_start = '2024-03-01' AND res.global_resource_id = 'i-mid'`).Scan(&changeDate, &direction); err != nil {
		t.Fatal(err)
	}
	if changeDate != "2024-03-20" {
		t.Fatalf("change_date=%s", changeDate)
	}
	if direction != "DOWNSIZE" {
		t.Fatalf("direction=%s", direction)
	}
}

// TestRightsizing_IntraMonth_CrossServiceNoise verifies that SKU transitions are
// only detected within the same service. A network SKU appearing alongside a
// compute SKU on the same resource must NOT trigger a rightsizing event.
func TestRightsizing_IntraMonth_CrossServiceNoise(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "rightsizing_noise.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	// Compute service: SKU changes from sku-large to sku-small on day 20.
	importRightsizingRowWithService(ctx, t, s, "c1.parquet", "2024-05-10", "2024-05-01", "i-noise", "sku-large", "10", "100", "Amazon Elastic Compute Cloud")
	importRightsizingRowWithService(ctx, t, s, "c2.parquet", "2024-05-20", "2024-05-01", "i-noise", "sku-small", "10", "40", "Amazon Elastic Compute Cloud")

	// Network service on the same resource and same days: different service_sk, should be ignored.
	importRightsizingRowWithService(ctx, t, s, "n1.parquet", "2024-05-10", "2024-05-01", "i-noise", "sku-net-a", "50", "5", "Amazon Virtual Private Cloud")
	importRightsizingRowWithService(ctx, t, s, "n2.parquet", "2024-05-20", "2024-05-01", "i-noise", "sku-net-b", "50", "3", "Amazon Virtual Private Cloud")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openRightsizingTestDB(t, path)
	defer db.Close()

	// Must have exactly 1 intramonth event: the compute SKU transition only.
	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_rightsizing_intramonth WHERE month_start = '2024-05-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth event (compute only), got %d", intraCount)
	}

	// Confirm the one event is the compute transition (downsize).
	var direction string
	var projectedAnnual float64
	if err := db.QueryRowContext(ctx, `
		SELECT change_direction, CAST(projected_annual_savings AS REAL)
		FROM agg_resource_rightsizing_intramonth r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE month_start = '2024-05-01' AND res.global_resource_id = 'i-noise'`).Scan(&direction, &projectedAnnual); err != nil {
		t.Fatal(err)
	}
	if direction != "DOWNSIZE" {
		t.Fatalf("direction=%s want DOWNSIZE", direction)
	}
	if projectedAnnual <= 0 {
		t.Fatalf("projected_annual_savings=%v want >0", projectedAnnual)
	}
}

func importRightsizingRowWithService(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, qty, cost, serviceName string) {
	t.Helper()
	batchID, err := s.BeginBatch(ctx, store.BatchMeta{
		SourceProvider: "aws",
		FocusVersion:   "1.0",
		SourceFile:     file,
	})
	if err != nil {
		t.Fatal(err)
	}
	row := focus.StagingRow{
		SourceProvider:     "aws",
		Provider:           strPtr("Amazon Web Services"),
		BillingAccountId:   strPtr("123456789012"),
		SubAccountId:       strPtr("123456789012"),
		ChargePeriodStart:  strPtr(chargeDate + "T00:00:00Z"),
		ChargePeriodEnd:    strPtr(chargeDate + "T00:00:00Z"),
		BillingPeriodStart: strPtr(billingMonth),
		BillingPeriodEnd:   strPtr(billingMonth),
		ChargeCategory:     strPtr("Usage"),
		PricingCategory:    strPtr("OnDemand"),
		ServiceName:        strPtr(serviceName),
		ResourceId:         strPtr(resourceID),
		ResourceType:       strPtr("instance"),
		SkuId:              strPtr(skuID),
		PricingQuantity:    strPtr(qty),
		BilledCost:         strPtr(cost),
		EffectiveCost:      strPtr(cost),
	}
	if err := s.InsertStaging(ctx, batchID, "1.0", file, []focus.StagingRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batchID, "1.0"); err != nil {
		t.Fatal(err)
	}
}

func importRightsizingRow(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, qty, cost string) {
	t.Helper()
	batchID, err := s.BeginBatch(ctx, store.BatchMeta{
		SourceProvider: "aws",
		FocusVersion:   "1.0",
		SourceFile:     file,
	})
	if err != nil {
		t.Fatal(err)
	}
	row := focus.StagingRow{
		SourceProvider:     "aws",
		Provider:           strPtr("Amazon Web Services"),
		BillingAccountId:   strPtr("123456789012"),
		SubAccountId:       strPtr("123456789012"),
		ChargePeriodStart:  strPtr(chargeDate + "T00:00:00Z"),
		ChargePeriodEnd:    strPtr(chargeDate + "T00:00:00Z"),
		BillingPeriodStart: strPtr(billingMonth),
		BillingPeriodEnd:   strPtr(billingMonth),
		ChargeCategory:     strPtr("Usage"),
		PricingCategory:    strPtr("OnDemand"),
		ServiceName:        strPtr("Amazon Elastic Compute Cloud"),
		ResourceId:         strPtr(resourceID),
		ResourceType:       strPtr("instance"),
		SkuId:              strPtr(skuID),
		PricingQuantity:    strPtr(qty),
		BilledCost:         strPtr(cost),
		EffectiveCost:      strPtr(cost),
	}
	if err := s.InsertStaging(ctx, batchID, "1.0", file, []focus.StagingRow{row}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBatch(ctx, batchID, "1.0"); err != nil {
		t.Fatal(err)
	}
}

func openRightsizingTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func strPtr(s string) *string { return &s }
