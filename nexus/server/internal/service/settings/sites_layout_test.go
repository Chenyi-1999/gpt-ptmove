package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/pt-nexus/server/internal/config"
)

func openDashboardLayoutTestConfig(t *testing.T, path string) *SettingsService {
	t.Helper()
	manager, err := config.NewManager(config.RuntimePaths{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return NewSettingsService(manager)
}

func TestDashboardLayoutLegacySettings(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "layout-test")
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"ui_settings":{"sites_layout":{"columns":1,"order":["local-scan","unknown","local-scan","summary",3]}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	service := openDashboardLayoutTestConfig(t, path)

	wantHome := DashboardLayoutSettings{2, []string{"tasks", "speed", "traffic", "downloaders"}, []string{}}
	if got := service.GetHomeLayoutSettings(); !reflect.DeepEqual(got, wantHome) {
		t.Fatalf("default home layout = %+v, want %+v", got, wantHome)
	}
	wantSites := DashboardLayoutSettings{1, []string{"local-scan", "summary", "site-chart", "group-chart", "site-stats", "group-stats"}, []string{}}
	if got := service.GetSitesLayoutSettings(); !reflect.DeepEqual(got, wantSites) {
		t.Fatalf("legacy sites layout = %+v, want %+v", got, wantSites)
	}
}

func TestDashboardLayoutPreservesAndClearsHiddenPanels(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "layout-test")
	path := filepath.Join(t.TempDir(), "config.json")
	service := openDashboardLayoutTestConfig(t, path)

	save := func(draft DashboardLayoutSettings) {
		t.Helper()
		if err := service.SaveSitesLayoutSettings(draft); err != nil {
			t.Fatal(err)
		}
	}
	check := func(expected DashboardLayoutSettings) {
		t.Helper()
		reloaded := openDashboardLayoutTestConfig(t, path)
		if got := reloaded.GetSitesLayoutSettings(); !reflect.DeepEqual(got, expected) {
			t.Fatalf("reloaded layout = %+v, want %+v", got, expected)
		}
	}

	save(DashboardLayoutSettings{1, []string{"site-stats", "site-stats", "unknown"}, []string{"local-scan", "unknown", "local-scan"}})
	check(DashboardLayoutSettings{
		1, []string{"site-stats", "summary", "site-chart", "group-chart", "group-stats", "local-scan"}, []string{"local-scan"},
	})

	// 旧版写入缺少 hidden 时，已有的显示选择仍保留。
	save(DashboardLayoutSettings{2, []string{"group-stats"}, nil})
	expected := DashboardLayoutSettings{
		2, []string{"group-stats", "summary", "site-chart", "group-chart", "site-stats", "local-scan"}, []string{"local-scan"},
	}
	check(expected)

	save(DashboardLayoutSettings{2, []string{"group-stats"}, []string{}})
	expected.Hidden = []string{}
	check(expected)
}

func TestDashboardLayoutsSaveConcurrently(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "layout-test")
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"ui_settings":{"background_url":"/backgrounds/test.png"},"upload_settings":{"anonymous_upload":false}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	service := openDashboardLayoutTestConfig(t, path)

	home := DashboardLayoutSettings{1, []string{"downloaders", "tasks", "speed", "traffic"}, []string{"tasks", "speed"}}
	sites := DashboardLayoutSettings{2, []string{"site-chart", "summary", "group-chart", "site-stats", "group-stats", "local-scan"}, []string{"site-chart", "group-stats"}}
	logs := map[string]any{"page_size": 100, "search_query": "fixture"}
	updates := []func() error{
		func() error { return service.SaveHomeLayoutSettings(home) },
		func() error { return service.SaveSitesLayoutSettings(sites) },
		func() error { return service.SavePublishLogsUIViewSettings(logs) },
	}
	start := make(chan struct{})
	errors := make(chan error, len(updates))
	var writers sync.WaitGroup
	for _, update := range updates {
		writers.Add(1)
		go func(update func() error) {
			defer writers.Done()
			<-start
			for i := 0; i < 12; i++ {
				if err := update(); err != nil {
					errors <- err
					return
				}
			}
		}(update)
	}
	close(start)
	writers.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}

	reloaded := openDashboardLayoutTestConfig(t, path)
	if got := reloaded.GetHomeLayoutSettings(); !reflect.DeepEqual(got, home) {
		t.Fatalf("reloaded home layout = %+v, want %+v", got, home)
	}
	if got := reloaded.GetSitesLayoutSettings(); !reflect.DeepEqual(got, sites) {
		t.Fatalf("reloaded sites layout = %+v, want %+v", got, sites)
	}
	if got := reloaded.GetPublishLogsUIViewSettings()["search_query"]; got != "fixture" {
		t.Fatalf("reloaded publish log search = %v, want fixture", got)
	}
	cfg := reloaded.cfg.Get()
	if background := nestedMap(cfg, "ui_settings")["background_url"]; background != "/backgrounds/test.png" {
		t.Fatalf("unrelated UI setting changed: %v", background)
	}
	if anonymous := nestedMap(cfg, "upload_settings")["anonymous_upload"]; anonymous != false {
		t.Fatalf("unrelated upload setting changed: %v", anonymous)
	}
}
