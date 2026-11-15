package guard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaOnlyProxyDoesNotSkipPublicationGuard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTNEXUS_DATA_DIR", dir)
	t.Setenv("GO_SERVICE_URL", "")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "fixture unavailable", 503) }))
	defer server.Close()
	raw, _ := json.Marshal(map[string]any{"downloaders": []any{map[string]any{"id": "nas", "name": "nas", "type": "qbittorrent", "host": server.URL, "api_key": "qb-key", "enabled": true, "use_proxy": true, "proxy_media_only": true}}})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	downloaders, err := loadDownloadersConfig()
	if err != nil || len(downloaders) != 1 || downloaders[0].UseProxy {
		t.Fatalf("media-only skipped direct guard: %+v %v", downloaders, err)
	}
	if canContinue, _ := checkByLocalGuard("nas"); canContinue || calls == 0 {
		t.Fatalf("unavailable direct qB must fail closed calls=%d", calls)
	}
}
