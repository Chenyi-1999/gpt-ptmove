package migrationflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type automationTransport func(*http.Request) (*http.Response, error)

func (f automationTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func automationFixture(t *testing.T) (*AutomationService, *gorm.DB) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEV_ENV", "true")
	t.Setenv("PTNEXUS_DATA_DIR", dir)
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "jobs.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.Exec(`CREATE TABLE sites (site TEXT PRIMARY KEY,nickname TEXT,base_url TEXT,migration INTEGER,"group" TEXT,cookie TEXT,passkey TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []map[string]any{{"site": "zmpt", "nickname": "Source", "base_url": "https://source.example.com", "migration": 1, "group": "GROUP", "cookie": "secret"}, {"site": "novahd", "nickname": "Target", "base_url": "https://target.example.com", "migration": 2, "group": "", "cookie": "secret"}} {
		if err = db.Table("sites").Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := &repository.Store{DB: db, DBType: "sqlite"}
	if err = db.AutoMigrate(&repository.PublishQueueTask{}); err != nil {
		t.Fatal(err)
	}
	profiles, err := NewSiteProfileService(store, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAutomationService(store, nil, nil, profiles)
	if err != nil {
		t.Fatal(err)
	}
	return service, db
}
func automationFakePages(t *testing.T, s *AutomationService) *int {
	t.Helper()
	calls := new(int)
	s.scanner.Client.Transport = automationTransport(func(req *http.Request) (*http.Response, error) {
		*calls++
		if req.Method != "GET" {
			t.Fatalf("read-only operation performed %s", req.Method)
		}
		body := ""
		switch {
		case req.URL.Host == "target.example.com":
			body = "<p>没有找到相关种子</p>"
		case req.URL.Path == "/details.php":
			body = `<h1>Movie-GROUP</h1><div id="kdescr">description</div><table>基本信息 做种者: 5 下载者: 2</table><a href="/download.php?id=1">download</a>`
		case req.URL.Path == "/download.php":
			body = "d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name4:test12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee"
		default:
			body = `<a href="/details.php?id=1">Movie</a>`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	return calls
}
func TestAutomationPreviewHasNoDurableOrDownloaderEffects(t *testing.T) {
	s, db := automationFixture(t)
	calls := automationFakePages(t, s)
	s.downloaderHook = func(string) (automationDownloader, error) { t.Fatal("preview touched downloader"); return nil, nil }
	result, status := s.Preview(context.Background(), map[string]any{})
	if status != 200 || result["success"] != true {
		t.Fatalf("preview status=%d result=%v", status, result)
	}
	if *calls != 4 {
		t.Fatalf("requests=%d", *calls)
	}
	for _, model := range []any{&repository.AutomationTask{}, &repository.AutomationQuota{}, &repository.AutomationRun{}, &repository.PublishQueueTask{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("preview persisted %T: %d", model, count)
		}
	}
}
func TestAutomationRunIsDurableIntentWithoutNetworkInsideMachineTransaction(t *testing.T) {
	s, db := automationFixture(t)
	s.scanner.Client.Transport = automationTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("Run performed HTTP inside API transaction")
		return nil, nil
	})
	err := db.Transaction(func(tx *gorm.DB) error {
		result, status := s.Run(repository.WithMachineTransaction(context.Background(), tx), map[string]any{})
		if status != 202 || result["success"] != true {
			t.Fatalf("run status=%d result=%v", status, result)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := s.repo.Runs()
	if len(runs) != 1 || runs[0].Status != "queued" || runs[0].Execute {
		t.Fatalf("run intent %+v", runs)
	}
	calls := automationFakePages(t, s)
	s.Tick(context.Background())
	if *calls != 4 {
		t.Fatalf("background scanner requests=%d", *calls)
	}
	runs, _ = s.repo.Runs()
	if runs[0].Status != "completed" || runs[0].CandidatesJSON == "" {
		t.Fatalf("scan result not persisted %+v", runs[0])
	}
	q, _ := s.repo.Quota(policy.Month(s.now()))
	if q.Count != 0 {
		t.Fatal("disabled manual scan reserved quota")
	}
}

type automationFakeDownloader struct {
	status   downloaderclient.AutomationTorrentStatus
	complete bool
	calls    int
}

func (f *automationFakeDownloader) TorrentStatus(string) (downloaderclient.AutomationTorrentStatus, error) {
	f.calls++
	return f.status, nil
}
func (f *automationFakeDownloader) TorrentFilesComplete(string) (bool, error) {
	f.calls++
	return f.complete, nil
}
func (f *automationFakeDownloader) Trackers(string) ([]downloaderclient.AutomationTracker, error) {
	f.calls++
	return nil, nil
}
func (f *automationFakeDownloader) AddSourceTorrent([]byte, string, string, string) error {
	f.calls++
	return nil
}
func (f *automationFakeDownloader) EnsureTargetSeeding([]byte, string, string, string) error {
	f.calls++
	return nil
}
func (f *automationFakeDownloader) FreeSpace(string) (int64, error) {
	f.calls++
	return 100 * policy.GiB, nil
}
func automationDownloadTask(t *testing.T, s *AutomationService, db *gorm.DB) repository.AutomationTask {
	t.Helper()
	v := policy.Defaults()
	v.DownloaderID = "qb"
	v.SavePath = "/nas/data"
	source, target, err := s.resolve(v)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(automationSnapshot{Settings: v, Source: *source, Target: *target})
	task := repository.AutomationTask{ID: "download-task", Source: "zmpt", Target: "novahd", SourceID: "1", InfoHash: strings.Repeat("a", 40), Month: policy.Month(s.now()), Size: policy.GiB, State: "downloading", Snapshot: string(raw), Explicit: true, DownloaderID: "qb", SavePath: "/nas/data", DownloadAttempted: true}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}
func TestAutomationPartialFilesNeverEnterNativePreparation(t *testing.T) {
	s, db := automationFixture(t)
	task := automationDownloadTask(t, s, db)
	fake := &automationFakeDownloader{status: downloaderclient.AutomationTorrentStatus{Found: true, Progress: 1, AmountLeft: 0, SavePath: task.SavePath}, complete: false}
	s.downloaderHook = func(string) (automationDownloader, error) { return fake, nil }
	s.Tick(context.Background())
	row, _ := s.repo.Task(task.ID)
	if row.State != "downloading" {
		t.Fatalf("partial files advanced to %s", row.State)
	}
	fake.complete = true
	s.Tick(context.Background())
	row, _ = s.repo.Task(task.ID)
	if row.State != "failed" || !strings.Contains(row.Message, "原生迁移") {
		t.Fatalf("whole files did not reach native preparation boundary: %+v", row)
	}
}
func TestAutomationDisableAndFrozenOriginPreventNewExternalWork(t *testing.T) {
	s, db := automationFixture(t)
	task := automationDownloadTask(t, s, db)
	fake := &automationFakeDownloader{}
	s.downloaderHook = func(string) (automationDownloader, error) { return fake, nil }
	if err := db.Table("sites").Where("site = ?", "novahd").Update("base_url", "https://changed.example.com").Error; err != nil {
		t.Fatal(err)
	}
	s.Tick(context.Background())
	if fake.calls != 0 {
		t.Fatal("frozen profile origin changed but downloader was called")
	}
	if err := s.repo.SaveSettings(`{"enabled":false,"auto_publish":false}`, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Tick(context.Background())
	row, _ := s.repo.Task(task.ID)
	if row.Explicit || fake.calls != 0 {
		t.Fatal("disable did not revoke pending explicit work")
	}
}

func TestAutomationReconcileOnlyPersistsIntentInsideMachineTransaction(t *testing.T) {
	s, db := automationFixture(t)
	target, err := s.profiles.Resolve("novahd")
	if err != nil {
		t.Fatal(err)
	}
	target.AnnounceHosts = []string{"announce.target.example.com"}
	target.Revision = 2
	target.Digest = "target-frozen"
	raw, _ := json.Marshal(target)
	if err = db.Create(&repository.SiteProfileRow{ID: target.ID, Origin: target.BaseURL, Revision: 2, JSON: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	task := automationDownloadTask(t, s, db)
	if err = db.Model(&repository.AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{"state": "needs_review", "publish_attempted": true}).Error; err != nil {
		t.Fatal(err)
	}
	s.scanner.Client.Transport = automationTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("Reconcile performed HTTP in machine transaction")
		return nil, nil
	})
	s.downloaderHook = func(string) (automationDownloader, error) {
		t.Fatal("Reconcile touched downloader in API transaction")
		return nil, nil
	}
	if err = db.Transaction(func(tx *gorm.DB) error {
		result, status := s.Reconcile(repository.WithMachineTransaction(context.Background(), tx), task.ID, "https://target.example.com/details.php?id=2")
		if status != 200 || result["success"] != true {
			t.Fatalf("reconcile status=%d result=%v", status, result)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	row, _ := s.repo.Task(task.ID)
	if row.State != "reconciling" || row.ResultURL != "https://target.example.com/details.php?id=2" || !row.Explicit {
		t.Fatalf("missing durable repair intent %+v", row)
	}
}
