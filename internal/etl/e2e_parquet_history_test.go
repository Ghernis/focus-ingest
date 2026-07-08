package etl_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
	pqread "github.com/ghernis/focus_dt/internal/parquet"
	"github.com/ghernis/focus_dt/internal/store"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/deprecated"

	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

type fixtureParquetRow struct {
	BilledCost         *[16]byte        `parquet:"BilledCost"`
	BillingAccountId   *string          `parquet:"BillingAccountId"`
	BillingPeriodEnd   deprecated.Int96 `parquet:"BillingPeriodEnd"`
	BillingPeriodStart deprecated.Int96 `parquet:"BillingPeriodStart"`
	ChargeCategory     *string          `parquet:"ChargeCategory"`
	ChargePeriodEnd    deprecated.Int96 `parquet:"ChargePeriodEnd"`
	ChargePeriodStart  deprecated.Int96 `parquet:"ChargePeriodStart"`
	EffectiveCost      *[16]byte        `parquet:"EffectiveCost"`
	PricingCategory    *string          `parquet:"PricingCategory"`
	PricingQuantity    *[16]byte        `parquet:"PricingQuantity"`
	ProviderName       *string          `parquet:"ProviderName"`
	ResourceId         *string          `parquet:"ResourceId"`
	ResourceType       *string          `parquet:"ResourceType"`
	ServiceName        *string          `parquet:"ServiceName"`
	SkuId              *string          `parquet:"SkuId"`
	SkuMeter           *string          `parquet:"SkuMeter"`
	SkuPriceId         *string          `parquet:"SkuPriceId"`
	SubAccountId       *string          `parquet:"SubAccountId"`
}

type fixturePaths struct {
	jan string
	feb string
	mar string
}

func TestE2EParquetHistoryOverlap_SQLite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	work := t.TempDir()
	dbPath := filepath.Join(work, "e2e_history.db")

	s, err := store.OpenSQLite(dbPath, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	paths, err := writeHistoryFixtureParquets(work)
	if err != nil {
		t.Fatal(err)
	}

	if err := importFixtureParquet(ctx, s, paths.jan, "jan.parquet"); err != nil {
		t.Fatal(err)
	}
	if err := importFixtureParquet(ctx, s, paths.feb, "feb.parquet"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildAggregates(ctx, false); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := queryMonthCost(t, ctx, db, "sqlite", "2024-02-01"); !almostEqual(got, 40) {
		t.Fatalf("feb cost before overlap=%v want 40", got)
	}
	if got := queryCarryForwardMonthDelta(t, ctx, db, "sqlite", "2024-02-01", "vm-hist"); !almostEqual(got, 60) {
		t.Fatalf("feb carryforward delta before overlap=%v want 60", got)
	}

	if err := importFixtureParquet(ctx, s, paths.mar, "mar.parquet"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildAggregates(ctx, false); err != nil {
		t.Fatal(err)
	}

	if got := queryMonthCost(t, ctx, db, "sqlite", "2024-02-01"); !almostEqual(got, 36) {
		t.Fatalf("feb cost after overlap=%v want 36", got)
	}
	if got := queryCarryForwardMonthDelta(t, ctx, db, "sqlite", "2024-02-01", "vm-hist"); !almostEqual(got, 54) {
		t.Fatalf("feb carryforward delta after overlap=%v want 54", got)
	}
	if got := queryCarryForwardCumulative(t, ctx, db, "sqlite", "2024-03-01", "vm-hist"); !almostEqual(got, 114) {
		t.Fatalf("mar cumulative carryforward=%v want 114", got)
	}
}

func TestE2EParquetHistoryOverlap_SQLServer_OptIn(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FOCUS_E2E_SQLSERVER_DSN"))
	if dsn == "" {
		t.Skip("set FOCUS_E2E_SQLSERVER_DSN to run SQL Server E2E")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := store.OpenSQLServer(dsn, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ResetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	paths, err := writeHistoryFixtureParquets(work)
	if err != nil {
		t.Fatal(err)
	}

	if err := importFixtureParquet(ctx, s, paths.jan, "jan.parquet"); err != nil {
		t.Fatal(err)
	}
	if err := importFixtureParquet(ctx, s, paths.feb, "feb.parquet"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildAggregates(ctx, false); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if got := queryMonthCost(t, ctx, db, "sqlserver", "2024-02-01"); !almostEqual(got, 40) {
		t.Fatalf("feb cost before overlap=%v want 40", got)
	}
	if got := queryCarryForwardMonthDelta(t, ctx, db, "sqlserver", "2024-02-01", "vm-hist"); !almostEqual(got, 60) {
		t.Fatalf("feb carryforward delta before overlap=%v want 60", got)
	}

	if err := importFixtureParquet(ctx, s, paths.mar, "mar.parquet"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildAggregates(ctx, false); err != nil {
		t.Fatal(err)
	}

	if got := queryMonthCost(t, ctx, db, "sqlserver", "2024-02-01"); !almostEqual(got, 36) {
		t.Fatalf("feb cost after overlap=%v want 36", got)
	}
	if got := queryCarryForwardMonthDelta(t, ctx, db, "sqlserver", "2024-02-01", "vm-hist"); !almostEqual(got, 54) {
		t.Fatalf("feb carryforward delta after overlap=%v want 54", got)
	}
	if got := queryCarryForwardCumulative(t, ctx, db, "sqlserver", "2024-03-01", "vm-hist"); !almostEqual(got, 114) {
		t.Fatalf("mar cumulative carryforward=%v want 114", got)
	}
}

func importFixtureParquet(ctx context.Context, s store.Store, parquetPath, sourceFile string) error {
	id, err := s.BeginBatch(ctx, store.BatchMeta{SourceProvider: "MIXED", FocusVersion: "1.2", SourceFile: sourceFile})
	if err != nil {
		return err
	}
	if _, err := pqread.ReadFile(parquetPath, 1000, func(rows []focus.StagingRow) error {
		return s.InsertStaging(ctx, id, "1.2", sourceFile, rows)
	}); err != nil {
		return err
	}
	return s.ProcessBatch(ctx, id, "1.2")
}

func queryMonthCost(t *testing.T, ctx context.Context, db *sql.DB, dialect, month string) float64 {
	t.Helper()
	where := monthFilter(dialect, "month_start", month)
	q := fmt.Sprintf(`SELECT COALESCE(SUM(CAST(effective_cost AS %s)),0) FROM agg_cost_monthly WHERE %s`, castFloatType(dialect), where)
	var out float64
	if err := db.QueryRowContext(ctx, q).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func queryCarryForwardMonthDelta(t *testing.T, ctx context.Context, db *sql.DB, dialect, month, resourceID string) float64 {
	t.Helper()
	where := monthFilter(dialect, "c.month_start", month)
	q := fmt.Sprintf(`SELECT CAST(c.month_realized_delta AS %s)
		FROM fact_resource_tier_carryforward c
		INNER JOIN dim_resource r ON c.resource_sk = r.resource_sk
		WHERE %s AND r.global_resource_id = '%s'`, castFloatType(dialect), where, escapeSQL(resourceID))
	var out float64
	if err := db.QueryRowContext(ctx, q).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func queryCarryForwardCumulative(t *testing.T, ctx context.Context, db *sql.DB, dialect, month, resourceID string) float64 {
	t.Helper()
	where := monthFilter(dialect, "c.month_start", month)
	q := fmt.Sprintf(`SELECT CAST(c.cumulative_realized_delta AS %s)
		FROM fact_resource_tier_carryforward c
		INNER JOIN dim_resource r ON c.resource_sk = r.resource_sk
		WHERE %s AND r.global_resource_id = '%s'`, castFloatType(dialect), where, escapeSQL(resourceID))
	var out float64
	if err := db.QueryRowContext(ctx, q).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func castFloatType(dialect string) string {
	if dialect == "sqlserver" {
		return "FLOAT"
	}
	return "REAL"
}

func monthFilter(dialect, col, month string) string {
	if dialect == "sqlserver" {
		return fmt.Sprintf("CAST(%s AS DATE) = '%s'", col, escapeSQL(month))
	}
	return fmt.Sprintf("substr(%s, 1, 10) = '%s'", col, escapeSQL(month))
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func writeHistoryFixtureParquets(dir string) (fixturePaths, error) {
	jan := filepath.Join(dir, "2024-01.parquet")
	feb := filepath.Join(dir, "2024-02.parquet")
	mar := filepath.Join(dir, "2024-03.parquet")

	if err := writeFixtureParquet(jan, []fixtureParquetRow{
		vmFixtureRow("2024-01-15", "2024-01-01", "vm-hist", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5", "10", "100"),
	}); err != nil {
		return fixturePaths{}, err
	}
	if err := writeFixtureParquet(feb, []fixtureParquetRow{
		vmFixtureRow("2024-02-15", "2024-02-01", "vm-hist", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40"),
	}); err != nil {
		return fixturePaths{}, err
	}
	if err := writeFixtureParquet(mar, []fixtureParquetRow{
		vmFixtureRow("2024-03-15", "2024-03-01", "vm-hist", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "10", "40"),
		vmFixtureRow("2024-02-20", "2024-02-01", "vm-hist", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5", "-1", "-4"),
	}); err != nil {
		return fixturePaths{}, err
	}
	return fixturePaths{jan: jan, feb: feb, mar: mar}, nil
}

func writeFixtureParquet(path string, rows []fixtureParquetRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := parquet.NewGenericWriter[fixtureParquetRow](f)
	if _, err := w.Write(rows); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return nil
}

func vmFixtureRow(chargeDate, monthStart, resourceID, skuPriceID, skuMeter, qty, cost string) fixtureParquetRow {
	provider := "Microsoft"
	billingAccount := "bill-1"
	subAccount := "sub-1"
	chargeCategory := "Usage"
	pricingCategory := "OnDemand"
	serviceName := "Virtual Machines"
	resourceType := "instance"
	skuID := "DZH318Z08M9W"

	return fixtureParquetRow{
		BilledCost:         decimalToFixed128(cost),
		BillingAccountId:   &billingAccount,
		BillingPeriodEnd:   int96Date(monthStart),
		BillingPeriodStart: int96Date(monthStart),
		ChargeCategory:     &chargeCategory,
		ChargePeriodEnd:    int96Date(chargeDate),
		ChargePeriodStart:  int96Date(chargeDate),
		EffectiveCost:      decimalToFixed128(cost),
		PricingCategory:    &pricingCategory,
		PricingQuantity:    decimalToFixed128(qty),
		ProviderName:       &provider,
		ResourceId:         &resourceID,
		ResourceType:       &resourceType,
		ServiceName:        &serviceName,
		SkuId:              &skuID,
		SkuMeter:           &skuMeter,
		SkuPriceId:         &skuPriceID,
		SubAccountId:       &subAccount,
	}
}

func int96Date(s string) deprecated.Int96 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return deprecated.Int96{}
	}
	julianDay := uint32(t.UTC().Unix()/86400 + 2440588)
	return deprecated.Int96{0, 0, julianDay}
}

func decimalToFixed128(s string) *[16]byte {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	rat := new(big.Rat)
	if _, ok := rat.SetString(s); !ok {
		return nil
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	num := new(big.Int).Mul(rat.Num(), scale)
	val := new(big.Int).Quo(num, rat.Denom())

	enc := new(big.Int)
	if val.Sign() < 0 {
		two128 := new(big.Int).Lsh(big.NewInt(1), 128)
		enc.Add(two128, val)
	} else {
		enc.Set(val)
	}
	b := enc.Bytes()
	if len(b) > 16 {
		return nil
	}
	var out [16]byte
	copy(out[16-len(b):], b)
	return &out
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
