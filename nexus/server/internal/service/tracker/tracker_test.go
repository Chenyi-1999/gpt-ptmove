package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/stats"
	"gorm.io/gorm"
)

func TestRecentSpeedDataIncludesLiveSamplesAcrossFlush(t *testing.T) {
	testDir := t.TempDir()
	cfgPath := filepath.Join(testDir, "config.json")
	cfgJSON := `{
		"auth": {"password_hash": "test-fixture"},
		"downloaders": [{"id": "qb", "name": "qBittorrent"}, {"id": "tr", "name": "Transmission"}]
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(testDir, "traffic.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := repository.NewStatsRepository(&repository.Store{DB: db, DBType: "sqlite"})
	if err := repo.EnsureTrafficStatsTables(); err != nil {
		t.Fatal(err)
	}
	tracker := New(repo, cfg)
	service := stats.New(repo, cfg, tracker.RecentSpeedSamples)
	now := time.Now().Truncate(time.Second)
	history := now.Add(-12 * time.Second)
	first := now.Add(-8 * time.Second)
	second := now.Add(-7 * time.Second)

	if _, err := repo.UpsertTrafficStats([]repository.TrafficStatRecord{
		{StatDatetime: history, DownloaderID: "qb", UploadSpeed: 90, DownloadSpeed: 190},
		{StatDatetime: first, DownloaderID: "qb", UploadSpeed: 13, DownloadSpeed: 23},
		{StatDatetime: first, DownloaderID: "tr", UploadSpeed: 53, DownloadSpeed: 63},
	}); err != nil {
		t.Fatal(err)
	}

	tracker.recordRecentSpeed(first, "qb", 123, 456)
	tracker.appendRecords([]repository.TrafficStatRecord{
		{StatDatetime: first, DownloaderID: "qb", UploadSpeed: 123, DownloadSpeed: 456},
	})
	initial, err := service.GetRecentSpeedData(0)
	if err != nil {
		t.Fatal(err)
	}
	assertRecentSpeed(t, initial, history, "qb", 90, 190)
	assertRecentSpeed(t, initial, first, "qb", 123, 456)
	assertRecentSpeed(t, initial, first, "tr", 53, 63)

	// 正常采到的零速率与另一台下载器缺少样本是两种状态。
	tracker.recordRecentSpeed(second, "qb", 0, 0)
	updated, err := service.GetRecentSpeedData(0)
	if err != nil {
		t.Fatal(err)
	}
	assertRecentSpeed(t, updated, second, "qb", 0, 0)
	if entry := recentSpeedEntry(t, updated, second, "tr"); entry["sampled"] != false {
		t.Fatalf("independent downloader unexpectedly marked as sampled: %v", entry)
	}

	tracker.flushBuffer("test")
	persisted, err := repo.QuerySpeedAverages(first.Add(-time.Second), first.Add(time.Second), "%H:%M:%S")
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 || persisted[0].DownloaderID == persisted[1].DownloaderID {
		t.Fatalf("flush should persist one row per downloader: %v", persisted)
	}
	for _, row := range persisted {
		if row.DownloaderID == "qb" && row.ULSpeed != 123 {
			t.Fatalf("flush did not replace the older persisted speed: %v", row)
		}
	}
	afterFlush, err := service.GetRecentSpeedData(0)
	if err != nil {
		t.Fatal(err)
	}
	assertRecentSpeed(t, afterFlush, first, "qb", 123, 456)
	assertRecentSpeed(t, afterFlush, first, "tr", 53, 63)
	assertRecentSpeed(t, afterFlush, second, "qb", 0, 0)
}

func recentSpeedEntry(t *testing.T, response map[string]any, at time.Time, downloaderID string) map[string]any {
	t.Helper()
	label := at.Format(time.TimeOnly)
	var match map[string]any
	for _, dataset := range response["datasets"].([]map[string]any) {
		if dataset["time"] != label {
			continue
		}
		if match != nil {
			t.Fatalf("duplicate dataset for %s", label)
		}
		match = dataset["speeds"].(map[string]any)[downloaderID].(map[string]any)
	}
	if match == nil {
		t.Fatalf("no dataset for %s/%s in %v", label, downloaderID, response)
	}
	return match
}

func assertRecentSpeed(t *testing.T, response map[string]any, at time.Time, downloaderID string, ul, dl float64) {
	t.Helper()
	entry := recentSpeedEntry(t, response, at, downloaderID)
	if entry["sampled"] != true || entry["ul_speed"] != ul || entry["dl_speed"] != dl {
		t.Fatalf("rate at %s/%s = %v, want sampled upload=%v download=%v", at.Format(time.TimeOnly), downloaderID, entry, ul, dl)
	}
}

func TestRecentSpeedSamplesSameSecondAndWindow(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	start := time.Date(2026, time.November, 22, 23, 59, 50, 0, loc)
	tracker := New(nil, nil)
	for i := 0; i <= recentSpeedWindowSeconds; i++ {
		tracker.recordRecentSpeed(start.Add(time.Duration(i)*time.Second), "qb", int64(i), 0)
	}
	last := start.Add(recentSpeedWindowSeconds * time.Second)
	tracker.recordRecentSpeed(last.Add(time.Millisecond), "qb", 500, 0)

	rows := tracker.RecentSpeedSamples(start, last.Add(time.Second))
	if len(rows) != recentSpeedWindowSeconds {
		t.Fatalf("retained %d samples, want %d", len(rows), recentSpeedWindowSeconds)
	}
	rows = tracker.RecentSpeedSamples(last, last.Add(time.Second))
	if len(rows) != 1 || rows[0].ULSpeed != 500 || rows[0].TimeGroup != last.Format(time.TimeOnly) {
		t.Fatalf("latest sample in local time = %v", rows)
	}
	rows = tracker.RecentSpeedSamples(start, last.Add((recentSpeedWindowSeconds+1)*time.Second))
	if len(rows) != 0 {
		t.Fatalf("expired samples remain: %v", rows)
	}
}
