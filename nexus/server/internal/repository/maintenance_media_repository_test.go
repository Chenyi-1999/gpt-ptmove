package repository

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newMaintenanceMediaRepository(t *testing.T) *MigrateRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "maintenance-media.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE seed_parameters (
		hash TEXT, torrent_id TEXT, site_name TEXT, screenshots TEXT, screenshot_review_status TEXT,
		mediainfo TEXT, mediainfo_status TEXT, bdinfo_task_id TEXT, statement TEXT, body TEXT,
		updated_at TEXT, PRIMARY KEY (hash, torrent_id, site_name))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO seed_parameters
		(hash, torrent_id, site_name, screenshots, screenshot_review_status, mediainfo, mediainfo_status, bdinfo_task_id, statement, body, updated_at)
		VALUES ('hash-1', '1', 'site-a', '', 'none', '', '', NULL, 'statement-v1', 'body-v1', '2026-11-01 00:00:00')`).Error; err != nil {
		t.Fatal(err)
	}
	return NewMigrateRepository(&Store{DB: db})
}

func TestUpdateMaintenanceMediaScreenshotsCASAllowsUnrelatedChanges(t *testing.T) {
	repo := newMaintenanceMediaRepository(t)
	expected := map[string]any{"screenshots": "", "screenshot_review_status": "none"}
	if err := repo.DB().Table("seed_parameters").Where("hash = ?", "hash-1").Update("body", "body-v2").Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateMaintenanceMedia("hash-1", "1", "site-a", expected, map[string]any{
		"screenshots": "[img]https://example.invalid/1.jpg[/img]", "screenshot_review_status": "confirmed",
	})
	if err != nil || !updated {
		t.Fatalf("expected screenshot update to succeed after unrelated edit, updated=%t err=%v", updated, err)
	}
	row, err := repo.GetMaintenanceSeed("1", "site-a", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if row["screenshots"] != "[img]https://example.invalid/1.jpg[/img]" || row["screenshot_review_status"] != "confirmed" || row["body"] != "body-v2" {
		t.Fatalf("unexpected saved media or overwritten unrelated field: %#v", row)
	}
}

func TestUpdateMaintenanceMediaRejectsConcurrentManualScreenshotChange(t *testing.T) {
	repo := newMaintenanceMediaRepository(t)
	expected := map[string]any{"screenshots": "", "screenshot_review_status": "none"}
	if err := repo.DB().Table("seed_parameters").Where("hash = ?", "hash-1").Updates(map[string]any{
		"screenshots": "manual images", "screenshot_review_status": "confirmed",
	}).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateMaintenanceMedia("hash-1", "1", "site-a", expected, map[string]any{
		"screenshots": "automatic images", "screenshot_review_status": "confirmed",
	})
	if err != nil || updated {
		t.Fatalf("stale screenshot update must be rejected, updated=%t err=%v", updated, err)
	}
}

func TestUpdateMaintenanceMediaInfoGuardsDescriptionAndBDInfoTask(t *testing.T) {
	repo := newMaintenanceMediaRepository(t)
	expected := map[string]any{
		"mediainfo": "", "mediainfo_status": "", "bdinfo_task_id": nil,
		"statement": "statement-v1", "body": "body-v1",
	}
	if err := repo.DB().Table("seed_parameters").Where("hash = ?", "hash-1").Updates(map[string]any{
		"statement": "statement-v2", "bdinfo_task_id": "new-bdinfo-task",
	}).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateMaintenanceMedia("hash-1", "1", "site-a", expected, map[string]any{
		"mediainfo": "a complete report", "mediainfo_status": "completed",
	})
	if err != nil || updated {
		t.Fatalf("stale report or BDInfo task must be rejected, updated=%t err=%v", updated, err)
	}
}

func TestUpdateMaintenanceMediaAllowsBodyChangeWithoutDescriptionGuard(t *testing.T) {
	repo := newMaintenanceMediaRepository(t)
	expected := map[string]any{"mediainfo": "", "mediainfo_status": "", "bdinfo_task_id": nil}
	if err := repo.DB().Table("seed_parameters").Where("hash = ?", "hash-1").Update("body", "body-v2").Error; err != nil {
		t.Fatal(err)
	}

	updated, err := repo.UpdateMaintenanceMedia("hash-1", "1", "site-a", expected, map[string]any{
		"mediainfo": "a complete report", "mediainfo_status": "completed",
	})
	if err != nil || !updated {
		t.Fatalf("unrelated body changes should not block downloader result, updated=%t err=%v", updated, err)
	}
}

func TestUpdateMaintenanceMediaMissingRowDoesNotInsertAndRejectsInvalidFields(t *testing.T) {
	repo := newMaintenanceMediaRepository(t)
	updated, err := repo.UpdateMaintenanceMedia("missing", "1", "site-a", map[string]any{
		"mediainfo": "", "mediainfo_status": "", "bdinfo_task_id": nil,
	}, map[string]any{"mediainfo": "report", "mediainfo_status": "completed"})
	if err != nil || updated {
		t.Fatalf("missing row must not be inserted, updated=%t err=%v", updated, err)
	}
	var count int64
	if err := repo.DB().Table("seed_parameters").Where("hash = ?", "missing").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("missing row was inserted, count=%d err=%v", count, err)
	}

	_, err = repo.UpdateMaintenanceMedia("hash-1", "1", "site-a", map[string]any{
		"mediainfo": "", "mediainfo_status": "", "bdinfo_task_id": nil,
	}, map[string]any{"screenshots": "only one screenshot field"})
	if err == nil {
		t.Fatal("partial screenshot update must be rejected")
	}
}
