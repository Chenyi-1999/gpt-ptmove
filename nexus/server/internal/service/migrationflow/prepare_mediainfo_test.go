package migrationflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pt-nexus/server/internal/config"
	"gorm.io/gorm"
)

const preparedReportFixture = "General\nFormat : Matroska\nDuration : 90 min\n\nVideo\nFormat : HEVC\nWidth : 3840 pixels\n\nAudio\nFormat : AAC"

// TestPrepareSeedMediaInfo 覆盖完整和受限跳过、精确代理请求、失败保护以及并发编辑。
func TestPrepareSeedMediaInfo(t *testing.T) {
	cases := []struct {
		name, current, tags, status string
		mediaResponse               string
		writeDuringRequest          string
		failProxy                   bool
		disableProxy                bool
		partialDownload             bool
		expectedError               string
		expectedCalls               int32
	}{
		{name: "缺失", mediaResponse: preparedReportFixture, expectedCalls: 1},
		{name: "非空占位", current: "  等待获取  ", mediaResponse: preparedReportFixture, expectedCalls: 1},
		{name: "报告已有", current: preparedReportFixture},
		{name: "已有BDInfo", current: "DISC INFO:\nDisc Size: 10 GB\n\nPLAYLIST REPORT:\nVIDEO:\nHEVC"},
		{name: "禁转", tags: `["tag.禁转"]`},
		{name: "旧分集标签", tags: "分集"},
		{name: "BDInfo处理中", status: "processing_bdinfo", expectedError: "已有媒体分析"},
		{name: "没有远程代理", disableProxy: true, expectedError: "远程媒体代理"},
		{name: "未下载完整", partialDownload: true, expectedError: "未找到已完整下载"},
		{name: "代理失败", current: "  等待获取  ", failProxy: true, expectedError: "提取失败", expectedCalls: 1},
		{name: "不完整结果", mediaResponse: "仅有占位", expectedError: "未返回完整", expectedCalls: 1},
		{name: "并发编辑", mediaResponse: preparedReportFixture, writeDuringRequest: strings.ReplaceAll(preparedReportFixture, "HEVC", "AVC"), expectedCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, service, db := automationNativeFixture(t)
			const schema = `CREATE TABLE seed_parameters (hash TEXT,torrent_id TEXT,site_name TEXT,title TEXT,tags TEXT,mediainfo TEXT,mediainfo_status TEXT,bdinfo_error TEXT,updated_at TEXT,screenshots TEXT,PRIMARY KEY(hash,torrent_id,site_name))`
			if err := db.Exec(schema).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TABLE torrents (hash TEXT,name TEXT,save_path TEXT,downloader_id TEXT,sites TEXT,details TEXT,size INTEGER,last_seen TEXT,is_hidden INTEGER)`).Error; err != nil {
				t.Fatal(err)
			}
			hash := strings.Repeat("c", 40)
			if err := db.Exec(`INSERT INTO torrents (hash,name,save_path,downloader_id,last_seen) VALUES (?,?,?,?,?)`,
				hash, "陈旧名称", "/stale", "qb-fixture", "2026-11-21").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`INSERT INTO seed_parameters (hash,torrent_id,site_name,title,tags,mediainfo,mediainfo_status,bdinfo_error,screenshots) VALUES (?,?,?,?,?,?,?,?,?)`,
				hash, "45", "zmpt", "Harboring", tc.tags, tc.current, tc.status, "旧诊断", "截图原文").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`INSERT INTO seed_parameters (hash,torrent_id,site_name,mediainfo) VALUES (?,?,?,?)`,
				hash, "45", "other", "不相关草稿").Error; err != nil {
				t.Fatal(err)
			}
			var qbCalls, mediaCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/app/version":
					qbCalls.Add(1)
					if r.Header.Get("Authorization") != "Bearer qb-fixture-token" {
						t.Error("qB token mismatch")
					}
					_, _ = w.Write([]byte("v5.0"))
				case "/api/v2/torrents/info":
					qbCalls.Add(1)
					if r.URL.Query().Get("hashes") != hash || r.Header.Get("Authorization") != "Bearer qb-fixture-token" {
						t.Error("qB lookup did not use exact hash and expected authentication")
					}
					progress, amountLeft := 1.0, 0
					if tc.partialDownload {
						progress, amountLeft = 0.9, 123
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{{
						"hash": hash, "name": "无扩展名的种子名称", "save_path": "/keep/TEST",
						"content_path": "/keep/TEST/藏凶者/影片.mkv", "progress": progress, "amount_left": amountLeft,
					}})
				case "/api/media/mediainfo":
					mediaCalls.Add(1)
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil ||
						payload["remote_path"] != "/keep/TEST/藏凶者/影片.mkv" || payload["content_name"] != "Harboring" ||
						r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer media-fixture-token" {
						t.Error("media request did not use qB content_path and media-only authentication")
					}
					if tc.failProxy {
						http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
						return
					}
					if tc.writeDuringRequest != "" {
						concurrentlyUpdatePreparedMedia(t, db, hash, tc.writeDuringRequest)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "mediainfo": tc.mediaResponse})
				default:
					t.Errorf("unexpected network request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			parsed, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(parsed.Port())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("AUTH_PASSWORD", "isolated-placeholder")
			service.cfg, err = config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.cfg.Save(map[string]any{"downloaders": []any{map[string]any{
				"id": "qb-fixture", "type": "qbittorrent", "host": server.URL, "enabled": true, "use_proxy": !tc.disableProxy,
				"proxy_media_only": true, "proxy_port": port, "api_key": "qb-fixture-token", "proxy_token": "media-fixture-token",
			}}}); err != nil {
				t.Fatal(err)
			}

			err = service.prepareSeedMediaInfo("", hash, "45", "zmpt", "")
			if tc.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedError) {
					t.Fatalf("expected %q, got %v", tc.expectedError, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.disableProxy && !errors.Is(err, errPrepareMediaUnavailable) {
				t.Fatalf("missing unavailable marker: %v", err)
			}
			if calls := mediaCalls.Load(); calls != tc.expectedCalls {
				t.Fatalf("media calls=%d, want %d", calls, tc.expectedCalls)
			}
			expectedQBCalls := int32(0)
			if tc.expectedCalls == 1 || tc.partialDownload {
				expectedQBCalls = 2
			}
			if calls := qbCalls.Load(); calls != expectedQBCalls {
				t.Fatalf("qB calls=%d, want %d", calls, expectedQBCalls)
			}
			expectedReport, expectedStatus := tc.current, tc.status
			if tc.expectedCalls == 1 && tc.expectedError == "" {
				expectedReport, expectedStatus = tc.mediaResponse, "completed"
			}
			if tc.writeDuringRequest != "" {
				expectedReport = tc.writeDuringRequest
			}
			got, err := service.repo.GetSeedParameterByKey(hash, "45", "zmpt")
			if err != nil || got["mediainfo"] != expectedReport || got["mediainfo_status"] != expectedStatus || got["screenshots"] != "截图原文" {
				t.Fatalf("prepared draft mismatch: %v err=%v", got, err)
			}
			other, err := service.repo.GetSeedParameterByKey(hash, "45", "other")
			if err != nil || other["mediainfo"] != "不相关草稿" {
				t.Fatalf("unrelated seed modified: %v err=%v", other, err)
			}
			if tc.expectedCalls == 1 && tc.expectedError == "" {
				if err := service.prepareSeedMediaInfo("", hash, "45", "zmpt", ""); err != nil || mediaCalls.Load() != 1 || qbCalls.Load() != 2 {
					t.Fatalf("second preparation must reuse saved report: err=%v media=%d qb=%d", err, mediaCalls.Load(), qbCalls.Load())
				}
			}
		})
	}
}

func concurrentlyUpdatePreparedMedia(t *testing.T, db *gorm.DB, hash string, report string) {
	t.Helper()
	if err := db.Exec(`UPDATE seed_parameters SET mediainfo=?, mediainfo_status='completed' WHERE hash=? AND torrent_id='45' AND site_name='zmpt'`, report, hash).Error; err != nil {
		t.Errorf("fixture edit failed: %v", err)
	}
}
