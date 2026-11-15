package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureToken = "offline-fixture-token-is-at-least-32-characters"

func testPolicy(t *testing.T) (*mediaPolicy, string) {
	t.Helper()
	root := t.TempDir()
	raw, _ := json.Marshal([]string{root})
	p, err := loadMediaPolicy(fixtureToken, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p, root
}

func TestMediaPolicyFailsClosed(t *testing.T) {
	for _, input := range []struct{ token, roots string }{{"", `["/tmp"]`}, {"short", `["/tmp"]`}, {fixtureToken, ``}, {fixtureToken, `[]`}, {fixtureToken, `["relative"]`}, {fixtureToken, `["/"]`}, {fixtureToken, `["/nonexistent-root-ptmove"]`}} {
		if _, err := loadMediaPolicy(input.token, input.roots); err == nil {
			t.Fatalf("accepted invalid policy roots=%q", input.roots)
		}
	}
}

func TestMediaPathBoundaries(t *testing.T) {
	policy, root := testPolicy(t)
	allowed := filepath.Join(root, "missing", "video.mkv")
	if _, err := policy.resolve(allowed); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"video.mkv", "https://example.invalid/video.mkv", root + "-other/video.mkv", root + string(filepath.Separator) + "../video.mkv", "//server/share/video.mkv"} {
		if _, err := policy.resolve(path); err == nil {
			t.Fatalf("accepted outside path %q", path)
		}
	}
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlink unavailable: %v", err)
		return
	}
	if _, err := policy.resolve(filepath.Join(link, "video.mkv")); err == nil {
		t.Fatal("accepted symlink escape")
	}
}

func TestMediaHTTPAuthAndBatchPaths(t *testing.T) {
	policy, root := testPolicy(t)
	handler := newMediaHandler(policy)
	run := func(path, token, body string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out.Code
	}
	body, _ := json.Marshal(map[string]string{"remote_path": filepath.Join(root, "missing.mkv")})
	for _, token := range []string{"", "wrong"} {
		if got := run("/api/file/check", token, string(body)); got != 401 {
			t.Fatalf("auth status=%d", got)
		}
	}
	if got := run("/api/file/check", fixtureToken, string(body)); got != 200 {
		t.Fatalf("file check status=%d", got)
	}
	bad, _ := json.Marshal(map[string]any{"remote_paths": []string{filepath.Join(root, "ok"), filepath.Join(t.TempDir(), "bad")}})
	if got := run("/api/file/batch-check", fixtureToken, string(bad)); got != 403 {
		t.Fatalf("batch escape status=%d", got)
	}
	if got := run("/api/media/screenshot", fixtureToken, strings.Repeat("x", maxMediaBody+1)); got != 413 {
		t.Fatalf("body status=%d", got)
	}
	if got := run("/api/torrents/all", "", `{}`); got != 401 {
		t.Fatal("qB endpoint bypassed authentication")
	}
	mixed, _ := json.Marshal(map[string]any{"remote_path": root, "remote_paths": []string{filepath.Join(t.TempDir(), "outside")}})
	if got := run("/api/file/batch-check", fixtureToken, string(mixed)); got != 400 {
		t.Fatalf("mixed fields bypassed path checks status=%d", got)
	}
	for _, fields := range []map[string]any{
		{"remote_path": root, "REMOTE_PATH": filepath.Join(t.TempDir(), "outside")},
		{"remote_paths": []string{root}, "REMOTE_PATH": filepath.Join(t.TempDir(), "outside")},
		{"remote_path": root, "Remote_Path": filepath.Join(t.TempDir(), "outside")},
		{"remote_path": root, "unrecognized_field": "value"},
	} {
		body, _ := json.Marshal(fields)
		if got := run("/api/media/screenshot", fixtureToken, string(body)); got != 400 {
			t.Fatalf("field alias bypass status=%d fields=%v", got, fields)
		}
	}
	batchAlias, _ := json.Marshal(map[string]any{"remote_paths": []string{root}, "Remote_Paths": []string{filepath.Join(t.TempDir(), "outside")}})
	if got := run("/api/file/batch-check", fixtureToken, string(batchAlias)); got != 400 {
		t.Fatalf("batch alias bypass status=%d", got)
	}
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest("GET", "/api/health", nil))
	if health.Code != 200 || strings.Contains(health.Body.String(), fixtureToken) {
		t.Fatal("invalid health")
	}
}

func TestMediaGateRejectsConcurrentWork(t *testing.T) {
	policy, root := testPolicy(t)
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	next := policy.protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("executed while busy") }), gate)
	raw, _ := json.Marshal(map[string]string{"remote_path": root})
	req := httptest.NewRequest("POST", "/api/file/check", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+fixtureToken)
	out := httptest.NewRecorder()
	next.ServeHTTP(out, req)
	if out.Code != 429 {
		t.Fatalf("status %d", out.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockPixhostClient(t *testing.T, posts *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" {
			t.Fatal("non-HTTPS upload")
		}
		body := ""
		if req.Method == "POST" {
			*posts++
			if req.URL.String() != "https://api.pixhost.to/images" {
				t.Fatal("unexpected upload target")
			}
			if err := req.ParseMultipartForm(12 << 20); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			if req.FormValue("content_type") != "0" || req.FormValue("max_th_size") != "500" {
				t.Fatal("incorrect Pixhost fields")
			}
			file, _, err := req.FormFile("img")
			if err != nil {
				t.Fatal(err)
			}
			file.Close()
			body = `{"show_url":"https://pixhost.to/show/42/image.png","th_url":"https://t99.pixhost.to/thumbs/42/image.png"}`
		} else {
			if req.URL.String() != "https://pixhost.to/show/42/image.png" {
				t.Fatal("unexpected page fetch")
			}
			body = `<img id="image" src="https://img99.pixhost.to/images/42/original.png">`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
}

func TestPixhostUsesOfficialAPIAndActualFullImage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("fixture-image"), 0600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	image, err := uploadPixhostImage(file, mockPixhostClient(t, &posts))
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || image.ImageURL != "https://img99.pixhost.to/images/42/original.png" || strings.Contains(image.bbcode(), "thumbs/") {
		t.Fatalf("lost full-resolution image: %+v", image)
	}
	if !strings.Contains(image.bbcode(), image.ShowURL) {
		t.Fatal("lost show URL")
	}
}

func TestPixhostRejectsOversizedAndHostileResponses(t *testing.T) {
	file := filepath.Join(t.TempDir(), "large.png")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(10*1024*1024 + 1)
	f.Close()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"show_url":"https://evil.invalid/x","th_url":"https://t1.pixhost.to/thumbs/x"}`))}, nil
	})}
	if _, err := uploadPixhostImage(file, client); err == nil || calls != 0 {
		t.Fatal("oversize upload was sent")
	}
	_ = os.WriteFile(file, []byte("small"), 0600)
	if _, err := uploadPixhostImage(file, client); err == nil || calls != 1 {
		t.Fatal("accepted hostile response or retried upload")
	}
}

func TestCaptureSelectionSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	small := filepath.Join(root, "Show.S01E02.mkv")
	large := filepath.Join(root, "Show.S01E03.mkv")
	_ = os.WriteFile(small, []byte("small"), 0600)
	_ = os.WriteFile(large, []byte(strings.Repeat("x", 500)), 0600)
	selected, err := findTargetVideoFile(root, "Show.S01E02")
	if err != nil || selected != small {
		t.Fatalf("selected %q: %v", selected, err)
	}
	if err := os.Symlink(large, filepath.Join(root, "Show.S01E01.mkv")); err == nil {
		selected, err = findTargetVideoFile(root, "Show.S01E01")
		if err != nil || strings.HasSuffix(selected, "S01E01.mkv") {
			t.Fatal("selected symlink")
		}
	}
	if times := sanitizeSelectedScreenshotTimes([]float64{-1, 0, 2, 2.1, 4, 6, 8, 10, 12, 14, 999}, 20); len(times) != 5 {
		t.Fatalf("capture limit=%d", len(times))
	}
}
