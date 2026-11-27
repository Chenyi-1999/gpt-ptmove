package downloaderclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRemoteScanUsesExistingReadOnlyLanes(t *testing.T) {
	var scanned, listed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/file/scan":
			scanned.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer media-fixture" ||
				r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
				t.Errorf("unexpected media scan request: %s %s", r.Method, r.URL.Path)
			}
			var got RemoteDirectoryScanRequest
			want := RemoteDirectoryScanRequest{RemotePath: "/data/影视", Expected: []string{"missing.mkv"}, Referenced: []string{"missing.mkv", "other-task.mkv"}}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("request=%+v, decode error=%v", got, err)
			}
			fmt.Fprint(w, `{"success":true,"complete":true,"total_items":2,"missing":["missing.mkv"],"orphaned":[{"name":"alone.mkv","size":42}]}`)
		case "/api/local-scan/references":
			listed.Add(1)
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer qb-fixture" ||
				r.URL.RawQuery != "" || r.RequestURI != "/api/local-scan/references" || r.ContentLength != 0 {
				t.Errorf("unexpected qB inventory request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			}
			fmt.Fprintf(w, `[{"hash":%q,"save_path":"/data/影视/","content_path":"/data/影视/other-task.mkv"}]`, strings.Repeat("A", 40))
		default:
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := remoteScanFixture(t, server)
	references, err := d.ListRemoteScanReferences(context.Background())
	wantRefs := []RemoteScanReference{{Hash: strings.Repeat("a", 40), SavePath: "/data/影视", ContentPath: "/data/影视/other-task.mkv"}}
	if err != nil || !reflect.DeepEqual(references, wantRefs) {
		t.Fatalf("references=%+v, err=%v", references, err)
	}
	result, err := d.ScanMediaDirectoryByProxy(context.Background(), RemoteDirectoryScanRequest{
		RemotePath: "/data/影视/", Expected: []string{"missing.mkv"}, Referenced: []string{"missing.mkv", "other-task.mkv"},
	})
	wantResult := RemoteDirectoryScanResult{TotalItems: 2, Missing: []string{"missing.mkv"}, Orphaned: []RemoteDirectoryFile{{Name: "alone.mkv", Size: 42}}}
	if err != nil || !reflect.DeepEqual(result, wantResult) {
		t.Fatalf("result=%+v, err=%v", result, err)
	}
	if listed.Load() != 1 || scanned.Load() != 1 {
		t.Fatalf("got %d qB calls and %d media calls", listed.Load(), scanned.Load())
	}
}

func TestRemoteScanFailsClosedForIncompleteOrMisclassifiedResults(t *testing.T) {
	expected := map[string]struct{}{"missing.mkv": {}}
	references := map[string]struct{}{"missing.mkv": {}, "other-task.mkv": {}}
	for _, tc := range []struct{ name, response string }{
		{"missing completion", `{"success":true,"total_items":0,"missing":[],"orphaned":[]}`},
		{"partial", `{"success":true,"complete":false,"total_items":0,"missing":[],"orphaned":[]}`},
		{"missing total", `{"success":true,"complete":true,"missing":[],"orphaned":[]}`},
		{"null arrays", `{"success":true,"complete":true,"total_items":0,"missing":null,"orphaned":null}`},
		{"unknown missing", `{"success":true,"complete":true,"total_items":0,"missing":["other.mkv"],"orphaned":[]}`},
		{"duplicate missing", `{"success":true,"complete":true,"total_items":0,"missing":["missing.mkv","missing.mkv"],"orphaned":[]}`},
		{"referenced orphan", `{"success":true,"complete":true,"total_items":1,"missing":[],"orphaned":[{"name":"other-task.mkv","size":42}]}`},
		{"nested orphan", `{"success":true,"complete":true,"total_items":1,"missing":[],"orphaned":[{"name":"../private.mkv","size":42}]}`},
		{"duplicate orphan", `{"success":true,"complete":true,"total_items":2,"missing":[],"orphaned":[{"name":"a","size":42},{"name":"a","size":42}]}`},
		{"missing and orphan", `{"success":true,"complete":true,"total_items":1,"missing":["missing.mkv"],"orphaned":[{"name":"missing.mkv","size":42}]}`},
		{"missing size", `{"success":true,"complete":true,"total_items":1,"missing":[],"orphaned":[{"name":"a"}]}`},
		{"negative size", `{"success":true,"complete":true,"total_items":1,"missing":[],"orphaned":[{"name":"a","size":-1}]}`},
		{"too few items", `{"success":true,"complete":true,"total_items":0,"missing":[],"orphaned":[{"name":"a","size":0}]}`},
		{"claims expected file exists in empty directory", `{"success":true,"complete":true,"total_items":0,"missing":[],"orphaned":[]}`},
		{"trailing document", `{"success":true,"complete":true,"total_items":0,"missing":[],"orphaned":[]} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseRemoteDirectoryResult([]byte(tc.response), expected, references)
			if err == nil || result.Missing != nil || result.Orphaned != nil {
				t.Fatalf("accepted invalid result: %+v, %v", result, err)
			}
		})
	}
	result, err := parseRemoteDirectoryResult([]byte(`{"success":true,"complete":true,"total_items":0,"missing":[],"orphaned":[]}`), nil, nil)
	if err != nil || result.TotalItems != 0 || result.Missing == nil || result.Orphaned == nil {
		t.Fatalf("genuinely empty directory rejected: %+v, %v", result, err)
	}
}

func TestRemoteScanRejectsUnverifiableInventory(t *testing.T) {
	hash := strings.Repeat("a", 40)
	valid := fmt.Sprintf(`{"hash":%q,"save_path":"/data","content_path":"/data/saved.mkv"}`, hash)
	for _, tc := range []struct{ name, response string }{
		{"null inventory", `null`},
		{"embedded error", `{"error":"private"}`},
		{"missing path", fmt.Sprintf(`[{"hash":%q,"save_path":"/data"}]`, hash)},
		{"duplicate hash", `[` + valid + `,` + valid + `]`},
		{"path traversal", fmt.Sprintf(`[{"hash":%q,"save_path":"/data","content_path":"/data/../secret"}]`, hash)},
		{"malformed hash", `[{"hash":"bad","save_path":"/data","content_path":"/data/saved.mkv"}]`},
		{"extra downloader metadata", fmt.Sprintf(`[{"hash":%q,"save_path":"/data","content_path":"/data/saved.mkv","tracker":"secret"}]`, hash)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rows, err := parseRemoteScanReferences([]byte(tc.response)); err == nil || rows != nil {
				t.Fatalf("accepted invalid inventory: %+v, %v", rows, err)
			}
		})
	}
	if rows, err := parseRemoteScanReferences([]byte(`[]`)); err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("actual empty inventory rejected: %+v, %v", rows, err)
	}
}

func TestRemoteScanReportsBusyAndRejectsLegacyOrRedirectResponses(t *testing.T) {
	var leaks atomic.Int32
	redirected := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaks.Add(1) }))
	defer redirected.Close()
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusForbidden, http.StatusNotFound, http.StatusTemporaryRedirect, http.StatusUnprocessableEntity} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", redirected.URL)
				w.WriteHeader(status)
				fmt.Fprint(w, "private upstream message")
			}))
			defer server.Close()
			d := remoteScanFixture(t, server)
			_, err := d.ScanMediaDirectoryByProxy(context.Background(), RemoteDirectoryScanRequest{RemotePath: "/data"})
			if err == nil || strings.Contains(err.Error(), "private upstream") {
				t.Fatalf("invalid or leaked media failure: %v", err)
			}
			if status == http.StatusServiceUnavailable && !strings.Contains(err.Error(), "离线或繁忙") {
				t.Fatalf("unhelpful offline error: %v", err)
			}
			if status == http.StatusUnprocessableEntity && !strings.Contains(err.Error(), "目录不可访问或扫描未完成") {
				t.Fatalf("unhelpful unverified directory error: %v", err)
			}
			if _, err := d.ListRemoteScanReferences(context.Background()); err == nil || strings.Contains(err.Error(), "private upstream") {
				t.Fatalf("invalid or leaked inventory failure: %v", err)
			}
		})
	}
	if leaks.Load() > 0 {
		t.Fatal("remote scan followed redirect")
	}
}

func TestRemoteScanDoesNotDispatchUnboundedOrInvalidRequests(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	d := remoteScanFixture(t, server)
	for _, scan := range []RemoteDirectoryScanRequest{
		{RemotePath: "/"},
		{RemotePath: "/data/../tmp"},
		{RemotePath: "/data", Expected: []string{"a/b"}},
		{RemotePath: "/data", Expected: []string{"missing-reference"}},
		{RemotePath: "/data", Referenced: []string{"a", "a"}},
		{RemotePath: "/data", Referenced: oversizedRemoteScanNames()},
	} {
		if _, err := d.ScanMediaDirectoryByProxy(context.Background(), scan); err == nil {
			t.Fatalf("invalid scan was accepted: %.25s", scan.RemotePath)
		}
	}
	d.APIKey = ""
	if _, err := d.ListRemoteScanReferences(context.Background()); err == nil {
		t.Fatal("inventory accepted a missing bearer instead of refusing the request")
	}
	if hits.Load() != 0 {
		t.Fatal("invalid remote scan reached a network endpoint")
	}
}

func oversizedRemoteScanNames() []string {
	names := make([]string, 300)
	for i := range names {
		names[i] = fmt.Sprintf("%03d-%s", i, strings.Repeat("a", 250))
	}
	return names
}

func remoteScanFixture(t *testing.T, server *httptest.Server) Downloader {
	t.Helper()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return Downloader{
		Host: server.URL, Type: "qbittorrent", UseProxy: true, Enabled: true,
		APIKey: "qb-fixture", ProxyPort: number, ProxyToken: "media-fixture",
	}
}
