package migrationflow

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

func TestPublishUsesServerSourceContextAndIgnoresEditableFields(t *testing.T) {
	const torrent = "d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name4:test12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee"
	const detail = `<h1>Movie</h1><div id="kdescr">基本信息</div><a href="/download.php?id=12">download</a>`
	meta, err := acquirefetch.ParseTorrentMeta([]byte(torrent))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body, hash, status string
		reads                    int
	}{
		{"denied source", detail + "<p>限转</p>", meta.InfoHash, "denied", 1},
		{"unknown source", "<h1>Maintenance</h1>", meta.InfoHash, "unknown", 1},
		{"old task missing hash", detail, "", "unknown", 0},
		{"old task wrong hash", detail, strings.Repeat("a", 40), "unknown", 2},
		{"verified preview", detail, meta.InfoHash, "allowed", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, m, _ := automationNativeFixture(t)
			reads := 0
			a.scanner.Client.Transport = automationTransport(func(req *http.Request) (*http.Response, error) {
				reads++
				if req.Method != "GET" || req.URL.Host != "source.example.com" || req.URL.Query().Get("id") != "12" {
					t.Fatalf("request fields overrode source identity: %s %s", req.Method, req.URL)
				}
				body := test.body
				if req.URL.Path == "/download.php" {
					body = torrent
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			m.contextState.Set("trusted", publishworkflow.Context{
				TaskID: "trusted", TorrentID: "12", SiteName: "zmpt", SourceDetailURL: "12", Hash: test.hash,
			})
			payload := map[string]any{
				"task_id": "trusted", "targetSite": "Target", "sourceSite": "fabricated",
				"source_restriction_status": "allowed", "torrent_id": "99", "hash": meta.InfoHash,
				"upload_data": map[string]any{"restricted": false, "tags": []any{}},
			}
			var result map[string]any
			var code int
			if test.status == "allowed" {
				result, code = m.CheckSourceRestriction("trusted")
				if code != 200 || result["success"] != true {
					t.Fatalf("code=%d result=%v", code, result)
				}
			} else {
				result, code = m.Publish(payload)
				if code != 409 || result["success"] != false {
					t.Fatalf("code=%d result=%v", code, result)
				}
			}
			if result["source_restriction_status"] != test.status || reads != test.reads {
				t.Fatalf("reads=%d result=%v", reads, result)
			}
		})
	}
}

func TestSourceRestrictionStopsAllBatchTargetsAndOldQueueTask(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	if err := db.Table("sites").Create(map[string]any{"site": "target2", "nickname": "Other", "migration": 3}).Error; err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	a.scanner.Client.Transport = automationTransport(func(req *http.Request) (*http.Response, error) {
		reads.Add(1)
		if req.Method != "GET" || req.URL.Host != "source.example.com" || req.URL.Path != "/details.php" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<div>禁止转载</div>"))}, nil
	})
	ctx := publishworkflow.Context{TaskID: "trusted", TorrentID: "12", SiteName: "zmpt", Hash: strings.Repeat("a", 40)}
	m.contextState.Set(ctx.TaskID, ctx)
	m.publishState.Start("test", 2, 2, time.Now())
	m.runPublishBatch("test", map[string]any{"task_id": ctx.TaskID, "source_restriction_status": "allowed"}, []string{"Target", "Other"}, 2)
	batch, ok := m.publishState.Status("test")
	if !ok || batch.Success != 0 || batch.Failed != 2 || reads.Load() != 1 {
		t.Fatalf("batch=%+v reads=%d", batch, reads.Load())
	}
	for _, result := range batch.Results {
		if result["source_restriction_status"] != "denied" {
			t.Fatalf("batch lost source denial: %v", result)
		}
	}

	ctx.Hash = "" // 模拟升级前缺少可信种子身份的待上传任务。
	snapshot, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	queue := repository.PublishQueueTask{
		Trigger: "queue", Status: "running", SourceSite: "zmpt", TorrentID: "12", TargetSite: "Target",
		PayloadJSON: `{"targetSite":"Target","source_restriction_status":"allowed"}`, UploadDataJSON: "{}", ContextJSON: string(snapshot),
	}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	m.executePublishQueueTask(publishQueueConfig{}, queue, nil)
	finished, _, err := m.queueRepo.FindTaskByID(queue.ID)
	if err != nil || finished.Status != "failed" || finished.NextRunAt != nil ||
		!strings.Contains(finished.LastResult, `"source_restriction_status":"unknown"`) || reads.Load() != 1 {
		t.Fatalf("old queue task=%+v reads=%d err=%v", finished, reads.Load(), err)
	}
}

func TestAutomaticSourceDenialFailsBeforeAttempt(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	raw := []byte("d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name9:video.mkv12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee")
	meta, err := acquirefetch.ParseTorrentMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.torrent")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	task.InfoHash = meta.InfoHash
	task.TorrentPath = path
	automationReviewSeed(t, db, task, "none", 3)
	ctx := publishworkflow.Context{TorrentID: task.SourceID, SiteName: "zmpt", Hash: task.InfoHash, OriginalTorrentPath: path}
	ctxJSON, _ := json.Marshal(ctx)
	queue := repository.PublishQueueTask{Trigger: "automation", Status: "running", TargetSite: "Target", PayloadJSON: `{"targetSite":"Target"}`, UploadDataJSON: "{}", ContextJSON: string(ctxJSON)}
	if err := db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Updates(map[string]any{
		"state": "queued_publish", "queue_id": queue.ID, "info_hash": task.InfoHash, "torrent_path": path,
	}).Error; err != nil {
		t.Fatal(err)
	}
	settings := policy.Defaults()
	settings.Enabled, settings.AutoPublish = true, true
	settings.DownloaderID, settings.SavePath = task.DownloaderID, task.SavePath
	settingsJSON, _ := json.Marshal(settings)
	if err := a.repo.SaveSettings(string(settingsJSON), false, a.now()); err != nil {
		t.Fatal(err)
	}
	a.downloaderHook = func(string) (automationDownloader, error) {
		return &automationFakeDownloader{
			status: downloaderclient.AutomationTorrentStatus{Found: true, Progress: 1, SavePath: task.SavePath}, complete: true,
		}, nil
	}
	reads := 0
	a.scanner.Client.Transport = automationTransport(func(req *http.Request) (*http.Response, error) {
		reads++
		if req.Method != "GET" || req.URL.Host != "source.example.com" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		// 详情页给出明确禁止；后续读取失败也不得丢失已知禁转。
		status, body := 200, "<div>仅限本站</div>"
		if req.URL.Path != "/details.php" {
			status, body = http.StatusForbidden, ""
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	m.executePublishQueueTask(publishQueueConfig{}, queue, nil)
	row, err := a.repo.Task(task.ID)
	if err != nil || row.State != "failed" || row.PublishAttempted || !strings.Contains(row.Message, "禁转") || reads < 1 {
		t.Fatalf("automation row=%+v reads=%d err=%v", row, reads, err)
	}
}
