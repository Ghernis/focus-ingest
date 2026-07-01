package publish

import (
	"testing"
	"time"
)

func TestCoerceSQLDate(t *testing.T) {
	tests := []struct {
		in   interface{}
		want string
	}{
		{"2026-01-01T00:00:00Z", "2026-01-01"},
		{"2026-01-01 00:00:00", "2026-01-01"},
		{time.Date(2026, 2, 15, 13, 0, 0, 0, time.UTC), "2026-02-15"},
	}
	for _, tc := range tests {
		dt, err := coerceSQLDate(tc.in)
		if err != nil {
			t.Fatalf("input %#v: %v", tc.in, err)
		}
		if got := dt.Format("2006-01-02"); got != tc.want {
			t.Fatalf("input %#v: got %s want %s", tc.in, got, tc.want)
		}
	}
}

func TestCoerceSQLDateTimeLegacyPlaceholder(t *testing.T) {
	dt, err := coerceSQLDateTime("datetime('now')")
	if err != nil {
		t.Fatal(err)
	}
	if dt.IsZero() {
		t.Fatal("expected non-zero time")
	}
}

func TestCoerceSQLDateTimeGoStringFormat(t *testing.T) {
	in := "2026-06-25 18:17:28.6970829 +0000 UTC"
	dt, err := coerceSQLDateTime(in)
	if err != nil {
		t.Fatal(err)
	}
	if dt.Year() != 2026 || dt.Month() != time.June || dt.Day() != 25 {
		t.Fatalf("unexpected date: %v", dt)
	}
}

func TestCoerceAggValsRightsizingIntramonthRow(t *testing.T) {
	vals := []interface{}{
		"2026-01-01T00:00:00Z",
		"AWS",
		int64(10),
		int64(20),
		int64(1),
		"prod",
		"2026-01-15T00:00:00Z",
		int64(100),
		int64(101),
		int64(5),
		int64(10),
		"120.50",
		"30.00",
		"500.00",
		"DOWNSIZE",
		"datetime('now')",
	}
	kinds := []aggColKind{
		aggColDate, aggColString, aggColInt, aggColInt, aggColInt, aggColString,
		aggColDate, aggColInt, aggColInt, aggColInt, aggColInt,
		aggColDecimal, aggColDecimal, aggColDecimal, aggColString, aggColDateTime,
	}
	if err := coerceAggVals(vals, kinds); err != nil {
		t.Fatal(err)
	}
	if _, ok := vals[0].(time.Time); !ok {
		t.Fatalf("month_start type %T", vals[0])
	}
	if _, ok := vals[6].(time.Time); !ok {
		t.Fatalf("change_date type %T", vals[6])
	}
	if _, ok := vals[15].(time.Time); !ok {
		t.Fatalf("refreshed_utc type %T", vals[15])
	}
}

func TestCoerceAggValsCostDailyRow(t *testing.T) {
	vals := []interface{}{
		"2026-01-15T00:00:00Z",
		"2026-01-01",
		"AZURE",
		int64(1),
		int64(2),
		nil,
		"100.00",
		"90.00",
		"110.00",
		"95.00",
		int64(42),
		"2026-06-25 18:17:28.6970829 +0000 UTC",
	}
	kinds := []aggColKind{
		aggColDate, aggColDate, aggColString,
		aggColInt, aggColInt, aggColIntNull,
		aggColDecimal, aggColDecimal, aggColDecimal, aggColDecimal,
		aggColInt, aggColDateTime,
	}
	if err := coerceAggVals(vals, kinds); err != nil {
		t.Fatal(err)
	}
	if _, ok := vals[0].(time.Time); !ok {
		t.Fatalf("charge_date type %T", vals[0])
	}
	if _, ok := vals[1].(time.Time); !ok {
		t.Fatalf("billing_period_start type %T", vals[1])
	}
	if _, ok := vals[11].(time.Time); !ok {
		t.Fatalf("refreshed_utc type %T", vals[11])
	}
}
