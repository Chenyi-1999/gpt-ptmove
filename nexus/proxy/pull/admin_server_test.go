package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type agentAdminFixture struct {
	update agentLinkUpdate
	saves  int
	tests  int
}

func (f *agentAdminFixture) currentConfig() agentAdminConfig {
	return agentAdminConfig{Address: "https://hub.example:28443", TokenConfigured: true}
}

func (f *agentAdminFixture) saveAndTest(_ context.Context, update agentLinkUpdate) (agentAdminConfig, error) {
	f.update = update
	f.saves++
	return f.currentConfig(), nil
}

func (f *agentAdminFixture) testCurrent(_ context.Context) (agentAdminConfig, error) {
	f.tests++
	return f.currentConfig(), nil
}

func TestAgentAdminProtectionAndWorkflow(t *testing.T) {
	fixture := &agentAdminFixture{}
	const password = "random-admin-fixture-password"
	handler := newAgentAdminHandler(password, fixture, agentAdminAccess{})
	request := func(method, path, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1:19447"+path, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:50080"
		req.SetBasicAuth("admin", password)
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://127.0.0.1:19447")
			req.Header.Set("X-STARPT-Request", "1")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		if mutate != nil {
			mutate(req)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"unauthenticated", func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		{"wrong password", func(r *http.Request) { r.SetBasicAuth("admin", "wrong") }, http.StatusUnauthorized},
		{"wrong host", func(r *http.Request) { r.Host = "other.example:19447" }, http.StatusForbidden},
		{"remote peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.41:12000" }, http.StatusForbidden},
		{"wrong origin", func(r *http.Request) { r.Header.Set("Origin", "http://other.example:19447") }, http.StatusForbidden},
		{"missing intent header", func(r *http.Request) { r.Header.Del("X-STARPT-Request") }, http.StatusForbidden},
		{"cross origin site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
		{"wrong content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := request(http.MethodPost, "/api/config", "{}", tc.mutate).Code; got != tc.status {
				t.Fatalf("status = %d; expected %d", got, tc.status)
			}
		})
	}
	if fixture.saves != 0 || fixture.tests != 0 {
		t.Fatal("invalid requests reached the state manager")
	}

	page := request(http.MethodGet, "/", "", nil)
	policy := page.Header().Get("Content-Security-Policy")
	prefix := "'nonce-"
	start := strings.Index(policy, prefix)
	if page.Code != http.StatusOK || start < 0 {
		t.Fatalf("page=%d policy=%q", page.Code, policy)
	}
	nonce, _, _ := strings.Cut(policy[start+len(prefix):], "'")
	if strings.Count(page.Body.String(), `nonce="`+nonce+`"`) != 2 || strings.Contains(page.Body.String(), "{{nonce}}") {
		t.Fatal("the page does not apply the CSP nonce to its two inline resources")
	}

	initial := request(http.MethodGet, "/api/config", "", nil)
	if initial.Code != http.StatusOK || !strings.Contains(initial.Body.String(), "https://hub.example:28443") {
		t.Fatalf("config = %d %s", initial.Code, initial.Body.String())
	}
	const newToken = "new-private-agent-token"
	saved := request(http.MethodPost, "/api/config", `{"address":"https://other.example:28443","token":"`+newToken+`"}`, nil)
	var response agentAdminResponse
	if saved.Code != http.StatusOK || json.Unmarshal(saved.Body.Bytes(), &response) != nil || !response.OK || response.Config == nil ||
		strings.Contains(saved.Body.String(), newToken) || fixture.saves != 1 || fixture.update.Token != newToken {
		t.Fatal("expected a sanitized save receipt")
	}
	if test := request(http.MethodPost, "/api/test", "{}", nil); test.Code != http.StatusOK || fixture.tests != 1 {
		t.Fatal("test did not use the saved connection")
	}
}

func TestAgentAdminLANListenerAndAccess(t *testing.T) {
	const address = "192.168.31.238:19447"
	for _, bad := range []struct{ address, cidr string }{
		{"0.0.0.0:19447", "192.168.31.0/24"},
		{"8.8.8.8:19447", "8.8.8.0/24"},
		{"public.example:19447", "192.168.31.0/24"},
		{address, ""},
		{address, "192.168.3.0/24"},
		{address, "192.168.0.0/15"},
		{address, "0.0.0.0/0"},
		{"127.0.0.1:19447", "192.168.31.0/24"},
	} {
		if _, err := parseAgentAdminAccess(bad.address, bad.cidr); err == nil {
			t.Errorf("unexpected access policy for %+v", bad)
		}
	}
	loopback, err := parseAgentAdminAccess("127.0.0.1:19447", "")
	if err != nil || loopback.lan.IsValid() {
		t.Fatalf("default localhost policy = %+v, %v", loopback, err)
	}
	access, err := parseAgentAdminAccess(address, "192.168.31.0/24")
	if err != nil {
		t.Fatal(err)
	}
	const password = "test-client-admin-password"
	fixture := &agentAdminFixture{}
	handler := newAgentAdminHandler(password, fixture, access)
	for _, tc := range []struct {
		name, peer, host, origin string
		auth                     bool
		status                   int
	}{
		{"authorized LAN", "192.168.31.19:50123", address, "http://" + address, true, http.StatusOK},
		{"password still required", "192.168.31.19:50123", address, "http://" + address, false, http.StatusUnauthorized},
		{"other private subnet", "192.168.3.83:50123", address, "http://" + address, true, http.StatusForbidden},
		{"public peer", "203.0.113.10:50123", address, "http://" + address, true, http.StatusForbidden},
		{"loopback peer", "127.0.0.1:50123", address, "http://" + address, true, http.StatusForbidden},
		{"DNS rebinding authority", "192.168.31.19:50123", "attacker.invalid:19447", "http://attacker.invalid:19447", true, http.StatusForbidden},
		{"wrong authority port", "192.168.31.19:50123", "192.168.31.238:80", "http://192.168.31.238:80", true, http.StatusForbidden},
		{"cross origin", "192.168.31.19:50123", address, "http://attacker.invalid", true, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+address+"/api/test", strings.NewReader("{}"))
			req.RemoteAddr, req.Host = tc.peer, tc.host
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-STARPT-Request", "1")
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-For", "192.168.31.20")
			if tc.auth {
				req.SetBasicAuth("admin", password)
			}
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, req)
			if result.Code != tc.status {
				t.Fatalf("status=%d, expected %d", result.Code, tc.status)
			}
		})
	}
	if fixture.tests != 1 {
		t.Fatal("unexpected request reached the connection test")
	}
}
