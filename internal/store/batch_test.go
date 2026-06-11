package store

import (
	"context"
	"database/sql"
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

func TestGetBatchInfo(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(t.TempDir()+"/batch_info.db", false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	id, err := s.BeginBatch(ctx, BatchMeta{SourceProvider: "MIXED", FocusVersion: "1.2", SourceFile: "sample.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	db := s.(*sqliteStore).db
	if _, err := db.ExecContext(ctx, `INSERT INTO stg_focus_cost_line (ingestion_batch_id) VALUES (?)`, id); err != nil {
		t.Fatal(err)
	}

	info, err := s.GetBatchInfo(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if info.FocusVersion != "1.2" || info.SourceFile != "sample.parquet" || info.Status != "LOADING" || info.StagingRows != 1 {
		t.Fatalf("info=%+v", info)
	}

	_, err = s.GetBatchInfo(ctx, 99999)
	if err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestMarkBatchFailedRetry(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(t.TempDir()+"/batch_failed.db", false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}

	id, err := s.BeginBatch(ctx, BatchMeta{SourceProvider: "MIXED", FocusVersion: "1.2", SourceFile: "x.parquet"})
	if err != nil {
		t.Fatal(err)
	}
	db := s.(*sqliteStore).db
	if _, err := db.ExecContext(ctx, `UPDATE dim_ingestion_batch SET status = 'FAILED' WHERE ingestion_batch_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkBatchFailed(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM dim_ingestion_batch WHERE ingestion_batch_id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" {
		t.Fatalf("status=%q want FAILED", status)
	}
}
