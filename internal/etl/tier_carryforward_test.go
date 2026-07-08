package etl

import (
	"fmt"
	"testing"
)

func TestParseDecimal_DoesNotUseFmtSprintOnByteSlice(t *testing.T) {
	// go-mssqldb can return DECIMAL aggregates as []byte; fmt.Sprint([]byte("54"))
	// yields "[54 ...]" which parseDecimal rejects as zero.
	val := []byte("54.0000000000")
	if got := parseDecimal(fmt.Sprint(val)); got != 0 {
		t.Fatalf("expected fmt.Sprint([]byte) to fail parsing, got %v", got)
	}
	if got := parseDecimal(string(val)); got != 54 {
		t.Fatalf("string([]byte)=%v want 54", got)
	}
}
