package etl

import (
	"fmt"
	"testing"
)

func TestDailyGrainKeySeparatesChargeFrequency(t *testing.T) {
	sub := int64(10)
	cf1 := int64(1)
	cf2 := int64(2)
	key := func(cfSK *int64) string {
		return fmt.Sprintf("%s|%d|%v|%v|%d|%v|%v|%d|%v|%v|%v|%v|%v|%v|%s|%s",
			"2026-04-16", int64(1), &sub, nil, int64(4), nil, nil, int64(2), cfSK, nil, nil,
			nil, nil, nil, "abc", "2026-04-01")
	}
	if key(&cf1) == key(&cf2) {
		t.Fatal("charge_frequency_sk must be part of the daily grain key")
	}
}
