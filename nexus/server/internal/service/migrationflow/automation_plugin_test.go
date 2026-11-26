package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

func TestAutomationPluginBlocksExplicitRunsAndAllowsPreview(t *testing.T) {
	a, db := automationFixture(t)
	ctx := context.Background()
	if err := a.SetPluginEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	calls := automationFakePages(t, a)
	a.downloaderHook = func(string) (automationDownloader, error) {
		t.Fatal("disabled execution looked up downloader")
		return nil, nil
	}
	for _, request := range []map[string]any{
		{"enqueue": true},
		{"selected_ids": []string{"1"}},
	} {
		result, status := a.Run(ctx, request)
		if status != 409 || result["message"] != repository.ErrAutomationPluginDisabled.Error() {
			t.Fatalf("disabled explicit action returned %d: %v", status, result)
		}
	}
	a.Tick(ctx)
	var runs int64
	if err := db.Model(&repository.AutomationRun{}).Count(&runs).Error; err != nil || runs != 0 || *calls != 0 {
		t.Fatalf("disabled automation ran: network=%d runs=%d err=%v", *calls, runs, err)
	}
	result, status := a.Preview(ctx, map[string]any{})
	if status != 200 || result["success"] != true || *calls == 0 {
		t.Fatalf("disabled plugin blocked read-only preview: %d %v", status, result)
	}
}

func TestAutomationPluginStopsMediaAndQueueBeforeSideEffects(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	automationReviewSettings(t, a)
	ctx := context.Background()
	if err := db.Model(&task).Update("state", "preparing").Error; err != nil {
		t.Fatal(err)
	}
	task, err := a.repo.Claim(task.ID, "claimed-before-disable", a.now())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetPluginEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	var snapshot automationSnapshot
	if err := json.Unmarshal([]byte(task.Snapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	mock := newAutomationFakeMediaClient(task)
	a.downloaderHook = func(string) (automationDownloader, error) {
		t.Fatal("stopped preparation opened the downloader")
		return nil, nil
	}
	a.scanner.Client.Transport = automationTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("stopped preparation contacted a tracker")
		return nil, nil
	})
	a.Tick(ctx)
	if err := a.prepareAutomationMedia(ctx, task, snapshot, mock); !errors.Is(err, repository.ErrAutomationPluginDisabled) {
		t.Fatalf("media preparation: %v", err)
	}
	if mock.fileChecks != 0 || mock.reads != 0 || mock.shots != 0 {
		t.Fatalf("disabled prepare requested media: %+v", mock)
	}
	queue := repository.PublishQueueTask{Status: "running", Trigger: "automation"}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.prepareAutomationQueue(queue, publishworkflow.Context{}); !errors.Is(err, repository.ErrAutomationPluginDisabled) {
		t.Fatalf("queue preflight bypassed plugin gate: %v", err)
	}
	if err := m.authorizeAutomationUpload(queue); !errors.Is(err, repository.ErrAutomationPluginDisabled) {
		t.Fatalf("final upload authorization bypassed plugin gate: %v", err)
	}
	m.executePublishQueueTask(publishQueueConfig{}, queue, nil)
	after, _, err := m.queueRepo.FindTaskByID(queue.ID)
	if err != nil || after.Status != repository.PublishQueueStatusQueued || after.AttemptCount != 0 {
		t.Fatalf("paused queue should stay queued: %+v, %v", after, err)
	}
}

func TestAutomationPluginReconcilesExistingSeedWithoutAdding(t *testing.T) {
	a, _, db := automationNativeFixture(t)
	target, err := a.profiles.Resolve("novahd")
	if err != nil {
		t.Fatal(err)
	}
	target.AnnounceHosts = []string{"announce.target.example.com"}
	target.Revision = 2
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repository.SiteProfileRow{ID: target.ID, Origin: target.BaseURL, Revision: 2, JSON: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	task := automationDownloadTask(t, a, db)
	var snapshot automationSnapshot
	if err := json.Unmarshal([]byte(task.Snapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.SetPluginEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{"state": "seeding", "target_hash": task.InfoHash, "publish_attempted": true}).Error; err != nil {
		t.Fatal(err)
	}
	mock := &automationPluginReconcileDownloader{
		automationFakeDownloader: automationFakeDownloader{
			status:   downloaderclient.AutomationTorrentStatus{Found: true, Hash: task.InfoHash, Progress: 1, SavePath: task.SavePath, State: "uploading"},
			complete: true,
		},
		announceHost: snapshot.Target.AnnounceHosts[0],
	}
	a.downloaderHook = func(string) (automationDownloader, error) { return mock, nil }
	a.Tick(ctx)
	got, err := a.repo.Task(task.ID)
	if err != nil || got.State != "succeeded" || mock.target != 0 {
		t.Fatalf("paused reconciliation should read and finish without a qB add: %+v mock=%+v err=%v", got, mock, err)
	}
	// Simulate a qB request reaching the guard as the plugin is toggled off and back on.
	path := filepath.Join(t.TempDir(), "target.torrent")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPluginEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{"state": "seeding", "target_torrent_path": path}).Error; err != nil {
		t.Fatal(err)
	}
	mock.onTarget = func() error {
		if err := a.SetPluginEnabled(ctx, false); err != nil {
			t.Fatal(err)
		}
		refused := a.requirePluginEnabled(ctx)
		if err := a.SetPluginEnabled(ctx, true); err != nil {
			t.Fatal(err)
		}
		return refused
	}
	a.Tick(ctx)
	got, err = a.repo.Task(task.ID)
	if err != nil || got.State != "succeeded" || mock.target != 1 {
		t.Fatalf("quick re-enable lost the guard refusal: %+v mock=%+v err=%v", got, mock, err)
	}
}

type automationPluginReconcileDownloader struct {
	automationFakeDownloader
	announceHost string
	target       int
	onTarget     func() error
}

func (m *automationPluginReconcileDownloader) Trackers(string) ([]downloaderclient.AutomationTracker, error) {
	return []downloaderclient.AutomationTracker{{URL: "https://" + m.announceHost + "/announce", Status: 2}}, nil
}

func (m *automationPluginReconcileDownloader) EnsureTargetSeeding([]byte, string, string, string) error {
	m.target++
	if m.onTarget != nil {
		return m.onTarget()
	}
	return nil
}

func TestAutomationPluginCanResumeOnlyBeforeSiteWriteAuthorization(t *testing.T) {
	a, m, _ := automationNativeFixture(t)
	if err := a.SetPluginEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	disabled := m.authorizeAutomationUpload(repository.PublishQueueTask{})
	if !errors.Is(disabled, repository.ErrAutomationPluginDisabled) {
		t.Fatalf("disabled gate returned %v", disabled)
	}
	for _, tc := range []struct {
		name       string
		checks     []error
		canRestore bool
	}{
		{name: "disabled before first write", checks: []error{disabled}, canRestore: true},
		{name: "prior authorized write", checks: []error{nil, disabled}},
		{name: "unknown workflow error", checks: []error{errors.New("response unavailable")}},
		{name: "guard not reached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fence automationUploadFence
			for _, err := range tc.checks {
				if fence.record(err) != err {
					t.Fatal("write guard result changed")
				}
			}
			if got := fence.stoppedBeforeFirstWrite(); got != tc.canRestore {
				t.Fatalf("can restore=%v, want %v", got, tc.canRestore)
			}
		})
	}
}
