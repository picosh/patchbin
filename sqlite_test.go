package patchbin

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/picosh/patchbin/util"
)

func TestSqliteFreshSchema(t *testing.T) {
	dataDir := util.CreateTmpDir()
	defer func() {
		_ = os.RemoveAll(dataDir)
	}()

	dbPath := filepath.Join(dataDir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := SqliteOpen("file:"+dbPath+"?_fk=on", logger)
	if err != nil {
		t.Fatalf("SqliteOpen failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	var userVersion int
	err = db.QueryRow("PRAGMA user_version;").Scan(&userVersion)
	if err != nil {
		t.Fatalf("failed to query user_version: %v", err)
	}
	if userVersion != 21 {
		t.Errorf("expected user_version 21, got %d", userVersion)
	}
}

func TestSqliteMigrationV21(t *testing.T) {
	// Check if data/pr.db exists
	if _, err := os.Stat("data/pr.db"); os.IsNotExist(err) {
		t.Skip("data/pr.db does not exist, skipping migration test")
	}

	dataDir := util.CreateTmpDir()
	defer func() {
		_ = os.RemoveAll(dataDir)
	}()

	dbPath := filepath.Join(dataDir, "migrated.db")

	// Copy data/pr.db to temp dir
	src, err := os.Open("data/pr.db")
	if err != nil {
		t.Fatalf("failed to open source db: %v", err)
	}
	defer func() {
		_ = src.Close()
	}()

	dst, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("failed to create dest db: %v", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatalf("failed to copy db: %v", err)
	}
	_ = dst.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := SqliteOpen("file:"+dbPath+"?_fk=on", logger)
	if err != nil {
		t.Fatalf("SqliteOpen failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	var userVersion int
	err = db.QueryRow("PRAGMA user_version;").Scan(&userVersion)
	if err != nil {
		t.Fatalf("failed to query user_version: %v", err)
	}
	if userVersion != 21 {
		t.Errorf("expected user_version 21, got %d", userVersion)
	}

	// Verify existing PRs got slug backfilled to CAST(id AS TEXT)
	rows, err := db.Query("SELECT id, slug FROM patch_requests LIMIT 10;")
	if err != nil {
		t.Fatalf("failed to query patch_requests: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	count := 0
	for rows.Next() {
		var id int64
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		count++
		if slug == "" {
			t.Errorf("PR #%d has empty slug", id)
		}
	}
	if count == 0 {
		t.Errorf("expected at least 1 PR row, got 0")
	}

	// Verify unique constraint on (repo_name, slug)
	_, err = db.Exec("INSERT INTO patch_requests (user_id, repo_name, slug, name) VALUES (1, 'pico', '2', 'duplicate');")
	if err == nil {
		t.Errorf("expected unique constraint violation on (repo_name, slug), got nil")
	}
}
