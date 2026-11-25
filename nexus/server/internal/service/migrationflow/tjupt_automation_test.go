package migrationflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeTjuptAcquisition struct {
	rows []tjupt.Torrent
}

func (s fakeTjuptAcquisition) AutoCandidates(context.Context, string) ([]tjupt.AutoCandidate, error) {
	items := make([]tjupt.AutoCandidate, 0, len(s.rows))
	for _, row := range s.rows {
		items = append(items, tjupt.AutoCandidate{ID: row.Hash[:1], Title: "测试种子"})
	}
	return items, nil
}

func (s fakeTjuptAcquisition) DownloadCandidate(_ context.Context, candidate tjupt.AutoCandidate) (tjupt.Torrent, error) {
	for _, row := range s.rows {
		if candidate.ID == row.Hash[:1] {
			return row, nil
		}
	}
	return tjupt.Torrent{}, fmt.Errorf("unknown mock")
}

func TestTjuptAutomationFirstPassMarksThenDeletesOnlyAfterFreshHNR(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo, err := repository.NewTjuptAutomationRepository(db, "policy", 5)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	const root = "/test-only"
	const lease = "current"
	if err = repo.SaveControl(ctx, "policy", true, 5, now, "qb", root); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Claim(ctx, "policy", lease, now, now+180); !ok || err != nil {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	fakeSite := &fakeTjuptSite{}
	hash := strings.Repeat("a", 40)
	var orphan atomic.Bool
	var effectsMu sync.Mutex
	var effects []string
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			if orphan.Load() {
				_ = json.NewEncoder(w).Encode([]map[string]string{{"hash": hash, "tags": tjuptAutoTag}})
			} else {
				_, _ = w.Write([]byte("[]"))
			}
		case "/api/v2/starpt/tjupt/auto":
			var e downloaderclient.TjuptAutoEffect
			if json.NewDecoder(r.Body).Decode(&e) != nil || e.Proof == "" || e.Hash != hash || e.Root != root {
				t.Error("request failed scoped signature contract")
			}
			effectsMu.Lock()
			effects = append(effects, e.Action)
			effectsMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"operation": "starpt-tjupt-auto-v1", "action": e.Action, "hash": e.Hash})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer qb.Close()
	d := downloaderclient.Downloader{ID: "qb", Host: qb.URL, Enabled: true, Type: "qbittorrent", APIKey: "mock-hub-creds"}
	a := &TjuptAutomation{
		plugin: &TjuptRSSService{site: fakeSite, authorizedHNR: []byte("mock-token"), retireKey: []byte("mock-key"), trackerAccount: strings.Repeat("c", 64)},
		repo:   repo, saveRoot: root, rss: "fake site",
		site: fakeTjuptAcquisition{rows: []tjupt.Torrent{
			{Hash: hash, SizeBytes: 4 * tjuptGiB, Data: []byte("test-data")},
			{Hash: strings.Repeat("b", 40), SizeBytes: 3 * tjuptGiB, Data: []byte("test-data")},
		}},
	}
	settings := defaultTjuptAutomation()
	settings.CleanupMode, settings.TotalLimitGiB = "delete", 5
	adds, _, err := a.addTjuptCandidates(ctx, d, settings, lease)
	if err != nil || adds != 1 {
		t.Fatalf("verified total quota expected one addition, got %d %v", adds, err)
	}
	tasks, err := repo.LiveTasks(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("quota ledger: %v %v", tasks, err)
	}
	task := tasks[0]
	addedOn := now - 40*3600
	fakeSite.record = tjupt.Assessment{CanRetire: true, AddedAt: time.Unix(addedOn+1, 0)}
	row := downloaderclient.TaggedQBTorrent{
		Hash: hash, AddedOn: addedOn, SavePath: root + "/" + hash, Tags: "starpt-tjupt,starpt-tjupt-auto",
		State: "stalledUP", Progress: floatPtrTjupt(1), UploadBPS: intPtrTjupt(0),
		SeedingTime: intPtrTjupt(tjuptSeedSeconds(task.SizeBytes, settings.BufferHours)), Uploaded: intPtrTjupt(0),
	}
	sample := func() {
		n := time.Now().Unix()
		task.LowSince, task.LastSample = n-3800, n-60
		task.PolicyStamp = fmt.Sprintf("%d/%d/%d/%d", *row.SeedingTime, settings.UploadThresholdKiB*1024, settings.LowSpeedMinutes, settings.IntervalMinutes)
	}
	sample()
	mark, removed, err := a.manageTjuptTask(ctx, d, settings, &task, row, fakeSite.record, lease)
	if err != nil || mark != 1 || removed != 0 || task.State != tjuptMarked || fakeSite.reads != 0 {
		t.Fatalf("first eligible pass did more than tag: %d %d %v %v", mark, removed, task, err)
	}
	row.Tags += ",starpt-tjupt-pending-delete"
	sample()
	staleAssessment := fakeSite.record
	fakeSite.record.CanRetire = false
	mark, removed, err = a.manageTjuptTask(ctx, d, settings, &task, row, staleAssessment, lease)
	if err != nil || mark != 0 || removed != 0 || fakeSite.reads != 1 || task.LowSince != 0 {
		t.Fatalf("fresh H&R refusal did not reset: %d %d %v", mark, removed, err)
	}
	sample()
	fakeSite.record.CanRetire = true
	mark, removed, err = a.manageTjuptTask(ctx, d, settings, &task, row, fakeSite.record, lease)
	if err != nil || mark != 0 || removed != 1 || fakeSite.reads != 2 || task.State != tjuptDeleted {
		t.Fatalf("eligible later cleanup: %d %d %v", mark, removed, err)
	}
	effectsMu.Lock()
	actualEffects := strings.Join(effects, ",")
	effectsMu.Unlock()
	if actualEffects != "add,mark,delete" {
		t.Fatalf("unexpected effects: %s", actualEffects)
	}
	tasks, err = repo.LiveTasks(ctx)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("confirmed delete did not release capacity: %v %v", tasks, err)
	}
	t.Setenv("AUTH_PASSWORD", "test-only-password")
	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "fixtures.json")})
	if err != nil {
		t.Fatal(err)
	}
	configured := cfg.Get()
	configured["downloaders"] = []any{map[string]any{
		"id": d.ID, "type": d.Type, "host": d.Host, "enabled": true, "api_key": d.APIKey,
	}}
	if err = cfg.Save(configured); err != nil {
		t.Fatal(err)
	}
	a.plugin.cfg = cfg
	settings.Enabled, settings.DownloaderID = true, d.ID
	policy, _ := json.Marshal(settings)
	orphan.Store(true)
	msg, runErr := a.runTjuptCycle(ctx, string(policy), lease)
	if runErr != nil || !strings.Contains(msg, "未登记") {
		t.Fatalf("untracked qB row was ignored: %s, %v", msg, runErr)
	}
}

func TestTjuptLowSpeedResetsAfterGapOrRateRecovery(t *testing.T) {
	var task repository.TjuptAutomationTask
	if observeTjuptLowSpeed(&task, "same", 100, true, 5, 10) ||
		observeTjuptLowSpeed(&task, "same", 400, true, 5, 10) ||
		!observeTjuptLowSpeed(&task, "same", 700, true, 5, 10) {
		t.Fatal("uninterrupted low speed did not respect its duration")
	}
	if observeTjuptLowSpeed(&task, "same", 1300, true, 5, 10) || task.LowSince != 1300 {
		t.Fatal("gap was mistakenly counted as low speed")
	}
	if observeTjuptLowSpeed(&task, "same", 1301, false, 5, 10) || task.LowSince != 0 {
		t.Fatal("speed recovery did not reset the interval")
	}
	if tjuptSeedSeconds(10*tjuptGiB, 6) != 30*3600 || tjuptSeedSeconds(10*tjuptGiB+1, 6) != 54*3600 {
		t.Fatal("incorrect boundary or added seeding buffer")
	}
}

func floatPtrTjupt(v float64) *float64 { return &v }
func intPtrTjupt(v int64) *int64       { return &v }
