package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	hubAgentFixture = "fixture-agent-token-only"
	hubLocalFixture = "fixture-controller-token-only"
)

func hubRequest(method, path, token, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}

func hubCall(handler http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func hubSubmit(t *testing.T, h *pullHub, result Result) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return hubCall(h.agentHandler, hubRequest(http.MethodPost, "/v1/result", hubAgentFixture, string(body)))
}

func hubEnqueue(t *testing.T, lane *hubLane, ctx context.Context) *hubTask {
	t.Helper()
	task, err := lane.enqueue(ctx, Job{Lane: "qb", Method: http.MethodGet, URI: "/api/v2/app/version"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func hubClaim(t *testing.T, lane *hubLane) Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	job, err := lane.claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return *job
}

func hubWaitTask(t *testing.T, task *hubTask) {
	t.Helper()
	select {
	case <-task.done:
	case <-time.After(time.Second):
		t.Fatal("task did not complete")
	}
}

func TestHubClaimsAreExclusiveAndResultsAreFenced(t *testing.T) {
	h := newPullHub(hubAgentFixture, hubLocalFixture)
	lane := h.lanes["qb"]
	first := hubEnqueue(t, lane, context.Background())
	second := hubEnqueue(t, lane, context.Background())
	claimed := make(chan *Job, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			job, _ := lane.claim(ctx)
			claimed <- job
		}()
	}
	close(start)
	job := <-claimed
	if job == nil || job.ID != first.job.ID || !validOpaqueID(job.Lease) {
		t.Fatalf("unexpected first claim: %#v", job)
	}
	select {
	case extra := <-claimed:
		t.Fatalf("concurrent claim escaped the running slot: %#v", extra)
	case <-time.After(20 * time.Millisecond):
	}
	wrongLease := strings.Repeat("a", 32)
	if job.Lease == wrongLease {
		wrongLease = strings.Repeat("b", 32)
	}
	good := Result{ID: job.ID, Lease: job.Lease, Status: http.StatusAccepted, Body: []byte("first result")}
	wrong := good
	wrong.Lease = wrongLease
	if got := hubSubmit(t, h, wrong).Code; got != http.StatusGone {
		t.Fatalf("wrong lease got %d, want 410", got)
	}
	select {
	case <-first.done:
		t.Fatal("wrong lease completed the caller")
	default:
	}
	if got := hubSubmit(t, h, good).Code; got != http.StatusNoContent {
		t.Fatalf("current lease got %d, want 204", got)
	}
	secondJob := <-claimed
	if secondJob == nil || secondJob.ID != second.job.ID || secondJob.Lease == job.Lease {
		t.Fatalf("unexpected second claim: %#v", secondJob)
	}
	replayed := good
	replayed.Body = []byte("different body on replay")
	if got := hubSubmit(t, h, replayed).Code; got != http.StatusNoContent {
		t.Fatalf("accepted lease replay got %d, want 204", got)
	}
	hubWaitTask(t, first)
	if first.err != nil || !bytes.Equal(first.result.Body, good.Body) {
		t.Fatalf("first completion changed on replay: %#v, %v", first.result, first.err)
	}
	if !lane.accept(Result{ID: secondJob.ID, Lease: secondJob.Lease, Status: http.StatusOK}) {
		t.Fatal("second lease lost after idempotent replay")
	}
}

func TestHubCallerCancellationNeverRequeuesClaimedWork(t *testing.T) {
	lane := newHubLane(time.Second, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := hubEnqueue(t, lane, ctx)
	job := hubClaim(t, lane)
	cancel()
	lane.cancel(first)
	hubWaitTask(t, first)
	if !errors.Is(first.err, context.Canceled) {
		t.Fatalf("canceled task error = %v", first.err)
	}
	second := hubEnqueue(t, lane, context.Background())
	shortPoll, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if next, err := lane.claim(shortPoll); next != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel released slot while agent could still be running: %#v, %v", next, err)
	}
	stale := Result{ID: job.ID, Lease: job.Lease, Status: http.StatusOK}
	if lane.accept(stale) {
		t.Fatal("canceled lease accepted")
	}
	claimed := hubClaim(t, lane)
	if claimed.ID != second.job.ID || claimed.ID == job.ID || lane.accept(stale) {
		t.Fatalf("canceled lease escaped fencing: %#v", claimed)
	}
	if !lane.accept(Result{ID: claimed.ID, Lease: claimed.Lease, Status: http.StatusOK}) {
		t.Fatal("next task did not complete")
	}
}

func TestHubBoundsQueueAndExpiresUnclaimedAndClaimedWork(t *testing.T) {
	t.Run("queue capacity and cancellation", func(t *testing.T) {
		lane := newHubLane(time.Second, time.Second)
		callers, tasks := make([]context.CancelFunc, 0, hubQueueSize), make([]*hubTask, 0, hubQueueSize)
		for range hubQueueSize {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			callers = append(callers, cancel)
			tasks = append(tasks, hubEnqueue(t, lane, ctx))
		}
		if _, err := lane.enqueue(context.Background(), Job{}); !errors.Is(err, errHubQueueFull) {
			t.Fatalf("queue did not reject overflow: %v", err)
		}
		for i := range callers {
			callers[i]()
			lane.cancel(tasks[i])
		}
		next := hubEnqueue(t, lane, context.Background())
		if job := hubClaim(t, lane); job.ID != next.job.ID {
			t.Fatalf("canceled queued task was claimed: %#v", job)
		} else if !lane.accept(Result{ID: job.ID, Lease: job.Lease, Status: http.StatusOK}) {
			t.Fatal("next task did not complete")
		}
	})
	t.Run("no agent responds", func(t *testing.T) {
		h := newPullHub(hubAgentFixture, hubLocalFixture)
		h.lanes["qb"] = newHubLane(20*time.Millisecond, time.Second)
		response := hubCall(h.localHandler("qb").ServeHTTP, hubRequest(http.MethodGet, "/api/v2/app/version", hubLocalFixture, ""))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unclaimed status=%d, want 503", response.Code)
		}
	})
	t.Run("agent disconnects after claim", func(t *testing.T) {
		lane := newHubLane(time.Second, 30*time.Millisecond)
		task := hubEnqueue(t, lane, context.Background())
		job := hubClaim(t, lane)
		hubWaitTask(t, task)
		if !errors.Is(task.err, errHubLeaseExpiry) {
			t.Fatalf("lease expiration error = %v", task.err)
		}
		if lane.accept(Result{ID: job.ID, Lease: job.Lease, Status: http.StatusOK}) {
			t.Fatal("expired lease accepted")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if next, err := lane.claim(ctx); next != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired work was requeued: %#v, %v", next, err)
		}
	})
}

func TestHubPrivateHTTPRoundTripDoesNotForwardCredentialsOrRedirects(t *testing.T) {
	h := newPullHub(hubAgentFixture, hubLocalFixture)
	tests := []struct {
		name           string
		upstreamStatus int
		wantStatus     int
		wantBody       string
	}{
		{"body and status", http.StatusUnprocessableEntity, http.StatusUnprocessableEntity, "agent body"},
		{"redirect denied", http.StatusTemporaryRedirect, http.StatusBadGateway, "agent upstream returned a redirect\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			const body = `{"mode":"preview","filePath":"/keep/fixture"}`
			r := hubRequest(http.MethodPost, "/api/media/screenshot", hubLocalFixture, body).WithContext(ctx)
			r.Header.Set("Cookie", "cookie-fixture=private")
			r.Header.Set("Proxy-Authorization", "Bearer proxy-auth-fixture")
			completed := make(chan *httptest.ResponseRecorder, 1)
			go func() { completed <- hubCall(h.localHandler("media").ServeHTTP, r) }()
			claim := hubRequest(http.MethodPost, "/v1/claim", hubAgentFixture, `{"lane":"media"}`).WithContext(ctx)
			w := hubCall(h.agentHandler, claim)
			if w.Code != http.StatusOK {
				t.Fatalf("claim returned %d: %s", w.Code, w.Body)
			}
			for _, credential := range []string{hubLocalFixture, hubAgentFixture, "cookie-fixture", "proxy-auth-fixture"} {
				if strings.Contains(w.Body.String(), credential) {
					t.Fatal("claim included a credential")
				}
			}
			var job Job
			if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
			if job.Lane != "media" || job.Method != r.Method || job.URI != r.RequestURI ||
				job.ContentType != "application/json" || string(job.Body) != body {
				t.Fatalf("request changed in transit: %#v", job)
			}
			result := Result{ID: job.ID, Lease: job.Lease, Status: tt.upstreamStatus, ContentType: "application/x-fixture", Body: []byte("agent body")}
			if got := hubSubmit(t, h, result).Code; got != http.StatusNoContent {
				t.Fatalf("result got %d, want 204", got)
			}
			select {
			case local := <-completed:
				if local.Code != tt.wantStatus || local.Body.String() != tt.wantBody {
					t.Fatalf("local response = %d %q", local.Code, local.Body)
				}
				if tt.wantStatus == tt.upstreamStatus && local.Header().Get("Content-Type") != result.ContentType {
					t.Fatalf("local content-type = %q", local.Header().Get("Content-Type"))
				}
				if local.Header().Get("Location") != "" || local.Header().Get("Set-Cookie") != "" {
					t.Fatal("local response leaked headers")
				}
			case <-ctx.Done():
				t.Fatal("private HTTP caller did not receive its result")
			}
		})
	}
}

func TestHubRoutesCredentialsAndLimits(t *testing.T) {
	h := newPullHub(hubAgentFixture, hubLocalFixture)
	check := func(name string, handler http.HandlerFunc, r *http.Request, want int) {
		t.Run(name, func(t *testing.T) {
			if got := hubCall(handler, r).Code; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
		})
	}
	media := h.localHandler("media").ServeHTTP
	qb := h.localHandler("qb").ServeHTTP
	check("local token required", qb, hubRequest(http.MethodGet, "/api/v2/app/version", "", ""), http.StatusUnauthorized)
	check("agent token cannot call private API", qb, hubRequest(http.MethodGet, "/api/v2/app/version", hubAgentFixture, ""), http.StatusUnauthorized)
	check("local token cannot claim", h.agentHandler, hubRequest(http.MethodPost, "/v1/claim", hubLocalFixture, `{"lane":"qb"}`), http.StatusUnauthorized)
	check("qB write denied", qb, hubRequest(http.MethodPost, "/api/v2/torrents/delete", hubLocalFixture, ""), http.StatusForbidden)
	check("unknown qB write denied", qb, hubRequest(http.MethodPost, "/api/v2/torrents/setCategory", hubLocalFixture, ""), http.StatusForbidden)
	check("qB alias denied", qb, hubRequest(http.MethodGet, "/api/v2/app/%76ersion", hubLocalFixture, ""), http.StatusForbidden)
	check("cross lane denied", media, hubRequest(http.MethodGet, "/api/v2/app/version", hubLocalFixture, ""), http.StatusForbidden)
	check("HTTPS denies private API", h.agentHandler, hubRequest(http.MethodGet, "/api/v2/app/version", hubAgentFixture, ""), http.StatusNotFound)
	check("HTTPS denies query variant", h.agentHandler, hubRequest(http.MethodPost, "/v1/claim?lane=qb", hubAgentFixture, ""), http.StatusNotFound)
	check("HTTPS denies non-POST", h.agentHandler, hubRequest(http.MethodGet, "/v1/claim", hubAgentFixture, ""), http.StatusMethodNotAllowed)
	check("invalid lane", h.agentHandler, hubRequest(http.MethodPost, "/v1/claim", hubAgentFixture, `{"lane":"other"}`), http.StatusBadRequest)
	check("oversized app request", media, hubRequest(http.MethodPost, "/api/file/check", hubLocalFixture, strings.Repeat("x", maxRequestBytes+1)), http.StatusRequestEntityTooLarge)
	check("oversized claim", h.agentHandler, hubRequest(http.MethodPost, "/v1/claim", hubAgentFixture, `{"lane":"`+strings.Repeat("a", 1024)+`"}`), http.StatusRequestEntityTooLarge)
	check("second JSON document", h.agentHandler, hubRequest(http.MethodPost, "/v1/claim", hubAgentFixture, `{"lane":"qb"} {}`), http.StatusBadRequest)
	check("extra result field", h.agentHandler, hubRequest(http.MethodPost, "/v1/result", hubAgentFixture, `{"set-cookie":"private"}`), http.StatusBadRequest)
	oversizedResult := hubRequest(http.MethodPost, "/v1/result", hubAgentFixture, "")
	oversizedResult.ContentLength = hubResultLimit + 1
	check("oversized result", h.agentHandler, oversizedResult, http.StatusRequestEntityTooLarge)
}

func TestHubAndAgentUploadLargeQBTorrentOnlyOnce(t *testing.T) {
	var localCalls atomic.Int32
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("torrents", "fixture.torrent")
	if err != nil {
		t.Fatal(err)
	}
	comment := strings.Repeat("x", maxRequestBytes+1)
	const info = "d6:lengthi1e4:name7:fixture12:piece lengthi16384e6:pieces20:abcdefghijklmnopqrst7:privatei1ee"
	torrent := "d7:comment" + strconv.Itoa(len(comment)) + ":" + comment + "4:info" + info + "e"
	if _, err := io.WriteString(part, torrent); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	requestBody := upload.Bytes()
	if int64(len(requestBody)) <= maxRequestBytes {
		t.Fatalf("fixture body is only %d bytes", len(requestBody))
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localCalls.Add(1)
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read local upload: %v", err)
		}
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/v2/torrents/add" ||
			r.Header.Get("Authorization") != "Bearer qb-local-fixture-token" ||
			r.Header.Get("Content-Type") != form.FormDataContentType() || !bytes.Equal(got, requestBody) {
			t.Errorf("local upload changed in transit: method=%s uri=%s content-type=%q bytes=%d", r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), len(got))
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Ok.")
	}))
	defer local.Close()

	h := newPullHub(hubAgentFixture, hubLocalFixture)
	coordinator := httptest.NewTLSServer(http.HandlerFunc(h.agentHandler))
	defer coordinator.Close()
	localHub := httptest.NewServer(h.localHandler("qb"))
	defer localHub.Close()

	rootTransport := coordinator.Client().Transport.(*http.Transport)
	coordinatorClient := newAgentClient(&tls.Config{RootCAs: rootTransport.TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12}, false)
	defer coordinatorClient.CloseIdleConnections()
	drop := &dropFirstResultResponse{inner: coordinatorClient.Transport}
	coordinatorClient.Transport = drop
	agent := &pullAgent{
		origin: coordinator.URL, token: hubAgentFixture, coordinator: coordinatorClient,
		local: newAgentClient(nil, true),
	}
	defer agent.local.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodPost, localHub.URL+"/api/v2/torrents/add", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+hubLocalFixture)
	request.Header.Set("Content-Type", form.FormDataContentType())
	callerResult := make(chan *http.Response, 1)
	callerErr := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			callerErr <- err
			return
		}
		callerResult <- response
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	job, err := agent.claim(ctx, "qb", []byte(`{"lane":"qb"}`))
	if err != nil || job == nil {
		t.Fatalf("claim large upload: job=%#v err=%v", job, err)
	}
	if int64(len(job.Body)) != int64(len(requestBody)) {
		t.Fatalf("claimed upload body length=%d, want %d", len(job.Body), len(requestBody))
	}
	agent.finishJob(context.Background(), agentTarget{origin: local.URL, token: "qb-local-fixture-token"}, *job)

	select {
	case err := <-callerErr:
		t.Fatalf("local caller failed: %v", err)
	case response := <-callerResult:
		defer response.Body.Close()
		result, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK || string(result) != "Ok." {
			t.Fatalf("caller got HTTP %d %q", response.StatusCode, result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local caller did not receive the upload result")
	}
	if got := drop.attempts.Load(); got != 2 {
		t.Fatalf("result delivery attempted %d times, want 2", got)
	}
	if got := localCalls.Load(); got != 1 {
		t.Fatalf("qB add executed %d times after ambiguous result delivery, want exactly once", got)
	}
}

type dropFirstResultResponse struct {
	inner    http.RoundTripper
	attempts atomic.Int32
}

func (d *dropFirstResultResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := d.inner.RoundTrip(r)
	if err == nil && r.URL.Path == "/v1/result" && d.attempts.Add(1) == 1 {
		response.Body.Close()
		return nil, errors.New("simulated ambiguous result delivery")
	}
	return response, err
}
