package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const scanTestRequest = `{"remote_path":"/keep", "expected":["missing.mkv"], "referenced":["missing.mkv","active.mkv"]}`

func TestFileScanUsesOnlyReadOnlyMediaLane(t *testing.T) {
	valid := Job{Lane: "media", Method: http.MethodPost, URI: "/api/file/scan", ContentType: "application/json", Body: []byte(scanTestRequest)}
	if err := validateJob(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Job){
		func(job *Job) { job.Lane = "qb" },
		func(job *Job) { job.Method = http.MethodGet },
		func(job *Job) { job.Method = http.MethodDelete },
		func(job *Job) { job.URI += "?directory=/keep" },
		func(job *Job) { job.URI = "/api/file/delete" },
		func(job *Job) { job.Body = []byte("{invalid") },
		func(job *Job) { job.ContentType = "text/plain" },
	} {
		job := valid
		change(&job)
		if err := validateJob(job); err == nil {
			t.Errorf("unexpected scan operation accepted: %s %s %s", job.Lane, job.Method, job.URI)
		}
	}
}

func TestFileScanRoundTripUsesHTTPSPull(t *testing.T) {
	const (
		mediaToken = "offline-media-scan-fixture"
		result     = `{"success":true,"complete":true,"total_items":2,"missing":["missing.mkv"],"orphaned":[{"name":"orphan.mkv","size":7}]}`
	)
	job := Job{Lane: "media", Method: http.MethodPost, URI: "/api/file/scan", Body: []byte(scanTestRequest)}
	response := runScanPullJob(t, job, mediaToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := io.ReadAll(r.Body)
		if err != nil || string(request) != scanTestRequest || r.URL.RequestURI() != "/api/file/scan" ||
			r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+mediaToken {
			t.Error("media received an unexpected or unauthorized scan request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, result)
	}))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != result {
		t.Fatalf("scan metadata did not return through HTTPS pull: %d %s", response.Code, response.Body)
	}
}

func TestLocalScanReferencesUseAnExactReadOnlyRoute(t *testing.T) {
	valid := Job{Lane: "qb", Method: http.MethodGet, URI: localScanReferencesURI}
	if err := validateJob(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Job){
		func(job *Job) { job.Lane = "media" },
		func(job *Job) { job.Method = http.MethodPost },
		func(job *Job) { job.Method = http.MethodDelete },
		func(job *Job) { job.URI += "?" },
		func(job *Job) { job.URI += "?filter=completed" },
		func(job *Job) { job.Body = []byte("{}") },
	} {
		job := valid
		change(&job)
		if err := validateJob(job); err == nil {
			t.Errorf("unexpected references operation accepted: %s %s %s", job.Lane, job.Method, job.URI)
		}
	}
}

func TestLocalScanReferencesAreProjectedOverHTTPS(t *testing.T) {
	const (
		authToken = "offline-qb-scan-fixture"
		raw       = `[{"hash":"abc","save_path":"/keep","content_path":"/keep/video.mkv","tracker":"https://tracker.invalid/?passkey=fixture-secret","password":"fixture-password"}]`
		projected = `[{"hash":"abc","save_path":"/keep","content_path":"/keep/video.mkv"}]`
	)
	for _, tc := range []struct {
		name       string
		uri        string
		body       string
		qbStatus   int
		wantStatus int
		wantBody   string
	}{
		{"reference projection", localScanReferencesURI, raw, 200, 200, projected},
		{"ordinary qB response unchanged", "/api/v2/torrents/info?filter=all", raw, 200, 200, raw},
		{"failure body stays local", localScanReferencesURI, raw, 401, 401, "local qB reference list unavailable\n"},
		{"malformed response", localScanReferencesURI, "null", 200, 502, "invalid local qB reference list\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := Job{Lane: "qb", Method: http.MethodGet, URI: tc.uri}
			response := runScanPullJob(t, job, authToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, err := io.ReadAll(r.Body)
				if err != nil || len(request) != 0 || r.URL.RequestURI() != "/api/v2/torrents/info?filter=all" ||
					r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+authToken {
					t.Error("unexpected or unauthorized qB reference request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.qbStatus)
				_, _ = io.WriteString(w, tc.body)
			}))
			if response.Code != tc.wantStatus || response.Body.String() != tc.wantBody {
				t.Fatalf("status %d, body %q", response.Code, response.Body.String())
			}
			if tc.uri == localScanReferencesURI && (response.Code == 200 && response.Header().Get("Content-Type") != "application/json" ||
				strings.Contains(response.Body.String(), "fixture-secret") || strings.Contains(response.Body.String(), "fixture-password")) {
				t.Fatal("qB reference projection leaked private extra fields")
			}
		})
	}
}

func TestProjectLocalScanReferencesRequiresAnArrayWithCompleteRows(t *testing.T) {
	for _, input := range []string{"", "null", "{}", "[null]", "[{}]", `[{"hash":"abc","save_path":"/keep"}]`, "[] {}"} {
		if _, err := projectLocalScanReferences([]byte(input)); err == nil {
			t.Errorf("accepted malformed qB references %q", input)
		}
	}
	if result, err := projectLocalScanReferences([]byte("[]")); err != nil || string(result) != "[]" {
		t.Fatalf("complete empty qB inventory: %q %v", result, err)
	}
}

func runScanPullJob(t *testing.T, job Job, targetToken string, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	hub := newPullHub(hubAgentFixture, hubLocalFixture)
	coordinator := httptest.NewTLSServer(http.HandlerFunc(hub.agentHandler))
	defer coordinator.Close()
	target := httptest.NewServer(handler)
	defer target.Close()
	roots := x509.NewCertPool()
	roots.AddCert(coordinator.Certificate())
	a := pullAgent{
		origin: coordinator.URL, token: hubAgentFixture,
		coordinator: newAgentClient(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, false),
		local:       newAgentClient(nil, true),
	}
	defer a.coordinator.CloseIdleConnections()
	defer a.local.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAgentLane(ctx, job.Lane, agentTarget{origin: target.URL, token: targetToken}, func() *pullAgent { return &a })
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("agent did not stop after cancellation")
		}
	}()
	return hubCall(hub.localHandler(job.Lane).ServeHTTP,
		hubRequest(job.Method, job.URI, hubLocalFixture, string(job.Body)).WithContext(ctx))
}
