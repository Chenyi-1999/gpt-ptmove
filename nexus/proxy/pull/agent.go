package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	agentQBOrigin    = "http://127.0.0.1:8080"
	agentMediaOrigin = "http://127.0.0.1:19090"
	agentClaimLimit  = maxTorrentRequestBytes*4/3 + 8192
	agentMinBackoff  = 250 * time.Millisecond
)

type agentTarget struct {
	origin string
	token  string
}

type pullAgent struct {
	origin      string
	token       string
	coordinator *http.Client
	local       *http.Client
	qb          agentTarget
	media       agentTarget
}

func runAgent() error {
	qbToken, err := secret("PTMOVE_QB_TOKEN")
	if err != nil {
		return err
	}
	mediaToken, err := secret("PTMOVE_PROXY_TOKEN")
	if err != nil {
		return err
	}
	agent := &pullAgent{
		local: newAgentClient(nil, true),
		qb:    agentTarget{origin: agentQBOrigin, token: qbToken},
		media: agentTarget{origin: agentMediaOrigin, token: mediaToken},
	}
	defer agent.local.CloseIdleConnections()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if address := os.Getenv("PTMOVE_PULL_ADMIN_LISTEN"); address != "" {
		password, err := secret("PTMOVE_PULL_ADMIN_PASSWORD")
		if err != nil {
			return err
		}
		links, err := newAgentLinkManager(os.Getenv("PTMOVE_PULL_STATE_FILE"), agent)
		if err != nil {
			return err
		}
		closeAdmin, err := startAgentAdmin(ctx, address, password, links)
		if err != nil {
			links.currentAgent().coordinator.CloseIdleConnections()
			return err
		}
		defer func() {
			closeAdmin()
			links.currentAgent().coordinator.CloseIdleConnections()
		}()
		agent.runWithCoordinator(ctx, links.currentAgent)
		return nil
	}

	agent.origin, err = agentCoordinatorOrigin(os.Getenv("PTMOVE_PULL_URL"))
	if err != nil {
		return err
	}
	roots, err := agentRootCAs(os.Getenv("PTMOVE_PULL_CA"))
	if err != nil {
		return err
	}
	agent.token, err = secret("PTMOVE_PULL_TOKEN")
	if err != nil {
		return err
	}
	agent.coordinator = newAgentClient(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, false)
	defer agent.coordinator.CloseIdleConnections()
	agent.run(ctx)
	return nil
}

func agentCoordinatorOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") ||
		u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("PTMOVE_PULL_URL must be an HTTPS origin")
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("PTMOVE_PULL_URL port is invalid")
		}
	}
	u.Path = ""
	return u.String(), nil
}

func agentRootCAs(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open PTMOVE_PULL_CA PEM file")
	}
	defer file.Close()
	const maxPEM = 1 << 20
	pem, err := io.ReadAll(io.LimitReader(file, maxPEM+1))
	pool := x509.NewCertPool()
	if err != nil || len(pem) > maxPEM || !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("PTMOVE_PULL_CA must be a valid PEM certificate file")
	}
	return pool, nil
}

// Check the concrete address after Go resolves a coordinator hostname, before connect.
func protectAgentCoordinator(_ context.Context, _ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if net.ParseIP(host).Equal(net.IPv4(81, 70, 24, 5)) {
		return errors.New("coordinator address is protected")
	}
	return nil
}

func newAgentClient(tlsConfig *tls.Config, local bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !local {
		dialer.ControlContext = protectAgentCoordinator
	}
	return &http.Client{
		Transport: &http.Transport{
			// A nil Proxy bypasses proxy environment variables. A fresh connection for
			// each local call also prevents transport retries on stale connections.
			DialContext:            dialer.DialContext,
			TLSClientConfig:        tlsConfig,
			TLSHandshakeTimeout:    5 * time.Second,
			IdleConnTimeout:        time.Minute,
			MaxResponseHeaderBytes: 16 << 10,
			DisableKeepAlives:      local,
			DisableCompression:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (a *pullAgent) run(ctx context.Context) {
	a.runWithCoordinator(ctx, func() *pullAgent { return a })
}

func (a *pullAgent) runWithCoordinator(ctx context.Context, currentAgent func() *pullAgent) {
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		runAgentLane(ctx, "qb", a.qb, currentAgent)
	}()
	go func() {
		defer workers.Done()
		runAgentLane(ctx, "media", a.media, currentAgent)
	}()
	workers.Wait()
}

func runAgentLane(ctx context.Context, lane string, target agentTarget, currentAgent func() *pullAgent) {
	claimBody, _ := json.Marshal(Claim{Lane: lane})
	backoff := agentMinBackoff
	var previous *pullAgent
	for ctx.Err() == nil {
		// Use the same coordinator for the claim and any result associated with
		// it, even when the administrator changes the address mid-request.
		a := currentAgent()
		if a != previous {
			backoff = agentMinBackoff
			previous = a
		}
		job, err := a.claim(ctx, lane, claimBody)
		if err == nil && job != nil {
			a.finishJob(ctx, target, *job)
		}
		if a != currentAgent() {
			a.coordinator.CloseIdleConnections()
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("pull agent %s claim: %v", lane, err)
			if !agentPause(ctx, backoff) {
				return
			}
			backoff = min(2*backoff, 10*time.Second)
			continue
		}
		backoff = agentMinBackoff
		if job == nil && !agentPause(ctx, backoff) {
			return
		}
	}
}

func (a *pullAgent) claim(ctx context.Context, lane string, body []byte) (*Job, error) {
	ctx, cancel := context.WithTimeout(ctx, claimWait+8*time.Second)
	defer cancel()
	response, err := a.post(ctx, "/v1/claim", body)
	if err != nil {
		return nil, errors.New("coordinator request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coordinator returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errors.New("claim response is not JSON")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, agentClaimLimit+1))
	if err != nil || len(data) > agentClaimLimit {
		return nil, errors.New("claim response unreadable or oversized")
	}
	var job Job
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&job) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF ||
		job.Lane != lane || !validOpaqueID(job.ID) || !validOpaqueID(job.Lease) {
		return nil, errors.New("invalid claim envelope")
	}
	return &job, nil
}

func (a *pullAgent) finishJob(ctx context.Context, target agentTarget, job Job) {
	limit := requestLimit(job.Lane)
	// Reserve time to deliver the result before the coordinator's lease expires.
	jobCtx, cancel := context.WithTimeout(ctx, limit+5*time.Second)
	defer cancel()
	localCtx, cancelLocal := context.WithTimeout(jobCtx, limit-6*time.Second)
	result := a.execute(localCtx, target, job)
	cancelLocal()
	// A submission retry reuses exactly these bytes and the original lease. The
	// local operation may have side effects even if it yielded a transport error.
	body, err := json.Marshal(result)
	if err != nil {
		log.Printf("pull agent %s result: encoding failed", job.Lane)
		return
	}
	for backoff := agentMinBackoff; jobCtx.Err() == nil; backoff = min(2*backoff, 2*time.Second) {
		status, err := a.submit(jobCtx, body)
		if err == nil && status == http.StatusNoContent {
			return
		}
		if err == nil && status < 500 && status != http.StatusTooManyRequests && status != http.StatusRequestTimeout {
			log.Printf("pull agent %s result: coordinator returned HTTP %d", job.Lane, status)
			return
		}
		if !agentPause(jobCtx, backoff) {
			break
		}
	}
	if ctx.Err() == nil {
		log.Printf("pull agent %s result: delivery deadline exceeded", job.Lane)
	}
}

func (a *pullAgent) execute(ctx context.Context, target agentTarget, job Job) Result {
	result := Result{ID: job.ID, Lease: job.Lease}
	fail := func(status int, message string) Result {
		result.Status = status
		result.ContentType = "text/plain; charset=utf-8"
		result.Body = []byte(message + "\n")
		return result
	}
	body := job.Body
	if job.Lane == "qb" && job.Method == http.MethodPost && job.URI == qbAddURI {
		var err error
		body, err = qbAddBody(job.ContentType, body, true)
		if err != nil {
			return fail(http.StatusBadRequest, "local request denied")
		}
	} else if validateJob(job) != nil {
		return fail(http.StatusBadRequest, "local request denied")
	}
	upstreamURI := job.URI
	scanReferences := job.URI == localScanReferencesURI
	if scanReferences {
		upstreamURI = "/api/v2/torrents/info?filter=all"
	}
	request, err := http.NewRequestWithContext(ctx, job.Method, target.origin+upstreamURI, bytes.NewReader(body))
	if err != nil {
		return fail(http.StatusBadRequest, "invalid local request")
	}
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+target.token)
	if job.Lane == "qb" {
		request.Header.Set("Referer", target.origin+"/")
	}
	if job.ContentType != "" {
		request.Header.Set("Content-Type", job.ContentType)
	}
	response, err := a.local.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail(http.StatusGatewayTimeout, "local request timed out")
		}
		return fail(http.StatusBadGateway, "local request failed")
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	encoding := response.Header.Get("Content-Encoding")
	if response.StatusCode < 200 || response.StatusCode > 599 || !validContentType(contentType) ||
		(encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return fail(http.StatusBadGateway, "unsupported local response")
	}
	if scanReferences && response.StatusCode != http.StatusOK {
		return fail(response.StatusCode, "local qB reference list unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && err != nil {
		return fail(http.StatusGatewayTimeout, "local response timed out")
	}
	if err != nil || len(data) > maxResponseBytes {
		return fail(http.StatusBadGateway, "local response unreadable or oversized")
	}
	if scanReferences {
		data, err = projectLocalScanReferences(data)
		if err != nil || len(data) > maxResponseBytes {
			return fail(http.StatusBadGateway, "invalid local qB reference list")
		}
		contentType = "application/json"
	}
	result.Status = response.StatusCode
	result.ContentType = contentType
	result.Body = data
	return result
}

func (a *pullAgent) submit(ctx context.Context, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := a.post(ctx, "/v1/result", body)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

func (a *pullAgent) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+a.token)
	request.Header.Set("Content-Type", "application/json")
	return a.coordinator.Do(request)
}

func agentPause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
