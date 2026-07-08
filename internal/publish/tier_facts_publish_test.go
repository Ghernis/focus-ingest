package publish

import "testing"

func TestMergeLocalOverrideRows_LocalWinsSameGrain(t *testing.T) {
	base := map[string][]interface{}{
		"k1": {"server", "row"},
		"k2": {"server-only"},
	}
	local := map[string][]interface{}{
		"k1": {"local", "row"},
		"k3": {"local-only"},
	}

	merged := mergeLocalOverrideRows(base, local)

	if got := len(merged); got != 3 {
		t.Fatalf("len=%d want 3", got)
	}
	if merged["k1"][0] != "local" {
		t.Fatalf("expected local override on k1, got %v", merged["k1"][0])
	}
	if merged["k2"][0] != "server-only" {
		t.Fatalf("expected server-only key preserved, got %v", merged["k2"][0])
	}
	if merged["k3"][0] != "local-only" {
		t.Fatalf("expected local-only key included, got %v", merged["k3"][0])
	}
}

func TestTierFactMergeSpecs_IncludesAllTierFactTables(t *testing.T) {
	specs := tierFactMergeSpecs("2024-02-01")
	if len(specs) != 3 {
		t.Fatalf("len=%d want 3", len(specs))
	}
	want := map[string]bool{
		"fact_resource_tier_daily":        false,
		"fact_resource_tier_change":       false,
		"fact_resource_tier_carryforward": false,
	}
	for _, s := range specs {
		if _, ok := want[s.table]; !ok {
			t.Fatalf("unexpected table %s", s.table)
		}
		want[s.table] = true
	}
	for tbl, seen := range want {
		if !seen {
			t.Fatalf("missing table %s", tbl)
		}
	}
}
