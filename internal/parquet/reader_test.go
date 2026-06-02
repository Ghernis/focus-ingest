package parquet

import (
	"testing"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
)

func TestInt96Str(t *testing.T) {
	// 2026-04-09 from sample Azure FOCUS export.
	i96 := deprecated.Int96{0, 0, 2461140}
	got := int96Str(i96)
	if got == nil {
		t.Fatal("expected timestamp")
	}
	if *got != "2026-04-09 00:00:00" {
		t.Fatalf("got %q want %q", *got, "2026-04-09 00:00:00")
	}

	zero := deprecated.Int96{}
	if int96Str(zero) != nil {
		t.Fatal("zero int96 should be nil")
	}
}

func TestInt96StrWithNanos(t *testing.T) {
	i96 := deprecated.Int96{1_234_567_890, 0, 2461140}
	got := int96Str(i96)
	if got == nil {
		t.Fatal("expected timestamp")
	}
	want := time.Unix((2461140-julianEpoch)*86400, 1_234_567_890).UTC().Format("2006-01-02 15:04:05")
	if *got != want {
		t.Fatalf("got %q want %q", *got, want)
	}
}
