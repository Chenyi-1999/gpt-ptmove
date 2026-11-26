package guard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestRemoteMediaModesDoNotSkipPublicationGuard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTNEXUS_DATA_DIR", dir)
	t.Setenv("GO_SERVICE_URL", "")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "fixture unavailable", 503) }))
	defer server.Close()
	for _, mode := range []struct {
		name      string
		mediaOnly bool
	}{
		{name: "remote media", mediaOnly: true},
		{name: "old full proxy config"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			before := calls.Load()
			raw, err := json.Marshal(map[string]any{"downloaders": []any{map[string]any{
				"id": "nas", "name": "nas", "type": "qbittorrent", "host": server.URL, "api_key": "qb-key",
				"enabled": true, "use_proxy": true, "proxy_media_only": mode.mediaOnly,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if canContinue, _ := checkByLocalGuard("nas"); canContinue || calls.Load() == before {
				t.Fatalf("unavailable qB must fail closed, calls=%d", calls.Load()-before)
			}
		})
	}
}
