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
