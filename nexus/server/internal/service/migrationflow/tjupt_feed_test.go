package migrationflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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

const (
	mockTjuptRSS = "https://tjupt.org/torrentrss.php?passkey=mock-feed-test"
	mockTjuptHNR = "mock-hnr-test"
)

type tjuptFeedFakeSite struct {
	items []tjupt.Torrent
	reads int
}

func (s *tjuptFeedFakeSite) AutoCandidates(_ context.Context, rss string) ([]tjupt.AutoCandidate, error) {
	if rss != mockTjuptRSS {
		return nil, errors.New("mismatched RSS")
	}
	s.reads++
	items := make([]tjupt.AutoCandidate, len(s.items))
	for index := range s.items {
		items[index] = tjupt.AutoCandidate{ID: string(rune('1' + index))}
	}
	return items, nil
}

func (s *tjuptFeedFakeSite) DownloadCandidate(_ context.Context, item tjupt.AutoCandidate) (tjupt.Torrent, error) {
	return s.items[int(item.ID[0]-'1')], nil
}

type tjuptFeedFakeQB struct {
	rows            map[string]downloaderclient.TaggedQBTorrent
	guard           func() error
	beforeWrite     func()
	adds            []bool
	uploadLimits    []int
	starts          []string
	unknown         bool
	removes         []string
	removeErr       error
	beforeRemove    func()
	lookupCalls     [][]string
	lookupErr       error
	afterEnsure     func()
	beforeTagRemove func()
	tagRemoveErr    error
	tagAttempts     []string
	pruned          []string
	removeMarkers   []string
}

func (d *tjuptFeedFakeQB) TjuptFeedTorrent(_ context.Context, hash string) (*downloaderclient.TaggedQBTorrent, error) {
	if row, exists := d.rows[hash]; exists {
		return &row, nil
	}
	return nil, nil
}

func (d *tjuptFeedFakeQB) TjuptFeedTorrents(_ context.Context, hashes []string) (map[string]downloaderclient.TaggedQBTorrent, error) {
	d.lookupCalls = append(d.lookupCalls, append([]string(nil), hashes...))
	result := make(map[string]downloaderclient.TaggedQBTorrent)
	for _, hash := range hashes {
		if row, exists := d.rows[hash]; exists {
			result[hash] = row
		}
	}
	return result, d.lookupErr
}

func (d *tjuptFeedFakeQB) AddTjuptFeedTorrent(_ context.Context, _ []byte, _, savePath string, paused bool, tags []string, category, hash string, uploadLimitMBps int) (*downloaderclient.TaggedQBTorrent, error) {
	if _, exists := d.rows[hash]; exists {
		return nil, downloaderclient.ErrTjuptFeedAlreadyExists
	}
	if d.beforeWrite != nil {
		d.beforeWrite()
	}
	if err := d.guard(); err != nil {
		return nil, err
	}
	d.adds = append(d.adds, paused)
	d.uploadLimits = append(d.uploadLimits, uploadLimitMBps)
	if d.unknown {
		return nil, downloaderclient.ErrTjuptFeedUnconfirmed
	}
	state := "downloading"
	if paused {
		state = "stoppedDL"
	}
	progress := float64(0)
	row := downloaderclient.TaggedQBTorrent{
		Hash: hash, Tags: strings.Join(tags, ","), AddedOn: 100, Category: category,
		SavePath: savePath, State: state, Progress: &progress,
	}
	d.rows[hash] = row
	return &row, nil
}

func (d *tjuptFeedFakeQB) ResumeTjuptFeedTorrent(_ context.Context, hash string, addedOn int64, marker, category string) (*downloaderclient.TaggedQBTorrent, error) {
	row, ok := d.rows[hash]
	if !ok || row.AddedOn != addedOn || !row.TjuptFeedStopped() || row.Category != category || !tjuptHasTag(row.Tags, marker) {
		return nil, downloaderclient.ErrTjuptFeedScope
	}
	if err := d.guard(); err != nil {
		return nil, err
	}
	d.starts = append(d.starts, hash)
	row.State = "downloading"
	d.rows[hash] = row
	return &row, nil
}

func (d *tjuptFeedFakeQB) RemoveTjuptFeedTorrent(_ context.Context, hash string, addedOn int64, marker string, threshold int64) error {
	if d.beforeRemove != nil {
		d.beforeRemove()
	}
	row, ok := d.rows[hash]
	if !ok || row.AddedOn != addedOn || !row.TjuptFeedSeeding() || !tjuptHasTag(row.Tags, marker) ||
		row.UploadBPS == nil || *row.UploadBPS >= threshold {
		return downloaderclient.ErrTjuptFeedScope
	}
	if err := d.guard(); err != nil {
		return err
	}
	d.removes = append(d.removes, hash)
	d.removeMarkers = append(d.removeMarkers, marker)
	if d.removeErr != nil {
		return d.removeErr
	}
	delete(d.rows, hash)
	return nil
}

func (d *tjuptFeedFakeQB) EnsureTjuptFeedDisplayTag(_ context.Context, hash string, addedOn int64, marker string) error {
	row, ok := d.rows[hash]
	common := downloaderclient.TjuptFeedDisplayTag
	if !ok || row.AddedOn != addedOn || !tjuptHasTag(row.Tags, marker) ||
		(!tjuptHasTag(row.Tags, tjuptTag) && !tjuptHasTag(row.Tags, common)) {
		return downloaderclient.ErrTjuptFeedScope
	}
	if err := d.guard(); err != nil {
		return err
	}
	d.tagAttempts = append(d.tagAttempts, hash)
	if !tjuptHasTag(row.Tags, common) {
		row.Tags += "," + common
		d.rows[hash] = row
	}
	if d.afterEnsure != nil {
		d.afterEnsure()
	}
	return nil
}

func (d *tjuptFeedFakeQB) RemoveTjuptFeedLegacyTags(_ context.Context, hash string, addedOn int64, marker string) error {
	row, ok := d.rows[hash]
	if !ok || row.AddedOn != addedOn || !tjuptHasTag(row.Tags, downloaderclient.TjuptFeedDisplayTag) {
		return downloaderclient.ErrTjuptFeedScope
	}
	if err := d.guard(); err != nil {
		return err
	}
	if d.beforeTagRemove != nil {
		d.beforeTagRemove()
	}
	if d.tagRemoveErr != nil {
		return d.tagRemoveErr
	}
	var tags []string
	for _, tag := range strings.Split(row.Tags, ",") {
		if tag = strings.TrimSpace(tag); tag != marker && tag != tjuptTag {
			tags = append(tags, tag)
		}
	}
	row.Tags = strings.Join(tags, ",")
	d.rows[hash] = row
	return nil
}

func (d *tjuptFeedFakeQB) PruneEmptyTjuptFeedMarker(_ context.Context, marker string) error {
	for _, row := range d.rows {
		if tjuptHasTag(row.Tags, marker) {
			return downloaderclient.ErrTjuptFeedScope
		}
	}
	if err := d.guard(); err != nil {
		return err
	}
	d.pruned = append(d.pruned, marker)
	return nil
}

func newTjuptFeedTest(t *testing.T) (*gorm.DB, *TjuptRSSService, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "rss.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(dir, "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	root := cfg.Get()
	root["downloaders"] = []any{map[string]any{"id": "qb", "type": "qbittorrent", "host": "http://example.invalid"}}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	return db, &TjuptRSSService{cfg: cfg, site: &fakeTjuptSite{}}, dir
}

func configureTjuptFeedTest(t *testing.T, feed *TjuptFeed) TjuptFeedInput {
	t.Helper()
	limit := 1
	input := TjuptFeedInput{
		TjuptFeedSettings: TjuptFeedSettings{
			DownloaderID: "qb", IntervalMinutes: 1, MinGiB: 1, MaxGiB: 2, TotalLimitGiB: 2,
			Category: "RSS", Tags: "seed, personal, seed", SavePath: "/rss",
		},
		RSSURL: mockTjuptRSS, HNRToken: mockTjuptHNR, ActiveLimit: &limit,
	}
	if err := feed.Save(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	return input
}

func attachTjuptFeedTest(feed *TjuptFeed, site *tjuptFeedFakeSite, qb *tjuptFeedFakeQB) {
	feed.site = site
	feed.connect = func(_ downloaderclient.Downloader, guard func() error) tjuptFeedDownloader {
		qb.guard = guard
		return qb
	}
}

func TestTjuptFeedSettingsArePersistentMaskedAndDefaultOff(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	old := defaultTjuptAutomation()
	old.DownloaderID, old.MinGiB, old.MaxGiB, old.TotalLimitGiB, old.ActiveLimit = "qb", 4, 7, 30, 4
	policy, _ := json.Marshal(old)
	if _, err := repository.NewTjuptAutomationRepository(db, string(policy), 5); err != nil {
		t.Fatal(err)
	}
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := feed.View(ctx)
	if err != nil || before.Enabled || before.Settings.IntervalMinutes != 1 ||
		before.Settings.DownloaderID != "qb" || before.Settings.MinGiB != 4 || before.Settings.MaxGiB != 7 ||
		before.Settings.TotalLimitGiB != 30 || before.Settings.ActiveLimit != 4 {
		t.Fatalf("legacy defaults not carried over: %#v, %v", before, err)
	}
	input := configureTjuptFeedTest(t, feed)
	input.RSSURL, input.HNRToken = "", ""
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	row, err := feed.repo.Control(ctx)
	if err != nil || bytes.Contains(row.RSS, []byte(mockTjuptRSS)) || bytes.Contains(row.HNR, []byte(mockTjuptHNR)) {
		t.Fatal("expected encrypted persisted credentials")
	}
	feed, err = NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	after, err := feed.View(ctx)
	raw, _ := json.Marshal(after)
	if err != nil || !after.RSSConfigured || !after.HNRConfigured || after.Enabled ||
		after.Settings.Tags != "北洋园, seed, personal" || strings.Contains(string(raw), mockTjuptHNR) ||
		strings.Contains(string(raw), mockTjuptRSS) {
		t.Fatalf("view must preserve settings and mask credentials: %#v, %v", after, err)
	}
	if _, err := feed.Preview(ctx); err != nil {
		t.Fatalf("preview using saved RSS: %v", err)
	}
	input.ClearHNRToken = true
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	after, err = feed.View(ctx)
	if err != nil || after.HNRConfigured || !after.RSSConfigured {
		t.Fatalf("clear H&R Token must preserve RSS: %#v, %v", after, err)
	}
}

func TestTjuptFeedTrialResumesOnlyItsOwnPersistedStoppedTorrent(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	tooLarge, duplicate, good, other := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
		{Hash: tooLarge, SizeBytes: 2*tjuptGiB + 1, Data: []byte{1}},
		{Hash: duplicate, SizeBytes: tjuptGiB, Data: []byte{1}},
		{Hash: good, SizeBytes: 2 * tjuptGiB, Data: []byte{1}},
		{Hash: other, SizeBytes: tjuptGiB, Data: []byte{1}},
	}}
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{
		duplicate: {Hash: duplicate, State: "stoppedDL", AddedOn: 100, Category: "RSS", Tags: tjuptTag},
	}}
	attachTjuptFeedTest(feed, site, qb)
	if _, err := feed.run(ctx, false); err != nil || site.reads != 0 {
		t.Fatalf("disabled schedule consumed a feed: reads=%d, error=%v", site.reads, err)
	}
	result, err := feed.Trial(ctx)
	if err != nil || result.Added != 1 || result.Skipped != 2 || len(qb.adds) != 1 || !qb.adds[0] ||
		len(qb.starts) != 0 || !qb.rows[good].TjuptFeedStopped() || site.reads != 1 {
		t.Fatalf("trial must add only one paused torrent after size and hash checks: %#v, %v", result, err)
	}
	if item := qb.rows[good]; item.Category != "RSS" || item.SavePath != "/rss" || !tjuptHasTag(item.Tags, "seed") {
		t.Fatal("saved qB fields were not applied")
	}
	feed, err = NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	attachTjuptFeedTest(feed, site, qb)
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	result, err = feed.run(ctx, false)
	if err != nil || result.Added != 0 || len(qb.starts) != 1 || qb.starts[0] != good ||
		!qb.rows[good].TjuptFeedStarted() || site.reads != 2 || !strings.Contains(result.Message, "已启动 1 个试运行任务") {
		t.Fatalf("formal start must only start the persisted trial receipt: %#v, %v", result, err)
	}
	if _, err := feed.run(ctx, false); err != nil || site.reads != 2 {
		t.Fatalf("scheduled before saved interval: reads=%d, error=%v", site.reads, err)
	}
}

func TestTjuptFeedDoesNotRetryAmbiguousAddOrWriteAfterStop(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	limit := 3
	input.ActiveLimit, input.TotalLimitGiB = &limit, 10
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	hash, fresh := strings.Repeat("a", 40), strings.Repeat("b", 40)
	meta := tjupt.Torrent{Hash: hash, SizeBytes: tjuptGiB, Data: []byte{1}}
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{meta, meta}}
	qb := &tjuptFeedFakeQB{unknown: true, rows: map[string]downloaderclient.TaggedQBTorrent{}}
	attachTjuptFeedTest(feed, site, qb)
	if result, err := feed.Trial(ctx); err != nil || result.Failed != 1 || result.Added != 0 {
		t.Fatalf("ambiguous add must produce no confirmed add: %#v, %v", result, err)
	}
	if result, err := feed.Trial(ctx); err != nil || result.Skipped != 2 || len(qb.adds) != 1 {
		t.Fatalf("source and same hash must not be attempted again: %#v, %v", result, err)
	}
	site.items = append(site.items, tjupt.Torrent{Hash: fresh, SizeBytes: tjuptGiB, Data: []byte{1}})
	qb.unknown = false
	qb.beforeWrite = func() {
		if err := feed.SetEnabled(ctx, false); err != nil {
			t.Error(err)
		}
	}
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := feed.Save(ctx, input); !errors.Is(err, ErrTjuptFeedBusy) {
		t.Fatalf("save should conflict with an enabled loop: %v", err)
	}
	if _, err := feed.Trial(ctx); !errors.Is(err, ErrTjuptFeedBusy) {
		t.Fatalf("trial should conflict with an enabled loop: %v", err)
	}
	if result, err := feed.run(ctx, false); err != nil || result.Added != 0 || len(qb.adds) != 1 {
		t.Fatalf("stop must fence the next qB write: %#v, %v", result, err)
	}
	state, err := feed.View(ctx)
	if err != nil || state.Enabled || state.Running || state.LastRunAt == "" || state.LastMessage == "" {
		t.Fatalf("stopped state and run result must persist: %#v, %v", state, err)
	}
}

func TestTjuptFeedPackageAvailabilityFencesTrialWrites(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	ready := false
	feed.BindPluginAvailability(func() bool { return ready })
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
		{Hash: strings.Repeat("a", 40), SizeBytes: tjuptGiB, Data: []byte{1}},
	}}
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{}}
	attachTjuptFeedTest(feed, site, qb)
	if _, err := feed.Trial(ctx); !errors.Is(err, ErrTjuptFeedBusy) || site.reads != 0 {
		t.Fatalf("trial without the package must stop before RSS: reads=%d, err=%v", site.reads, err)
	}
	if err := feed.SetEnabled(ctx, true); !errors.Is(err, ErrTjuptFeedBusy) {
		t.Fatalf("start without the package must fail: %v", err)
	}
	ready = true
	qb.beforeWrite = func() { ready = false }
	result, err := feed.Trial(ctx)
	if err != nil || result.Added != 0 || len(qb.adds) != 0 || site.reads != 1 {
		t.Fatalf("unavailable package must fence qB writes in trial: %+v, err=%v", result, err)
	}
	view, err := feed.View(ctx)
	if err != nil || view.Enabled || !view.RSSConfigured || !view.HNRConfigured {
		t.Fatalf("package availability must not clear configuration: %+v, err=%v", view, err)
	}
}

func TestTjuptFeedRefusesAnEnabledLegacyLoop(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	old, err := repository.NewTjuptAutomationRepository(db, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SaveControl(ctx, "{}", true, 5, time.Now().Unix(), "qb", "/rss"); err != nil {
		t.Fatal(err)
	}
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	if err := feed.SetEnabled(ctx, true); !errors.Is(err, ErrTjuptFeedBusy) {
		t.Fatalf("legacy overlap should prevent starting: %v", err)
	}
	if _, err := feed.Trial(ctx); !errors.Is(err, ErrTjuptFeedBusy) {
		t.Fatalf("legacy overlap should prevent a trial: %v", err)
	}
}

func TestTjuptFeedChangedLimitsKeepTrialPausedAndAllowOtherCandidates(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	h1, h2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
		{Hash: h1, SizeBytes: 2 * tjuptGiB, Data: []byte{1}},
		{Hash: h2, SizeBytes: tjuptGiB, Data: []byte{1}},
	}}
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{}}
	attachTjuptFeedTest(feed, site, qb)
	if result, err := feed.Trial(ctx); err != nil || result.Added != 1 {
		t.Fatalf("cannot create trial: %#v, %v", result, err)
	}
	limit := 2
	input.ActiveLimit, input.MaxGiB, input.TotalLimitGiB = &limit, 1, 3
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	result, err := feed.run(ctx, false)
	if err != nil || result.Added != 1 || len(qb.starts) != 0 || !qb.rows[h1].TjuptFeedStopped() ||
		!qb.rows[h2].TjuptFeedStarted() || len(qb.adds) != 2 || qb.adds[1] {
		t.Fatalf("ineligible trial must remain stopped while qualifying formal item starts: %#v, %v", result, err)
	}
}

func TestTjuptFeedOldAbsentUnconfirmedAddReleasesOnlyCapacity(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	h1, h2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
		{Hash: h1, SizeBytes: tjuptGiB, Data: []byte{1}},
		{Hash: h2, SizeBytes: tjuptGiB, Data: []byte{1}},
	}}
	qb := &tjuptFeedFakeQB{unknown: true, rows: map[string]downloaderclient.TaggedQBTorrent{}}
	attachTjuptFeedTest(feed, site, qb)
	if result, err := feed.Trial(ctx); err != nil || result.Failed != 1 {
		t.Fatalf("expected uncertain receipt: %#v, %v", result, err)
	}
	qb.unknown = false
	if result, err := feed.Trial(ctx); err != nil || result.Added != 0 || len(qb.adds) != 1 {
		t.Fatalf("unresolved add must hold its capacity during the reconciliation window: %#v, %v", result, err)
	}
	if err := db.Model(&repository.TjuptFeedTask{}).Where("hash = ?", h1).
		Update("reserved_at", time.Now().Unix()-tjuptFeedPendingSeconds-1).Error; err != nil {
		t.Fatal(err)
	}
	result, err := feed.Trial(ctx)
	if err != nil || result.Added != 1 || result.Skipped != 1 || len(qb.adds) != 2 ||
		!qb.rows[h2].TjuptFeedStopped() {
		t.Fatalf("confirmed absence must permit another candidate without retrying the first: %#v, %v", result, err)
	}
}
