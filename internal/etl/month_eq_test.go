package etl

import "testing"

func TestMonthEq_SQLiteDatetimePrefix(t *testing.T) {
	p := &Processor{Dialect: "sqlite"}
	filter := p.monthEq("f.billing_period_start", "2026-01-01")
	want := "substr(f.billing_period_start, 1, 10) = '2026-01-01'"
	if filter != want {
		t.Fatalf("got %q want %q", filter, want)
	}
}

func TestMonthEq_SQLServerCast(t *testing.T) {
	p := &Processor{Dialect: "sqlserver"}
	filter := p.monthEq("f.billing_period_start", "2026-01-01T00:00:00Z")
	want := "CAST(f.billing_period_start AS DATE) = '2026-01-01'"
	if filter != want {
		t.Fatalf("got %q want %q", filter, want)
	}
}
