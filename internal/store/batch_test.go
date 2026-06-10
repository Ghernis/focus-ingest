package store

import (
	"context"
	"testing"
)

func TestPurgeLoadingBatchesForFile(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(t.TempDir()+"/batch_test.db", false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	db := s.(*sqliteStore).db
	id, err := s.BeginBatch(ctx, BatchMeta{SourceProvider: "MIXED", FocusVersion: "1.2", SourceFile: "sample.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO stg_focus_cost_line (ingestion_batch_id) VALUES (?)`, id); err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeStaleLoading(ctx, "sample.parquet", "1.2")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged=%d want 1", n)
	}
	var cnt int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dim_ingestion_batch WHERE ingestion_batch_id = ?`, id).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("batch row still exists")
	}
}
