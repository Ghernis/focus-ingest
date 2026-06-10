package etl

import (
	"testing"
)

func TestDailyGrainKeyIgnoresPointerAddress(t *testing.T) {
	a, b := int64(30), int64(30)
	pa, pb := &a, &b
	cf1, cf2 := int64(2), int64(2)
	pcf1, pcf2 := &cf1, &cf2
	status := "Used"

	k1 := dailyGrainKey(
		"2026-04-14", 3, pa, nil, 9, nil, nil, 2,
		pcf1, nil, nil, nil, &status, nil,
		"abc123", "2026-04-01",
	)
	k2 := dailyGrainKey(
		"2026-04-14", 3, pb, nil, 9, nil, nil, 2,
		pcf2, nil, nil, nil, &status, nil,
		"abc123", "2026-04-01",
	)
	if k1 != k2 {
		t.Fatalf("keys differ for same grain values:\n%q\n%q", k1, k2)
	}
}

func TestDailyGrainKeySeparatesChargeFrequency(t *testing.T) {
	sub := int64(10)
	cf1, cf2 := int64(1), int64(2)
	k1 := dailyGrainKey("2026-04-16", 1, &sub, nil, 4, nil, nil, 2, &cf1, nil, nil, nil, nil, nil, "abc", "2026-04-01")
	k2 := dailyGrainKey("2026-04-16", 1, &sub, nil, 4, nil, nil, 2, &cf2, nil, nil, nil, nil, nil, "abc", "2026-04-01")
	if k1 == k2 {
		t.Fatal("charge_frequency_sk must separate daily grain keys")
	}
}
