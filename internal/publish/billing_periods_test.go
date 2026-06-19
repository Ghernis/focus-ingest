package publish

import "testing"

func TestComputePublishPeriods(t *testing.T) {
	tests := []struct {
		name     string
		periods  []string
		primary  string
		previous string
		publish  []string
		overlap  bool
	}{
		{
			name:     "single month",
			periods:  []string{"2026-07-01"},
			primary:  "2026-07-01",
			previous: "2026-06-01",
			publish:  []string{"2026-07-01"},
		},
		{
			name:     "overlap excludes previous month",
			periods:  []string{"2026-06-01", "2026-07-01"},
			primary:  "2026-07-01",
			previous: "2026-06-01",
			publish:  []string{"2026-07-01"},
			overlap:  true,
		},
		{
			name:     "historical month two with merge",
			periods:  []string{"2026-01-01", "2026-02-01"},
			primary:  "2026-02-01",
			previous: "2026-01-01",
			publish:  []string{"2026-02-01"},
			overlap:  true,
		},
		{
			name:    "empty",
			periods: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary, previous, publish, overlap := computePublishPeriods(tc.periods)
			if primary != tc.primary {
				t.Fatalf("primary: got %q want %q", primary, tc.primary)
			}
			if previous != tc.previous {
				t.Fatalf("previous: got %q want %q", previous, tc.previous)
			}
			if overlap != tc.overlap {
				t.Fatalf("overlap: got %v want %v", overlap, tc.overlap)
			}
			if len(publish) != len(tc.publish) {
				t.Fatalf("publish: got %v want %v", publish, tc.publish)
			}
			for i := range publish {
				if publish[i] != tc.publish[i] {
					t.Fatalf("publish[%d]: got %q want %q", i, publish[i], tc.publish[i])
				}
			}
		})
	}
}
