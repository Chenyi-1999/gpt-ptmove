package downloaderclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyCallsUseDedicatedBearer(t *testing.T) {
	expected := "separate-proxy-secret"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+expected {
			t.Errorf("missing dedicated proxy credential on %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/file/check":
			fmt.Fprint(w, `{"success":true,"exists":false}`)
		case "/api/media/screenshot":
			fmt.Fprint(w, `{"success":true,"bbcode":"[img]https://img99.pixhost.to/images/1/a.png[/img]"}`)
		case "/api/media/mediainfo":
			fmt.Fprint(w, `{"success":true,"mediainfo":"General"}`)
		case "/api/media/episode-count":
			fmt.Fprint(w, `{"success":true,"episode_count":2,"season_number":1}`)
		case "/api/media/bdinfo":
			fmt.Fprint(w, `{"success":true}`)
		case "/api/media/bdinfo/progress/task":
			fmt.Fprint(w, `{"success":true,"task":{"task_id":"task"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	root := map[string]any{"downloaders": []any{map[string]any{"id": "test", "type": "qbittorrent", "host": server.URL, "enabled": true, "use_proxy": true, "proxy_port": number, "proxy_token": expected, "api_key": "never-proxy-qb-key"}}}
	d, err := FromConfig(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.FetchScreenshotsByProxy("/media/a.mkv", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.FetchMediaInfoByProxy("/media/a.mkv", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.FetchEpisodeCountByProxy("/media/a"); err != nil {
		t.Fatal(err)
	}
	if err := d.StartBDInfoByProxy("/media/a", "task", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FetchBDInfoProgressByProxy("task"); err != nil {
		t.Fatal(err)
	}
	if exists, err := d.CheckMediaPathByProxy(context.Background(), "/media/a.mkv"); exists || err != nil {
		t.Fatalf("file check exists=%v error=%v", exists, err)
	}
	if calls.Load() != 6 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestProxyTransportDoesNotFollowRedirectsOrReuseQBKey(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer proxy-only" {
			t.Error("incorrect credential")
		}
		http.Redirect(w, r, target.URL, 302)
	}))
	defer source.Close()
	d := Downloader{ProxyToken: "proxy-only", APIKey: "qb-only"}
	response, err := d.proxyHTTPClient(time.Second).Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 302 || leaked.Load() != 0 {
		t.Fatal("followed redirect with proxy secret")
	}
	d.ProxyToken = ""
	request := httptest.NewRequest("GET", source.URL, nil)
	// Blank tokens remain compatible with an old unauthenticated agent.
	var header string
	transport := proxyAuthTransport{base: roundTripFixture(func(r *http.Request) (*http.Response, error) {
		header = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})}
	_, _ = transport.RoundTrip(request)
	if header != "" {
		t.Fatal("blank proxy token reused qB key")
	}
}

type roundTripFixture func(*http.Request) (*http.Response, error)

func (f roundTripFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloaderListsAndStatsAlwaysUseNativeAPI(t *testing.T) {
	var listCalls, statsCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer qb-key" {
			t.Error("direct qB key missing")
		}
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			listCalls.Add(1)
			fmt.Fprint(w, `[]`)
		case "/api/v2/sync/maindata":
			statsCalls.Add(1)
			fmt.Fprint(w, `{"server_state":{"dl_info_speed":7}}`)
		case "/api/v2/app/version":
			fmt.Fprint(w, "fixture")
		default:
			t.Errorf("unexpected qB route %s", r.URL.Path)
		}
	}))
	defer server.Close()
	for _, mode := range []struct {
		name      string
		useProxy  bool
		mediaOnly bool
	}{
		{name: "direct"},
		{name: "remote media", useProxy: true, mediaOnly: true},
		{name: "old full proxy settings", useProxy: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			listBefore, statsBefore := listCalls.Load(), statsCalls.Load()
			config := map[string]any{"downloaders": []any{map[string]any{
				"id": "test", "type": "qbittorrent", "host": server.URL, "api_key": "qb-key",
				"use_proxy": mode.useProxy, "proxy_media_only": mode.mediaOnly, "proxy_port": 1, "proxy_token": "proxy-key",
			}}}
			d, err := FromConfig(config, "test")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.FetchTorrents(); err != nil {
				t.Fatal(err)
			}
			stats, err := d.FetchTrafficStats()
			if err != nil || stats.DownloadSpeed != 7 {
				t.Fatalf("native stats %+v %v", stats, err)
			}
			if listCalls.Load()-listBefore != 1 || statsCalls.Load()-statsBefore != 1 {
				t.Fatalf("native list calls %d, stats calls %d", listCalls.Load()-listBefore, statsCalls.Load()-statsBefore)
			}
		})
	}
}
