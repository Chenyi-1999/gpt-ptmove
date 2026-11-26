package migrationflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
	"gorm.io/gorm"
)

func automationReviewSeed(t *testing.T, db *gorm.DB, task repository.AutomationTask, status string, count int) string {
	t.Helper()
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS seed_parameters (hash TEXT,torrent_id TEXT,site_name TEXT,nickname TEXT,name TEXT,title TEXT,screenshots TEXT,screenshot_review_status TEXT,mediainfo TEXT,mediainfo_status TEXT,tags TEXT,body TEXT,statement TEXT,bdinfo_error TEXT,updated_at TEXT,PRIMARY KEY(hash,torrent_id,site_name))`).Error; err != nil {
		t.Fatal(err)
	}
	images := ""
	for i := 0; i < count; i++ {
		images += fmt.Sprintf("[img]https://images.example.com/%d.png[/img]\n", i)
	}
	if err := db.Table("seed_parameters").Create(map[string]any{"hash": task.InfoHash, "torrent_id": task.SourceID, "site_name": "zmpt", "nickname": "Source", "name": "movie", "screenshots": images, "screenshot_review_status": status, "mediainfo": preparedReportFixture, "mediainfo_status": "completed", "updated_at": "2026-11-15 00:00:00"}).Error; err != nil {
		t.Fatal(err)
	}
	return images
}

func automationReviewSettings(t *testing.T, a *AutomationService) {
	t.Helper()
	v := policy.Defaults()
	v.Enabled, v.AutoPublish = true, true
	v.DownloaderID, v.SavePath = "qb", "/nas/data"
	raw, _ := json.Marshal(v)
	if err := a.repo.SaveSettings(string(raw), false, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestAutomationScreenshotCanonicalGate 验证未知状态、空图和不足三张不能借标准化默认值自动通过。
func TestAutomationScreenshotCanonicalGate(t *testing.T) {
	for _, tc := range []struct {
		status  string
		count   int
		allowed bool
	}{{"none", 3, true}, {"confirmed", 1, true}, {"pending", 3, false}, {"unexpected", 3, false}, {"", 3, false}, {"none", 2, false}, {"none", 0, false}, {"confirmed", 0, false}} {
		t.Run(fmt.Sprintf("%s_%d", tc.status, tc.count), func(t *testing.T) {
			a, m, db := automationNativeFixture(t)
			task := automationDownloadTask(t, a, db)
			automationReviewSeed(t, db, task, tc.status, tc.count)
			var snapshot automationSnapshot
			_ = json.Unmarshal([]byte(task.Snapshot), &snapshot)
			_, err := m.automationSeedForPublish(task, snapshot)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
	for _, screenshots := range []string{"[img]javascript:alert(1)[/img]", "[img]https://image.example/1.png[/img][img]https://image.example/1.png[/img]"} {
		if err := automationScreenshotPayload(screenshots, "none"); err == nil {
			t.Fatal("invalid or duplicate images counted")
		}
	}
}

// TestAutomationPendingPreparationResumesConfirmedWithoutRefetch 验证待审查时不入队、不重新截图，确认后自动恢复。
func TestAutomationPendingPreparationResumesConfirmedWithoutRefetch(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	images := automationReviewSeed(t, db, task, "pending", 3)
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).Update("state", "preparing").Error; err != nil {
		t.Fatal(err)
	}
	fake := &automationFakeDownloader{status: downloaderclient.AutomationTorrentStatus{Found: true, Progress: 1, SavePath: task.SavePath}, complete: true}
	a.downloaderHook = func(string) (automationDownloader, error) { return fake, nil }
	now := a.now()
	a.now = func() time.Time { return now }
	a.Tick(context.Background())
	row, _ := a.repo.Task(task.ID)
	if row.State != repository.AutomationStateWaitingMedia || row.PublishAttempted || row.QueueID != nil || !strings.Contains(row.Message, "截图") {
		t.Fatalf("pending advanced: %+v", row)
	}
	result, status := m.UpdateScreenshotReviewStatus(map[string]any{"torrent_id": task.SourceID, "site_name": "Source", "screenshot_review_status": "confirmed"})
	if status != 200 {
		t.Fatalf("confirmation: %v", result)
	}
	now = now.Add(automationMediaRetryDelay)
	a.Tick(context.Background())
	row, _ = a.repo.Task(task.ID)
	seed, _ := m.repo.GetSeedParameter(task.SourceID, "Source")
	if row.State != "ready" || row.PublishAttempted || row.QueueID != nil || seed["screenshots"] != images || seed["screenshot_review_status"] != "confirmed" {
		t.Fatalf("confirmation refetched or failed: %+v seed=%v", row, seed)
	}
}

// TestAutomationPendingCannotEnqueueOrPreflight 验证缓存已确认队列不能覆盖当前待审核资料，且检查发生在下载器与网络前。
func TestAutomationPendingCannotEnqueueOrPreflight(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	automationReviewSeed(t, db, task, "pending", 3)
	var snapshot automationSnapshot
	_ = json.Unmarshal([]byte(task.Snapshot), &snapshot)
	if err := a.enqueueNative(task, snapshot); err == nil {
		t.Fatal("pending enqueued")
	}
	var count int64
	db.Model(&repository.PublishQueueTask{}).Count(&count)
	if count != 0 {
		t.Fatal("pending created a queue")
	}
	automationReviewSettings(t, a)
	queue := repository.PublishQueueTask{Status: "running", Trigger: "automation", UploadDataJSON: `{"screenshot_review_status":"confirmed","screenshots":"https://old.example/1.png"}`}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{"queue_id": queue.ID, "state": "queued_publish"}).Error; err != nil {
		t.Fatal(err)
	}
	a.downloaderHook = func(string) (automationDownloader, error) { t.Fatal("pending touched downloader"); return nil, nil }
	if err := m.prepareAutomationQueue(queue, publishworkflow.Context{}); err == nil || !strings.Contains(err.Error(), "截图") {
		t.Fatalf("preflight: %v", err)
	}
	if _, err := m.beginAutomationPublish(queue, a.now()); err == nil {
		t.Fatal("pending got attempt marker")
	}
	row, _ := a.repo.Task(task.ID)
	current, _, _ := m.queueRepo.FindTaskByID(queue.ID)
	if row.PublishAttempted || row.State != "queued_publish" || current.UploadDataJSON != queue.UploadDataJSON {
		t.Fatalf("pending changed queue/attempt: %+v %+v", row, current)
	}
}

// TestAutomationConfirmedQueueRefreshAndBoundaryFence 验证人工选图后原队列原子刷新，上传前再次退回待审核或改图都会拒绝。
func TestAutomationConfirmedQueueRefreshAndBoundaryFence(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	images := automationReviewSeed(t, db, task, "confirmed", 2)
	automationReviewSettings(t, a)
	queue := repository.PublishQueueTask{Status: "running", Trigger: "automation", UploadDataJSON: `{"screenshots":"old","screenshot_review_status":"pending","title":"frozen","intro":{"screenshots":"old","body":"frozen body"},"complete_publish_params":{"intro":{"screenshots":"old"}}}`}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{"state": "queued_publish", "queue_id": queue.ID}).Error; err != nil {
		t.Fatal(err)
	}
	upload, err := m.beginAutomationPublish(queue, a.now())
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := m.queueRepo.FindTaskByID(queue.ID)
	encoded, _ := json.Marshal(upload)
	if current.UploadDataJSON != string(encoded) || upload["screenshots"] != images || upload["title"] != "frozen" || upload["intro"].(map[string]any)["body"] != "frozen body" {
		t.Fatalf("snapshot refresh mismatch: %v %+v", upload, current)
	}
	queue.UploadDataJSON = current.UploadDataJSON
	if err := m.authorizeAutomationUpload(queue); err != nil {
		t.Fatalf("confirmed blocked: %v", err)
	}
	if _, err := m.beginAutomationPublish(queue, a.now()); err == nil {
		t.Fatal("second attempt allowed")
	}
	if err := db.Table("seed_parameters").Where("torrent_id = ?", task.SourceID).UpdateColumn("screenshot_review_status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := m.authorizeAutomationUpload(queue); err == nil {
		t.Fatal("stale confirmed queue allowed pending canonical")
	}
	if err := db.Table("seed_parameters").Where("torrent_id = ?", task.SourceID).Updates(map[string]any{"screenshot_review_status": "confirmed", "screenshots": "[img]https://new.example/new.png[/img]"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.authorizeAutomationUpload(queue); err == nil {
		t.Fatal("selection changed after marker allowed")
	}
	row, _ := a.repo.Task(task.ID)
	if !row.PublishAttempted || row.State != "publishing" {
		t.Fatalf("boundary reset attempt: %+v", row)
	}
}
