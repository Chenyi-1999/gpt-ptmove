package migrationflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
	"gorm.io/gorm"
)

func automationNativeFixture(t *testing.T) (*AutomationService, *MigrateService, *gorm.DB) {
	t.Helper()
	a, db := automationFixture(t)
	store := &repository.Store{DB: db, DBType: "sqlite"}
	m := NewMigrateService(repository.NewMigrateRepository(store), nil)
	m.automation = a
	a.migrate = m
	m.InitPublishQueue(repository.NewPublishQueueRepository(store), nil)
	return a, m, db
}

// TestNativeManualPublishCannotBypassAutomationEvidence 验证手动触发标记和站点昵称不能绕过自动发布账本。
func TestNativeManualPublishCannotBypassAutomationEvidence(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	for i, state := range []string{"needs_review", "publishing", "succeeded"} {
		hash := fmt.Sprintf("%040d", i+1)
		task := repository.AutomationTask{ID: fmt.Sprint(i), Source: "zmpt", Target: "novahd", SourceID: fmt.Sprint(i + 1), InfoHash: hash, Month: "2026-10", State: state, PublishAttempted: true}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		ctxID := "ctx-" + task.ID
		m.contextState.Set(ctxID, publishworkflow.Context{TaskID: ctxID, TorrentID: task.SourceID, SiteName: "Source", Hash: strings.ToUpper(hash)})
		result, status := m.Publish(map[string]any{"task_id": ctxID, "targetSite": "Target", "publish_trigger": "automation", "queue_task_id": 100, "upload_data": map[string]any{}})
		if status != 409 || result["success"] != false || !strings.Contains(result["message"].(string), "核对") {
			t.Fatalf("manual bypass state=%s status=%d result=%v", state, status, result)
		}
	}
	rows, _ := a.repo.Tasks()
	if len(rows) != 3 {
		t.Fatal("manual rejection modified ledger")
	}
}

// TestNativePublishGuardMatchesSourceIDAndAllowsOnlyCurrentAutomaticQueue 验证无哈希时按源标识拦截，自动任务仅豁免受信队列自身。
func TestNativePublishGuardMatchesSourceIDAndAllowsOnlyCurrentAutomaticQueue(t *testing.T) {
	_, m, db := automationNativeFixture(t)
	queueID := int64(77)
	task := repository.AutomationTask{ID: "uncertain", Source: "zmpt", Target: "novahd", SourceID: "123", InfoHash: strings.Repeat("a", 40), Month: "2026-10", State: "publishing", PublishAttempted: true, QueueID: &queueID}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"targetSite": "Target", "seed_id": "unused", "publish_trigger": "automation"}
	ctx := publishworkflow.Context{SiteName: "Source", TorrentID: "123"}
	if err := m.guardNativePublishEvidence(payload, ctx, 0); err == nil {
		t.Fatal("source ID fallback did not block")
	}
	ctx.Hash = task.InfoHash
	if err := m.guardNativePublishEvidence(payload, ctx, queueID); err != nil {
		t.Fatalf("current automatic queue blocked: %v", err)
	}
	ctx.SiteName = "different-source"
	ctx.TorrentID = "other"
	if err := m.guardNativePublishEvidence(payload, ctx, 0); err == nil {
		t.Fatal("same payload from another source bypassed ledger")
	}
	if err := m.guardNativePublishEvidence(map[string]any{"targetSite": "unrelated-target"}, ctx, 0); err != nil {
		t.Fatalf("unrelated target was blocked: %v", err)
	}
}

// TestNativeManualQueueCannotRepublishUncertainAutomation 验证普通队列也无法绕过不确定自动上传的账本。
func TestNativeManualQueueCannotRepublishUncertainAutomation(t *testing.T) {
	_, m, db := automationNativeFixture(t)
	hash := strings.Repeat("b", 40)
	task := repository.AutomationTask{ID: "prior", Source: "zmpt", Target: "novahd", SourceID: "123", InfoHash: hash, Month: "2026-10", State: "needs_review", PublishAttempted: true}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.torrent")
	raw := []byte("d4:infod6:lengthi1610612736e4:name9:video.mkvee")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := publishworkflow.Context{SiteName: "zmpt", TorrentID: "123", Hash: hash, OriginalTorrentPath: path}
	ctxJSON, _ := json.Marshal(ctx)
	queue := repository.PublishQueueTask{Trigger: "queue", Status: "running", SourceSite: "zmpt", TargetSite: "Target", TorrentID: "123", PayloadJSON: `{"targetSite":"Target"}`, UploadDataJSON: `{}`, ContextJSON: string(ctxJSON)}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	m.executePublishQueueTask(publishQueueConfig{}, queue, nil)
	row, _, err := m.queueRepo.FindTaskByID(queue.ID)
	if err != nil || row.Status != "failed" || !strings.Contains(row.LastError, "核对") {
		t.Fatalf("ordinary queue bypass: %+v %v", row, err)
	}
}

// TestAutomationNativePrecheckFailuresEndTaskWithoutUpload 验证完整种子大于1GiB但视频仅0.75GiB，以及无效入参，均同步结束自动任务。
func TestAutomationNativePrecheckFailuresEndTaskWithoutUpload(t *testing.T) {
	for _, failure := range []string{"video_size", "payload", "context", "upload_data"} {
		t.Run(failure, func(t *testing.T) {
			a, m, db := automationNativeFixture(t)
			video := int64(.75 * float64(policy.GiB))
			extra := policy.GiB / 2
			raw := []byte(fmt.Sprintf("d4:infod5:filesld6:lengthi%de4:pathl9:video.mkveed6:lengthi%de4:pathl10:extras.bineee4:name7:releaseee", video, extra))
			path := filepath.Join(t.TempDir(), "source.torrent")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			ctxJSON, _ := json.Marshal(publishworkflow.Context{SiteName: "zmpt", TorrentID: "123", OriginalTorrentPath: path})
			payload := `{"targetSite":"Target"}`
			if failure == "payload" {
				payload = "["
			}
			if failure == "context" {
				ctxJSON = []byte("[")
			}
			uploadJSON := "{}"
			if failure == "upload_data" {
				uploadJSON = "["
			}
			queue := repository.PublishQueueTask{Trigger: "automation", Status: "running", TargetSite: "Target", SourceSite: "zmpt", TorrentID: "123", PayloadJSON: payload, UploadDataJSON: uploadJSON, ContextJSON: string(ctxJSON)}
			if err := db.Create(&queue).Error; err != nil {
				t.Fatal(err)
			}
			task := repository.AutomationTask{ID: "precheck", Source: "zmpt", Target: "novahd", SourceID: "123", InfoHash: strings.Repeat("c", 40), Size: video + extra, Month: "2026-10", State: "queued_publish", QueueID: &queue.ID, DownloadAttempted: true}
			if err := a.repo.Reserve(&task, 6, 60*policy.GiB); err != nil {
				t.Fatal(err)
			}
			m.executePublishQueueTask(publishQueueConfig{}, queue, nil)
			row, _ := a.repo.Task(task.ID)
			native, _, _ := m.queueRepo.FindTaskByID(queue.ID)
			if row.State != "failed" || row.PublishAttempted || native.Status != "failed" {
				t.Fatalf("precheck left task active: %+v queue=%+v", row, native)
			}
			if failure == "video_size" && !strings.Contains(row.Message, "1GB") {
				t.Fatalf("wrong video precheck failure: %s", row.Message)
			}
			quota, _ := a.repo.Quota(task.Month)
			if quota.Count != 1 || quota.Bytes != task.Size {
				t.Fatalf("touched reservation released: %+v", quota)
			}
		})
	}
}

// TestAutomaticUploadBoundaryRechecksDisableWithoutResettingAttempt 验证标记后关闭模块会拒绝实际上传，且不清除不确定证据。
func TestAutomaticUploadBoundaryRechecksDisableWithoutResettingAttempt(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	screenshots := automationReviewSeed(t, db, task, "none", 3)
	queueID := int64(19)
	if err := db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{"queue_id": queueID, "state": "publishing", "publish_attempted": true}).Error; err != nil {
		t.Fatal(err)
	}
	settings := policy.Defaults()
	settings.Enabled = true
	settings.AutoPublish = true
	settings.DownloaderID = "qb"
	settings.SavePath = "/nas/data"
	raw, _ := json.Marshal(settings)
	if err := a.repo.SaveSettings(string(raw), false, time.Now()); err != nil {
		t.Fatal(err)
	}
	upload, _ := json.Marshal(map[string]any{"screenshots": screenshots})
	queue := repository.PublishQueueTask{ID: queueID, Trigger: "automation", UploadDataJSON: string(upload)}
	if err := m.authorizeAutomationUpload(queue); err != nil {
		t.Fatalf("valid attempt was blocked: %v", err)
	}
	path := filepath.Join(t.TempDir(), "changed.torrent")
	if err := os.WriteFile(path, []byte("d4:infod6:lengthi1e4:name1:xee"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.authorizeAutomationUpload(queue, path); err == nil || !strings.Contains(err.Error(), "载荷") {
		t.Fatalf("changed torrent was allowed: %v", err)
	}

	settings.Enabled = false
	raw, _ = json.Marshal(settings)
	if err := a.repo.SaveSettings(string(raw), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := m.authorizeAutomationUpload(queue); err == nil {
		t.Fatal("disabled module allowed upload")
	}
	row, _ := a.repo.Task(task.ID)
	if !row.PublishAttempted || row.State != "publishing" {
		t.Fatal("disable cleared irreversible attempt marker")
	}
}

// TestAutomationSavePathEquivalencePreservesRoots 验证尾部斜线兼容而非空根目录不会与空值或盘符相对路径混淆。
func TestAutomationSavePathEquivalencePreservesRoots(t *testing.T) {
	for _, pair := range [][2]string{{"/nas/data", "/nas/data/"}, {`C:\media`, `C:\media\`}, {"/", "///"}, {`C:\`, `C:/`}} {
		if !sameAutomationSavePath(pair[0], pair[1]) {
			t.Fatalf("equivalent paths rejected: %#v", pair)
		}
	}
	for _, pair := range [][2]string{{"/", ""}, {`C:\`, `C:`}, {"/nas/data", "/nas/other"}} {
		if sameAutomationSavePath(pair[0], pair[1]) {
			t.Fatalf("different paths accepted: %#v", pair)
		}
	}
}
