package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func scanHTTPRequest(t *testing.T, request fileScanRequest) *http.Request {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return scanRawHTTPRequest(http.MethodPost, fixtureToken, body)
}

func scanRawHTTPRequest(method, token string, body []byte) *http.Request {
	req := httptest.NewRequest(method, "/api/file/scan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestFileScanReturnsOnlyMissingAndOrphaned(t *testing.T) {
	policy, root := testPolicy(t)
	privateMedia := "media content must remain on disk"
	for _, name := range []string{"present.mkv", "hidden-task.dat", "orphan.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(privateMedia), 0600); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(root, "multi-file")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "nested.mkv"), []byte(privateMedia), 0600); err != nil {
		t.Fatal(err)
	}
	request := scanHTTPRequest(t, fileScanRequest{
		RemotePath: root,
		Expected:   []string{"present.mkv", "missing.mkv", "multi-file"},
		Referenced: []string{"present.mkv", "missing.mkv", "multi-file", "hidden-task.dat"},
	})
	response := httptest.NewRecorder()
	newMediaHandler(policy).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("scan status %d: %s", response.Code, response.Body)
	}
	var result fileScanResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := fileScanResult{
		Success: true, Complete: true, TotalItems: 4,
		Missing: []string{"missing.mkv"}, Orphaned: []fileScanEntry{{Name: "orphan.mkv", Size: int64(len(privateMedia))}},
	}
	if !reflect.DeepEqual(result, want) || strings.Contains(response.Body.String(), privateMedia) ||
		strings.Contains(response.Body.String(), "hidden-task.dat") || strings.Contains(response.Body.String(), "nested.mkv") {
		t.Fatalf("wrong or overbroad response: %s", response.Body)
	}
	if data, err := os.ReadFile(filepath.Join(root, "orphan.mkv")); err != nil || string(data) != privateMedia {
		t.Fatal("scan modified the orphaned file")
	}
}

func TestFileScanEmptyDirectoryAndUnverifiedPaths(t *testing.T) {
	policy, root := testPolicy(t)
	handler := newMediaHandler(policy)
	valid := fileScanRequest{RemotePath: root, Expected: []string{}, Referenced: []string{}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, valid))
	var empty fileScanResult
	if err := json.Unmarshal(response.Body.Bytes(), &empty); err != nil || response.Code != http.StatusOK ||
		!empty.Complete || empty.TotalItems != 0 || empty.Missing == nil || empty.Orphaned == nil {
		t.Fatalf("empty verified directory must return empty arrays: %d %s", response.Code, response.Body)
	}
	notDir := filepath.Join(root, "regular")
	if err := os.WriteFile(notDir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideRequest := fileScanRequest{RemotePath: outside, Expected: []string{}, Referenced: []string{}}
	badDirectory := valid
	badDirectory.RemotePath = filepath.Join(root, "missing")
	fileInstead := valid
	fileInstead.RemotePath = notDir
	unreferenced := valid
	unreferenced.Expected = []string{"other.mkv"}
	traversal := valid
	traversal.Referenced = []string{"../other.mkv"}
	mixed := valid
	mixed.Referenced = []string{`inside\other.mkv`}
	raw, err := json.Marshal(map[string]any{
		"remote_path": root, "expected": []string{}, "referenced": []string{}, "operation": "delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"unauthorized", scanRawHTTPRequest(http.MethodPost, "wrong", raw), http.StatusUnauthorized},
		{"delete denied", scanRawHTTPRequest(http.MethodDelete, fixtureToken, raw), http.StatusMethodNotAllowed},
		{"unknown field", scanRawHTTPRequest(http.MethodPost, fixtureToken, raw), http.StatusBadRequest},
		{"body limit", scanRawHTTPRequest(http.MethodPost, fixtureToken, bytes.Repeat([]byte(" "), maxMediaBody+1)), http.StatusRequestEntityTooLarge},
		{"whitelist", scanHTTPRequest(t, outsideRequest), http.StatusForbidden},
		{"directory unavailable", scanHTTPRequest(t, badDirectory), http.StatusUnprocessableEntity},
		{"directory type", scanHTTPRequest(t, fileInstead), http.StatusUnprocessableEntity},
		{"incomplete references", scanHTTPRequest(t, unreferenced), http.StatusBadRequest},
		{"relative traversal", scanHTTPRequest(t, traversal), http.StatusBadRequest},
		{"backslash separator", scanHTTPRequest(t, mixed), http.StatusBadRequest},
		{"canceled", scanHTTPRequest(t, valid).WithContext(canceled), http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, tc.req)
			if response.Code != tc.want || strings.Contains(response.Body.String(), `"complete":true`) {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.want, response.Body)
			}
		})
	}
}

func TestFileScanDoesNotFollowSymlinks(t *testing.T) {
	policy, root := testPolicy(t)
	outside := t.TempDir()
	external := filepath.Join(outside, "outside.mkv")
	if err := os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "link.mkv")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	handler := newMediaHandler(policy)
	valid := fileScanRequest{RemotePath: root, Expected: []string{}, Referenced: []string{}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, valid))
	var result fileScanResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK ||
		len(result.Orphaned) != 0 || strings.Contains(response.Body.String(), "link.mkv") {
		t.Fatalf("symlink should not appear as an orphan: %d %s", response.Code, response.Body)
	}
	valid.Expected = []string{"link.mkv"}
	valid.Referenced = []string{"link.mkv"}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, valid))
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), `"complete":true`) {
		t.Fatalf("a referenced symlink must be unverified: %d %s", response.Code, response.Body)
	}
	valid.Expected = []string{}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, valid))
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), `"complete":true`) {
		t.Fatalf("a symlink referenced by a filtered task must be unverified: %d %s", response.Code, response.Body)
	}
}

func TestFileScanDeniesWithinRootDirectoryAlias(t *testing.T) {
	policy, root := testPolicy(t)
	dir := filepath.Join(root, "canonical")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	handler := newMediaHandler(policy)
	request := fileScanRequest{RemotePath: alias, Expected: []string{}, Referenced: []string{}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, request))
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), `"complete":true`) {
		t.Fatalf("an alias to another allowed directory must be unverified: %d %s", response.Code, response.Body)
	}
	request.RemotePath = dir
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, scanHTTPRequest(t, request))
	if response.Code != http.StatusOK {
		t.Fatalf("canonical allowed directory was refused: %d %s", response.Code, response.Body)
	}
}

func TestFileScanDeniesReplacedMediaRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "trusted")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := loadMediaPolicy(fixtureToken, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, filepath.Join(parent, "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "must-not-expose.mkv"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	newMediaHandler(policy).ServeHTTP(response, scanHTTPRequest(t, fileScanRequest{
		RemotePath: root, Expected: []string{}, Referenced: []string{},
	}))
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "must-not-expose") {
		t.Fatalf("replacement at allowed path must not be scanned: %d %s", response.Code, response.Body)
	}
}
