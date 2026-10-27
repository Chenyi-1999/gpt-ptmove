package downloaderclient

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

func TestDisableAfterStateReadPreventsAddPOST(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing independent Bearer header")
		}
		switch r.URL.Path {
		case "/api/v2/app/version":
			fmt.Fprint(w, "v5.1.2")
		case "/api/v2/torrents/info":
			allowed.Store(false)
			fmt.Fprint(w, "[]")
		default:
			writes.Add(1)
			fmt.Fprint(w, "Ok.")
		}
	}))
	defer server.Close()
	tracker := "https://127.0.0.1/announce"
	info := "d6:lengthi1e4:name4:test12:piece lengthi1e6:pieces20:012345678901234567897:privatei1ee"
	data := []byte(fmt.Sprintf("d8:announce%d:%s4:info%se", len(tracker), tracker, info))
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if err != nil {
		t.Fatal(err)
	}
	downloader := Downloader{Type: "qbittorrent", Host: server.URL, APIKey: "fixture-key"}.WithEffectGuard(func() error {
		if !allowed.Load() {
			return errors.New("disabled")
		}
		return nil
	})
	if downloader.AddSourceTorrent(data, "fixture.torrent", "/nas/data", meta.InfoHash) == nil {
		t.Fatal("revoked effect accepted")
	}
	if writes.Load() != 0 {
		t.Fatal("qB POST after permission revoked")
	}
}

func TestTokenFailureDoesNotFallBackToLoginOrLeakResponse(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			logins.Add(1)
		}
		http.Error(w, "sensitive-upstream-body", 401)
	}))
	defer server.Close()
	client, _ := newQBClient(Downloader{Host: server.URL, APIKey: "fixture-key", Username: "admin", Password: "unused"})
	if err := client.Login(); err == nil || logins.Load() != 0 {
		t.Fatal("token failure fell back to password login")
	}
}

func TestAutomationDirectQBRetainsNativeNASMediaProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("direct qB token missing")
		}
		switch r.URL.Path {
		case "/api/v2/app/version":
			fmt.Fprint(w, "v5.2.1")
		case "/api/v2/torrents/info":
			fmt.Fprint(w, "[]")
		default:
			t.Error("unexpected proxy or write request")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	root := map[string]any{"downloaders": []any{map[string]any{"id": "nas", "type": "qbittorrent", "host": server.URL, "api_key": "fixture-key", "use_proxy": true, "proxy_port": 9090, "enabled": true}}}
	d, decision, err := DecideProxy(root, "nas")
	if err != nil || !decision.Enabled {
		t.Fatal("native media proxy configuration lost")
	}
	if status, err := d.TorrentStatus("fixture-hash"); err != nil || status.Found {
		t.Fatal("automation did not use direct qB API with media proxy enabled")
	}
	if !d.UseProxy {
		t.Fatal("automation changed native media proxy configuration")
	}
}
