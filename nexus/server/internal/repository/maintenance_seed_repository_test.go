package repository

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMaintenanceSeedUpdatesOnlyExactUnchangedRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "maintenance.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE seed_parameters (hash TEXT, torrent_id TEXT, site_name TEXT,
		updated_at TEXT, bdinfo_task_id TEXT, screenshot_review_status TEXT, body TEXT,
		PRIMARY KEY (hash, torrent_id, site_name))`).Error; err != nil {
		t.Fatal(err)
	}
	mainHash, otherHash := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, hash := range []string{mainHash, otherHash} {
		if err := db.Exec(`INSERT INTO seed_parameters (hash, torrent_id, site_name, updated_at, body)
			VALUES (?, '1', 'a', '2026-11-20 00:00:01', 'original')`, hash).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewMigrateRepository(&Store{DB: db})
	if _, err := repo.GetMaintenanceSeed("1", "a", ""); !errors.Is(err, ErrAmbiguousMaintenanceSeed) {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	snapshot, err := repo.GetMaintenanceSeed("1", "a", mainHash)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.UpdateMaintenanceSeed(snapshot, map[string]any{"body": "refetched"}); !ok || err != nil {
		t.Fatalf("first update: ok=%t err=%v", ok, err)
	}
	if ok, err := repo.UpdateMaintenanceSeed(snapshot, map[string]any{"body": "stale overwrite"}); ok || err != nil {
		t.Fatalf("stale update: ok=%t err=%v", ok, err)
	}
	updated, err := repo.GetMaintenanceSeed("1", "a", mainHash)
	if err != nil || updated["body"] != "refetched" {
		t.Fatalf("updated row: %v err=%v", updated, err)
	}
	untouched, err := repo.GetMaintenanceSeed("1", "a", otherHash)
	if err != nil || untouched["body"] != "original" {
		t.Fatalf("other row: %v err=%v", untouched, err)
	}
}
