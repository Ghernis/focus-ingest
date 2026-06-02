package parquet

import "testing"

func TestDecimalFlbaString(t *testing.T) {
	// 0.0037210639 at scale 18 (row 0 from Azure sample).
	b := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0d, 0x38, 0x49, 0xb5, 0x23, 0x5c, 0x00}
	got := decimalFlbaString(&b)
	if got == nil {
		t.Fatal("expected value")
	}
	if *got != "0.00372106392" {
		t.Fatalf("got %q want 0.00372106392", *got)
	}

	zero := decimalFlbaString(&[16]byte{})
	if zero == nil || *zero != "0" {
		t.Fatalf("zero got %v", zero)
	}
	if decimalFlbaString(nil) != nil {
		t.Fatal("nil input should be nil")
	}
}
