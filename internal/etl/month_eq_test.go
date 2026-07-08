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

func TestDateOnlySelectExpr(t *testing.T) {
	p := &Processor{Dialect: "sqlite"}
	if got := p.dateOnlySelectExpr("month_start"); got != "substr(month_start, 1, 10)" {
		t.Fatalf("sqlite expr=%q", got)
	}
	p = &Processor{Dialect: "sqlserver"}
	if got := p.dateOnlySelectExpr("baseline_change_date"); got != "CONVERT(VARCHAR(10), baseline_change_date, 23)" {
		t.Fatalf("sqlserver expr=%q", got)
	}
}
