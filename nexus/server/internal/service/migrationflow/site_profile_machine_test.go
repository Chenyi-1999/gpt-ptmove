package migrationflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
	"github.com/pt-nexus/server/internal/service/publish/mapping"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func machineTestStore(t *testing.T) (*repository.Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if e = db.Exec("CREATE TABLE sites (id INTEGER PRIMARY KEY, site TEXT UNIQUE, nickname TEXT, base_url TEXT, migration INTEGER DEFAULT 1, cookie TEXT, passkey TEXT, description TEXT, `group` TEXT)").Error; e != nil {
		t.Fatal(e)
	}
	t.Setenv("PTNEXUS_BASE_DIR", dir)
	t.Setenv("PTNEXUS_DATA_DIR", filepath.Join(dir, "data"))
	return &repository.Store{DB: db, DBType: "sqlite"}, dir
}
func profileJSON(t *testing.T, id, origin, native string) []byte {
	t.Helper()
	p := siteprofile.Default()
	p.ID = id
	p.Name = id
	p.BaseURL = origin
	p.PTNexusSite = native
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestSiteProfileRegistryAndNativeOverlay(t *testing.T) {
	store, dir := machineTestStore(t)
	if e := store.DB.Table("sites").Create(map[string]any{"site": "basefixture", "nickname": "Native Display", "base_url": "base.example.com", "migration": 3, "cookie": "cookie-secret"}).Error; e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(dir, "configs"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "configs", "basefixture.yaml"), []byte("form_fields:\n  category: fixture_type\nmappings:\n  type:\n    category.movie: '401'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	service, e := NewSiteProfileService(store, nil, filepath.Join(dir, "data"))
	if e != nil {
		t.Fatal(e)
	}
	before, _ := service.List()
	raw := profileJSON(t, "fixturecustom", "https://custom.example.com", "basefixture")
	if _, e = service.Preview(raw); e != nil {
		t.Fatal(e)
	}
	after, _ := service.List()
	if len(before) != len(after) {
		t.Fatal("preview created a native site")
	}
	p, e := service.Import(raw, false, 0)
	if e != nil || p.Revision != 1 {
		t.Fatalf("import: %+v %v", p, e)
	}
	var n int64
	store.DB.Table("sites").Where("site = ?", "fixturecustom").Count(&n)
	if n != 1 {
		t.Fatal("native site row missing")
	}
	cfg, e := mapping.LoadSitePublishConfig("fixturecustom")
	if e != nil || cfg.FormFields["category"] != "fixture_type" {
		t.Fatalf("native mapping reader ignored overlay: %+v %v", cfg, e)
	}
	if e = service.RequirePublishMapping("fixturecustom"); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Import(raw, false, 0); e == nil {
		t.Fatal("implicit overwrite accepted")
	}
	if _, e = service.Import(raw, true, 7); e == nil {
		t.Fatal("stale revision accepted")
	}
	p, e = service.Import(raw, true, 1)
	if e != nil || p.Revision != 2 {
		t.Fatalf("explicit update failed: %v", e)
	}
	if _, e = service.Import(profileJSON(t, "fixturecustom", "https://changed.example.com", "basefixture"), true, 2); e == nil {
		t.Fatal("origin mutation accepted")
	}
	if _, e = service.Import(profileJSON(t, "otherfixture", "https://base.example.com", ""), false, 0); e == nil {
		t.Fatal("duplicate bare native origin accepted")
	}
	native, e := service.Resolve("Native Display")
	if e != nil || native.NativeName() != "Native Display" || native.BaseURL != "https://base.example.com" || native.Digest == "" {
		t.Fatalf("native resolution failed: %+v %v", native, e)
	}
	exported, e := service.Export("basefixture")
	if e != nil {
		t.Fatal(e)
	}
	selectors := exported["selectors"].(map[string]any)
	selectors["restriction_allowed"] = ".explicit-allowed"
	nativeRaw, _ := json.Marshal(exported)
	if _, e = service.Import(nativeRaw, false, 0); e == nil {
		t.Fatal("implicit builtin overwrite accepted")
	}
	native, e = service.Import(nativeRaw, true, 1)
	if e != nil || !native.Builtin || native.Revision != 2 {
		t.Fatalf("same identity builtin overlay failed: %+v %v", native, e)
	}
	resolved, e := service.Resolve("Native Display")
	if e != nil || resolved.Selectors.RestrictionAllowed != ".explicit-allowed" || resolved.NativeName() != "Native Display" {
		t.Fatalf("native nickname bypassed overlay: %+v %v", resolved, e)
	}
	cookie := "private-cookie"
	rss := "https://custom.example.com/rss.php?passkey=private-rss"
	if e = service.SetCredentials("fixturecustom", &cookie, &rss); e != nil {
		t.Fatal(e)
	}
	actual, e := service.RSS("fixturecustom")
	if e != nil || actual != rss {
		t.Fatal("RSS failed round trip")
	}
	var secret repository.SiteProfileSecret
	if e = store.DB.First(&secret, "id = ?", "fixturecustom").Error; e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(secret.RSS), "private-rss") {
		t.Fatal("RSS plaintext persisted")
	}
	public, e := service.Export("fixturecustom")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(public)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "revision") {
		t.Fatal("export leaked metadata/credentials")
	}
	empty := ""
	if e = service.SetCredentials("fixturecustom", nil, &empty); e != nil {
		t.Fatal(e)
	}
	kept, _ := service.Cookie("fixturecustom")
	if kept != cookie {
		t.Fatal("omitted cookie was cleared")
	}
	clear, _ := service.RSS("fixturecustom")
	if clear != "" {
		t.Fatal("empty RSS was not cleared")
	}
}
func TestMachineKeysAndAtomicReplay(t *testing.T) {
	store, dir := machineTestStore(t)
	service, e := NewMachineAPIService(store, dir)
	if e != nil {
		t.Fatal(e)
	}
	created, e := service.CreateKey("read default", nil, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	token := created["token"].(string)
	p, e := service.Authenticate(token)
	if e != nil || !p.Allows("read") || p.Allows("write") {
		t.Fatal("default key gained write scope")
	}
	listed, _ := service.ListKeys()
	b, _ := json.Marshal(listed)
	if strings.Contains(string(b), token) || strings.Contains(string(b), "hash") {
		t.Fatal("key material leaked")
	}
	if _, e = service.Authenticate("admin-jwt"); e == nil {
		t.Fatal("non machine key accepted")
	}
	if _, e = service.CreateKey("expired", nil, time.Now().Add(-time.Second)); e == nil {
		t.Fatal("expired key creation accepted")
	}
	created, e = service.CreateKey("writer", []string{"read", "write"}, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	p, _ = service.Authenticate(created["token"].(string))
	if e = store.DB.Exec("CREATE TABLE machine_effects (id INTEGER PRIMARY KEY, value TEXT)").Error; e != nil {
		t.Fatal(e)
	}
	calls := 0
	run := func(ctx context.Context) (map[string]any, int) {
		calls++
		tx := repository.MachineTransaction(ctx)
		if tx == nil {
			t.Fatal("business missing ambient transaction")
		}
		if e := tx.Exec("INSERT INTO machine_effects(value) VALUES (?)", "secret-response").Error; e != nil {
			t.Fatal(e)
		}
		return map[string]any{"result": "secret-response"}, 200
	}
	_, status := service.Execute(context.Background(), p, "idem-0001", "POST", "/api/v1/example", []byte(`{"a":1,"b":2}`), "write", run)
	if status != 200 {
		t.Fatalf("first status %d", status)
	}
	_, status = service.Execute(context.Background(), p, "idem-0001", "POST", "/api/v1/example", []byte(`{"b":2,"a":1}`), "write", run)
	if status != 200 || calls != 1 {
		t.Fatal("canonical replay reran side effect")
	}
	_, status = service.Execute(context.Background(), p, "idem-0001", "POST", "/api/v1/example", []byte(`{"a":2}`), "write", run)
	if status != 409 {
		t.Fatal("changed request replay accepted")
	}
	var replay repository.MachineReplayRow
	store.DB.First(&replay, "key_id = ?", p.ID)
	if strings.Contains(string(replay.Response), "secret-response") {
		t.Fatal("replay response not encrypted")
	}
	_, status = service.Execute(context.Background(), p, "rollback", "POST", "/api/v1/example", nil, "write", func(ctx context.Context) (map[string]any, int) {
		repository.MachineTransaction(ctx).Exec("INSERT INTO machine_effects(value) VALUES ('rollback')")
		return map[string]any{"error": "rejected"}, 409
	})
	if status != 409 {
		t.Fatal("rejection status lost")
	}
	var n int64
	store.DB.Table("machine_effects").Count(&n)
	if n != 1 {
		t.Fatal("failed command committed side effect")
	}
	if e = service.RevokeKey(p.ID); e != nil {
		t.Fatal(e)
	}
	_, status = service.Execute(context.Background(), p, "idem-0001", "POST", "/api/v1/example", []byte(`{"a":1,"b":2}`), "write", run)
	if status != 401 || calls != 1 {
		t.Fatal("revoked cached replay still authorized")
	}
}

func TestMachineScopesExpiryAndInvalidReplayInputs(t *testing.T) {
	store, dir := machineTestStore(t)
	service, e := NewMachineAPIService(store, dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.CreateKey("missing read", []string{"publish"}, time.Now().Add(time.Hour)); e == nil {
		t.Fatal("key without read accepted")
	}
	created, e := service.CreateKey("publish only", []string{"read", "publish"}, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	p, e := service.Authenticate(created["token"].(string))
	if e != nil || p.Allows("write") || !p.Allows("publish") {
		t.Fatal("publish improperly implies write")
	}
	called := false
	run := func(context.Context) (map[string]any, int) { called = true; return map[string]any{}, 200 }
	for _, key := range []string{"short", "contains space", "nonascii-中", strings.Repeat("a", 129)} {
		_, status := service.Execute(context.Background(), p, key, "POST", "/test", nil, "publish", run)
		if status != 400 || called {
			t.Fatal("unsafe replay token accepted")
		}
	}
	_, status := service.Execute(context.Background(), p, "valid-key", "POST", "/test", []byte(`{} {}`), "publish", run)
	if status != 400 || called {
		t.Fatal("trailing JSON accepted")
	}
	if e = store.DB.Model(&repository.MachineKeyRow{}).Where("id = ?", p.ID).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error; e != nil {
		t.Fatal(e)
	}
	if _, e = service.Authenticate(created["token"].(string)); e == nil {
		t.Fatal("expired key authenticated")
	}
	_, status = service.Execute(context.Background(), p, "valid-key", "POST", "/test", nil, "publish", run)
	if status != 401 || called {
		t.Fatal("cached principal bypassed expiry")
	}
}

func TestMachineEncryptionKeyLossFailsClosed(t *testing.T) {
	store, dir := machineTestStore(t)
	service, e := NewMachineAPIService(store, dir)
	if e != nil {
		t.Fatal(e)
	}
	created, e := service.CreateKey("read", nil, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	p, _ := service.Authenticate(created["token"].(string))
	_, status := service.Execute(context.Background(), p, "key-loss-test", "POST", "/test", nil, "read", func(context.Context) (map[string]any, int) { return map[string]any{"value": "encrypted"}, 200 })
	if status != 200 {
		t.Fatal("unable to write encrypted replay")
	}
	if e = os.Remove(filepath.Join(dir, "machine-registry.key")); e != nil {
		t.Fatal(e)
	}
	if _, e = NewMachineAPIService(store, dir); e == nil {
		t.Fatal("missing encryption key was silently replaced")
	}
	if _, e = os.Stat(filepath.Join(dir, "machine-registry.key")); !os.IsNotExist(e) {
		t.Fatal("startup created replacement encryption key")
	}
}

func TestMachineReadCommandConcurrentFirstResponse(t *testing.T) {
	for _, differentBody := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict_%v", differentBody), func(t *testing.T) {
			store, dir := machineTestStore(t)
			sqlDB, _ := store.DB.DB()
			sqlDB.SetMaxOpenConns(1)
			service, err := NewMachineAPIService(store, dir)
			if err != nil {
				t.Fatal(err)
			}
			key, err := service.CreateKey("read scan", []string{"read", "write"}, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			p, _ := service.Authenticate(key["token"].(string))
			type result struct {
				body   map[string]any
				status int
			}
			firstStarted := make(chan struct{})
			secondStarted := make(chan struct{})
			releaseFirst := make(chan struct{})
			releaseSecond := make(chan struct{})
			first := make(chan result, 1)
			second := make(chan result, 1)
			var firstRelease, secondRelease sync.Once
			defer firstRelease.Do(func() { close(releaseFirst) })
			defer secondRelease.Do(func() { close(releaseSecond) })
			scan := func(start chan struct{}, release chan struct{}, winner string) func(context.Context) (map[string]any, int) {
				return func(ctx context.Context) (map[string]any, int) {
					if repository.MachineTransaction(ctx) != nil {
						return map[string]any{"error": "unexpected ambient transaction"}, 500
					}
					close(start)
					<-release
					return map[string]any{"winner": winner}, 200
				}
			}
			go func() {
				body, status := service.ExecuteReadCommand(context.Background(), p, "concurrent-scan", "POST", "/scan", []byte(`{"value":1}`), "write", scan(firstStarted, releaseFirst, "first"))
				first <- result{body, status}
			}()
			select {
			case <-firstStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("first preview did not start")
			}
			secondBody := []byte(`{"value":1}`)
			if differentBody {
				secondBody = []byte(`{"value":2}`)
			}
			go func() {
				body, status := service.ExecuteReadCommand(context.Background(), p, "concurrent-scan", "POST", "/scan", secondBody, "write", scan(secondStarted, releaseSecond, "second"))
				second <- result{body, status}
			}()
			select {
			case <-secondStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("second readonly preview blocked behind first")
			}
			firstRelease.Do(func() { close(releaseFirst) })
			select {
			case got := <-first:
				if got.status != 200 || got.body["winner"] != "first" {
					t.Fatalf("first response %+v", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("first response did not commit")
			}
			secondRelease.Do(func() { close(releaseSecond) })
			select {
			case got := <-second:
				if differentBody {
					if got.status != 409 {
						t.Fatalf("different concurrent body returned %+v", got)
					}
				} else if got.status != 200 || got.body["winner"] != "first" {
					t.Fatalf("second response did not use first cache: %+v", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("second response did not finish")
			}
			var rows []repository.MachineReplayRow
			if err := store.DB.Find(&rows).Error; err != nil || len(rows) != 1 {
				t.Fatalf("expected one durable first reply: %d %v", len(rows), err)
			}
		})
	}
}
func TestMachineReadCommandFailureAndExpiryNeverCache(t *testing.T) {
	store, dir := machineTestStore(t)
	service, err := NewMachineAPIService(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	key, err := service.CreateKey("read scan", []string{"read", "write"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := service.Authenticate(key["token"].(string))
	_, status := service.ExecuteReadCommand(context.Background(), p, "failed-preview", "POST", "/scan", nil, "write", func(context.Context) (map[string]any, int) {
		return map[string]any{"error": "readonly scan rejected"}, 409
	})
	if status != 409 {
		t.Fatalf("failure status %d", status)
	}
	_, status = service.ExecuteReadCommand(context.Background(), p, "expired-preview", "POST", "/scan", nil, "write", func(ctx context.Context) (map[string]any, int) {
		if err := store.DB.Model(&repository.MachineKeyRow{}).Where("id = ?", p.ID).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error; err != nil {
			t.Fatal(err)
		}
		return map[string]any{"sensitive": "discard"}, 200
	})
	if status != 401 {
		t.Fatalf("expired in-flight preview returned %d", status)
	}
	var n int64
	if err := store.DB.Model(&repository.MachineReplayRow{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("failed preview cached: %d %v", n, err)
	}
}
