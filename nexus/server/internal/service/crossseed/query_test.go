package crossseed

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
)

func newCrossSeedTestStore(t *testing.T) *repository.Store {
	t.Helper()

	dsn := "file:" + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	statements := []string{
		`CREATE TABLE sites (
			site TEXT,
			nickname TEXT,
			migration INTEGER DEFAULT 1
		)`,
		`CREATE TABLE seed_parameters (
			hash TEXT NOT NULL,
			torrent_id TEXT NOT NULL,
			site_name TEXT NOT NULL,
			nickname TEXT,
			title TEXT,
			subtitle TEXT,
			type TEXT,
			medium TEXT,
			video_codec TEXT,
			audio_codec TEXT,
			resolution TEXT,
			team TEXT,
			source TEXT,
			tags TEXT,
			title_components TEXT,
			poster TEXT,
			screenshots TEXT,
			body TEXT,
			mediainfo TEXT,
			screenshot_review_status TEXT,
			is_reviewed INTEGER DEFAULT 0,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE torrents (
			hash TEXT NOT NULL,
			name TEXT NOT NULL,
			size INTEGER,
			save_path TEXT,
			downloader_id TEXT,
			state TEXT,
			last_seen TEXT,
			sites TEXT,
			is_hidden INTEGER DEFAULT 0
		)`,
	}

	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema failed: %v", err)
		}
	}

	return &repository.Store{DB: db, DBType: "sqlite"}
}

func mustExecCrossSeedTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func TestQueryDataExcludeTargetSitesMatchesSiteAliases(t *testing.T) {
	store := newCrossSeedTestStore(t)
	repo := repository.NewCrossSeedRepository(store)
	service := NewCrossSeedService(repo)

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO sites (site, nickname, migration) VALUES (?, ?, ?)`, "mteam", "M-Team", 2)
	mustExecCrossSeedTest(t, store.DB, `INSERT INTO sites (site, nickname, migration) VALUES (?, ?, ?)`, "hdsky", "HDSky", 1)

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO seed_parameters (
		hash, torrent_id, site_name, nickname, title, subtitle, type, medium, video_codec,
		audio_codec, resolution, team, source, tags, title_components,
		screenshot_review_status, is_reviewed, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"source-hash", "1001", "hdsky", "HDSky", "Movie", "", "movie", "bluray", "h264",
		"dts", "1080p", "", "", "[]", "[]", "none", 1, "2026-04-14 10:00:00", "2026-04-14 10:00:00")

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"source-hash", "Movie", 100, "/downloads", "qb-1", "做种中", "2026-04-14 10:00:00", "HDSky")
	mustExecCrossSeedTest(t, store.DB, `INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"target-hash", "Movie", 100, "/downloads", "qb-1", "未做种", "2026-04-14 10:00:00", "M-Team")

	result, err := service.QueryData(CrossSeedQueryParams{
		Page:               1,
		PageSize:           20,
		ExcludeTargetSites: "mteam",
	})
	if err != nil {
		t.Fatalf("QueryData returned error: %v", err)
	}

	total, ok := result["total"].(int)
	if !ok {
		t.Fatalf("expected int total, got %#v", result["total"])
	}
	if total != 0 {
		t.Fatalf("expected total 0 when target alias already exists, got %d", total)
	}
}

func insertCrossSeedReviewRow(t *testing.T, db *gorm.DB, torrentID string, title string, team string, tags string, titleComponents string, isReviewed int) {
	t.Helper()
	hash := "hash-" + torrentID
	mustExecCrossSeedTest(t, db, `INSERT INTO seed_parameters (
		hash, torrent_id, site_name, nickname, title, subtitle, type, medium, video_codec,
		audio_codec, resolution, team, source, tags, title_components,
		screenshot_review_status, is_reviewed, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hash, torrentID, "hdsky", "HDSky", title, "", "category.movie", "medium.bluray", "video.h264",
		"audio.dts", "resolution.r1080p", team, "", tags, titleComponents, "none", isReviewed, "2026-04-14 10:00:00", "2026-04-14 10:00:00")
	mustExecCrossSeedTest(t, db, `INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		hash, title, 100, "/downloads", "qb-1", "做种中", "2026-04-14 10:00:00", "HDSky")
}

func TestQueryDataReviewStatusUsesPublishableClassification(t *testing.T) {
	store := newCrossSeedTestStore(t)
	repo := repository.NewCrossSeedRepository(store)
	service := NewCrossSeedService(repo)

	insertCrossSeedReviewRow(t, store.DB, "1001", "Valid", "team.other", `[]`, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "1002", "BadTeam", "badteam", `[]`, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "1003", "Restricted", "team.other", `["禁转"]`, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "1004", "Unrecognized", "team.other", `[]`, `[{"key":"无法识别","value":"bad"}]`, 1)

	reviewed, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20, ReviewStatus: "reviewed"})
	if err != nil {
		t.Fatalf("QueryData reviewed returned error: %v", err)
	}
	if reviewed["total"].(int) != 1 {
		t.Fatalf("expected reviewed total 1, got %#v", reviewed["total"])
	}
	data := reviewed["data"].([]map[string]any)
	if len(data) != 1 || data[0]["torrent_id"] != "1001" {
		t.Fatalf("expected only publishable row in reviewed data, got %#v", data)
	}

	unreviewed, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20, ReviewStatus: "unreviewed"})
	if err != nil {
		t.Fatalf("QueryData unreviewed returned error: %v", err)
	}
	if unreviewed["total"].(int) != 3 {
		t.Fatalf("expected unreviewed total 3, got %#v", unreviewed["total"])
	}
}

func TestQueryDataTagVisibilityFiltersBeforePaginationAndCombinesReview(t *testing.T) {
	store := newCrossSeedTestStore(t)
	service := NewCrossSeedService(repository.NewCrossSeedRepository(store))
	insertCrossSeedReviewRow(t, store.DB, "2001", "Restricted", "team.other", ` ["tag.禁转"] `, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "2002", "Episode", "team.other", `分集, `, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "2003", "Allowed", "team.other", ` ["tag.禁转预告"] `, `[]`, 1)
	insertCrossSeedReviewRow(t, store.DB, "2004", "OtherRestricted", "team.other", `限转`, `[]`, 1)

	// With both visibility filters on, filtering happens before offset/limit and
	// the total reports only the visible rows. Exact matching keeps similar tags.
	firstPage, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 1, HideRestrictedTags: true, HideEpisodeTags: true})
	if err != nil {
		t.Fatalf("QueryData filtered page 1 returned error: %v", err)
	}
	secondPage, err := service.QueryData(CrossSeedQueryParams{Page: 2, PageSize: 1, HideRestrictedTags: true, HideEpisodeTags: true})
	if err != nil {
		t.Fatalf("QueryData filtered page 2 returned error: %v", err)
	}
	if firstPage["total"].(int) != 1 || secondPage["total"].(int) != 1 {
		t.Fatalf("expected filtered total 1 on each page, got %v and %v", firstPage["total"], secondPage["total"])
	}
	if firstPage["count"].(int) != 1 || firstPage["data"].([]map[string]any)[0]["torrent_id"] != "2003" {
		t.Fatalf("expected the similar but nonmatching tag on page 1, got %#v", firstPage["data"])
	}
	if secondPage["count"].(int) != 0 {
		t.Fatalf("expected empty page 2 after filtering, got %#v", secondPage["data"])
	}

	// IncludeTaggedReviewed allows otherwise valid tagged rows through the
	// reviewed display query, but is_publishable remains false and hide wins.
	showTagged, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20, ReviewStatus: "reviewed", IncludeTaggedReviewed: true})
	if err != nil {
		t.Fatalf("QueryData include tagged reviewed returned error: %v", err)
	}
	if showTagged["total"].(int) != 3 {
		t.Fatalf("expected three reviewed rows including tagged items, got %#v", showTagged["total"])
	}
	for _, row := range showTagged["data"].([]map[string]any) {
		if row["torrent_id"] == "2001" || row["torrent_id"] == "2002" || row["torrent_id"] == "2004" {
			if row["is_publishable"] != false {
				t.Fatalf("tagged row must remain unpublishable: %#v", row)
			}
		}
	}
	showAndHide, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20, ReviewStatus: "reviewed", IncludeTaggedReviewed: true, HideRestrictedTags: true, HideEpisodeTags: true})
	if err != nil {
		t.Fatalf("QueryData include and hide returned error: %v", err)
	}
	if showAndHide["total"].(int) != 0 {
		t.Fatalf("hide must override tagged inclusion, got %#v", showAndHide["total"])
	}

	// Omitted options preserve the API's previous unfiltered behavior.
	legacy, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("QueryData legacy returned error: %v", err)
	}
	if legacy["total"].(int) != 4 {
		t.Fatalf("expected legacy total 4, got %#v", legacy["total"])
	}
}
