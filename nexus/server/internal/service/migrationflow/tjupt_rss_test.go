package migrationflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
)

type fakeTjuptSite struct {
	record  tjupt.Assessment
	fail    bool
	reads   int
	batches []int
}

func (f *fakeTjuptSite) Preview(context.Context, string, int) ([]tjupt.Candidate, error) {
	return []tjupt.Candidate{{ID: "1"}}, nil
}

func (f *fakeTjuptSite) Status(_ context.Context, _ string, hashes []string) (map[string]tjupt.Assessment, error) {
	f.reads++
	f.batches = append(f.batches, len(hashes))
	if f.fail {
		return nil, errors.New("站点查询失败")
	}
	records := make(map[string]tjupt.Assessment, len(hashes))
	for _, hash := range hashes {
		records[hash] = f.record
	}
	return records, nil
}

func TestTjuptStatusPagesAllTaggedTasksAndBatchesHNR(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "mock-ui-password")
	const taskCount = 109
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/torrents/info" ||
			r.URL.Query().Get("tag") != tjuptTag {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || offset < 0 || limit < 1 {
			t.Error("qB tag request must provide a valid page")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rows := make([]map[string]any, 0)
		for i := offset; i < min(offset+limit, taskCount); i++ {
			rows = append(rows, map[string]any{"hash": fmt.Sprintf("%040x", i+1), "tags": tjuptTag,
				"state": "stalledUP", "progress": 1, "tracker": "https://tracker.tjupt.org/announce"})
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer qb.Close()

	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings["downloaders"] = []any{map[string]any{
		"id": "qb", "name": "qB", "type": "qbittorrent", "host": qb.URL, "api_key": "mock-qb-token",
	}}
	if err := cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	site := &fakeTjuptSite{record: tjupt.Assessment{Label: "做种考核中"}}
	service := NewTjuptRSSService(cfg)
	service.site = site

	items, err := service.Status(context.Background(), "qb", "mock-token")
	if err != nil || len(items) != taskCount || !slices.Equal(site.batches, []int{50, 50, 9}) {
		t.Fatalf("expected all %d tasks in bounded batches, got %d, %v, %v", taskCount, len(items), site.batches, err)
	}
	for _, item := range items {
		if item.Assessment.Label != site.record.Label {
			t.Fatal("H&R record was lost across pages")
		}
	}
}

func TestTjuptPreviewStatusAndRetireUseDifferentEffects(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	const addedOn = 1_700_000_000
	const trackerSecret = "mock-private-passkey"
	const retireKey = "mock-retire-key-only-for-unit-testing"
	const hnrToken = "mock-site-token-only-for-unit-testing"
	t.Setenv("AUTH_PASSWORD", "mock-ui-password")
	t.Setenv("PTMOVE_TJUPT_RETIRE_KEY", retireKey)
	t.Setenv("PTMOVE_TJUPT_HNR_TOKEN", hnrToken)
	t.Setenv("PTMOVE_TJUPT_TRACKER_PASSKEY", trackerSecret)
	var reads, stops int
	var wrongReceipt bool
	tag := tjuptTag
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-qb-token" {
			t.Error("missing existing downloader authentication")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/torrents/info":
			reads++
			if r.URL.Query().Get("tag") != tjuptTag {
				t.Error("qB query was not scoped to the plugin tag")
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"hash": hash, "name": "北洋园测试种子", "state": "uploading", "tags": tag,
				"added_on": addedOn, "progress": 1,
				"tracker": "https://tracker.tjupt.org/announce?passkey=" + trackerSecret,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/starpt/tjupt/retire":
			stops++
			if r.ParseForm() != nil || r.Form.Get("hashes") != hash || r.Form.Get("added_on") != "1700000000" || len(r.Form) != 5 {
				t.Error("retire request was not bound to the exact task")
			}
			expires, err := strconv.ParseInt(r.Form.Get("expires_at"), 10, 64)
			if err != nil || expires <= time.Now().Unix() || expires > time.Now().Add(120*time.Second).Unix() {
				t.Error("retire proof does not have a short expiry")
			}
			accountMAC := hmac.New(sha256.New, []byte(retireKey))
			_, _ = fmt.Fprintf(accountMAC, "STARPT-TJUPT-ACCOUNT-V1\n%s", trackerSecret)
			account := hex.EncodeToString(accountMAC.Sum(nil))
			proofMAC := hmac.New(sha256.New, []byte(retireKey))
			_, _ = fmt.Fprintf(proofMAC, "STARPT-TJUPT-RETIRE-V1\n%s\n%d\n%d\n%s", hash, addedOn, expires, account)
			if r.Form.Get("account") != account || r.Form.Get("proof") != hex.EncodeToString(proofMAC.Sum(nil)) {
				t.Error("expected a dedicated signature bound to the north garden account")
			}
			if wrongReceipt {
				_, _ = w.Write([]byte("<html>Old proxy response</html>"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"operation": "starpt-tjupt-retired-v1", "hash": hash, "added_on": addedOn})
		default:
			t.Errorf("unexpected qB request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer qb.Close()
	cfg, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get()
	settings["downloaders"] = []any{map[string]any{
		"id": "feiniu", "name": "飞牛", "type": "qbittorrent", "host": qb.URL, "api_key": "mock-qb-token",
	}}
	if err = cfg.Save(settings); err != nil {
		t.Fatal(err)
	}
	service := NewTjuptRSSService(cfg)
	site := &fakeTjuptSite{record: tjupt.Assessment{
		CanRetire: true, Label: "已达标", AddedAt: time.Unix(addedOn+1, 0),
	}}
	service.site = site
	ctx := context.Background()
	if _, err := service.Preview(ctx, "synthetic RSS", 10); err != nil || reads != 0 || stops != 0 || site.reads != 0 {
		t.Fatalf("preview performed an unrelated effect, error=%v, reads=%d, stops=%d", err, reads, stops)
	}
	status, err := service.Status(ctx, "feiniu", hnrToken)
	if err != nil || len(status) != 1 || !status[0].Assessment.CanRetire || stops != 0 {
		t.Fatalf("read-only status lacked the eligible task: %+v, %v", status, err)
	}
	foreignStatus, err := service.Status(ctx, "feiniu", "other-account-token")
	if err != nil || len(foreignStatus) != 1 || foreignStatus[0].Assessment.CanRetire ||
		service.Retire(ctx, "feiniu", "other-account-token", hash) == nil || stops != 0 {
		t.Fatalf("another account must not authorize retirement: %+v, %v", foreignStatus, err)
	}
	for _, scenario := range []struct {
		name     string
		fail     bool
		eligible bool
		added    int64
		tag      string
	}{
		{"ongoing", false, false, addedOn + 1, tjuptTag},
		{"expired token or outage", true, true, addedOn + 1, tjuptTag},
		{"previous assessment", false, true, addedOn - 1, tjuptTag},
		{"unrelated tag", false, true, addedOn + 1, "starpt-tjupt-other"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			site.fail, tag = scenario.fail, scenario.tag
			site.record.CanRetire, site.record.AddedAt = scenario.eligible, time.Unix(scenario.added, 0)
			if err := service.Retire(ctx, "feiniu", hnrToken, hash); err == nil || stops != 0 {
				t.Fatalf("retire should fail closed: error=%v, stops=%d", err, stops)
			}
		})
	}
	site.record.Label = "管理员取消"
	site.record.AddedAt = time.Unix(addedOn+1, 0)
	tag = tjuptTag
	priorReads := site.reads
	if err := service.Retire(ctx, "feiniu", hnrToken, hash); err != nil || site.reads != priorReads+1 || stops != 1 {
		t.Fatalf("expected a new site query and one scoped stop: error=%v, site reads=%d, stops=%d", err, site.reads, stops)
	}
	wrongReceipt = true
	if err := service.Retire(ctx, "feiniu", hnrToken, hash); err == nil || stops != 2 {
		t.Fatalf("an unrelated upstream success must not be accepted or retried: error=%v, stops=%d", err, stops)
	}
}
