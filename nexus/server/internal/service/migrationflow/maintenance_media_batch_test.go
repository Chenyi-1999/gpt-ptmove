package migrationflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/config"
)

func TestMaintenanceMediaBatchFillsOnlySelectedMissingFields(t *testing.T) {
	_, s, db := automationNativeFixture(t)
	schema := `CREATE TABLE seed_parameters (hash TEXT,torrent_id TEXT,site_name TEXT,body TEXT,statement TEXT,mediainfo TEXT,mediainfo_status TEXT,bdinfo_task_id TEXT,updated_at TEXT,screenshots TEXT,screenshot_review_status TEXT,PRIMARY KEY(hash,torrent_id,site_name))`
	if err := db.Exec(schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE torrents (hash TEXT,name TEXT,save_path TEXT,downloader_id TEXT,sites TEXT,details TEXT,size INTEGER,last_seen TEXT,is_hidden INTEGER,state TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	const oldPicture = "[img]https://example.com/manual.png[/img]"
	hash := strings.Repeat("a", 40)
	addSeed := func(id, site, screenshot, report, desc string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO seed_parameters (hash,torrent_id,site_name,screenshots,mediainfo,body) VALUES (?,?,?,?,?,?)`, hash, id, site, screenshot, report, desc).Error; err != nil {
			t.Fatal(err)
		}
	}
	sample, err := os.ReadFile("../acquire/extract/testdata/mediainfo-quote-without-general.bbcode")
	if err != nil {
		t.Fatal(err)
	}
	quote := strings.TrimSpace(strings.ReplaceAll(string(sample), "\r\n", "\n"))
	quotedBody := strings.TrimSuffix(strings.TrimPrefix(quote, "[quote]"), "[/quote]")
	quotedReport := "General\n" + strings.TrimSpace(strings.ReplaceAll(quotedBody, "\\\n", "\n"))
	addSeed("1", "source", oldPicture, "", quote)
	addSeed("2", "source", "", preparedReportFixture, "")
	addSeed("3", "source", oldPicture, preparedReportFixture, "")
	addSeed("4", "source", "", "", quote)
	addSeed("5", "source", "", "", quote)
	addSeed("2", "other", "", "", "")
	if err := db.Exec(`INSERT INTO torrents (hash,name,save_path,downloader_id,last_seen) VALUES (?,?,?,?,?)`, hash, "缓存名", "/cache", "qb-fixture", "2026-11-23").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO torrents (hash,name,save_path,downloader_id,last_seen) VALUES (?,?,?,?,?)`, hash, "另一个缓存", "/other", "qb-other", "2026-11-24").Error; err != nil {
		t.Fatal(err)
	}
	var shots atomic.Int32
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	defer func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	}()
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/version":
			fmt.Fprint(w, "v5.0")
		case "/api/v2/torrents/info":
			if r.URL.Query().Get("hashes") != hash {
				t.Error("did not read exact selected hash")
			}
			fmt.Fprintf(w, `[{"hash":%q,"name":"Movie","save_path":"/keep","content_path":"/keep/Movie","progress":1,"amount_left":0}]`, hash)
		case "/api/v2/torrents/files":
			if r.URL.Query().Get("hash") != hash {
				t.Error("did not check exact selected file list")
			}
			fmt.Fprint(w, `[{"name":"Movie/video.mkv","size":500,"progress":1,"priority":1}]`)
		case "/api/file/check", "/api/media/screenshot":
			var request map[string]any
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil || request["remote_path"] != "/keep/Movie/video.mkv" {
				t.Error("used non-authoritative media path")
			}
			if r.URL.Path == "/api/file/check" {
				fmt.Fprint(w, `{"success":true,"exists":true}`)
				return
			}
			n := 5
			if shots.Add(1) == 1 {
				started <- struct{}{}
				<-unblock
			} else {
				n = 3
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "bbcode": maintenanceScreenshotFixture(n)})
		default:
			t.Errorf("unexpected request while filling metadata: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(qb.Close)
	port, _ := strconv.Atoi(qb.URL[strings.LastIndex(qb.URL, ":")+1:])
	t.Setenv("AUTH_PASSWORD", "isolated-maintenance")
	s.cfg, err = config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.Save(map[string]any{"downloaders": []any{map[string]any{
		"id": "qb-fixture", "type": "qbittorrent", "host": qb.URL, "use_proxy": true, "proxy_media_only": true,
		"proxy_port": port, "api_key": "fixture-token", "proxy_token": "fixture-token",
	}}}); err != nil {
		t.Fatal(err)
	}
	seeds := []MaintenanceMediaSeed{
		{Hash: strings.ToUpper(hash), TorrentID: "1", SiteName: "source"},
		{Hash: hash, TorrentID: "2", SiteName: "source", DownloaderID: "qb-fixture"},
		{Hash: hash, TorrentID: "2", SiteName: "source"},
		{Hash: hash, TorrentID: "3", SiteName: "source"},
		{Hash: hash, TorrentID: "4", SiteName: "source", DownloaderID: "qb-fixture"},
		{Hash: hash, TorrentID: "missing", SiteName: "source"},
	}
	result, status := s.StartMaintenanceMediaFill(seeds)
	if status != http.StatusAccepted || result["total"] != 5 {
		t.Fatalf("batch not accepted or not deduplicated: %#v %d", result, status)
	}
	select {
	case <-started:
		rows := s.observations.snapshot()
		if len(rows) != 1 || rows[0]["key"] != result["task_key"] || !strings.Contains(fmt.Sprint(rows[0]["message"]), "第 2/5 条") {
			t.Fatalf("missing in-flight progress: %#v", rows)
		}
		if _, duplicateStatus := s.StartMaintenanceMediaFill(seeds[:1]); duplicateStatus != http.StatusConflict {
			t.Fatal("concurrent batch started")
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("batch did not reach screenshot fixture: %#v", s.observations.snapshot())
	}
	close(unblock)
	deadline := time.Now().Add(4 * time.Second)
	for s.maintenanceMediaRunning.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.maintenanceMediaRunning.Load() {
		t.Fatal("batch did not finish")
	}
	rows := s.observations.snapshot()
	if len(rows) != 1 || rows[0]["outcome"] != "partial" ||
		rows[0]["progress_text"] != "共 5 条 · 已补全 2 · 已有资料 1 · 部分补全 1 · 失败 1" {
		t.Fatalf("incorrect final batch state: %#v", rows)
	}
	assertFields := func(id, site, screenshot, report string) {
		t.Helper()
		seed, err := s.repo.GetSeedParameterByKey(hash, id, site)
		if err != nil || mediaFieldForCompare(seed["screenshots"]) != screenshot || mediaFieldForCompare(seed["mediainfo"]) != report {
			t.Fatalf("unexpected saved media for %s %s: %#v / %v", site, id, seed, err)
		}
	}
	assertFields("1", "source", oldPicture, quotedReport)
	assertFields("2", "source", maintenanceScreenshotFixture(5), preparedReportFixture)
	assertFields("3", "source", oldPicture, preparedReportFixture)
	assertFields("4", "source", "", quotedReport)
	assertFields("5", "source", "", "")
	assertFields("2", "other", "", "")
	if shots.Load() != 2 {
		t.Fatalf("existing and unselected screenshot fields triggered capture: %d", shots.Load())
	}
}

func TestMaintenanceMediaRejectsInvalidSelectionAndDuplicatedScreenshotImages(t *testing.T) {
	s := NewMigrateService(nil, nil)
	for _, invalid := range [][]MaintenanceMediaSeed{nil, {{Hash: "bad/hash", TorrentID: "1", SiteName: "source"}}, make([]MaintenanceMediaSeed, 101)} {
		if _, status := s.StartMaintenanceMediaFill(invalid); status != http.StatusBadRequest {
			t.Fatalf("invalid selection returned %d", status)
		}
	}
	raw := maintenanceScreenshotFixture(4)
	first, _, _ := strings.Cut(raw, "\n")
	if _, err := maintenanceFiveScreenshots(raw + "\n" + first); err == nil {
		t.Fatal("repeated image counted as a fifth screenshot")
	}
}

func maintenanceScreenshotFixture(count int) string {
	images := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		images = append(images, fmt.Sprintf("[img]https://img3.pixhost.to/images/123/frame-%d.png[/img]", i))
	}
	return strings.Join(images, "\n")
}
