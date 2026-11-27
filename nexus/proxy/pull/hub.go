package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"
)

const (
	hubQueueSize    = 4
	hubQueueWait    = 6 * time.Second
	hubResultGrace  = 8 * time.Second
	hubReceiptCount = 128
	hubResultLimit  = maxResponseBytes*4/3 + 8192
)

var (
	errHubQueueFull   = errors.New("agent queue is full")
	errHubQueueExpiry = errors.New("agent did not claim the request in time")
	errHubLeaseExpiry = errors.New("agent did not return the result in time")
)

type hubTask struct {
	job    Job
	caller context.Context
	due    time.Time
	timer  *time.Timer
	done   chan struct{}
	result Result
	err    error
}

type hubReceipt struct {
	id    string
	lease string
}

type hubLane struct {
	mu       sync.Mutex
	changed  chan struct{}
	queue    []*hubTask
	active   *hubTask
	receipts [hubReceiptCount]hubReceipt
	next     int

	queueWait time.Duration
	runWait   time.Duration
}

func newHubLane(queueWait, runWait time.Duration) *hubLane {
	return &hubLane{changed: make(chan struct{}), queueWait: queueWait, runWait: runWait}
}

func (l *hubLane) notifyLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *hubLane) enqueue(caller context.Context, job Job) (*hubTask, error) {
	id, err := randomOpaqueID()
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := caller.Err(); err != nil {
		return nil, err
	}
	if len(l.queue) >= hubQueueSize {
		return nil, errHubQueueFull
	}
	job.ID = id
	task := &hubTask{job: job, caller: caller, due: time.Now().Add(l.queueWait), done: make(chan struct{})}
	l.queue = append(l.queue, task)
	task.timer = time.AfterFunc(l.queueWait, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.removeQueuedLocked(task) {
			l.completeLocked(task, Result{}, errHubQueueExpiry)
		}
	})
	l.notifyLocked()
	return task, nil
}

func (l *hubLane) removeQueuedLocked(task *hubTask) bool {
	index := slices.Index(l.queue, task)
	if index < 0 {
		return false
	}
	l.queue = slices.Delete(l.queue, index, index+1)
	task.timer.Stop()
	return true
}

func (l *hubLane) completeLocked(task *hubTask, result Result, err error) {
	select {
	case <-task.done:
		return
	default:
		task.result, task.err = result, err
		close(task.done)
	}
}

func (l *hubLane) cancel(task *hubTask) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.removeQueuedLocked(task)
	// A claimed task may still be running on the agent after its HTTP caller leaves.
	// Preserve its slot until the agent reports completion or the lease expires.
	l.completeLocked(task, Result{}, task.caller.Err())
}

func (l *hubLane) releaseLocked(task *hubTask) {
	task.timer.Stop()
	l.active = nil
	l.notifyLocked()
}

func (l *hubLane) expireActiveLocked() {
	task := l.active
	if task != nil && !time.Now().Before(task.due) {
		l.completeLocked(task, Result{}, errHubLeaseExpiry)
		l.releaseLocked(task)
	}
}

func (l *hubLane) claim(ctx context.Context) (*Job, error) {
	for {
		l.mu.Lock()
		if err := ctx.Err(); err != nil {
			l.mu.Unlock()
			return nil, err
		}
		l.expireActiveLocked()
		for l.active == nil && len(l.queue) != 0 {
			task := l.queue[0]
			if err := task.caller.Err(); err != nil {
				l.removeQueuedLocked(task)
				l.completeLocked(task, Result{}, err)
				continue
			}
			if !time.Now().Before(task.due) {
				l.removeQueuedLocked(task)
				l.completeLocked(task, Result{}, errHubQueueExpiry)
				continue
			}
			lease, err := randomOpaqueID()
			if err != nil {
				l.mu.Unlock()
				return nil, err
			}
			l.removeQueuedLocked(task)
			task.job.Lease = lease
			task.due = time.Now().Add(l.runWait)
			l.active = task
			task.timer = time.AfterFunc(l.runWait, func() {
				l.mu.Lock()
				defer l.mu.Unlock()
				if l.active == task {
					l.expireActiveLocked()
				}
			})
			job := task.job
			l.mu.Unlock()
			return &job, nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (l *hubLane) accept(result Result) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expireActiveLocked()
	if task := l.active; task != nil && task.job.ID == result.ID && task.job.Lease == result.Lease {
		defer l.releaseLocked(task)
		if err := task.caller.Err(); err != nil {
			l.completeLocked(task, Result{}, err)
			return false
		}
		l.completeLocked(task, result, nil)
		l.receipts[l.next] = hubReceipt{id: result.ID, lease: result.Lease}
		l.next = (l.next + 1) % len(l.receipts)
		return true
	}
	return slices.Contains(l.receipts[:], hubReceipt{id: result.ID, lease: result.Lease})
}

type pullHub struct {
	token      string
	localToken string
	lanes      map[string]*hubLane
}

func newPullHub(token, localToken string) *pullHub {
	lanes := make(map[string]*hubLane, 2)
	for _, name := range []string{"qb", "media"} {
		lanes[name] = newHubLane(hubQueueWait, requestLimit(name)+hubResultGrace)
	}
	return &pullHub{token: token, localToken: localToken, lanes: lanes}
}

func (h *pullHub) localHandler(name string) http.Handler {
	lane := h.lanes[name]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, h.localToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jobRequestLimit(name, r.Method, r.RequestURI)))
		if err != nil {
			writeHubBodyError(w, err)
			return
		}
		job := Job{Lane: name, Method: r.Method, URI: r.RequestURI, ContentType: r.Header.Get("Content-Type"), Body: body}
		if validateJob(job) != nil {
			http.Error(w, "request is outside the agent allowlist", http.StatusForbidden)
			return
		}
		task, err := lane.enqueue(r.Context(), job)
		if err != nil {
			writeHubTaskError(w, err)
			return
		}
		select {
		case <-r.Context().Done():
			lane.cancel(task)
			return
		case <-task.done:
		}
		if task.err != nil {
			writeHubTaskError(w, task.err)
			return
		}
		result := task.result
		if result.Status >= 300 && result.Status < 400 {
			http.Error(w, "agent upstream returned a redirect", http.StatusBadGateway)
			return
		}
		w.Header()["Content-Type"] = nil // Suppress automatic sniffing if the upstream omitted this header.
		if result.ContentType != "" {
			w.Header().Set("Content-Type", result.ContentType)
		}
		w.WriteHeader(result.Status)
		if len(result.Body) != 0 {
			_, _ = w.Write(result.Body)
		}
	})
}

func writeHubTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return
	case errors.Is(err, errHubQueueFull), errors.Is(err, errHubQueueExpiry):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	case errors.Is(err, errHubLeaseExpiry):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	default:
		http.Error(w, "agent queue unavailable", http.StatusInternalServerError)
	}
}

func writeHubBodyError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "request body exceeds the limit", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "invalid request body", http.StatusBadRequest)
}

func (h *pullHub) agentHandler(w http.ResponseWriter, r *http.Request) {
	switch r.RequestURI {
	case "/v1/claim", "/v1/result":
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r, h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.RequestURI == "/v1/claim" {
		h.handleClaim(w, r)
		return
	}
	h.handleResult(w, r)
}

func (h *pullHub) handleClaim(w http.ResponseWriter, r *http.Request) {
	var claim Claim
	if err := decodeBounded(w, r, 1024, &claim); err != nil {
		writeHubBodyError(w, err)
		return
	}
	lane, ok := h.lanes[claim.Lane]
	if !ok {
		http.Error(w, "unknown lane", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), claimWait)
	defer cancel()
	job, err := lane.claim(ctx)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "could not claim agent task", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// A failed response write has ambiguous delivery, so the task stays claimed.
	_ = json.NewEncoder(w).Encode(job)
}

func (h *pullHub) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > hubResultLimit {
		http.Error(w, "request body exceeds the limit", http.StatusRequestEntityTooLarge)
		return
	}
	var result Result
	if err := decodeBounded(w, r, hubResultLimit, &result); err != nil {
		writeHubBodyError(w, err)
		return
	}
	if len(result.Body) > maxResponseBytes {
		http.Error(w, "result body exceeds the limit", http.StatusRequestEntityTooLarge)
		return
	}
	if !validOpaqueID(result.ID) || !validOpaqueID(result.Lease) || !validContentType(result.ContentType) ||
		result.Status < 200 || result.Status > 599 || (result.Status == http.StatusNoContent && len(result.Body) != 0) {
		http.Error(w, "invalid result", http.StatusBadRequest)
		return
	}
	for _, lane := range h.lanes {
		if lane.accept(result) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "lease is no longer active", http.StatusGone)
}

func runHub() error {
	token, err := secret("PTMOVE_PULL_TOKEN")
	if err != nil {
		return err
	}
	localToken, err := secret("PTMOVE_PULL_LOCAL_TOKEN")
	if err != nil {
		return err
	}
	if token == localToken {
		return errors.New("pull hub requires separate agent and local tokens")
	}
	bindHost := os.Getenv("PTMOVE_PULL_LOCAL_BIND")
	switch bindHost {
	case "", "127.0.0.1":
		bindHost = "127.0.0.1"
	case "0.0.0.0":
	default:
		return errors.New("PTMOVE_PULL_LOCAL_BIND must be 127.0.0.1 or 0.0.0.0")
	}
	certPath, keyPath := os.Getenv("PTMOVE_PULL_CERT"), os.Getenv("PTMOVE_PULL_KEY")
	if certPath == "" || keyPath == "" {
		return errors.New("PTMOVE_PULL_CERT and PTMOVE_PULL_KEY are required")
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("load pull hub TLS certificate: %w", err)
	}
	address := os.Getenv("PTMOVE_PULL_LISTEN")
	if address == "" {
		address = ":28443"
	}
	hub := newPullHub(token, localToken)
	newServer := func(handler http.Handler, writeTimeout time.Duration) *http.Server {
		return &http.Server{
			Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second,
			WriteTimeout: writeTimeout, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
			DisableGeneralOptionsHandler: true,
		}
	}
	qbServer := newServer(hub.localHandler("qb"), requestLimit("qb")+hubResultGrace+hubQueueWait+15*time.Second)
	mediaServer := newServer(hub.localHandler("media"), requestLimit("media")+hubResultGrace+hubQueueWait+15*time.Second)
	agentServer := newServer(http.HandlerFunc(hub.agentHandler), 70*time.Second)
	agentServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	defer qbServer.Close()
	defer mediaServer.Close()
	defer agentServer.Close()

	qbListener, err := net.Listen("tcp4", net.JoinHostPort(bindHost, "28380"))
	if err != nil {
		return fmt.Errorf("listen on private qB endpoint: %w", err)
	}
	defer qbListener.Close()
	mediaListener, err := net.Listen("tcp4", net.JoinHostPort(bindHost, "29090"))
	if err != nil {
		return fmt.Errorf("listen on private media endpoint: %w", err)
	}
	defer mediaListener.Close()
	agentListener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on pull hub TLS endpoint: %w", err)
	}
	defer agentListener.Close()

	// Open all listeners before serving so a bind failure cannot leave a partial hub.
	stopped := make(chan error, 3)
	go func() { stopped <- fmt.Errorf("serve private qB endpoint: %w", qbServer.Serve(qbListener)) }()
	go func() { stopped <- fmt.Errorf("serve private media endpoint: %w", mediaServer.Serve(mediaListener)) }()
	go func() { stopped <- fmt.Errorf("serve pull hub TLS: %w", agentServer.ServeTLS(agentListener, "", "")) }()
	return <-stopped
}
