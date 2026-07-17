package etl_test

import (
	"context"
	"encoding/csv"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestTierDaily_B2msVM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_b2ms.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	// Real staging shape from stg_example_vm.csv: B2ms on Compute Hour lines.
	for _, day := range []string{"2026-01-09", "2026-01-10", "2026-01-11", "2026-01-29", "2026-01-30"} {
		importAzureVMRow(ctx, t, s, "vm-"+day+".parquet", day, "2026-01-01", "slpazrusadm03",
			"DZH318Z0BQ35", "DZH318Z0BQ35_00K2_1 Compute Hour", "B2ms", "24", "1.67")
	}

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var tierSkus int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_sku WHERE is_tier_meter = 1`).Scan(&tierSkus); err != nil {
		t.Fatal(err)
	}
	if tierSkus == 0 {
		t.Fatal("expected tier-capable SKUs in dim_sku")
	}

	var dailyRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_resource_tier_daily WHERE billing_period_start LIKE '2026-01%'`).Scan(&dailyRows); err != nil {
		t.Fatal(err)
	}
	if dailyRows == 0 {
		t.Fatal("expected fact_resource_tier_daily rows for B2ms VM month")
	}

	var tierCode string
	if err := db.QueryRowContext(ctx, `
		SELECT tier_code FROM fact_resource_tier_daily d
		INNER JOIN dim_resource r ON d.resource_sk = r.resource_sk
		WHERE r.global_resource_id = 'slpazrusadm03' LIMIT 1`).Scan(&tierCode); err != nil {
		t.Fatal(err)
	}
	if tierCode != "B2ms" {
		t.Fatalf("tier_code=%q want B2ms", tierCode)
	}
}

// TestTierDaily_NullSubAccountIncluded guards the commit fix that dropped the
// `f.sub_account_sk IS NOT NULL` filter in buildFactResourceTierDaily. Cost lines
// with a billing account but no sub-account must still flow into the tier fact
// (via the COALESCE(sa.billing_account_sk, f.billing_account_sk) join), otherwise
// tier savings under-report versus third-party billing that counts every line.
func TestTierDaily_NullSubAccountIncluded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_nullsub.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	// Same resource + month, tier change mid-month, NO sub-account on any line.
	importAzureVMRowNoSubAccount(ctx, t, s, "ns1.parquet", "2024-06-10", "2024-06-01", "vm-nosub",
		"DZH318Z08M9W", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100")
	importAzureVMRowNoSubAccount(ctx, t, s, "ns2.parquet", "2024-06-20", "2024-06-01", "vm-nosub",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	// Precondition: the fact rows really do have NULL sub_account_sk.
	var nullSubDaily int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fact_focus_cost_daily f
		INNER JOIN dim_resource r ON f.resource_sk = r.resource_sk
		WHERE r.global_resource_id = 'vm-nosub' AND f.sub_account_sk IS NULL`).Scan(&nullSubDaily); err != nil {
		t.Fatal(err)
	}
	if nullSubDaily == 0 {
		t.Fatal("test setup invalid: expected cost rows with NULL sub_account_sk")
	}

	var tierDaily int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fact_resource_tier_daily d
		INNER JOIN dim_resource r ON d.resource_sk = r.resource_sk
		WHERE r.global_resource_id = 'vm-nosub'`).Scan(&tierDaily); err != nil {
		t.Fatal(err)
	}
	if tierDaily == 0 {
		t.Fatal("regression: NULL sub_account tier rows were dropped from fact_resource_tier_daily")
	}

	var intraCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agg_resource_tier_change_intramonth WHERE month_start = '2024-06-01'`).Scan(&intraCount); err != nil {
		t.Fatal(err)
	}
	if intraCount != 1 {
		t.Fatalf("expected 1 intramonth change for NULL sub_account VM, got %d", intraCount)
	}
}

func TestTierCarryForward_MultiMonthBaselineAndCumulative(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_carryforward_multi.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	// Jan baseline tier (D4s @ 10).
	importAzureVMRow(ctx, t, s, "cf-jan.parquet", "2024-01-15", "2024-01-01", "vm-cf",
		"DZH318Z08M9W", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100")
	// Feb downsize to D2s @ 4.
	importAzureVMRow(ctx, t, s, "cf-feb.parquet", "2024-02-15", "2024-02-01", "vm-cf",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")
	// Mar stays on D2s @ 4 (same savings versus Jan baseline).
	importAzureVMRow(ctx, t, s, "cf-mar.parquet", "2024-03-15", "2024-03-01", "vm-cf",
		"DZH318Z08M9W", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40")

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var monthDeltaFeb, cumulativeFeb, baselineRateFeb float64
	if err := db.QueryRowContext(ctx, `
		SELECT CAST(month_realized_delta AS REAL), CAST(cumulative_realized_delta AS REAL), CAST(baseline_unit_rate AS REAL)
		FROM fact_resource_tier_carryforward c
		INNER JOIN dim_resource r ON c.resource_sk = r.resource_sk
		WHERE c.month_start = '2024-02-01' AND r.global_resource_id = 'vm-cf'`).Scan(&monthDeltaFeb, &cumulativeFeb, &baselineRateFeb); err != nil {
		t.Fatal(err)
	}
	if baselineRateFeb != 10 {
		t.Fatalf("feb baseline_unit_rate=%v want 10", baselineRateFeb)
	}
	if monthDeltaFeb != 60 {
		t.Fatalf("feb month_realized_delta=%v want 60", monthDeltaFeb)
	}
	if cumulativeFeb != 60 {
		t.Fatalf("feb cumulative_realized_delta=%v want 60", cumulativeFeb)
	}

	var monthDeltaMar, cumulativeMar, baselineRateMar float64
	if err := db.QueryRowContext(ctx, `
		SELECT CAST(month_realized_delta AS REAL), CAST(cumulative_realized_delta AS REAL), CAST(baseline_unit_rate AS REAL)
		FROM fact_resource_tier_carryforward c
		INNER JOIN dim_resource r ON c.resource_sk = r.resource_sk
		WHERE c.month_start = '2024-03-01' AND r.global_resource_id = 'vm-cf'`).Scan(&monthDeltaMar, &cumulativeMar, &baselineRateMar); err != nil {
		t.Fatal(err)
	}
	if baselineRateMar != 10 {
		t.Fatalf("mar baseline_unit_rate=%v want 10", baselineRateMar)
	}
	if monthDeltaMar != 60 {
		t.Fatalf("mar month_realized_delta=%v want 60", monthDeltaMar)
	}
	if cumulativeMar != 120 {
		t.Fatalf("mar cumulative_realized_delta=%v want 120", cumulativeMar)
	}
}

func TestTierDaily_SQLDatabaseSampleCoverage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	path := filepath.Join(t.TempDir(), "tier_sql_db_sample.db")
	s, err := store.OpenSQLite(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	rows := loadSQLDatabaseSampleRows(t)
	if len(rows) == 0 {
		t.Fatal("expected compute-like SQL database rows from sample")
	}

	maxRows := 60
	if len(rows) < maxRows {
		maxRows = len(rows)
	}
	for i := 0; i < maxRows; i++ {
		r := rows[i]
		importAzureServiceRow(ctx, t, s,
			"sql-database-sample.csv",
			r.chargeDate,
			r.billingMonth,
			r.resourceID,
			r.skuID,
			r.skuPriceID,
			r.skuMeter,
			r.qty,
			r.cost,
			"Azure SQL Database",
		)
	}

	if _, err := s.RebuildAggregates(ctx, true); err != nil {
		t.Fatal(err)
	}

	db := openTierTestDB(t, path)
	defer db.Close()

	var totalDaily int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_resource_tier_daily`).Scan(&totalDaily); err != nil {
		t.Fatal(err)
	}
	if totalDaily == 0 {
		t.Fatal("expected SQL database rows in fact_resource_tier_daily")
	}

	var recognizedDaily int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM fact_resource_tier_daily d
		INNER JOIN dim_service svc ON d.service_sk = svc.service_sk
		WHERE svc.service_name = 'Azure SQL Database'
		  AND d.tier_code IS NOT NULL
		  AND TRIM(d.tier_code) <> ''`).Scan(&recognizedDaily); err != nil {
		t.Fatal(err)
	}
	if recognizedDaily == 0 {
		t.Fatal("expected recognized Azure SQL Database tier rows")
	}

	coverage := float64(recognizedDaily) / float64(totalDaily)
	if coverage < 0.90 {
		t.Fatalf("expected SQL database tier coverage >= 0.90, got %.4f (%d/%d)", coverage, recognizedDaily, totalDaily)
	}
}

type sqlDatabaseSampleRow struct {
	chargeDate  string
	billingMonth string
	resourceID  string
	skuID       string
	skuPriceID  string
	skuMeter    string
	qty         string
	cost        string
}

func loadSQLDatabaseSampleRows(t *testing.T) []sqlDatabaseSampleRow {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	samplePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "validate", "reconciliation_output", "sql_database_service.csv")

	f, err := os.Open(samplePath)
	if err != nil {
		t.Fatalf("open sample csv: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read sample csv: %v", err)
	}
	if len(recs) < 2 {
		return nil
	}

	head := map[string]int{}
	for i, h := range recs[0] {
		head[strings.TrimSpace(h)] = i
	}
	required := []string{"ServiceName", "SkuPriceId", "SkuMeter", "ResourceId", "SkuId", "ChargePeriodStart", "BillingPeriodStart", "PricingQuantity", "EffectiveCost"}
	for _, col := range required {
		if _, ok := head[col]; !ok {
			t.Fatalf("missing column %q in sample csv", col)
		}
	}

	out := make([]sqlDatabaseSampleRow, 0, len(recs)-1)
	for _, rec := range recs[1:] {
		if len(rec) == 0 {
			continue
		}
		service := strings.TrimSpace(rec[head["ServiceName"]])
		if service != "Azure SQL Database" {
			continue
		}
		skuPriceID := strings.TrimSpace(rec[head["SkuPriceId"]])
		// Only include compute-like rows that should map to a rightsize tier.
		if !strings.Contains(skuPriceID, "DTU/Day") && !strings.Contains(skuPriceID, "eDTU/Day") && !strings.Contains(skuPriceID, "vCore Hour") {
			continue
		}
		resourceID := strings.TrimSpace(rec[head["ResourceId"]])
		skuID := strings.TrimSpace(rec[head["SkuId"]])
		skuMeter := strings.TrimSpace(rec[head["SkuMeter"]])
		if resourceID == "" || skuID == "" || skuPriceID == "" || skuMeter == "" {
			continue
		}
		chargeDate := strings.TrimSpace(rec[head["ChargePeriodStart"]])
		billingMonth := strings.TrimSpace(rec[head["BillingPeriodStart"]])
		if len(chargeDate) >= 10 {
			chargeDate = chargeDate[:10]
		}
		if len(billingMonth) >= 10 {
			billingMonth = billingMonth[:10]
		}
		qty := strings.TrimSpace(rec[head["PricingQuantity"]])
		cost := strings.TrimSpace(rec[head["EffectiveCost"]])
		if qty == "" || cost == "" {
			continue
		}
		out = append(out, sqlDatabaseSampleRow{
			chargeDate:  chargeDate,
			billingMonth: billingMonth,
			resourceID:  resourceID,
			skuID:       skuID,
			skuPriceID:  skuPriceID,
			skuMeter:    skuMeter,
			qty:         qty,
			cost:        cost,
		})
	}
	return out
}

func importAzureVMRow(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost string) {
	importAzureServiceRow(ctx, t, s, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost, "Virtual Machines")
}

func importAzureVMRowNoSubAccount(ctx context.Context, t *testing.T, s store.Store, file, chargeDate, billingMonth, resourceID, skuID, skuPriceID, skuMeter, qty, cost string) {
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
		BillingAccountId:   strPtr("bill-1"),
		ChargePeriodStart:  strPtr(chargeDate + "T00:00:00Z"),
		ChargePeriodEnd:    strPtr(chargeDate + "T00:00:00Z"),
		BillingPeriodStart: strPtr(billingMonth),
		BillingPeriodEnd:   strPtr(billingMonth),
		ChargeCategory:     strPtr("Usage"),
		PricingCategory:    strPtr("OnDemand"),
		ServiceName:        strPtr("Virtual Machines"),
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
