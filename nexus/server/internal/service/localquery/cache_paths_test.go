package localquery

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDownloaderPathsReportIndexAndAccessibility(t *testing.T) {
	mount := filepath.ToSlash(t.TempDir())
	service, db := scanFixture(t, []any{
		map[string]any{
			"id": "local", "name": "本机 qB",
			"path_mappings": []any{map[string]any{"remote": "/qb/media", "local": mount}},
		},
		remoteConfig("remote", "http://localhost:29080"),
	})
	addFixtureTorrent(t, db, "a", "present.mkv", "/qb/media", "local", false, true)
	addFixtureTorrent(t, db, "b", "unmounted.flac", "/qb/media/unmounted", "local", false, false)
	addFixtureTorrent(t, db, "c", "hidden.mkv", "/hidden", "local", true, true)
	addFixtureTorrent(t, db, "d", "remote.mkv", "/remote/save", "remote", false, false)

	result, err := service.GetDownloadersWithPaths(false)
	if err != nil {
		t.Fatal(err)
	}
	downloaders := result["downloaders"].([]map[string]any)
	want := []map[string]any{
		{"path": "/qb/media", "count": int64(1), "access_status": "available"},
		{"path": "/qb/media/unmounted", "count": int64(1), "access_status": "unavailable"},
	}
	if len(downloaders) != 2 || downloaders[0]["indexed_count"] != int64(2) ||
		!reflect.DeepEqual(downloaders[0]["paths"], want) {
		t.Fatalf("downloaders = %#v", downloaders)
	}
	if want := []map[string]any{{"path": "/remote/save", "count": int64(1), "access_status": "remote"}}; !reflect.DeepEqual(downloaders[1]["paths"], want) {
		t.Fatalf("remote = %#v", downloaders[1])
	}

	result, err = service.GetDownloadersWithPaths(true)
	if err != nil {
		t.Fatal(err)
	}
	filtered := result["downloaders"].([]map[string]any)
	if filtered[0]["indexed_count"] != int64(2) || !reflect.DeepEqual(filtered[0]["paths"], want[:1]) ||
		filtered[1]["indexed_count"] != int64(1) || len(filtered[1]["paths"].([]map[string]any)) != 0 {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestScanWithoutIndexedTasksRemainsUnverified(t *testing.T) {
	service, _ := scanFixture(t, []any{map[string]any{"id": "local", "name": "本机 qB"}})
	for _, path := range []string{"", "/unindexed"} {
		result, err := service.Scan(context.Background(), path, false)
		if err != nil || result["scan_status"] != "unverified" {
			t.Fatalf("scan %q = %#v, %v", path, result, err)
		}
		issues := result["scan_issues"].([]scanIssue)
		if len(issues) != 1 || issues[0].Path != path || issues[0].Status != "unverified" {
			t.Fatalf("issues %q = %#v", path, issues)
		}
	}
}
