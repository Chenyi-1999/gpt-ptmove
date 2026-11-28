package torrentdata

import (
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
		count := snapshot.summary(test.dimension)
		if count.Total != 4 || count.Counts["seeding"] != 2 || count.RefreshedAt != 1234 {
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
	page := snapshot.details("tag", tag, "", 2, 1)
	if page.Total != 2 || page.Page != 2 || len(page.Rows) != 1 || page.Rows[0].Name != "Beta" {
		t.Fatalf("unexpected second page: %+v", page)
	}
	filtered := snapshot.details("tag", tag, "seeding", 1, 25)
	if filtered.Total != 1 || len(filtered.Rows) != 1 || filtered.Rows[0].Name != "Alpha" {
		t.Fatalf("unexpected status filter: %+v", filtered)
	}
	empty := snapshot.details("tag", tag, "error", 1, 25)
	if empty.Total != 0 || empty.Rows == nil || len(empty.Rows) != 0 {
		t.Fatalf("unexpected empty status result: %+v", empty)
	}
}

func TestInventoryTrackerHostOmitsCredentials(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:password@Tracker.Example/secret?passkey=SECRET": "tracker.example",
		"udp://tracker.example:6969/announce":                         "tracker.example",
		"tracker.example:443":                                         "tracker.example",
		"SECRET":                                                      "",
	} {
		if got := inventoryTrackerHost(raw); got != want {
			t.Errorf("host = %q, want %q", got, want)
		}
	}
}
