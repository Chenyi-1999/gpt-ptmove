package migrationflow

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"gorm.io/gorm"
)

func TestTjuptFeedSiteUploadLimitTogglePersistsIndependently(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	createTjuptFeedSiteSettings(t, db)
	if err := db.Exec("INSERT INTO sites (site, speed_limit) VALUES (?, ?)", "tjupt", 90).Error; err != nil {
		t.Fatal(err)
	}
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	initial, err := feed.View(ctx)
	if err != nil || initial.Settings.UseSiteUploadLimit || initial.SiteUploadLimitMBps == nil || *initial.SiteUploadLimitMBps != 90 {
		t.Fatalf("toggle should default off and expose the saved site limit: %#v, %v", initial, err)
	}
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	before, err := feed.repo.Control(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := feed.SetSiteUploadLimit(ctx, true); err != nil {
		t.Fatal(err)
	}
	toggled, err := feed.repo.Control(ctx)
	if err != nil || !toggled.Enabled || !toggled.UseSiteUploadLimit || toggled.Policy != before.Policy ||
		toggled.Revision != before.Revision || toggled.SiteUploadLimitRevision != before.SiteUploadLimitRevision+1 ||
		toggled.LastCleanup != before.LastCleanup || !bytes.Equal(toggled.RSS, before.RSS) || !bytes.Equal(toggled.HNR, before.HNR) {
		t.Fatalf("active toggle should only change its own persisted fields: %#v, %v", toggled, err)
	}
	if applied, err := feed.repo.SetSiteUploadLimit(ctx, before.SiteUploadLimitRevision, false); err != nil || applied {
		t.Fatalf("stale switch write was accepted: applied=%t, err=%v", applied, err)
	}
	if err := feed.SetSiteUploadLimit(ctx, true); err != nil {
		t.Fatal(err)
	}
	same, err := feed.repo.Control(ctx)
	if err != nil || same.SiteUploadLimitRevision != toggled.SiteUploadLimitRevision {
		t.Fatalf("same switch value should be idempotent: %#v, %v", same, err)
	}
	if err := feed.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	input.Category = "another category"
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.View(ctx)
	if err != nil || !current.Settings.UseSiteUploadLimit || current.Settings.Category != input.Category {
		t.Fatalf("normal save or reopening lost the switch: %#v, %v", current, err)
	}
}

func TestTjuptFeedSiteUploadLimitUsesCurrentSettingsWhenAdding(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	createTjuptFeedSiteSettings(t, db)
	if err := db.Exec("INSERT INTO sites (site, speed_limit) VALUES (?, ?)", "tjupt", 90).Error; err != nil {
		t.Fatal(err)
	}
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	capacity := 6
	input.ActiveLimit = &capacity
	input.TotalLimitGiB = float64(capacity)
	if err := feed.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	source := &tjuptFeedFakeSite{}
	qb := &tjuptFeedFakeQB{rows: make(map[string]downloaderclient.TaggedQBTorrent)}
	attachTjuptFeedTest(feed, source, qb)
	for i, trial := range []struct {
		stored, expected int
		enabled          bool
	}{
		{stored: 90, expected: 0, enabled: false},
		{stored: 90, expected: 90, enabled: true},
		{stored: 125, expected: 125, enabled: true},
		{stored: 0, expected: 0, enabled: true},
		{stored: 99999, expected: 0, enabled: true},
		{stored: 90, expected: 0, enabled: false},
	} {
		if err := db.Exec("UPDATE sites SET speed_limit = ? WHERE site = ?", trial.stored, "tjupt").Error; err != nil {
			t.Fatal(err)
		}
		if err := feed.SetSiteUploadLimit(ctx, trial.enabled); err != nil {
			t.Fatal(err)
		}
		source.items = append(source.items, tjupt.Torrent{
			Hash: strings.Repeat(string(rune('a'+i)), 40), SizeBytes: tjuptGiB, Data: []byte("mock-torrent"),
		})
		result, err := feed.Trial(ctx)
		if err != nil || result.Added != 1 || len(qb.uploadLimits) != i+1 || qb.uploadLimits[i] != trial.expected || !qb.adds[i] {
			t.Fatalf("trial %d didn't use current upload limit: result=%#v limits=%v err=%v", i, result, qb.uploadLimits, err)
		}
	}
}

func TestTjuptFeedSiteUploadLimitUnavailablePreventsNewAdd(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []struct {
		name string
		rows []string
	}{
		{name: "missing"},
		{name: "different site", rows: []string{"('hdsky', 90)"}},
		{name: "null", rows: []string{"('tjupt', NULL)"}},
		{name: "negative", rows: []string{"('tjupt', -1)"}},
		{name: "duplicate", rows: []string{"('tjupt', 90)", "('tjupt', 125)"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db, plugin, dir := newTjuptFeedTest(t)
			createTjuptFeedSiteSettings(t, db)
			for _, row := range scenario.rows {
				if err := db.Exec("INSERT INTO sites (site, speed_limit) VALUES " + row).Error; err != nil {
					t.Fatal(err)
				}
			}
			feed, err := NewTjuptFeed(db, plugin, dir)
			if err != nil {
				t.Fatal(err)
			}
			configureTjuptFeedTest(t, feed)
			if err := feed.SetSiteUploadLimit(ctx, true); err != nil {
				t.Fatal(err)
			}
			state, err := feed.View(ctx)
			if err != nil || !state.Settings.UseSiteUploadLimit || state.SiteUploadLimitMBps != nil {
				t.Fatalf("unavailable site limit should be shown as unknown: %#v, %v", state, err)
			}
			qb := &tjuptFeedFakeQB{rows: make(map[string]downloaderclient.TaggedQBTorrent)}
			attachTjuptFeedTest(feed, &tjuptFeedFakeSite{items: []tjupt.Torrent{{
				Hash: strings.Repeat("a", 40), SizeBytes: tjuptGiB, Data: []byte("mock-torrent"),
			}}}, qb)
			result, err := feed.Trial(ctx)
			if err != nil || result.Added != 0 || len(qb.adds) != 0 || !strings.Contains(result.Message, "无法读取北洋园站点上传限速") {
				t.Fatalf("an unknown site cap must stop the add: %#v, writes=%v err=%v", result, qb.adds, err)
			}
		})
	}
}

func TestTjuptFeedSiteUploadLimitToggleFencesPendingAdd(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	createTjuptFeedSiteSettings(t, db)
	if err := db.Exec("INSERT INTO sites (site, speed_limit) VALUES (?, ?)", "tjupt", 90).Error; err != nil {
		t.Fatal(err)
	}
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	qb := &tjuptFeedFakeQB{rows: make(map[string]downloaderclient.TaggedQBTorrent)}
	source := &tjuptFeedFakeSite{items: []tjupt.Torrent{{
		Hash: strings.Repeat("a", 40), SizeBytes: tjuptGiB, Data: []byte("mock-torrent"),
	}}}
	attachTjuptFeedTest(feed, source, qb)
	qb.beforeWrite = func() {
		if err := feed.SetSiteUploadLimit(ctx, true); err != nil {
			t.Fatal(err)
		}
		active, err := feed.repo.Control(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = feed.repo.Verify(ctx, active.Lease, false, time.Now().Unix(), nil); err != nil {
			t.Fatalf("existing qB task effects must keep their lease: %v", err)
		}
	}
	result, err := feed.run(ctx, false)
	if err != nil || result.Added != 0 || len(qb.adds) != 0 || !strings.Contains(result.Message, "限速开关已变更") {
		t.Fatalf("stale add was not fenced: %#v, writes=%v err=%v", result, qb.adds, err)
	}
	if seen, err := feed.repo.SeenSource(ctx, "1"); err != nil || seen {
		t.Fatalf("rejected add should release its reservation: seen=%t err=%v", seen, err)
	}
	qb.beforeWrite = nil
	if err := db.Model(&repository.TjuptFeedControl{}).Where("id = ?", 1).UpdateColumn("last_run", 0).Error; err != nil {
		t.Fatal(err)
	}
	result, err = feed.run(ctx, false)
	if err != nil || result.Added != 1 || !slices.Equal(qb.uploadLimits, []int{90}) || !slices.Equal(qb.adds, []bool{false}) {
		t.Fatalf("following cycle must add under the latest limit: %#v, writes=%v limits=%v err=%v", result, qb.adds, qb.uploadLimits, err)
	}
}

func createTjuptFeedSiteSettings(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("CREATE TABLE sites (site TEXT NOT NULL, speed_limit INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
}
