package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentLinkSaveValidatesBeforePersisting(t *testing.T) {
	const (
		oldToken = "old-agent-credential-fixture"
		newToken = "new-agent-credential-fixture"
	)
	probeServer := func(token string) (*httptest.Server, *atomic.Int32) {
		calls := new(atomic.Int32)
		hub := newPullHub(token, hubLocalFixture)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil || r.Method != http.MethodPost || r.URL.Path != "/v1/claim" || string(body) != "{}" {
				t.Errorf("connection test tried to perform an unexpected action: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			hub.agentHandler(w, r)
		}))
		t.Cleanup(server.Close)
		return server, calls
	}
	oldServer, oldCalls := probeServer(oldToken)
	newServer, newCalls := probeServer(newToken)
	certificate := func(server *httptest.Server) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	}
	dir := t.TempDir()
	oldCA := filepath.Join(dir, "bootstrap.crt")
	state := filepath.Join(dir, "link.json")
	if err := os.WriteFile(oldCA, []byte(certificate(oldServer)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PTMOVE_PULL_URL", oldServer.URL)
	t.Setenv("PTMOVE_PULL_TOKEN", oldToken)
	t.Setenv("PTMOVE_PULL_TOKEN_FILE", "")
	t.Setenv("PTMOVE_PULL_CA", oldCA)
	template := &pullAgent{qb: agentTarget{origin: agentQBOrigin, token: hubLocalFixture}}
	manager, err := newAgentLinkManager(state, template)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.currentAgent().coordinator.CloseIdleConnections() })

	ctx := context.Background()
	config, err := manager.testCurrent(ctx)
	if err != nil || config.Address != oldServer.URL || !config.CertificateConfigured || oldCalls.Load() != 1 {
		t.Fatalf("current configuration was not tested, error=%v calls=%d", err, oldCalls.Load())
	}
	if _, err := manager.saveAndTest(ctx, agentLinkUpdate{Address: newServer.URL}); !errors.Is(err, errAgentLinkInvalid) || newCalls.Load() != 0 {
		t.Fatalf("cross-origin change must require a replacement token, error=%v calls=%d", err, newCalls.Load())
	}
	for _, candidate := range []agentLinkUpdate{
		{Address: newServer.URL, Token: "bad-agent-credential-fixture", CertificatePEM: certificate(newServer)},
		{Address: newServer.URL, Token: newToken, UseSystemCAs: true},
	} {
		if _, err := manager.saveAndTest(ctx, candidate); !errors.Is(err, errAgentLinkProbe) {
			t.Fatalf("expected rejected connection, got %v", err)
		}
		if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed connection persisted an unverified configuration")
		}
		if manager.currentConfig().Address != oldServer.URL {
			t.Fatal("failed connection changed the running configuration")
		}
	}
	config, err = manager.saveAndTest(ctx, agentLinkUpdate{
		Address: newServer.URL + "/", Token: newToken, CertificatePEM: certificate(newServer),
	})
	if err != nil || config.Address != newServer.URL || config.CertificateFingerprint == "" || config.CertificateExpires == "" {
		t.Fatalf("new origin was not activated, error=%v", err)
	}
	info, err := os.Stat(state)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("configuration file does not have owner-only read/write access: %v, %v", info, err)
	}
	contents, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	jsonConfig, err := json.Marshal(config)
	if err != nil || bytes.Contains(jsonConfig, []byte(newToken)) || bytes.Contains(jsonConfig, []byte("BEGIN CERTIFICATE")) {
		t.Fatal("public configuration exposed a credential")
	}
	if _, err := manager.saveAndTest(ctx, agentLinkUpdate{Address: newServer.URL, Token: oldToken}); !errors.Is(err, errAgentLinkProbe) {
		t.Fatalf("accepted an invalid replacement token: %v", err)
	}
	afterReject, err := os.ReadFile(state)
	if err != nil || !bytes.Equal(contents, afterReject) {
		t.Fatal("rejected update changed the saved configuration")
	}

	t.Setenv("PTMOVE_PULL_URL", "")
	t.Setenv("PTMOVE_PULL_TOKEN", "")
	t.Setenv("PTMOVE_PULL_CA", filepath.Join(dir, "missing.crt"))
	restarted, err := newAgentLinkManager(state, template)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.currentAgent().coordinator.CloseIdleConnections)
	reloaded, err := restarted.testCurrent(ctx)
	if err != nil || reloaded != config || restarted.currentAgent().qb != template.qb {
		t.Fatalf("restart did not restore the configured connection, error=%v", err)
	}
}

func TestAgentLinkRejectsMalformedOriginAndCertificate(t *testing.T) {
	for _, address := range []string{
		"http://example.com:28443", "https://example.com/path", "https://example.com:65536", "https://example..com",
		"https://foo@example.com", "https://exam_ple.com", "https://example.com?query=yes",
	} {
		if _, err := managedAgentOrigin(address); !errors.Is(err, errAgentLinkInvalid) {
			t.Errorf("expected invalid HTTPS origin for %q", address)
		}
	}
	for input, expected := range map[string]string{
		"https://EXAMPLE.COM:443/":     "https://example.com",
		"https://[2001:db8::1]:28443/": "https://[2001:db8::1]:28443",
	} {
		if got, err := managedAgentOrigin(input); err != nil || got != expected {
			t.Errorf("normalize %q = %q, %v", input, got, err)
		}
	}
	for _, certificate := range []string{
		"-----BEGIN CERTIFICATE-----END CERTIFICATE-----",
		"garbage",
		"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----",
		strings.Repeat("x", agentLinkCertificateLimit+1),
	} {
		if _, _, err := parseAgentLinkCertificates(certificate); !errors.Is(err, errAgentLinkInvalid) {
			t.Error("malformed certificate should be rejected")
		}
	}
}

func TestAgentLinkSwapKeepsOldLeaseAndResultTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	enteredClaim := make(chan struct{})
	allowLease := make(chan struct{})
	workerDone := make(chan struct{})
	oldResult := make(chan Result, 1)
	nextFromNew := make(chan struct{}, 1)
	var active atomic.Pointer[pullAgent]
	var oldClaims atomic.Int32
	qb := agentTarget{origin: agentQBOrigin, token: hubLocalFixture}
	job := Job{ID: strings.Repeat("a", 32), Lease: strings.Repeat("b", 32), Lane: "qb", Method: http.MethodGet, URI: "/api/v2/app/version"}
	leaseJSON, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	response := func(status int, body string) *http.Response {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	}
	local := &http.Client{Transport: agentTestTransport(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `"v5.3.0"`), nil
	})}
	old := &pullAgent{origin: "https://old.example", token: hubAgentFixture, local: local}
	old.coordinator = &http.Client{Transport: agentTestTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/claim":
			if oldClaims.Add(1) != 1 {
				t.Error("another claim went to the prior coordinator")
				return response(http.StatusNoContent, ""), nil
			}
			close(enteredClaim)
			select {
			case <-allowLease:
				return response(http.StatusOK, string(leaseJSON)), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "/v1/result":
			var result Result
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Error(err)
			}
			oldResult <- result
			return response(http.StatusNoContent, ""), nil
		}
		t.Error("unexpected request to prior coordinator")
		return response(http.StatusBadRequest, ""), nil
	})}
	fresh := &pullAgent{origin: "https://new.example", token: "new-agent-token-fixture", local: local}
	fresh.coordinator = &http.Client{Transport: agentTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/claim" {
			t.Error("result was sent to new coordinator")
		}
		nextFromNew <- struct{}{}
		cancel()
		return response(http.StatusNoContent, ""), nil
	})}
	active.Store(old)
	go func() {
		defer close(workerDone)
		runAgentLane(ctx, "qb", qb, active.Load)
	}()
	select {
	case <-enteredClaim:
	case <-ctx.Done():
		t.Fatal("old coordinator did not get the initial claim")
	}
	active.Store(fresh)
	close(allowLease)
	select {
	case <-workerDone:
	case <-ctx.Done():
		<-workerDone
	}
	select {
	case result := <-oldResult:
		if result.ID != job.ID || result.Lease != job.Lease || result.Status != http.StatusOK {
			t.Fatalf("prior coordinator did not receive the claimed job result: %+v", result)
		}
	default:
		t.Fatal("claimed job did not return a result to its original coordinator")
	}
	select {
	case <-nextFromNew:
	default:
		t.Fatal("client did not use the new coordinator after completing the in-flight lease")
	}
}
