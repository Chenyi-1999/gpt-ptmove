package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

type tjuptCleanupFixture struct {
	feed       *TjuptFeed
	qb         *tjuptFeedFakeQB
	task       repository.TjuptFeedTask
	lease      string
	sampleAt   int64
	firstRunAt int64
	calls      int
	status     tjupt.Assessment
	siteErr    error
}

type tjuptCleanupSite struct {
	fakeTjuptSite
	calls int
}

func (s *tjuptCleanupSite) Status(context.Context, string, []string) (map[string]tjupt.Assessment, error) {
	s.calls++
	return nil, errors.New("test H&R unavailable")
}

func newTjuptCleanupFixture(t *testing.T) *tjuptCleanupFixture {
	t.Helper()
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	row, err := feed.repo.Control(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lease := "test-deletion-lease"
	ok, err := feed.repo.Claim(ctx, row, lease, false, true, true, now, now+120)
	if err != nil || !ok {
		t.Fatalf("claim test lease: %t, %v", ok, err)
	}
	started := now - 3600
	task := repository.TjuptFeedTask{
		Hash: strings.Repeat("a", 40), SourceID: "42", Scope: "test-scope", Marker: tjuptFeedMarkerPrefix + strings.Repeat("f", 24),
		State: repository.TjuptFeedStarted, SizeBytes: tjuptGiB, AddedOn: started - 60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	progress, speed, uploaded := float64(1), int64(0), int64(300)
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{
		task.Hash: {Hash: task.Hash, AddedOn: task.AddedOn, State: "stalledUP", Progress: &progress,
			Tags: tjuptTag + ", " + task.Marker, Tracker: "https://tracker.tjupt.org/announce", UploadBPS: &speed, Uploaded: &uploaded},
	}}
	qb.guard = func() error { return feed.repo.Verify(ctx, lease, false, time.Now().Unix(), nil) }
	code := 20
	return &tjuptCleanupFixture{
		feed: feed, qb: qb, task: task, lease: lease, sampleAt: started, firstRunAt: started,
		status: tjupt.Assessment{Code: &code, CanRetire: true, RecordID: 900, TorrentID: 42, AddedAt: time.Unix(task.AddedOn, 0)},
	}
}

func (f *tjuptCleanupFixture) sample(t *testing.T, count, lowMinutes int) *tjuptFeedCycle {
	t.Helper()
	ctx := context.Background()
	var cycle *tjuptFeedCycle
	for range count {
		rows, err := f.feed.repo.LiveTasks(ctx, f.task.Scope)
		if err != nil || len(rows) != 1 {
			t.Fatalf("load cleanup fixture: %#v, %v", rows, err)
		}
		task := rows[0]
		cycle = &tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, downloader: f.qb, scope: task.Scope,
			active: 1, used: task.SizeBytes, cleanup: &tjuptFeedCleanup{
				scope: "observation-epoch", previousRunAt: f.sampleAt - 60, runAt: f.sampleAt,
				token: mockTjuptHNR, uploadThreshold: 100 * 1024, lowSeconds: int64(lowMinutes) * 60, sampleGap: 105,
				lookup: func(_ context.Context, token string, hashes []string) (map[string]tjupt.Assessment, error) {
					f.calls++
					if token != mockTjuptHNR || len(hashes) != 1 || hashes[0] != task.Hash {
						t.Fatalf("H&R was requested for an unexpected account or hash")
					}
					return map[string]tjupt.Assessment{task.Hash: f.status}, f.siteErr
				},
			},
		}
		if task.State == repository.TjuptFeedStarted {
			row, _ := f.qb.TjuptFeedTorrent(ctx, task.Hash)
			if err := cycle.observeCleanup(ctx, task, row, f.sampleAt); err != nil {
				t.Fatal(err)
			}
		} else {
			cycle.active, cycle.used = 0, 0
			if err := cycle.reconcile(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if err := cycle.runCleanup(ctx, func() time.Time { return time.Unix(f.sampleAt, 0) }); err != nil {
			t.Fatal(err)
		}
		f.sampleAt += 60
	}
	return cycle
}

func TestTjuptFeedCleanupRequiresFreshSecondHNRAndContinuousLowUpload(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	f.sample(t, 15, 3)
	if f.calls != 1 || len(f.qb.removes) != 0 {
		t.Fatalf("before the 15 minute interval: %d H&R checks, %v removals", f.calls, f.qb.removes)
	}
	row := f.qb.rows[f.task.Hash]
	*row.UploadBPS = 100 * 1024
	f.sample(t, 1, 3)
	*row.UploadBPS = 1
	f.sample(t, 3, 3)
	if f.calls != 1 || len(f.qb.removes) != 0 {
		t.Fatalf("high rate must restart low-speed timer: %d H&R checks, %v removals", f.calls, f.qb.removes)
	}
	result := f.sample(t, 1, 3)
	if f.calls != 2 || len(f.qb.removes) != 1 || result.active != 0 || result.used != 0 || result.cleanup.removed != 1 {
		t.Fatalf("second H&R plus full low-speed window: checks=%d removes=%v result=%+v", f.calls, f.qb.removes, result)
	}
	seen, err := f.feed.repo.SeenSource(context.Background(), f.task.SourceID)
	if err != nil || !seen {
		t.Fatalf("retired seed must keep source dedupe: %t, %v", seen, err)
	}
}

func TestTjuptFeedCleanupRetainsTaskWhenSecondCheckCannotConfirm(t *testing.T) {
	for _, tt := range []struct {
		name   string
		update func(*tjuptCleanupFixture)
	}{
		{"unknown H&R", func(f *tjuptCleanupFixture) { f.status.Code = nil }},
		{"cancelled record", func(f *tjuptCleanupFixture) { code := 40; f.status.Code = &code }},
		{"other H&R record", func(f *tjuptCleanupFixture) { f.status.RecordID++ }},
		{"other source torrent", func(f *tjuptCleanupFixture) { f.status.TorrentID++ }},
		{"prior addition", func(f *tjuptCleanupFixture) { f.status.AddedAt = time.Unix(f.task.AddedOn-1, 0) }},
		{"H&R upstream failure", func(f *tjuptCleanupFixture) { f.siteErr = errors.New("test outage") }},
		{"speed recovered after next H&R check", func(f *tjuptCleanupFixture) {
			f.qb.beforeRemove = func() { *f.qb.rows[f.task.Hash].UploadBPS = 150 * 1024 }
		}},
		{"loop disabled before qB deletion", func(f *tjuptCleanupFixture) {
			f.qb.beforeRemove = func() {
				if err := f.feed.SetEnabled(context.Background(), false); err != nil {
					t.Error(err)
				}
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newTjuptCleanupFixture(t)
			f.sample(t, 15, 1)
			tt.update(f)
			result := f.sample(t, 1, 1)
			if f.calls != 2 || len(f.qb.removes) != 0 || result.active != 1 {
				t.Fatalf("expected retained task: checks=%d removes=%v active=%d", f.calls, f.qb.removes, result.active)
			}
		})
	}
}

func TestTjuptFeedCleanupUnconfirmedRemovalIsNeverResent(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	f.qb.removeErr = downloaderclient.ErrTjuptFeedUnconfirmed
	result := f.sample(t, 16, 1)
	if result.active != 1 || result.cleanup.uncertain != 1 || len(f.qb.removes) != 1 {
		t.Fatalf("uncertain removal must retain capacity: %+v, %v", result, f.qb.removes)
	}
	result = f.sample(t, 19, 1)
	if result.cleanup.uncertain != 1 || len(f.qb.removes) != 1 || f.calls != 2 {
		t.Fatalf("must only reconcile after ambiguous removal: %+v, %v", result, f.qb.removes)
	}
	delete(f.qb.rows, f.task.Hash)
	result = f.sample(t, 1, 1)
	if result.active != 0 || result.used != 0 {
		t.Fatal("confirmed absence must leave free capacity")
	}
	rows, err := f.feed.repo.LiveTasks(context.Background(), f.task.Scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("absence must release reservation: %#v, %v", rows, err)
	}
}

func TestTjuptFeedCleanupSampleRestartsOnMissingObservationOrUploadDelta(t *testing.T) {
	progress, speed := float64(1), int64(10)
	uploaded := int64(100) * 60
	row := &downloaderclient.TaggedQBTorrent{Progress: &progress, State: "uploading", UploadBPS: &speed, Uploaded: &uploaded}
	rules := &tjuptFeedCleanup{scope: "current", previousRunAt: 950, runAt: 1010, sampleGap: 105, lowSeconds: 60, uploadThreshold: 100}
	last := repository.TjuptFeedCleanupObservation{
		Scope: "current", RunAt: 950, SampledAt: 950, Uploaded: 0, LowSince: 500, CheckedAt: 100, ConfirmedAt: 100, RecordID: 3,
	}
	observed, due := rules.sample(last, row, 1010)
	if observed.LowSince != 0 || observed.ConfirmedAt != 100 || due {
		t.Fatalf("average upload at limit must restart only the timer: %+v, %t", observed, due)
	}
	for _, tt := range []struct {
		name   string
		change func(*repository.TjuptFeedCleanupObservation)
	}{
		{"other configuration or process", func(row *repository.TjuptFeedCleanupObservation) { row.Scope = "old" }},
		{"skipped sample", func(row *repository.TjuptFeedCleanupObservation) { row.RunAt -= 60 }},
		{"long gap", func(row *repository.TjuptFeedCleanupObservation) { row.SampledAt -= 120 }},
		{"qB counter reset", func(row *repository.TjuptFeedCleanupObservation) { row.Uploaded += uploaded + 1 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			old := last
			tt.change(&old)
			observed, _ := rules.sample(old, row, 1010)
			if observed.LowSince != 0 || observed.ConfirmedAt != 0 || observed.RecordID != 0 {
				t.Fatalf("earlier confirmation must not survive missing speed proof: %+v", observed)
			}
		})
	}
}

func TestTjuptFeedCleanupConfigRemainsOptInAndSupportsExistingClients(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	baseline, err := feed.View(ctx)
	if err != nil || baseline.Settings.CleanupEnabled || baseline.Settings.UploadThresholdKiB != 100 || baseline.Settings.LowSpeedMinutes != 60 {
		t.Fatalf("unexpected cleanup defaults: %+v, %v", baseline.Settings, err)
	}
	var change TjuptFeedInput
	if err := json.Unmarshal([]byte(`{"cleanup_enabled":true,"upload_threshold_kib":128,"low_speed_minutes":30}`), &change); err != nil {
		t.Fatal(err)
	}
	change.TjuptFeedSettings = input.TjuptFeedSettings
	change.ClearHNRToken = true
	if err := feed.Save(ctx, change); !errors.Is(err, ErrTjuptFeedConfig) {
		t.Fatalf("need saved H&R credential for cleanup: %v", err)
	}
	change.ClearHNRToken = false
	if err := feed.Save(ctx, change); err != nil {
		t.Fatal(err)
	}
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	final, err := feed.View(ctx)
	if err != nil || !final.Settings.CleanupEnabled || final.Settings.UploadThresholdKiB != 128 || final.Settings.LowSpeedMinutes != 30 {
		t.Fatalf("existing API body without cleanup settings must preserve them: %+v, %v", final.Settings, err)
	}
}

func TestTjuptFeedCleanupCadenceDoesNotReadRSSEarlyOrRunInTrial(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	hnr := &tjuptCleanupSite{}
	plugin.site = hnr
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	optIn := true
	input.IntervalMinutes, input.CleanupEnabled = 120, &optIn
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	selected, err := plugin.downloader(input.DownloaderID)
	if err != nil {
		t.Fatal(err)
	}
	task := repository.TjuptFeedTask{
		Hash: strings.Repeat("a", 40), SourceID: "42", Scope: tjuptFeedDigest(selected.ID, selected.Host, selected.Username),
		Marker: tjuptFeedMarkerPrefix + strings.Repeat("f", 24), State: repository.TjuptFeedStarted, SizeBytes: tjuptGiB,
		AddedOn: time.Now().Unix() - 60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	progress, speed, uploaded := float64(1), int64(0), int64(300)
	receipt := downloaderclient.TaggedQBTorrent{Hash: task.Hash, AddedOn: task.AddedOn, State: "stalledUP", Progress: &progress,
		Tags: tjuptTag + ", " + task.Marker, Tracker: "https://tracker.tjupt.org/announce",
		UploadBPS: &speed, Uploaded: &uploaded}
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{task.Hash: receipt}}
	rss := &tjuptFeedFakeSite{}
	attachTjuptFeedTest(feed, rss, qb)
	if _, err := feed.Trial(ctx); err != nil || rss.reads != 1 || hnr.calls != 0 {
		t.Fatalf("trial must only fetch RSS and never check H&R: RSS=%d H&R=%d, %v", rss.reads, hnr.calls, err)
	}
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	recentRSS := time.Now().Unix() - 60
	if err := db.Model(&repository.TjuptFeedControl{}).Where("id = ?", 1).Update("last_run", recentRSS).Error; err != nil {
		t.Fatal(err)
	}
	result, err := feed.run(ctx, false)
	control, readErr := feed.repo.Control(ctx)
	if err != nil || readErr != nil || rss.reads != 1 || hnr.calls != 1 || control.LastCleanup <= recentRSS ||
		control.LastRun != recentRSS || !strings.Contains(result.Message, "部分 H&R 查询未完成") {
		t.Fatalf("cleanup-only round must keep RSS cadence: RSS=%d H&R=%d result=%+v err=%v/%v", rss.reads, hnr.calls, result, err, readErr)
	}
	if _, err := feed.run(ctx, false); err != nil || rss.reads != 1 || hnr.calls != 1 {
		t.Fatalf("neither cadence should run twice immediately: RSS=%d H&R=%d, %v", rss.reads, hnr.calls, err)
	}
}
