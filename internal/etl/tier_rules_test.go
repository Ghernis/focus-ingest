package etl

import (
	"testing"
)

func TestTierRules_VirtualMachineMeter(t *testing.T) {
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Virtual Machines", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5")
	if !ok {
		t.Fatal("expected VM tier match")
	}
	if match.TierCode != "D4s v5" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
	if match.TierRank <= 0 {
		t.Fatalf("tier_rank=%d", match.TierRank)
	}
}

func TestTierRules_AzureReservationsVM(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Azure Reservations", "DZH318Z08M9W_01X3_1 Compute Hour", "D4s v5")
	if !ok {
		t.Fatal("expected Azure Reservations VM tier match")
	}
	if match.TierCode != "D4s v5" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
	if match.TierRank <= 0 {
		t.Fatalf("tier_rank=%d", match.TierRank)
	}
}

func TestTierRules_B2msBurstableVM(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Virtual Machines", "DZH318Z0BQ35_00K2_1 Compute Hour", "B2ms")
	if !ok {
		t.Fatal("expected B2ms tier match from real VM staging data")
	}
	if match.TierCode != "B2ms" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
	if match.TierRank <= 0 {
		t.Fatalf("tier_rank=%d", match.TierRank)
	}
}

func TestTierRules_SlashMeterForm(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Virtual Machines", "X_1 Compute Hour", "D4 v3/D4s v3")
	if !ok {
		t.Fatal("expected slash-form meter to match")
	}
	if match.TierCode != "D4s v3" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
}

func TestTierRules_SameSkuIdDifferentMeter(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	d4, ok := engine.matchSKU("AZURE", "Virtual Machines", "DZH318Z08M9W_004T_1 Compute Hour", "D4s v5")
	if !ok {
		t.Fatal("d4")
	}
	d2, ok := engine.matchSKU("AZURE", "Virtual Machines", "DZH318Z08M9W_0061_1 Compute Hour", "D2s v5")
	if !ok {
		t.Fatal("d2")
	}
	if d4.TierCode == d2.TierCode {
		t.Fatal("tiers should differ for same sku_id")
	}
	if d4.TierRank <= d2.TierRank {
		t.Fatalf("D4 rank %d should exceed D2 rank %d", d4.TierRank, d2.TierRank)
	}
}

func TestTierRules_AppServiceHour(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	b1, ok := engine.matchSKU("AZURE", "Azure App Service", "DZH318Z0BXW9_0012_1 App Service Hour", "B1")
	if !ok {
		t.Fatal("b1")
	}
	noise, ok := engine.matchSKU("AZURE", "Azure App Service", "DZH318Z0BNVX_005J_Data Transfer Out (GB)", "Standard Data Transfer Out")
	if ok {
		t.Fatalf("data transfer should not match tier meter: %#v", noise)
	}
	if b1.TierCode != "B1" {
		t.Fatalf("tier=%q", b1.TierCode)
	}
}

func TestTierRules_SQLMIStorageIgnored(t *testing.T) {
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, ok := engine.matchSKU("AZURE", "SQL Managed Instance", "DZH318Z094PB_0036_Data Stored (GB/Month)", "LTR Backup ZRS Data Stored")
	if ok {
		t.Fatal("storage meter should not match tier rule")
	}
}

func TestTierRules_SQLDatabaseDTU(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Azure SQL Database", "DZH318Z0BQHF_01ML_10 DTUs/Day", "S2 DTUs")
	if !ok {
		t.Fatal("expected Azure SQL Database DTU tier match")
	}
	if match.TierCode != "S2 DTUs" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
	if match.TierRank <= 0 {
		t.Fatalf("tier_rank=%d", match.TierRank)
	}
}

func TestTierRules_SQLDatabaseElasticPoolEDTU(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "Azure SQL Database", "DZH318Z0BQHP_00LR_1 eDTU/Day", "eDTUs")
	if !ok {
		t.Fatal("expected Azure SQL Database eDTU tier match")
	}
	if match.TierCode != "eDTUs" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
	if match.TierRank <= 0 {
		t.Fatalf("tier_rank=%d", match.TierRank)
	}
}

func TestTierRules_FallbackWithoutServiceName(t *testing.T) {
	resetTierRulesEngine()
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	match, ok := engine.matchSKU("AZURE", "UNKNOWN", "DZH318Z0BQ35_00K2_1 Compute Hour", "B2ms")
	if !ok {
		t.Fatal("expected provider-level fallback match")
	}
	if match.TierCode != "B2ms" {
		t.Fatalf("tier_code=%q", match.TierCode)
	}
}

func TestTierChangeDirection_RankBased(t *testing.T) {
	if got := tierChangeDirection(680404, 680204, 0, 0); got != changeDownsize {
		t.Fatalf("got %s", got)
	}
	if got := tierChangeDirection(680204, 680404, 0, 0); got != changeUpsize {
		t.Fatalf("got %s", got)
	}
}

func TestResolveTierForFact_StoredTierWithoutMeterFlag(t *testing.T) {
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	code, rank, ok := resolveTierForFact(
		engine,
		"AZURE",
		"Virtual Machines",
		"unknown",
		"unknown",
		"D4s v5",
		680404,
		false,
	)
	if !ok {
		t.Fatal("expected stored tier metadata to be accepted")
	}
	if code != "D4s v5" {
		t.Fatalf("tier_code=%q", code)
	}
	if rank != 680404 {
		t.Fatalf("tier_rank=%d", rank)
	}
}

func TestResolveTierForFact_StoredTierInfersRank(t *testing.T) {
	engine, err := loadTierRulesEngine()
	if err != nil {
		t.Fatal(err)
	}
	code, rank, ok := resolveTierForFact(
		engine,
		"AZURE",
		"Virtual Machines",
		"unknown",
		"unknown",
		"D2s v5",
		0,
		false,
	)
	if !ok {
		t.Fatal("expected inferred rank from stored tier code")
	}
	if code != "D2s v5" {
		t.Fatalf("tier_code=%q", code)
	}
	if rank <= 0 {
		t.Fatalf("tier_rank=%d", rank)
	}
}
