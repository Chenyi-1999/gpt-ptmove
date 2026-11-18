package repair

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

func TestRemoteScreenshotFallbackPolicy(t *testing.T) {
	operations := []struct {
		name string
		mode string
		run  func(ScreenshotGenerateInput) error
	}{
		{name: "automatic", run: func(input ScreenshotGenerateInput) error {
			_, err := GenerateAndUploadScreenshots(input)
			return err
		}},
		{name: "preview", mode: "preview", run: func(input ScreenshotGenerateInput) error {
			_, err := GenerateScreenshotPreviewCandidates(input, 5)
			return err
		}},
		{name: "selected", mode: "finalize", run: func(input ScreenshotGenerateInput) error {
			_, err := GenerateAndUploadSelectedScreenshots(input, []float64{30})
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			const fixtureToken = "test-media-token"
			for _, tc := range []struct {
				name      string
				status    int
				useProxy  bool
				mediaOnly bool
				token     string
				wantCalls int32
				wantError int
			}{
				{"bad gateway", http.StatusBadGateway, true, true, fixtureToken, 1, http.StatusBadGateway},
				{"gateway timeout", http.StatusGatewayTimeout, true, true, fixtureToken, 1, http.StatusGatewayTimeout},
				{"route not found", http.StatusNotFound, true, true, fixtureToken, 1, http.StatusNotFound},
				{"next path", http.StatusBadRequest, true, true, fixtureToken, 2, 0},
				{"ordinary proxy fallback", http.StatusBadGateway, true, false, fixtureToken, 1, 0},
				{"legacy unauthenticated fallback", http.StatusBadGateway, true, true, "", 1, 0},
				{"local only", http.StatusBadGateway, false, false, "", 0, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					savePath := filepath.Join(t.TempDir(), "unmounted")
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						call := calls.Add(1)
						var request struct {
							Path string `json:"remote_path"`
							Mode string `json:"mode"`
						}
						if r.Method != http.MethodPost || r.URL.Path != "/api/media/screenshot" ||
							json.NewDecoder(r.Body).Decode(&request) != nil || request.Mode != operation.mode {
							t.Error("unexpected screenshot proxy request")
						}
						wantAuth := ""
						if tc.token != "" {
							wantAuth = "Bearer " + tc.token
						}
						if r.Header.Get("Authorization") != wantAuth {
							t.Error("unexpected media proxy authorization")
						}
						if call == 1 {
							if request.Path != filepath.Join(savePath, "first.mkv") {
								t.Errorf("wrong first candidate %q", request.Path)
							}
							http.Error(w, "media fixture failure", tc.status)
							return
						}
						if request.Path != filepath.Join(savePath, "second.mkv") {
							t.Errorf("wrong next candidate %q", request.Path)
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"success":true,"bbcode":"[img]https://img.example.test/frame.png[/img]","preview_candidates":[{},{},{},{},{}]}`)
					}))
					defer server.Close()
					input := ScreenshotGenerateInput{
						Payload:     map[string]any{"downloader_id": "fixture", "save_path": savePath, "torrent_name": "first.mkv"},
						ContentName: "second.mkv",
						RootConfig: map[string]any{"downloaders": []any{map[string]any{
							"id": "fixture", "type": "qbittorrent", "host": server.URL, "use_proxy": tc.useProxy,
							"proxy_media_only": tc.mediaOnly, "proxy_port": server.Listener.Addr().(*net.TCPAddr).Port, "proxy_token": tc.token,
						}}},
					}
					err := operation.run(input)
					if calls.Load() != tc.wantCalls {
						t.Errorf("proxy calls = %d, want %d", calls.Load(), tc.wantCalls)
					}
					var proxyErr *downloaderclient.ProxyAPIError
					switch {
					case tc.wantError != 0:
						if !errors.As(err, &proxyErr) || proxyErr.StatusCode != tc.wantError {
							t.Fatalf("expected original remote HTTP %d error, got %v", tc.wantError, err)
						}
					case tc.wantCalls == 2:
						if err != nil {
							t.Fatalf("next media path should succeed: %v", err)
						}
					default:
						if err == nil || errors.As(err, &proxyErr) {
							t.Fatalf("expected failure from existing local path handling, got %v", err)
						}
					}
				})
			}
		})
	}
}
