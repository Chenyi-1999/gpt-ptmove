package migrationflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// TestZmptBatchWaitsForAllDownloadsAndHandlesMixedEvidence exercises the persisted IYUU barrier.
func TestZmptBatchWaitsForAllDownloadsAndHandlesMixedEvidence(t *testing.T) {
	s, db := automationFixture(t)
	if err := db.Table("sites").Where("site = ?", "zmpt").Update("base_url", "https://zmpt.cc").Error; err != nil {
		t.Fatal(err)
	}
	var err error
	s.cfg, err = config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.cfg.Save(map[string]any{"iyuu_token": "fixture-only"}); err != nil {
		t.Fatal(err)
	}
	settings := policy.Defaults()
	settings.Enabled = true
	settings.DownloaderID = "qb-fixture"
	settings.SavePath = "/fixture/downloads"
	rawSettings, _ := json.Marshal(settings)
	now := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if err = s.repo.SaveSettings(string(rawSettings), false, now); err != nil {
		t.Fatal(err)
	}
	source, target, err := s.resolve(settings)
	if err != nil {
		t.Fatal(err)
	}
	route := automationSnapshot{Settings: settings, Source: *source, Target: *target}
	rawRun, _ := json.Marshal(automationRunSnapshot{Route: route})
	rawRoute, _ := json.Marshal(route)
	run := repository.AutomationRun{ID: "batch-fixture", Month: policy.Month(now), Execute: true, Status: "batch_active", Selected: 3, Snapshot: string(rawRun)}
	if err := s.repo.CreateRun(&run); err != nil {
		t.Fatal(err)
	}
	for i, letter := range []string{"a", "b", "c"} {
		state := repository.AutomationStateWaitingBatch
		if i == 2 {
			state = "downloading"
		}
		row := repository.AutomationTask{ID: letter, RunID: run.ID, Month: run.Month, State: state,
			Source: source.ID, Target: target.ID, SourceID: letter, InfoHash: strings.Repeat(letter, 40),
			DownloadAttempted: true, BatchCheck: true, Snapshot: string(rawRoute)}
		if err = db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	s.SetIYUUDuplicateLookup(func(_ context.Context, url string, hashes []string) (map[string]bool, error) {
		calls++
		if url != target.BaseURL || len(hashes) != 3 {
			t.Fatalf("unexpected IYUU batch target or size: %s %v", url, hashes)
		}
		return map[string]bool{strings.Repeat("a", 40): true, strings.Repeat("b", 40): false}, nil
	})
	s.processIYUUBatches(context.Background(), settings)
	if calls != 0 {
		t.Fatal("IYUU queried before all downloads completed")
	}
	if err = db.Model(&repository.AutomationTask{}).Where("id = ?", "c").Update("state", repository.AutomationStateWaitingBatch).Error; err != nil {
		t.Fatal(err)
	}
	s.processIYUUBatches(context.Background(), settings)
	s.processIYUUBatches(context.Background(), settings)
	if calls != 1 {
		t.Fatalf("expected one batched lookup and backoff for unknown result, got %d", calls)
	}
	for id, state := range map[string]string{"a": repository.AutomationStateDuplicateCleanup, "b": "preparing", "c": repository.AutomationStateWaitingBatch} {
		row, err := s.repo.Task(id)
		if err != nil || row.State != state || (row.BatchCheckedAt > 0) != (id != "c") {
			t.Fatalf("unexpected task %s state=%s checked=%d err=%v", id, row.State, row.BatchCheckedAt, err)
		}
	}
	if pending, err := s.repo.BatchCleanupPending(run.ID, now); err != nil || !pending {
		t.Fatalf("confirmed duplicate should be handled before other publishes: %v %v", pending, err)
	}
	duplicate, err := s.repo.Claim("a", "cleanup-fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	s.finishBatchDuplicate(duplicate, route)
	if pending, err := s.repo.BatchCleanupPending(run.ID, now); err != nil || pending {
		t.Fatalf("handled duplicate should not delay supported publications: %v %v", pending, err)
	}
	var queued int64
	if err := db.Model(&repository.PublishQueueTask{}).Count(&queued).Error; err != nil || queued != 0 {
		t.Fatalf("batch check attempted publication before media preparation: %d %v", queued, err)
	}
}

// TestAutomationActiveProgressShowsConcurrentTasksAndQBSample 覆盖进度快照、活跃过滤和实际派单顺序。
func TestAutomationActiveProgressShowsConcurrentTasksAndQBSample(t *testing.T) {
	s, db := automationFixture(t)
	now := time.Date(2026, 11, 23, 2, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	settings := policy.Defaults()
	settings.Enabled = true
	settings.DownloaderID = "qb"
	settings.SavePath = "/downloads"
	settingsJSON, _ := json.Marshal(settings)
	if err := s.repo.SaveSettings(string(settingsJSON), false, now); err != nil {
		t.Fatal(err)
	}
	source, target, err := s.resolve(settings)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(automationSnapshot{Settings: settings, Source: *source, Target: *target})
	for _, task := range []repository.AutomationTask{
		{ID: "big", State: "reserved", Size: 5 * policy.GiB},
		{ID: "small", State: "reserved", Size: policy.GiB},
		{ID: "downloading", State: "downloading", Size: 2 * policy.GiB},
		{ID: "closed", State: "succeeded"},
	} {
		task.SourceID, task.Source, task.Target = task.ID, source.ID, target.ID
		task.Snapshot, task.DownloaderID, task.SavePath = string(snapshot), settings.DownloaderID, settings.SavePath
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	work, err := s.repo.WorkTasks()
	if err != nil || len(work) != 3 || work[1].ID != "small" || work[2].ID != "big" {
		t.Fatalf("unexpected task dispatch order: %v, %v", len(work), err)
	}
	fake := &automationFakeDownloader{status: downloaderclient.AutomationTorrentStatus{
		Found: true, SavePath: settings.SavePath, Progress: 0.42, ProgressKnown: true, AmountLeft: 42,
	}}
	s.downloaderHook = func(string) (automationDownloader, error) { return fake, nil }
	claimed, err := s.repo.Claim("downloading", "progress-lease", now)
	if err != nil {
		t.Fatal(err)
	}
	s.advanceTask(context.Background(), claimed)
	result, status := s.ListActiveTasks(context.Background())
	rows, ok := result["tasks"].([]repository.AutomationTaskProgress)
	if status != 200 || !ok || len(rows) != 3 || rows[0].ID != "downloading" ||
		rows[0].DownloadProgress == nil || *rows[0].DownloadProgress != 0.42 || fake.adds != 0 {
		t.Fatalf("unexpected dashboard task snapshot: %+v, %d", result, status)
	}
}
