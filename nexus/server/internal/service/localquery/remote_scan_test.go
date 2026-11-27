package localquery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"gorm.io/gorm"
)

type fixtureDirectoryClient struct {
	inventory []downloaderclient.RemoteScanReference
	listError error
	scanError error
	dir       string
	requests  []downloaderclient.RemoteDirectoryScanRequest
	afterScan func()
}

func (f *fixtureDirectoryClient) ListRemoteScanReferences(context.Context) ([]downloaderclient.RemoteScanReference, error) {
	return f.inventory, f.listError
}

func (f *fixtureDirectoryClient) ScanMediaDirectoryByProxy(_ context.Context, req downloaderclient.RemoteDirectoryScanRequest) (downloaderclient.RemoteDirectoryScanResult, error) {
	f.requests = append(f.requests, req)
	if f.scanError != nil {
		return downloaderclient.RemoteDirectoryScanResult{}, f.scanError
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return downloaderclient.RemoteDirectoryScanResult{}, err
	}
	result := downloaderclient.RemoteDirectoryScanResult{TotalItems: len(entries), Missing: []string{}, Orphaned: []downloaderclient.RemoteDirectoryFile{}}
	found, referenced := map[string]bool{}, map[string]bool{}
	for _, name := range req.Referenced {
		referenced[name] = true
	}
	for _, entry := range entries {
		found[entry.Name()] = true
		if !referenced[entry.Name()] {
			item, err := entry.Info()
			if err != nil {
				return result, err
			}
			result.Orphaned = append(result.Orphaned, downloaderclient.RemoteDirectoryFile{Name: entry.Name(), Size: item.Size()})
		}
	}
	for _, name := range req.Expected {
		if !found[name] {
			result.Missing = append(result.Missing, name)
		}
	}
	if f.afterScan != nil {
		f.afterScan()
	}
	return result, nil
}

func scanFixture(t *testing.T, downloaders []any) (*Service, *gorm.DB) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"auth":{"password_hash":"test-placeholder"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(map[string]any{"downloaders": downloaders}); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "scan.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, ddl := range []string{
		"CREATE TABLE torrents (hash TEXT, name TEXT, save_path TEXT, size INTEGER, downloader_id TEXT, is_hidden INTEGER)",
		"CREATE TABLE seed_parameters (hash TEXT, type TEXT)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	return New(repository.NewLocalQueryRepository(&repository.Store{DB: db}), cfg, root), db
}

func addFixtureTorrent(t *testing.T, db *gorm.DB, hash, name, savePath, id string, hidden, video bool) {
	t.Helper()
	if err := db.Exec("INSERT INTO torrents VALUES (?,?,?,?,?,?)", hash, name, savePath, 100, id, hidden).Error; err != nil {
		t.Fatal(err)
	}
	if video {
		if err := db.Exec("INSERT INTO seed_parameters VALUES (?,?)", hash, "category.movie").Error; err != nil {
			t.Fatal(err)
		}
	}
}

func remoteConfig(id, host string) map[string]any {
	return map[string]any{"id": id, "name": id, "host": host, "proxy_port": 29090, "type": "qbittorrent", "use_proxy": true}
}

func writeFixtureFile(t *testing.T, root, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestScanRemoteUsesFullInventoryAndActualContentPaths(t *testing.T) {
	const directory = "/downloads/library"
	service, db := scanFixture(t, []any{remoteConfig("remote-a", "http://127.0.0.1:29080"), remoteConfig("remote-b", "http://localhost:39080")})
	addFixtureTorrent(t, db, "a", "Display Name", directory, "remote-a", false, true)
	addFixtureTorrent(t, db, "b", "Missing Display Name", directory, "remote-a", false, true)
	addFixtureTorrent(t, db, "c", "Hidden Name", directory, "remote-a", true, true)
	addFixtureTorrent(t, db, "d", "Audio Name", directory, "remote-a", false, false)
	root := t.TempDir()
	for _, name := range []string{"actual.mkv", "hidden.mkv", "music.flac", "unindexed.mkv", "sibling.mkv", "alone.mkv"} {
		writeFixtureFile(t, root, name)
	}
	owner := &fixtureDirectoryClient{dir: root, inventory: []downloaderclient.RemoteScanReference{
		{Hash: "a", SavePath: directory, ContentPath: directory + "/actual.mkv"},
		{Hash: "b", SavePath: directory, ContentPath: directory + "/missing.mkv"},
		{Hash: "c", SavePath: directory, ContentPath: directory + "/hidden.mkv"},
		{Hash: "d", SavePath: directory, ContentPath: directory + "/music.flac"},
		{Hash: "e", SavePath: directory, ContentPath: directory + "/unindexed.mkv"},
	}}
	sibling := &fixtureDirectoryClient{inventory: []downloaderclient.RemoteScanReference{
		{Hash: "f", SavePath: directory, ContentPath: directory + "/sibling.mkv"},
	}}
	service.openRemoteScan = func(_ map[string]any, id string) (remoteScanClient, error) {
		if id == "remote-a" {
			return owner, nil
		}
		return sibling, nil
	}
	result, err := service.Scan(context.Background(), directory, true)
	if err != nil || result["scan_status"] != "complete" {
		t.Fatalf("scan = %#v, %v", result, err)
	}
	if len(owner.requests) != 1 || len(sibling.requests) != 0 ||
		!reflect.DeepEqual(owner.requests[0].Expected, []string{"actual.mkv", "missing.mkv"}) ||
		!reflect.DeepEqual(owner.requests[0].Referenced, []string{"actual.mkv", "hidden.mkv", "missing.mkv", "music.flac", "sibling.mkv", "unindexed.mkv"}) {
		t.Fatalf("remote request = %#v, sibling = %#v", owner.requests, sibling.requests)
	}
	missing := result["missing_files"].([]map[string]any)
	orphaned := result["orphaned_files"].([]map[string]any)
	if len(missing) != 1 || missing[0]["expected_path"] != directory+"/missing.mkv" ||
		len(orphaned) != 1 || orphaned[0]["full_path"] != directory+"/alone.mkv" {
		t.Fatalf("missing = %#v, orphaned = %#v", missing, orphaned)
	}
}

func TestScanRemoteUnavailabilityIsNotZeroVerified(t *testing.T) {
	const directory = "/downloads/library"
	for _, tc := range []struct {
		name        string
		client      fixtureDirectoryClient
		wantRequest bool
		wantMessage string
	}{
		{"offline", fixtureDirectoryClient{listError: errors.New("远程客户端离线或繁忙")}, false, "离线"},
		{"inaccessible", fixtureDirectoryClient{inventory: []downloaderclient.RemoteScanReference{{Hash: "a", SavePath: directory, ContentPath: directory + "/a.mkv"}}, scanError: errors.New("远程目录无法完整核验")}, true, "无法完整核验"},
		{"ambiguous-directory", fixtureDirectoryClient{inventory: []downloaderclient.RemoteScanReference{{Hash: "a", SavePath: directory, ContentPath: directory}}}, false, "无法完整判定"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db := scanFixture(t, []any{remoteConfig("nas", "http://127.0.0.1:29080")})
			addFixtureTorrent(t, db, "a", "a.mkv", directory, "nas", false, true)
			service.openRemoteScan = func(map[string]any, string) (remoteScanClient, error) { return &tc.client, nil }
			result, err := service.Scan(context.Background(), directory, true)
			if err != nil || result["scan_status"] != "unverified" {
				t.Fatalf("result = %#v, %v", result, err)
			}
			issues := result["scan_issues"].([]scanIssue)
			if len(issues) != 1 || issues[0].Path != directory || !strings.Contains(issues[0].Message, tc.wantMessage) ||
				(len(tc.client.requests) == 1) != tc.wantRequest {
				t.Fatalf("issues = %#v, requests = %#v", issues, tc.client.requests)
			}
			cache, exists, err := service.GetScanCache()
			if !exists || err != nil || cache["scan_status"] != "unverified" {
				t.Fatalf("cache = %#v, exists = %v, err = %v", cache, exists, err)
			}
		})
	}
}

func TestScanPartialRetainsOnlyVerifiedCountsAndRespectsHiddenLocalReference(t *testing.T) {
	local := t.TempDir()
	writeFixtureFile(t, local, "found.mkv")
	writeFixtureFile(t, local, "hidden.mkv")
	writeFixtureFile(t, local, "orphan.mkv")
	service, db := scanFixture(t, []any{
		remoteConfig("nas", "http://127.0.0.1:29080"),
		map[string]any{"id": "local", "name": "local", "type": "qbittorrent"},
	})
	addFixtureTorrent(t, db, "a", "not-verified.mkv", "/downloads/remote", "nas", false, true)
	addFixtureTorrent(t, db, "b", "missing.mkv", local, "local", false, true)
	addFixtureTorrent(t, db, "c", "found.mkv", local, "local", false, true)
	addFixtureTorrent(t, db, "d", "hidden.mkv", local, "local", true, true)
	service.openRemoteScan = func(map[string]any, string) (remoteScanClient, error) {
		return &fixtureDirectoryClient{listError: errors.New("远程客户端离线或繁忙")}, nil
	}
	result, err := service.Scan(context.Background(), "", true)
	if err != nil || result["scan_status"] != "partial" {
		t.Fatalf("result = %#v, %v", result, err)
	}
	summary := result["scan_summary"].(map[string]any)
	if summary["total_torrents"] != 3 || summary["synced_count"] != 1 ||
		summary["missing_count"] != 1 || summary["orphaned_count"] != 1 || summary["skipped_remote"] != true {
		t.Fatalf("summary = %#v", summary)
	}
	if orphan := result["orphaned_files"].([]map[string]any)[0]["name"]; orphan != "orphan.mkv" {
		t.Fatalf("orphan name = %q", orphan)
	}
}

func TestScanRejectsOrphanResultWhenLiveReferencesChange(t *testing.T) {
	const directory = "/downloads/library"
	service, db := scanFixture(t, []any{remoteConfig("nas", "http://127.0.0.1:29080")})
	addFixtureTorrent(t, db, "a", "present.mkv", directory, "nas", false, true)
	root := t.TempDir()
	writeFixtureFile(t, root, "present.mkv")
	writeFixtureFile(t, root, "claimed.mkv")
	client := &fixtureDirectoryClient{dir: root, inventory: []downloaderclient.RemoteScanReference{
		{Hash: "a", SavePath: directory, ContentPath: directory + "/present.mkv"},
	}}
	client.afterScan = func() {
		client.inventory = append(client.inventory, downloaderclient.RemoteScanReference{
			Hash: "b", SavePath: directory, ContentPath: directory + "/claimed.mkv",
		})
	}
	service.openRemoteScan = func(map[string]any, string) (remoteScanClient, error) { return client, nil }
	result, err := service.Scan(context.Background(), directory, false)
	if err != nil || result["scan_status"] != "unverified" || len(result["orphaned_files"].([]map[string]any)) != 0 ||
		!strings.Contains(result["scan_issues"].([]scanIssue)[0].Message, "引用发生变化") {
		t.Fatalf("changed references accepted: %#v, %v", result, err)
	}
}

func TestScanRemoteMissingIsNotHiddenByLocalCopy(t *testing.T) {
	local := t.TempDir()
	writeFixtureFile(t, local, "movie.mkv")
	const remote = "/downloads/remote"
	service, db := scanFixture(t, []any{
		remoteConfig("nas", "http://127.0.0.1:29080"), map[string]any{"id": "local", "name": "local", "type": "qbittorrent"},
	})
	addFixtureTorrent(t, db, "a", "movie.mkv", remote, "nas", false, true)
	addFixtureTorrent(t, db, "b", "movie.mkv", local, "local", false, true)
	client := &fixtureDirectoryClient{dir: t.TempDir(), inventory: []downloaderclient.RemoteScanReference{
		{Hash: "a", SavePath: remote, ContentPath: remote + "/movie.mkv"},
	}}
	service.openRemoteScan = func(map[string]any, string) (remoteScanClient, error) { return client, nil }
	result, err := service.Scan(context.Background(), "", false)
	if err != nil || result["scan_status"] != "complete" {
		t.Fatalf("scan = %#v, %v", result, err)
	}
	missing := result["missing_files"].([]map[string]any)
	if len(missing) != 1 || missing[0]["expected_path"] != remote+"/movie.mkv" {
		t.Fatalf("remote missing copy was suppressed: %#v", missing)
	}
}

func TestLocalScanRespectsCaseInsensitiveFilename(t *testing.T) {
	local := t.TempDir()
	writeFixtureFile(t, local, "movie.mkv")
	if _, err := os.Stat(filepath.Join(local, "Movie.mkv")); os.IsNotExist(err) {
		t.Skip("requires case-insensitive filesystem")
	} else if err != nil {
		t.Fatal(err)
	}
	scan, err := scanLocalDirectory(context.Background(), local, local, map[string]bool{"Movie.mkv": true})
	if err != nil || scan.missing["Movie.mkv"] || len(scan.orphaned) != 0 {
		t.Fatalf("case alias failed: %#v, %v", scan, err)
	}
}
