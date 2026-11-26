package settings

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/service"
)

func TestDownloaderConnectionModes(t *testing.T) {
	const apiToken = "fixture-qb-token"
	const mediaToken = "fixture-media-token-at-least-32-characters"

	for _, tt := range []struct {
		name        string
		useProxy    bool
		mediaOnly   bool
		qbStatus    int
		mediaStatus int
		qbCalls     int32
		mediaCalls  int32
		wantSuccess bool
		wantMessage string
	}{
		{"direct", false, false, 200, 405, 2, 0, true, "连接测试成功"},
		{"remote media", true, true, 200, 405, 2, 1, true, "与媒体代理"},
		{"old full proxy config", true, false, 200, 405, 2, 1, true, "与媒体代理"},
		{"invalid media token", true, true, 200, 401, 2, 1, false, "媒体代理令牌"},
		{"unavailable downloader API", true, false, 503, 405, 1, 0, false, "qB API Token认证失败"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var qbCalls, mediaCalls atomic.Int32
			qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				qbCalls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/app/version" ||
					r.Header.Get("Authorization") != "Bearer "+apiToken {
					t.Error("expected native qB version check with downloader credentials")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(tt.qbStatus)
				_, _ = io.WriteString(w, "v5.3.1")
			}))
			defer qb.Close()
			media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mediaCalls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/file/check" ||
					r.Header.Get("Authorization") != "Bearer "+mediaToken {
					t.Error("expected read-only file endpoint probe with media credentials")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(tt.mediaStatus)
			}))
			defer media.Close()

			manager, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
			if err != nil {
				t.Fatal(err)
			}
			settings := service.NewSettingsService(manager)
			entry := map[string]any{
				"id": "fixture", "type": "qbittorrent", "host": qb.URL, "use_proxy": tt.useProxy,
				"proxy_port": media.Listener.Addr().(*net.TCPAddr).Port, "proxy_media_only": tt.mediaOnly,
				"api_key": apiToken, "proxy_token": mediaToken,
			}
			if err := settings.UpdateSettings(map[string]any{"downloaders": []any{entry}}); err != nil {
				t.Fatal(err)
			}
			// As with a masked settings form, blank request credentials are read from storage.
			delete(entry, "api_key")
			delete(entry, "proxy_token")
			body, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/test_connection", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = request
			New(settings, nil, nil, nil).TestConnection(c)
			var result struct {
				Success bool
				Message string
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || result.Success != tt.wantSuccess || !strings.Contains(result.Message, tt.wantMessage) {
				t.Fatalf("HTTP %d, result %+v", response.Code, result)
			}
			if qbCalls.Load() != tt.qbCalls || mediaCalls.Load() != tt.mediaCalls {
				t.Fatalf("qB calls %d, media calls %d", qbCalls.Load(), mediaCalls.Load())
			}
			if strings.Contains(result.Message, apiToken) || strings.Contains(result.Message, mediaToken) {
				t.Fatal("test response exposed credentials")
			}
		})
	}
}
