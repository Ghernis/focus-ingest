package sqlserver

import "testing"

func TestChunkRows(t *testing.T) {
	tests := []struct {
		cols  int
		want  int
		maxP  int
	}{
		{57, 35, 35 * 57},
		{29, 68, 68 * 29},
		{15, 133, 133 * 15},
	}
	for _, tc := range tests {
		got := ChunkRows(tc.cols)
		if got != tc.want {
			t.Errorf("ChunkRows(%d) = %d, want %d", tc.cols, got, tc.want)
		}
		if tc.maxP > HardLimit {
			t.Errorf("chunk %d x %d cols = %d params exceeds hard limit", got, tc.cols, tc.maxP)
		}
	}
}

func TestCheckParamCount(t *testing.T) {
	if err := CheckParamCount(HardLimit); err != nil {
		t.Fatalf("at limit: %v", err)
	}
	if err := CheckParamCount(HardLimit + 1); err == nil {
		t.Fatal("expected error over hard limit")
	}
}
