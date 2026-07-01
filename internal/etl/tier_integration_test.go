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

func TestTierChange_MoM_VM_SameSkuId(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_mom.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importAzureVMRow(ctx, t, s, "jan.parquet", "2024-01-15", "2024-01-01", "vm-1",
		"DZH318Z08M9W", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100")
	importAzureVMRow(ctx, t, s, "feb.parquet", "2024-02-15", "2024-02-01", "vm-1",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var momCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_tier_change_monthly WHERE month_start = '2024-02-01'`).Scan(&momCount); err != nil {
		t.Fatal(err)
	}
	if momCount != 1 {
		t.Fatalf("expected 1 MoM tier change, got %d", momCount)
	}

	var priorTier, newTier, direction string
	if err := db.QueryRowContext(ctx, `
		SELECT prior_tier_code, new_tier_code, change_direction
		FROM agg_resource_tier_change_monthly r
		INNER JOIN dim_resource res ON r.resource_sk = res.resource_sk
		WHERE month_start = '2024-02-01' AND res.global_resource_id = 'vm-1'`).Scan(&priorTier, &newTier, &direction); err != nil {
		t.Fatal(err)
	}
	if priorTier != "D4s v5" || newTier != "D2s v5" {
		t.Fatalf("tiers %s -> %s", priorTier, newTier)
	}
	if direction != "DOWNSIZE" {
		t.Fatalf("direction=%s", direction)
	}
}

func TestTierChange_IntraMonth_VM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_intra.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importAzureVMRow(ctx, t, s, "m1.parquet", "2024-03-10", "2024-03-01", "vm-mid",
		"DZH318Z08M9W", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100")
	importAzureVMRow(ctx, t, s, "m2.parquet", "2024-03-20", "2024-03-01", "vm-mid",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_tier_change_intramonth WHERE month_start = '2024-03-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth tier change, got %d", intraCount)
	}
}

func TestTierChange_AppServiceNoiseIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_appsvc.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importAzureAppServiceRow(ctx, t, s, "a1.parquet", "2024-04-10", "2024-04-01", "app-1",
		"DZH318Z0BXW9", "DZH318Z0BXW9_0012_1 App Service Hour", "B1", "10", "50")
	importAzureAppServiceRow(ctx, t, s, "a1noise.parquet", "2024-04-10", "2024-04-01", "app-1",
		"DZH318Z0BNVX", "DZH318Z0BNVX_005J_Data Transfer Out (GB)", "Standard Data Transfer Out", "100", "200")
	importAzureAppServiceRow(ctx, t, s, "a2.parquet", "2024-04-20", "2024-04-01", "app-1",
		"DZH318Z0DCR2", "DZH318Z0DCR2_000R_1 App Service Hour", "P1v3 App", "10", "120")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_tier_change_intramonth WHERE month_start = '2024-04-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth event, got %d", intraCount)
	}
}

func TestTierChange_CrossServiceNoise(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_noise.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	importAzureVMRow(ctx, t, s, "c1.parquet", "2024-05-10", "2024-05-01", "i-noise",
		"DZH318Z08M9W", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100")
	importAzureVMRow(ctx, t, s, "c2.parquet", "2024-05-20", "2024-05-01", "i-noise",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")
	importAzureAppServiceRow(ctx, t, s, "n1.parquet", "2024-05-10", "2024-05-01", "i-noise",
		"DZH318Z0BXW9", "DZH318Z0BXW9_0012_1 App Service Hour", "B1", "10", "5")
	importAzureAppServiceRow(ctx, t, s, "n2.parquet", "2024-05-20", "2024-05-01", "i-noise",
		"DZH318Z0BXW9", "DZH318Z0BXW9_0012_1 App Service Hour", "B1", "10", "8")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_tier_change_intramonth WHERE month_start = '2024-05-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth event (compute only), got %d", intraCount)
	}
}

func importAzureVMRow(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost string) {
	importAzureServiceRow(ctx, t, s, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost, "Virtual Machines")
}

func importAzureAppServiceRow(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost string) {
	importAzureServiceRow(ctx, t, s, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost, "Azure App Service")
}

func importAzureServiceRow(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost, serviceName string) {
	t.Helper()
	batchID, err := s.BeginBatch(ctx, store.BatchMeta{
		SourceProvider: "azure",
		FocusVersion:   "1.0",
		SourceFile:     file,
	})
	if err != nil {
		t.Fatal(err)
	}
	row := focus.StagingRow{
		SourceProvider:     "azure",
		Provider:           strPtr("Microsoft"),
		BillingAccountId:   strPtr("sub-1"),
		SubAccountId:       strPtr("sub-1"),
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
		SkuPriceId:         strPtr(skuPriceID),
		SkuMeter:           strPtr(skuMeter),
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

func openTierTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func strPtr(s string) *string { return &s }
