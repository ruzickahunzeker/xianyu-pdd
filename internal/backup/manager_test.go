package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

func TestSQLiteBackupCreatesVerifiedSnapshotAndMappingExport(t *testing.T) {
	ctx := context.Background()
	database, dialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO users (username,password_hash,is_admin,created_at) VALUES ('backup-user','hash',1,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := database.QueryRow(`SELECT id FROM users WHERE username='backup-user'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO item_pdd_sku_mappings (user_id,cookie_id,item_id,xianyu_sku_id,source_goods_id,source_sku_id,xianyu_properties_json,mapping_source,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, userID, "c1", "i1", "x1", "g1", "s1", "[]", "manual", 1, 1); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	manager := New(database, dialect, "", dir)
	manager.now = func() time.Time { return time.Unix(1_700_000_000, 123) }
	entry, err := manager.Create(ctx, 14)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Verified || entry.MappingRows != 1 || entry.SizeBytes == 0 || len(entry.SHA256) != 64 {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if _, err := os.Stat(filepath.Join(dir, entry.Filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "xianyu-sku-mappings-"+entry.ID+".csv")); err != nil {
		t.Fatal(err)
	}
	listed, err := manager.List()
	if err != nil || len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	if _, _, err := manager.Path("../../etc/passwd"); !os.IsNotExist(err) {
		t.Fatalf("path traversal must be rejected: %v", err)
	}
}

func TestSanitizeOutputRedactsDatabaseURL(t *testing.T) {
	m := New(nil, db.DialectPostgres, "postgres://user:secret@db/x", t.TempDir())
	got := m.sanitizeOutput("connection to postgres://user:secret@db/x failed")
	if got == "" || got == "connection to postgres://user:secret@db/x failed" {
		t.Fatalf("database URL was not redacted: %q", got)
	}
}
