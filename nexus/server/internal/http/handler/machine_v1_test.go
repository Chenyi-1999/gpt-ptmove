package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/migrationflow"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// machinePortFixture 不执行任何网络请求，验证 HTTP 范围边界和公开响应。
type machinePortFixture struct {
	updates int
	pending bool
	enabled bool
}
type machineSettingsFixture struct {
	Enabled     bool `json:"enabled"`
	AutoPublish bool `json:"auto_publish"`
}

func (p *machinePortFixture) GetSettings() map[string]any {
	if p.enabled {
		return map[string]any{"settings": machineSettingsFixture{Enabled: true}}
	}
	return map[string]any{"settings": map[string]any{"enabled": false, "auto_publish": false, "cookie": "never-leak", "password": "never-leak"}}
}
func (p *machinePortFixture) GetSettingsContext(context.Context) map[string]any {
	return p.GetSettings()
}
func (p *machinePortFixture) ListTasksContext(context.Context) (map[string]any, int) {
	return p.ListTasks()
}
func (p *machinePortFixture) UpdateSettingsContext(ctx context.Context, v map[string]any) (map[string]any, int) {
	p.updates++
	return map[string]any{"success": true}, 200
}
func (p *machinePortFixture) Preview(context.Context, map[string]any) (map[string]any, int) {
	return map[string]any{"candidates": []any{}}, 200
}
func (p *machinePortFixture) Run(context.Context, map[string]any) (map[string]any, int) {
	return map[string]any{"success": true}, 200
}
func (p *machinePortFixture) ListTasks() (map[string]any, int) {
	tasks := []any{}
	if p.pending {
		tasks = append(tasks, map[string]any{"state": "reserved"})
	}
	return map[string]any{"tasks": tasks}, 200
}
func (p *machinePortFixture) ListRuns() (map[string]any, int) {
	return map[string]any{"runs": []any{}}, 200
}
func (p *machinePortFixture) Cancel(string) (map[string]any, int) { return map[string]any{}, 200 }
func (p *machinePortFixture) Retry(string) (map[string]any, int)  { return map[string]any{}, 200 }
func (p *machinePortFixture) Reconcile(context.Context, string, string) (map[string]any, int) {
	return map[string]any{}, 200
}
func TestMachineHTTPIsolationAndScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	db, e := gorm.Open(sqlite.Open(filepath.Join(dir, "http.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	service, e := migrationflow.NewMachineAPIService(&repository.Store{DB: db, DBType: "sqlite"}, dir)
	if e != nil {
		t.Fatal(e)
	}
	port := &machinePortFixture{}
	router := gin.New()
	NewMachineHandler(service, nil, port).RegisterV1(router.Group("/api/v1"))
	read, _ := service.CreateKey("read", nil, time.Now().Add(time.Hour))
	writer, _ := service.CreateKey("write", []string{"read", "write"}, time.Now().Add(time.Hour))
	publisher, _ := service.CreateKey("publish-only", []string{"read", "publish"}, time.Now().Add(time.Hour))
	request := func(method, path, token, body, idem string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
		req.Header.Set("Cookie", "session=browser-session")
		req.Header.Set("X-API-Key", "NAS-key")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for _, active := range []bool{false, true} {
		port.enabled = active
		for _, command := range []struct{ method, path, body string }{{"PUT", "/api/v1/rules", `{}`}, {"POST", "/api/v1/sites/import", `{}`}, {"PUT", "/api/v1/sites/example/credentials", `{}`}} {
			if r := request(command.method, command.path, publisher["token"].(string), command.body, "publish-no-write"); r.Code != 403 {
				t.Fatalf("publish scope replaced write active=%v path=%s status=%d", active, command.path, r.Code)
			}
		}
	}
	port.enabled = false
	if r := request(http.MethodGet, "/api/v1/dashboard", "", "", ""); r.Code != 401 {
		t.Fatal("Cookie or NAS fallback accepted")
	}
	if r := request("GET", "/api/v1/dashboard", read["token"].(string), "", ""); r.Code != 200 || strings.Contains(r.Body.String(), "never-leak") {
		t.Fatalf("readonly response: %d %s", r.Code, r.Body.String())
	}
	if r := request("PUT", "/api/v1/rules", read["token"].(string), `{}`, "readonly"); r.Code != 403 || port.updates != 0 {
		t.Fatal("read key wrote settings")
	}
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, ""); r.Code != 400 {
		t.Fatal("write without idempotency accepted")
	}
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{"auto_publish":true}`, "publish01"); r.Code != 403 || port.updates != 0 {
		t.Fatal("write key enabled publication")
	}
	port.pending = true
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, "pending01"); r.Code != 403 || port.updates != 0 {
		t.Fatal("write key changed pending execution rules")
	}
	port.pending = false
	port.enabled = true
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, "typed-enabled"); r.Code != 403 || port.updates != 0 {
		t.Fatal("typed settings bypassed publish guard")
	}
	port.enabled = false
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, "success01"); r.Code != 200 || port.updates != 1 {
		t.Fatalf("write scope unable to save disabled settings: %d", r.Code)
	}
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, "success01"); r.Code != 200 || port.updates != 1 {
		t.Fatal("HTTP replay repeated service mutation")
	}
	port.pending = true
	if r := request("PUT", "/api/v1/rules", writer["token"].(string), `{}`, "success01"); r.Code != 403 || port.updates != 1 {
		t.Fatal("cached replay bypassed newly required publish scope")
	}
	port.pending = false
	if r := request("POST", "/api/v1/tasks", writer["token"].(string), `{}`, "run-0001"); r.Code != 403 {
		t.Fatal("write key enqueued publish work")
	}
	if e = service.RevokeKey(read["key"].(map[string]any)["id"].(string)); e != nil {
		t.Fatal(e)
	}
	if r := request("GET", "/api/v1/dashboard", read["token"].(string), "", ""); r.Code != 401 {
		t.Fatal("revoked key accepted")
	}
}
func TestMachineOpenAPIOnlyDocumentsMachineSurface(t *testing.T) {
	doc := MachineOpenAPI()
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/api/v1/sites/import"]; !ok {
		t.Fatal("import route undocumented")
	}
	if _, ok := paths["/api/v1/connection"]; !ok {
		t.Fatal("connection route undocumented")
	}
	operationIDs := map[string]string{}
	for path, raw := range paths {
		if !strings.HasPrefix(path, "/api/v1/") {
			t.Fatalf("global admin API exposed in OpenAPI: %s", path)
		}
		for method, op := range raw.(map[string]any) {
			operation := op.(map[string]any)
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" {
				t.Fatalf("missing operationId for %s %s", method, path)
			}
			if previous, exists := operationIDs[operationID]; exists {
				t.Fatalf("operationId %q collides for %s and %s", operationID, previous, method+" "+path)
			}
			operationIDs[operationID] = method + " " + path
			b, _ := json.Marshal(op)
			if method != "get" && !strings.Contains(string(b), "Idempotency-Key") {
				t.Fatalf("missing command invariant in %s", path)
			}
		}
	}
}

type connectionMachinePort struct {
	machinePortFixture
	calls atomic.Int32
}

func (p *connectionMachinePort) GetSettings() map[string]any {
	p.calls.Add(1)
	return p.machinePortFixture.GetSettings()
}
func (p *connectionMachinePort) ListTasks() (map[string]any, int) {
	p.calls.Add(1)
	return p.machinePortFixture.ListTasks()
}
func (p *connectionMachinePort) ListRuns() (map[string]any, int) {
	p.calls.Add(1)
	return p.machinePortFixture.ListRuns()
}
func (p *connectionMachinePort) Preview(context.Context, map[string]any) (map[string]any, int) {
	p.calls.Add(1)
	return map[string]any{}, 200
}
func (p *connectionMachinePort) Run(context.Context, map[string]any) (map[string]any, int) {
	p.calls.Add(1)
	return map[string]any{}, 200
}

func TestMachineConnectionIdentityAuthAndIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "connection.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	service, err := migrationflow.NewMachineAPIService(&repository.Store{DB: db, DBType: "sqlite"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	port := &connectionMachinePort{}
	router := gin.New()
	NewMachineHandler(service, nil, port).RegisterV1(router.Group("/api/v1"))
	created, err := service.CreateKey("agent-key", []string{"read", "write"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	token := created["token"].(string)
	request := func(auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connection", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Cookie", "session=browser-session")
		req.Header.Set("X-API-Key", "NAS-key")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for _, auth := range []string{"", "Bearer not-a-machine-key"} {
		if response := request(auth); response.Code != http.StatusUnauthorized {
			t.Fatalf("fallback authentication accepted: auth %q got %d", auth, response.Code)
		}
	}
	response := request("Bearer " + token)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("connection response: status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("connection response exposed the bearer token")
	}
	var replayCount int64
	if err := db.Model(&repository.MachineReplayRow{}).Count(&replayCount).Error; err != nil || replayCount != 0 {
		t.Fatalf("connection wrote replay state: count=%d error=%v", replayCount, err)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["service"] != "ptmove-nexus" || body["api_version"] != "v1" || body["openapi_url"] != "/api/v1/openapi.json" || body["idempotency_header"] != "Idempotency-Key" {
		t.Fatalf("unexpected connection identity: %#v", body)
	}
	key := body["key"].(map[string]any)
	createdKey := created["key"].(map[string]any)
	if key["id"] != createdKey["id"] || key["name"] != "agent-key" || key["expires_at"] != createdKey["expires_at"] {
		t.Fatalf("connection key metadata differs: %#v", key)
	}
	if got, want := key["scopes"], []any{"read", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("connection scopes = %#v, want %#v", got, want)
	}
	if got, want := body["management"], []any{"automation", "site_profiles"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("management = %#v, want %#v", got, want)
	}
	if calls := port.calls.Load(); calls != 0 {
		t.Fatalf("connection called automation port %d times", calls)
	}
	if err := service.RevokeKey(createdKey["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if response := request("Bearer " + token); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key accepted by connection: %d", response.Code)
	}
	if err := db.Model(&repository.MachineKeyRow{}).Where("id = ?", createdKey["id"]).Updates(map[string]any{"revoked": false, "expires_at": time.Now().Add(-time.Minute).Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	if response := request("Bearer " + token); response.Code != http.StatusUnauthorized {
		t.Fatalf("expired key accepted by connection: %d", response.Code)
	}
}

// blockingMachinePort 将扫描停在事务外，模拟不确定时长的只读网络请求。
type blockingMachinePort struct {
	machinePortFixture
	started chan bool
	release chan struct{}
	calls   atomic.Int32
}

func (p *blockingMachinePort) Preview(ctx context.Context, _ map[string]any) (map[string]any, int) {
	n := p.calls.Add(1)
	p.started <- repository.MachineTransaction(ctx) != nil
	select {
	case <-p.release:
		return map[string]any{"candidates": []any{}, "scan": n}, 200
	case <-ctx.Done():
		return map[string]any{"error": "preview cancelled"}, 408
	}
}
func machineScanFixture(t *testing.T) (*gorm.DB, *migrationflow.MachineAPIService, *blockingMachinePort, *gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "scan.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	service, err := migrationflow.NewMachineAPIService(&repository.Store{DB: db, DBType: "sqlite"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	port := &blockingMachinePort{started: make(chan bool, 4), release: make(chan struct{})}
	router := gin.New()
	NewMachineHandler(service, nil, port).RegisterV1(router.Group("/api/v1"))
	key, err := service.CreateKey("scan", []string{"read", "write"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return db, service, port, router, key["token"].(string), key["key"].(map[string]any)["id"].(string)
}
func machineScanRequest(router *gin.Engine, token, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/scans", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	return out
}
func TestMachineScanDoesNotHoldDatabaseDuringPreview(t *testing.T) {
	db, _, port, router, token, _ := machineScanFixture(t)
	var release sync.Once
	defer release.Do(func() { close(port.release) })
	if err := db.Exec("CREATE TABLE machine_admin_state (id INTEGER PRIMARY KEY, enabled INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO machine_admin_state VALUES (1,1)")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- machineScanRequest(router, token, "scan-unlocked", `{}`) }()
	select {
	case ambient := <-port.started:
		if ambient {
			t.Fatal("preview received ambient transaction")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preview did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("UPDATE machine_admin_state SET enabled=0 WHERE id=1").Error; err != nil {
		t.Fatalf("administrator disable blocked behind preview: %v", err)
	}
	release.Do(func() { close(port.release) })
	select {
	case result := <-done:
		if result.Code != 200 {
			t.Fatalf("scan status %d: %s", result.Code, result.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not finish")
	}
	if replay := machineScanRequest(router, token, "scan-unlocked", `{}`); replay.Code != 200 || port.calls.Load() != 1 {
		t.Fatal("cached replay repeated network preview")
	}
	for _, body := range []string{`{} {}`, `null`, `[]`} {
		if result := machineScanRequest(router, token, "scan-malformed", body); result.Code != 400 {
			t.Fatalf("invalid body accepted: %s -> %d", body, result.Code)
		}
	}
	if result := machineScanRequest(router, token, "", `{}`); result.Code != 400 {
		t.Fatal("scan accepted missing Idempotency-Key")
	}
	if result := machineScanRequest(router, token, "scan-large-body", strings.Repeat(" ", 67585)); result.Code != 400 {
		t.Fatal("scan accepted oversized body")
	}
}
func TestMachineScanRevocationDuringPreviewDiscardsResponse(t *testing.T) {
	db, service, port, router, token, keyID := machineScanFixture(t)
	var release sync.Once
	defer release.Do(func() { close(port.release) })
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- machineScanRequest(router, token, "scan-revoked", `{}`) }()
	select {
	case ambient := <-port.started:
		if ambient {
			t.Fatal("preview received ambient transaction")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preview did not start")
	}
	revoked := make(chan error, 1)
	go func() { revoked <- service.RevokeKey(keyID) }()
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation blocked behind preview")
	}
	release.Do(func() { close(port.release) })
	select {
	case result := <-done:
		if result.Code != 401 {
			t.Fatalf("revoked in-flight preview returned %d", result.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not finish")
	}
	var cached int64
	if err := db.Model(&repository.MachineReplayRow{}).Where("key_id = ?", keyID).Count(&cached).Error; err != nil || cached != 0 {
		t.Fatalf("revoked preview cached response: %d %v", cached, err)
	}
}
