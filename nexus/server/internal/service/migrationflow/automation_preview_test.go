package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWebAutomationPreviewPersistsAcrossReopen(t *testing.T) {
	s, db := automationFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if result, status := s.LatestWebPreview(ctx); status != 200 || result["preview"] != nil {
		t.Fatalf("initial preview should be absent: %d %+v", status, result)
	}

	automationFakePages(t, s)
	result, status := s.WebPreview(ctx, map[string]any{"min_seeders": 4, "save_path": "/fixture-private-path"})
	if status != 200 || result["previewed_at"] != now.UnixMilli() || result["preview_saved"] != true {
		t.Fatalf("web preview failed: %d %+v", status, result)
	}
	row, err := s.repo.LatestPreview()
	if err != nil || row == nil {
		t.Fatalf("web preview not persisted: %+v %v", row, err)
	}
	for _, unwanted := range []string{"fixture-private-path", "secret", "info_hash", "torrent"} {
		if strings.Contains(row.JSON, unwanted) {
			t.Fatalf("web preview persisted extra data %q: %s", unwanted, row.JSON)
		}
	}
	assertWebPreviewDidNotWriteExecutionRows(t, db)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err = sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := gorm.Open(sqlite.Open(filepath.Join(os.Getenv("PTNEXUS_DATA_DIR"), "jobs.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	reopenedSQL, err := reopened.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedSQL.Close() })
	repo, err := repository.NewAutomationRepository(&repository.Store{DB: reopened, DBType: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	restored := &AutomationService{repo: repo}
	got := getWebPreviewFixture(t, restored)
	rows := got["candidates"].([]automationPreviewCandidate)
	settings := got["settings"].(automationPreviewSettings)
	if len(rows) != 1 || rows[0].Title != "Movie-GROUP" || rows[0].ID != "1" ||
		rows[0].DetailURL != "https://source.example.com/details.php?id=1" || !rows[0].Eligible ||
		settings.SourceSite != "zmpt" || settings.TargetSite != "novahd" || settings.MinSeeders != 4 ||
		got["previewed_at"] != now.UnixMilli() ||
		got["source_digest"] == nil || got["source_digest"] != result["source_digest"] ||
		got["target_digest"] == nil || got["target_digest"] != result["target_digest"] {
		t.Fatalf("wrong restored preview: %+v", got)
	}
}

func TestWebAutomationPreviewRetainsSuccessOnFailureAndReplacesWithEmptySuccess(t *testing.T) {
	s, db := automationFixture(t)
	ctx := context.Background()
	automationFakePages(t, s)
	if result, status := s.Preview(ctx, nil); status != 200 || result["previewed_at"] != nil {
		t.Fatalf("machine preview contract changed: %d %+v", status, result)
	}
	if result, status := s.LatestWebPreview(ctx); status != 200 || result["preview"] != nil {
		t.Fatalf("machine preview wrote a web snapshot: %d %+v", status, result)
	}
	if result, status := s.WebPreview(ctx, nil); status != 200 || result["preview_saved"] != true {
		t.Fatalf("initial web preview failed: %d %+v", status, result)
	}
	committed, err := s.repo.LatestPreview()
	if err != nil || committed == nil {
		t.Fatalf("expected successful snapshot: %+v %v", committed, err)
	}
	s.scanner.Client.Transport = automationTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fixture request failed")
	})
	if result, status := s.WebPreview(ctx, nil); status == 200 {
		t.Fatalf("failed scan was reported as successful: %+v", result)
	}
	unchanged, err := s.repo.LatestPreview()
	if err != nil || unchanged == nil || *unchanged != *committed {
		t.Fatalf("failed scan replaced snapshot: %+v %v", unchanged, err)
	}

	automationFakePages(t, s, "Machine-only-GROUP")
	if result, status := s.Preview(ctx, nil); status != 200 {
		t.Fatalf("machine preview failed: %d %+v", status, result)
	}
	unchanged, err = s.repo.LatestPreview()
	if err != nil || unchanged == nil || *unchanged != *committed {
		t.Fatalf("machine preview replaced snapshot: %+v %v", unchanged, err)
	}

	s.scanner.Client.Transport = automationTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<p>没有找到种子</p>")), Header: make(http.Header)}, nil
	})
	if result, status := s.WebPreview(ctx, nil); status != 200 || result["preview_saved"] != true {
		t.Fatalf("empty scan did not save: %d %+v", status, result)
	}
	if rows := getWebPreviewFixture(t, s)["candidates"].([]automationPreviewCandidate); rows == nil || len(rows) != 0 {
		t.Fatalf("empty scan did not replace snapshot with an empty array: %+v", rows)
	}
	assertWebPreviewDidNotWriteExecutionRows(t, db)
}

func TestWebAutomationPreviewIgnoresLateOlderCompletion(t *testing.T) {
	s, db := automationFixture(t)
	automationFakePages(t, s, "Newer-GROUP")
	if result, status := s.preview(context.Background(), nil, 2); status != 200 || result["preview_saved"] != true {
		t.Fatalf("newer scan failed: %d %+v", status, result)
	}
	automationFakePages(t, s, "Older-GROUP")
	if result, status := s.preview(context.Background(), nil, 1); status != 200 || result["preview_saved"] != false {
		t.Fatalf("older result should be returned but not persisted: %d %+v", status, result)
	}
	rows := getWebPreviewFixture(t, s)["candidates"].([]automationPreviewCandidate)
	if len(rows) != 1 || rows[0].Title != "Newer-GROUP" {
		t.Fatalf("older scan replaced newer: %+v", rows)
	}
	var count int64
	if err := db.Model(&repository.AutomationPreviewRecord{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("preview should use one row: %d %v", count, err)
	}
}

func TestWebAutomationPreviewStoresOnlyDisplayData(t *testing.T) {
	settings := policy.Defaults()
	settings.SavePath = "/private/path"
	settings.DuplicateDirectory = "/private/duplicates"
	rows := []policy.Candidate{
		{
			ID: "17", Title: "Example", InfoHash: "private_hash", Torrent: []byte("unexported_torrent"),
			DetailURL: "https://example.com/details.php?passkey=private_value&id=17&view=foo#fragment",
		},
		{ID: "18", DetailURL: "https://user:private_value@example.com/details.php?id=18"},
		{ID: "19", DetailURL: "https://example.com/torrents/19?opaque=private_value"},
	}
	snapshot := buildWebPreviewSnapshot(rows, automationSnapshot{Settings: settings})
	if snapshot.Candidates[0].DetailURL != "https://example.com/details.php?id=17" ||
		snapshot.Candidates[1].DetailURL != "" ||
		snapshot.Candidates[2].DetailURL != "https://example.com/torrents/19" {
		t.Fatalf("unsafe preview detail links: %+v", snapshot.Candidates)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"private", "unexported", "\"info_hash\"", "\"save_path\"", "\"duplicate_directory\""} {
		if strings.Contains(string(raw), unwanted) {
			t.Fatalf("unexpected display snapshot data %q in %s", unwanted, raw)
		}
	}
}

func TestAutomationRunChecksSelectedCandidateSitesBeforeCreatingRun(t *testing.T) {
	s, db := automationFixture(t)
	for _, tc := range []struct {
		name string
		body map[string]any
		want int
		hint string
	}{
		{"missing target", map[string]any{"preview_source_site": "zmpt"}, 400, "同时提供"},
		{"invalid source", map[string]any{"preview_source_site": false, "preview_target_site": "novahd"}, 400, "同时提供"},
		{"different source", map[string]any{"preview_source_site": "other", "preview_target_site": "novahd"}, 409, "不一致"},
		{"different target", map[string]any{"preview_source_site": "zmpt", "preview_target_site": "other"}, 409, "不一致"},
		{"missing target digest", map[string]any{"preview_source_digest": "source"}, 400, "声明摘要"},
		{"matching sites", map[string]any{"preview_source_site": "zmpt", "preview_target_site": "novahd"}, 400, "选择原生下载器"},
		{"legacy omitted sites", map[string]any{}, 400, "选择原生下载器"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.body["selected_ids"] = []string{"17"}
			tc.body["enqueue"] = true
			result, status := s.Run(context.Background(), tc.body)
			message, _ := result["message"].(string)
			if status != tc.want || !strings.Contains(message, tc.hint) {
				t.Fatalf("unexpected selected candidate response: %d %+v", status, result)
			}
		})
	}
	assertWebPreviewDidNotWriteExecutionRows(t, db)
}

func TestAutomationRunRejectsPreviewWhenSiteOriginChangedWithSameID(t *testing.T) {
	for _, site := range []string{"zmpt", "novahd"} {
		t.Run(site, func(t *testing.T) {
			s, db := automationFixture(t)
			ctx := context.Background()
			automationFakePages(t, s)
			if result, status := s.WebPreview(ctx, nil); status != 200 {
				t.Fatalf("web preview failed: %d %+v", status, result)
			}
			preview := getWebPreviewFixture(t, s)
			if result, status := s.UpdateSettings(map[string]any{"downloader_id": "qb", "save_path": "/fixture"}); status != 200 {
				t.Fatalf("could not configure fixture downloader: %d %+v", status, result)
			}
			s.downloaderHook = func(string) (automationDownloader, error) { return &automationFakeDownloader{}, nil }
			if err := db.Table("sites").Where("site = ?", site).Update("base_url", "https://changed.example.com").Error; err != nil {
				t.Fatal(err)
			}
			result, status := s.Run(ctx, map[string]any{
				"selected_ids":        []string{"1"},
				"enqueue":             true,
				"preview_source_site": "zmpt", "preview_target_site": "novahd",
				"preview_source_digest": preview["source_digest"], "preview_target_digest": preview["target_digest"],
			})
			message, _ := result["message"].(string)
			if status != 409 || !strings.Contains(message, "站点声明已改变") {
				t.Fatalf("same ID with changed profile was not rejected: %d %+v", status, result)
			}
			assertWebPreviewDidNotWriteExecutionRows(t, db)
		})
	}
}

func getWebPreviewFixture(t *testing.T, s *AutomationService) map[string]any {
	t.Helper()
	result, status := s.LatestWebPreview(context.Background())
	preview, ok := result["preview"].(map[string]any)
	if status != 200 || !ok {
		t.Fatalf("expected persisted preview: %d %+v", status, result)
	}
	return preview
}

func assertWebPreviewDidNotWriteExecutionRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range []any{&repository.AutomationTask{}, &repository.AutomationQuota{}, &repository.AutomationRun{}, &repository.PublishQueueTask{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("unexpected execution rows in %T: %d %v", model, count, err)
		}
	}
}
