package migrationflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"gorm.io/gorm"
)

type automationFakeMediaClient struct {
	automationFakeDownloader
	path                     string
	available                bool
	fileChecks, reads, shots int
	screenshotResult         string
	onScreenshot             func()
}

func (f *automationFakeMediaClient) CompletedMediaPath(hash, savePath, contentPath string) (string, error) {
	if hash != f.status.Hash || savePath != f.status.SavePath || contentPath != f.path {
		return "", errors.New("unexpected media path arguments")
	}
	return f.path, nil
}

func (f *automationFakeMediaClient) CheckMediaPathByProxy(_ context.Context, mediaPath string) (bool, error) {
	f.fileChecks++
	return f.available && mediaPath == f.path, nil
}

func (f *automationFakeMediaClient) FetchMediaInfoByProxyContext(_ context.Context, mediaPath, name string) (string, bool, error) {
	f.reads++
	if mediaPath != f.path || name != "Movie.mkv" {
		return "", false, errors.New("unexpected media path")
	}
	return strings.Replace(preparedReportFixture, "General\n", "General\nComplete name : "+mediaPath+"\n", 1), false, nil
}

func (f *automationFakeMediaClient) FetchScreenshotsByProxyContext(_ context.Context, mediaPath, name string, subtitle *int) (string, error) {
	f.shots++
	if mediaPath != f.path || name != "Movie.mkv" || subtitle != nil {
		return "", errors.New("unexpected screenshot arguments")
	}
	if f.onScreenshot != nil {
		f.onScreenshot()
	}
	return f.screenshotResult, nil
}

func automationShotFixture() string {
	var shots []string
	for i := 1; i <= 3; i++ {
		shots = append(shots, fmt.Sprintf("[url=https://pixhost.to/show/100/shot%d.png][img]https://img3.pixhost.to/images/100/shot%d.png[/img][/url]", i, i))
	}
	return strings.Join(shots, "\n")
}

func newAutomationFakeMediaClient(task repository.AutomationTask) *automationFakeMediaClient {
	mediaPath := task.SavePath + "/Movie.mkv"
	return &automationFakeMediaClient{
		automationFakeDownloader: automationFakeDownloader{complete: true, status: downloaderclient.AutomationTorrentStatus{
			Found: true, Hash: task.InfoHash, SavePath: task.SavePath, ContentPath: mediaPath, Progress: 1,
		}},
		path: mediaPath, available: true, screenshotResult: automationShotFixture(),
	}
}

func clearAutomationFixtureReport(t *testing.T, db *gorm.DB, task repository.AutomationTask, body string) {
	t.Helper()
	if err := db.Table("seed_parameters").Where("hash = ?", task.InfoHash).Updates(map[string]any{
		"mediainfo": "", "mediainfo_status": "queued", "body": body,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

// TestAutomationMediaWaitsResumesAndQueuesOnce 覆盖离线退避、身份绑定、自动回填及仅交接一次队列。
func TestAutomationMediaWaitsResumesAndQueuesOnce(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	automationReviewSeed(t, db, task, "none", 0)
	clearAutomationFixtureReport(t, db, task, "")
	automationReviewSettings(t, a)
	now := a.now()
	a.now = func() time.Time { return now }
	client := newAutomationFakeMediaClient(task)
	client.available = false
	a.downloaderHook = func(string) (automationDownloader, error) { return client, nil }

	a.Tick(context.Background())
	row, err := a.repo.Task(task.ID)
	if err != nil || row.State != repository.AutomationStateWaitingMedia || row.PublishAttempted || row.QueueID != nil ||
		client.fileChecks != 1 || client.reads != 0 || client.shots != 0 || client.adds != 0 {
		t.Fatalf("offline task: %+v client=%+v err=%v", row, client, err)
	}
	client.available = true
	now = now.Add(automationMediaRetryDelay / 2)
	a.Tick(context.Background())
	if client.fileChecks != 1 || client.reads != 0 || client.shots != 0 {
		t.Fatalf("retry ignored backoff: %+v", client)
	}
	now = now.Add(automationMediaRetryDelay / 2)
	a.Tick(context.Background())
	a.Tick(context.Background())
	row, err = a.repo.Task(task.ID)
	if err != nil || row.State != "queued_publish" || row.PublishAttempted || row.QueueID == nil ||
		client.adds != 0 || client.reads != 1 || client.shots != 1 {
		t.Fatalf("resumed task: %+v client=%+v err=%v", row, client, err)
	}
	seed, err := m.repo.GetSeedParameter(task.SourceID, "Source")
	screens := mediaFieldForCompare(seed["screenshots"])
	report := mediaFieldForCompare(seed["mediainfo"])
	if err != nil || seed["screenshot_review_status"] != "none" || seed["mediainfo_status"] != "completed" ||
		automationScreenshotPayload(screens, "none") != nil || !strings.Contains(report, "Complete name : Movie.mkv") ||
		strings.Contains(report, task.SavePath) {
		t.Fatalf("invalid merged media: %v err=%v", seed, err)
	}
	var count int64
	if err := db.Model(&repository.PublishQueueTask{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("queue count=%d err=%v", count, err)
	}
}

// TestAutomationMediaReusesQuoteAndScreenshots 验证只缺报告时先从简介补全，完全不派发客户端媒体任务。
func TestAutomationMediaReusesQuoteAndScreenshots(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	screens := automationReviewSeed(t, db, task, "none", 3)
	quoted := strings.Replace(preparedReportFixture, "Width : 3840 pixels", "Width : 3840 pixels\nHeight : 2160 pixels", 1) + "\nChannel(s) : 2 channels"
	clearAutomationFixtureReport(t, db, task, "[quote]"+quoted+"[/quote]")
	client := newAutomationFakeMediaClient(task)
	a.downloaderHook = func(string) (automationDownloader, error) { return client, nil }
	a.Tick(context.Background())
	row, _ := a.repo.Task(task.ID)
	seed, err := m.repo.GetSeedParameter(task.SourceID, "Source")
	if err != nil || row.State != "ready" || seed["screenshots"] != screens || seed["mediainfo"] != quoted ||
		client.fileChecks != 0 || client.reads != 0 || client.shots != 0 {
		t.Fatalf("source media not reused: %+v seed=%v client=%+v err=%v", row, seed, client, err)
	}
}

// TestAutomationMediaRejectsWrongHash 验证文件内容相似时也不能以另一下载代替冻结种子。
func TestAutomationMediaRejectsWrongHash(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	automationReviewSeed(t, db, task, "none", 0)
	client := newAutomationFakeMediaClient(task)
	client.status.Hash = strings.Repeat("b", 40)
	a.downloaderHook = func(string) (automationDownloader, error) { return client, nil }
	a.Tick(context.Background())
	row, _ := a.repo.Task(task.ID)
	seed, err := m.repo.GetSeedParameter(task.SourceID, "Source")
	if err != nil || row.State != repository.AutomationStateWaitingMedia || row.QueueID != nil ||
		seed["screenshots"] != "" || client.fileChecks != 0 || client.shots != 0 || client.adds != 0 {
		t.Fatalf("media request with wrong hash: %+v seed=%v client=%+v err=%v", row, seed, client, err)
	}
}

// TestAutomationMediaDoesNotOverwriteManualEdit 验证远程执行期间更改的人工截图不会被覆盖或自动放行。
func TestAutomationMediaDoesNotOverwriteManualEdit(t *testing.T) {
	a, m, db := automationNativeFixture(t)
	task := automationDownloadTask(t, a, db)
	automationReviewSeed(t, db, task, "none", 0)
	client := newAutomationFakeMediaClient(task)
	client.onScreenshot = func() {
		if err := db.Table("seed_parameters").Where("hash = ?", task.InfoHash).Updates(map[string]any{
			"screenshots": "[img]https://manual.example/1.png[/img]", "screenshot_review_status": "pending",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	a.downloaderHook = func(string) (automationDownloader, error) { return client, nil }
	a.Tick(context.Background())
	row, _ := a.repo.Task(task.ID)
	seed, err := m.repo.GetSeedParameter(task.SourceID, "Source")
	if err != nil || row.State != repository.AutomationStateWaitingMedia || row.QueueID != nil ||
		seed["screenshot_review_status"] != "pending" || seed["screenshots"] != "[img]https://manual.example/1.png[/img]" ||
		client.reads != 0 || client.shots != 1 {
		t.Fatalf("manual edit overwritten: %+v seed=%v client=%+v err=%v", row, seed, client, err)
	}
}

// TestAutomationPixhostScreenshotsRejectsUntrustedRows 验证只接受真实全尺寸图片及有效的独立帧。
func TestAutomationPixhostScreenshotsRejectsUntrustedRows(t *testing.T) {
	raw := automationShotFixture()
	if shots, err := automationPixhostScreenshots(raw); err != nil || strings.Count(shots, "[img]") != 3 || strings.Contains(shots, "[url=") {
		t.Fatalf("valid Pixhost result: %s %v", shots, err)
	}
	for _, invalid := range []string{
		strings.ReplaceAll(raw, "img3.pixhost.to", "img3.pixhost.to.example"),
		strings.ReplaceAll(raw, "/images/", "/thumbs/"),
		strings.ReplaceAll(raw, ".png", ".gif"),
		raw + "\n[url=https://external.example]unexpected[/url]",
		strings.Repeat(strings.Split(raw, "\n")[0]+"\n", 3),
	} {
		if _, err := automationPixhostScreenshots(invalid); err == nil {
			t.Fatalf("untrusted screenshot allowed: %s", invalid)
		}
	}
}
