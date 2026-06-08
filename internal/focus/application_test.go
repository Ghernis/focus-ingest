package focus

import "testing"

func TestNormalizeApplicationName(t *testing.T) {
	cases := map[string]string{
		"INS APP1":           "INS_APP1",
		"ins-app1":           "INS_APP1",
		"INS_APP1":           "INS_APP1",
		"  INS  AZ  OPENIA ": "INS__AZ__OPENIA",
		"":                   "(UNASSIGNED)",
		"   ":                "(UNASSIGNED)",
	}
	for in, want := range cases {
		if got := NormalizeApplicationName(in); got != want {
			t.Fatalf("NormalizeApplicationName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalApplicationName_Plural(t *testing.T) {
	cases := map[string]string{
		"NETWORKING_SERVICES": "NETWORKING_SERVICE",
		"NETWORKING_SERVICE":  "NETWORKING_SERVICE",
		"INS_APP1":            "INS_APP1",
		"(UNASSIGNED)":        "(UNASSIGNED)",
	}
	for in, want := range cases {
		if got := CanonicalApplicationName(in); got != want {
			t.Fatalf("CanonicalApplicationName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveApplicationName_PluralRaw(t *testing.T) {
	if got := ResolveApplicationName("Networking Services"); got != "NETWORKING_SERVICE" {
		t.Fatalf("plural raw resolve: %q", got)
	}
	if got := ResolveApplicationName("Networking Service"); got != "NETWORKING_SERVICE" {
		t.Fatalf("singular raw resolve: %q", got)
	}
}

func TestMergeAliasValues(t *testing.T) {
	if got := MergeAliasValues("", "INS APP1"); got != "INS APP1" {
		t.Fatalf("empty merge: %q", got)
	}
	if got := MergeAliasValues("INS APP1", "INS APP1"); got != "INS APP1" {
		t.Fatalf("dup merge: %q", got)
	}
	if got := MergeAliasValues("INS APP1", "ins app1"); got != "INS APP1" {
		t.Fatalf("case dup: %q", got)
	}
	if got := MergeAliasValues("INS APP1", "INS_APP1"); got != "INS APP1|INS_APP1" {
		t.Fatalf("append: %q", got)
	}
	if got := MergeAliasValues("INS APP1,INS_APP1", "ins app1"); got != "INS APP1|INS_APP1" {
		t.Fatalf("legacy comma: %q", got)
	}
}
