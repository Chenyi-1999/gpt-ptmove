package torrentdata

import (
	"maps"
	"slices"
	"testing"
	"time"
)

func TestDownloaderInventoryGroupsAndPagedDetails(t *testing.T) {
	snapshot := inventorySnapshot{refreshedAt: time.Unix(1234, 0), torrents: []InventoryTorrent{
		{Name: "Alpha", State: "seeding", Category: "电影", Tags: []string{"重复", "保留"}, Site: "织梦", Path: "/media/movies"},
		{Name: "Beta", State: "paused", Category: "剧集", Tags: []string{"重复"}, Site: "织梦", Path: "/media/tv"},
		{Name: "Gamma", State: "seeding", Category: "电影", Tags: []string{"保留"}, Site: "NovaHD", Path: "/media/movies"},
		{Name: "Zeta", State: "error"},
	}}
	tests := []struct {
		dimension, label, value string
		total, seeding          int
	}{
		{"category", "电影", "电影", 2, 2},
		{"tag", "重复", "重复", 2, 1},
		{"site", "织梦", "织梦", 2, 1},
		{"path", "/media/movies", "/media/movies", 2, 2},
		{"tag", "无标签", "", 1, 0},
	}
	for _, test := range tests {
		count := snapshot.summary(test.dimension, InventoryFilters{})
		if count.SourceTotal != 4 || count.Total != 4 || count.Counts["seeding"] != 2 || count.RefreshedAt != 1234 {
			t.Fatalf("incorrect snapshot totals: %+v", count)
		}
		index := slices.IndexFunc(count.Groups, func(group InventoryGroup) bool { return group.Label == test.label })
		if index < 0 {
			t.Fatalf("missing %s group %q: %+v", test.dimension, test.label, count.Groups)
		}
		group := count.Groups[index]
		if group.Total != test.total || group.Counts["seeding"] != test.seeding || group.Key != inventoryGroupKey(test.value) {
			t.Fatalf("incorrect group %s: %+v", test.dimension, group)
		}
	}
	tag := inventoryGroupKey("重复")
	page := snapshot.details("tag", tag, InventoryFilters{}, 2, 1)
	if page.Total != 2 || page.Page != 2 || len(page.Rows) != 1 || page.Rows[0].Name != "Beta" {
		t.Fatalf("unexpected second page: %+v", page)
	}
	filtered := snapshot.details("tag", tag, InventoryFilters{Status: "seeding"}, 1, 25)
	if filtered.Total != 1 || len(filtered.Rows) != 1 || filtered.Rows[0].Name != "Alpha" {
		t.Fatalf("unexpected status filter: %+v", filtered)
	}
	empty := snapshot.details("tag", tag, InventoryFilters{Status: "error"}, 1, 25)
	if empty.Total != 0 || empty.Rows == nil || len(empty.Rows) != 0 {
		t.Fatalf("unexpected empty status result: %+v", empty)
	}
}

func TestDownloaderInventoryCombinedFilters(t *testing.T) {
	snapshot := inventorySnapshot{refreshedAt: time.Unix(1234, 0), torrents: []InventoryTorrent{
		{Name: "Alpha", State: "seeding", Category: "电影", Tags: []string{"保留", "4K"},
			Trackers: inventoryTrackerHosts([]string{"https://a.example/announce", "udp://a.example:6969/announce", "https://b.example/announce"})},
		{Name: "Beta", State: "paused", Category: "电影", Tags: []string{"4K"}, Trackers: []string{"a.example"}},
		{Name: "Gamma", State: "seeding", Category: "剧集", Tags: []string{"4K"}, Trackers: []string{"a.example"}},
		{Name: "Delta", State: "seeding", Category: "电影", Tags: []string{"保留"}, Trackers: []string{"a.example"}},
		{Name: "Epsilon", State: "seeding", Category: "电影", Tags: []string{"4K"}, Trackers: []string{"b.example"}},
		{Name: "Zeta", State: "error"},
	}}
	filters := InventoryFilters{
		Category: inventoryGroupKey("电影"), Tag: inventoryGroupKey("4K"),
		Tracker: inventoryGroupKey("a.example"), Status: "seeding",
	}
	all := snapshot.summary("tracker", InventoryFilters{})
	combined := snapshot.summary("tracker", filters)
	if combined.SourceTotal != 6 || combined.Total != 1 || !maps.Equal(combined.Counts, map[string]int{"seeding": 1}) ||
		len(combined.Groups) != 2 || !maps.EqualFunc(all.Options, combined.Options, slices.Equal[[]InventoryOption]) {
		t.Fatalf("unexpected combined summary: %+v", combined)
	}
	for i, host := range []string{"a.example", "b.example"} {
		group := combined.Groups[i]
		if group.Label != host || group.Total != 1 || group.Counts["seeding"] != 1 {
			t.Fatalf("unexpected host group: %+v", group)
		}
		details := snapshot.details("tracker", group.Key, filters, 1, 10)
		if details.Total != group.Total || len(details.Rows) != 1 || details.Rows[0].Name != "Alpha" ||
			!slices.Equal(details.Rows[0].Trackers, []string{"a.example", "b.example"}) {
			t.Fatalf("detail mismatch for %s: %+v", host, details)
		}
	}
	state := snapshot.summary("state", filters)
	if len(state.Groups) != 1 || state.Groups[0].Label != "做种中" || state.Groups[0].Key != inventoryGroupKey("seeding") {
		t.Fatalf("unexpected state group: %+v", state.Groups)
	}
	statePage := snapshot.details("state", state.Groups[0].Key, filters, 1, 10)
	if statePage.Total != 1 || len(statePage.Rows) != 1 || statePage.Rows[0].Name != "Alpha" {
		t.Fatalf("unexpected state details: %+v", statePage)
	}

	filters.Status = "error"
	zero := snapshot.summary("tracker", filters)
	if zero.Total != 0 || zero.SourceTotal != 6 || len(zero.Counts) != 0 || zero.Groups == nil || len(zero.Groups) != 0 ||
		!maps.EqualFunc(all.Options, zero.Options, slices.Equal[[]InventoryOption]) {
		t.Fatalf("empty result lost snapshot options: %+v", zero)
	}

	blank := inventoryGroupKey("")
	for dimension, label := range map[string]string{"category": "未分类", "tag": "无标签", "tracker": "无 Tracker"} {
		if !slices.Contains(all.Options[dimension], InventoryOption{Key: blank, Label: label}) {
			t.Errorf("missing blank option in %s: %+v", dimension, all.Options[dimension])
		}
	}
	blankResult := snapshot.summary("tracker", InventoryFilters{Category: blank, Tag: blank, Tracker: blank})
	if blankResult.Total != 1 || len(blankResult.Groups) != 1 || blankResult.Groups[0].Key != blank {
		t.Fatalf("blank options did not match the uncategorized torrent: %+v", blankResult)
	}
}

func TestDownloaderInventoryFilterValidation(t *testing.T) {
	if !(InventoryFilters{Category: inventoryGroupKey("电影"), Tag: inventoryGroupKey(""), Tracker: inventoryGroupKey("a.example"), Status: "checking"}).valid() {
		t.Fatal("valid filter keys rejected")
	}
	for _, filters := range []InventoryFilters{
		{Category: "电影"}, {Tag: "zzzzzzzzzzzzzzzzzzzzzzzz"}, {Tracker: "https://a.example"}, {Status: "unknown"},
	} {
		if filters.valid() {
			t.Errorf("invalid filters accepted: %+v", filters)
		}
	}
}

func TestInventoryTrackerHostOmitsCredentials(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:password@Tracker.Example/secret?passkey=SECRET": "tracker.example",
		"udp://tracker.example:6969/announce":                         "tracker.example",
		"tracker.example:443":                                         "tracker.example",
		"https://TRACKER.EXAMPLE./announce#private":                   "tracker.example",
		"udp://[2001:db8::1]:6969/announce?key=private":               "2001:db8::1",
		"2001:0db8:0000::1":                                           "2001:db8::1",
		"https://*.example/path":                                      "",
		"SECRET":                                                      "",
	} {
		if got := inventoryTrackerHost(raw); got != want {
			t.Errorf("host = %q, want %q", got, want)
		}
	}
}
