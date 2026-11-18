package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const agentTestLease = "0123456789abcdef0123456789abcdef"

func TestAgentHTTPSOrigin(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:9443", "https://[::1]:9443", "https://pull.example.test/"} {
		if origin, err := agentCoordinatorOrigin(raw); err != nil || origin != strings.TrimSuffix(raw, "/") {
			t.Errorf("agentCoordinatorOrigin(%q) = %q, %v", raw, origin, err)
		}
	}
	for _, raw := range []string{
		"", "http://127.0.0.1", "https://", "https://user@127.0.0.1", "https://127.0.0.1/foo",
		"https://127.0.0.1/%2f", "https://127.0.0.1?", "https://127.0.0.1#", "https://127.0.0.1:", "https://127.0.0.1:65536",
	} {
		if _, err := agentCoordinatorOrigin(raw); err == nil {
			t.Errorf("accepted invalid origin %q", raw)
		}
	}
}

func TestAgentValidatesTLSRootAndIP(t *testing.T) {
	var reached atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		if r.URL.Path != "/v1/claim" || r.Header.Get("Authorization") != "Bearer coordinator-token" {
			t.Error("coordinator received incorrect routing or authorization")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rootFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(rootFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	roots, err := agentRootCAs(rootFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootFile, []byte("invalid PEM"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentRootCAs(rootFile); err == nil {
		t.Fatal("invalid configured CA accepted")
	}
	client := newAgentClient(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, false)
	defer client.CloseIdleConnections()
	origin, err := agentCoordinatorOrigin(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	agent := &pullAgent{origin: origin, token: "coordinator-token", coordinator: client}
	body := []byte(`{"lane":"qb"}`)
	resp, err := agent.post(ctx, "/v1/claim", body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("TLS response = %d", resp.StatusCode)
	}
	untrusted := newAgentClient(&tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}, false)
	defer untrusted.CloseIdleConnections()
	agent.coordinator = untrusted
	if resp, err := agent.post(ctx, "/v1/claim", body); err == nil {
		resp.Body.Close()
		t.Fatal("accepted a coordinator with an untrusted certificate")
	} else if !errors.As(err, new(x509.UnknownAuthorityError)) {
		t.Fatalf("expected unknown certificate authority, got %v", err)
	}
	wrongIP := newAgentClient(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, false)
	defer wrongIP.CloseIdleConnections()
	wrongIP.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	agent.coordinator = wrongIP
	agent.origin = "https://" + net.JoinHostPort("127.0.0.2", address.Port())
	if resp, err := agent.post(ctx, "/v1/claim", body); err == nil {
		resp.Body.Close()
		t.Fatal("accepted a coordinator whose certificate lacks the configured IP SAN")
	} else if !errors.As(err, new(x509.HostnameError)) {
		t.Fatalf("expected IP SAN validation error, got %v", err)
	}
	if reached.Load() != 1 {
		t.Fatalf("expected one authenticated HTTP request, got %d", reached.Load())
	}
}

func TestAgentDoesNotFollowLocalRedirectOrForwardHeaders(t *testing.T) {
	var followed atomic.Bool
	escape := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Store(true)
	}))
	defer escape.Close()
	const redirectBody = "moved"
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/v2/torrents/info?filter=completed" || r.Method != http.MethodGet {
			t.Error("local request does not match the allowed qB route")
		}
		if r.Header.Get("Authorization") != "Bearer qb-local-token" || r.Header.Get("Cookie") != "" {
			t.Error("local request did not use only its dedicated credentials")
		}
		w.Header().Set("Location", escape.URL)
		w.Header().Set("Set-Cookie", "local_cookie=local_secret")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = io.WriteString(w, redirectBody)
	}))
	defer local.Close()
	client := newAgentClient(nil, true)
	defer client.CloseIdleConnections()
	agent := &pullAgent{local: client}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := agentTarget{origin: local.URL, token: "qb-local-token"}
	job := Job{
		ID: agentTestLease, Lease: agentTestLease, Lane: "qb", Method: http.MethodGet,
		URI: "/api/v2/torrents/info?filter=completed",
	}
	result := agent.execute(ctx, target, job)
	if followed.Load() || result.Status != http.StatusTemporaryRedirect || string(result.Body) != redirectBody || result.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("redirect or response preservation failed: %+v", result)
	}
	wire, err := json.Marshal(result)
	if err != nil || bytes.Contains(wire, []byte(escape.URL)) || bytes.Contains(wire, []byte("local_secret")) {
		t.Fatal("upstream response headers escaped into the result envelope")
	}
	for _, uri := range []string{escape.URL, "//127.0.0.1/api/v2/torrents/info", "/api/v2/torrents/delete", "/api/v2/torrents/%69nfo"} {
		job.URI = uri
		if result := agent.execute(ctx, target, job); result.Status != http.StatusBadRequest {
			t.Errorf("local request accepted disallowed URI %q: %+v", uri, result)
		}
	}
}

func TestAgentClaimRejectsInvalidEnvelope(t *testing.T) {
	valid := Job{ID: agentTestLease, Lease: agentTestLease, Lane: "qb", Method: http.MethodGet, URI: "/api/v2/torrents/info"}
	validBytes, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"additional JSON":   append(bytes.Clone(validBytes), []byte(` {}`)...),
		"unexpected header": append(bytes.Clone(validBytes[:len(validBytes)-1]), []byte(`,"headers":{"Authorization":"overridden"}}`)...),
		"wrong lane":        bytes.Replace(validBytes, []byte(`"qb"`), []byte(`"media"`), 1),
		"invalid lease":     bytes.ReplaceAll(validBytes, []byte(agentTestLease), []byte("missing")),
		"oversized":         append(bytes.Clone(validBytes), bytes.Repeat([]byte(" "), agentClaimLimit)...),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(response)
			}))
			defer server.Close()
			client := newAgentClient(nil, false)
			defer client.CloseIdleConnections()
			a := pullAgent{origin: server.URL, coordinator: client}
			if job, err := a.claim(context.Background(), "qb", []byte(`{"lane":"qb"}`)); err == nil || job != nil {
				t.Fatalf("untrusted envelope produced a job: %+v, %v", job, err)
			}
		})
	}
}

type agentTestTransport func(*http.Request) (*http.Response, error)

func (f agentTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type agentTestZeroReader struct{}

func (agentTestZeroReader) Read(dst []byte) (int, error) {
	clear(dst)
	return len(dst), nil
}

func TestAgentBoundsLocalCall(t *testing.T) {
	job := Job{ID: agentTestLease, Lease: agentTestLease, Lane: "qb", Method: http.MethodGet, URI: "/api/v2/torrents/info"}
	target := agentTarget{origin: agentQBOrigin, token: "qb-local-token"}
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		a := pullAgent{local: &http.Client{Transport: agentTestTransport(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}}
		result := a.execute(ctx, target, job)
		if result.Status != http.StatusGatewayTimeout || result.ID != job.ID || result.Lease != job.Lease {
			t.Fatalf("local timeout yielded unexpected result: %+v", result)
		}
	})
	t.Run("oversized response", func(t *testing.T) {
		a := pullAgent{local: &http.Client{Transport: agentTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(io.LimitReader(agentTestZeroReader{}, maxResponseBytes+1)),
			}, nil
		})}}
		result := a.execute(context.Background(), target, job)
		if result.Status != http.StatusBadGateway || len(result.Body) > 128 {
			t.Fatalf("oversized response yielded status %d with %d bytes", result.Status, len(result.Body))
		}
	})
}

func TestAgentLanesAndResultRetry(t *testing.T) {
	const (
		mediaID = "10000000000000000000000000000000"
		qbID    = "20000000000000000000000000000000"
	)
	mediaJob := Job{ID: mediaID, Lease: agentTestLease, Lane: "media", Method: http.MethodPost, URI: "/api/media/screenshot", ContentType: "application/json", Body: []byte(`{"remote_path":"/media/movie.mkv"}`)}
	qbJob := Job{ID: qbID, Lease: agentTestLease, Lane: "qb", Method: http.MethodGet, URI: "/api/v2/torrents/info"}
	mediaStarted := make(chan struct{})
	qbReported := make(chan struct{})
	var mediaCalls atomic.Int32
	mediaResponse := []byte{0xff, 0, '\n'}
	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mediaCalls.Add(1) == 1 {
			close(mediaStarted)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(body, mediaJob.Body) || r.Method != mediaJob.Method || r.URL.RequestURI() != mediaJob.URI ||
			r.Header.Get("Authorization") != "Bearer media-local-token" || r.Header.Get("Content-Type") != mediaJob.ContentType {
			t.Error("unexpected media request")
		}
		// The media request cannot complete until the qB lane reports its result.
		select {
		case <-qbReported:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(mediaResponse)
	}))
	defer mediaServer.Close()
	qbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer qb-local-token" || r.Method != qbJob.Method || r.URL.RequestURI() != qbJob.URI {
			t.Error("unexpected qB request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[]")
	}))
	defer qbServer.Close()
	var mediaClaims, qbClaims, mediaReports, qbReports atomic.Int32
	firstMediaReport := make(chan []byte, 1)
	mediaReported := make(chan struct{})
	coordinator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer coordinator-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect coordinator request method or credentials")
		}
		switch r.URL.Path {
		case "/v1/claim":
			var claim Claim
			if err := json.NewDecoder(r.Body).Decode(&claim); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch claim.Lane {
			case "media":
				if mediaClaims.Add(1) == 1 {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(mediaJob)
					return
				}
			case "qb":
				if qbClaims.Add(1) == 1 {
					select {
					case <-mediaStarted:
					case <-r.Context().Done():
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(qbJob)
					return
				}
			default:
				t.Errorf("unexpected lane %q", claim.Lane)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/result":
			data, err := io.ReadAll(r.Body)
			var result Result
			if err != nil || json.Unmarshal(data, &result) != nil || result.Lease != agentTestLease || bytes.Contains(data, []byte("local-token")) {
				t.Error("invalid result envelope")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch result.ID {
			case qbID:
				if result.Status != http.StatusOK || result.ContentType != "application/json" || string(result.Body) != "[]" {
					t.Errorf("incorrect qB result: %+v", result)
				}
				if qbReports.Add(1) == 1 {
					close(qbReported)
				}
			case mediaID:
				if result.Status != http.StatusCreated || result.ContentType != "application/octet-stream" || !bytes.Equal(result.Body, mediaResponse) {
					t.Errorf("incorrect media result: %+v", result)
				}
				switch mediaReports.Add(1) {
				case 1:
					firstMediaReport <- data
					// Drop the connection after receiving the result. The agent must
					// retry delivery with the same bytes without rerunning the job.
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				case 2:
					if !bytes.Equal(data, <-firstMediaReport) {
						t.Error("network retry changed the result or lease")
					}
					close(mediaReported)
				}
			default:
				t.Errorf("unexpected job ID %q", result.ID)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected coordinator path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer coordinator.Close()
	localClient, coordinatorClient := newAgentClient(nil, true), newAgentClient(nil, false)
	defer localClient.CloseIdleConnections()
	defer coordinatorClient.CloseIdleConnections()
	a := &pullAgent{
		origin: coordinator.URL, token: "coordinator-token", coordinator: coordinatorClient, local: localClient,
		qb: agentTarget{origin: qbServer.URL, token: "qb-local-token"}, media: agentTarget{origin: mediaServer.URL, token: "media-local-token"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.run(ctx)
	}()
	select {
	case <-mediaReported:
	case <-ctx.Done():
		t.Error("the media result retry did not complete while both lanes were active")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent workers failed to stop after cancellation")
	}
	if mediaCalls.Load() != 1 || qbReports.Load() != 1 || mediaReports.Load() != 2 {
		t.Fatalf("unexpected local executions or reports: media executed=%d, qB reports=%d, media reports=%d", mediaCalls.Load(), qbReports.Load(), mediaReports.Load())
	}
}
