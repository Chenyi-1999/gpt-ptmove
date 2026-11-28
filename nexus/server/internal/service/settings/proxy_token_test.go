package settings

import (
	"github.com/pt-nexus/server/internal/config"
	"path/filepath"
	"testing"
)

func TestProxyTokenPreserveClearAndRedaction(t *testing.T) {
	manager, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService(manager)
	update := func(fields map[string]any) {
		t.Helper()
		fields["id"] = "nas"
		if err := service.UpdateSettings(map[string]any{"downloaders": []any{fields}}); err != nil {
			t.Fatal(err)
		}
	}
	update(map[string]any{"password": "downloader-password", "password_configured": false, "proxy_token": "first-proxy-secret-at-least-32-characters", "api_key": "separate-qb-key", "proxy_media_only": true})
	masked := proxyTestMap(toSlice(service.GetSettingsMasked()["downloaders"])[0])
	if masked["password"] != "" || masked["password_configured"] != true || masked["proxy_token"] != "" || masked["proxy_token_configured"] != true || masked["api_key"] != "" {
		t.Fatal("secret leaked")
	}
	saved := proxyTestMap(toSlice(manager.Get()["downloaders"])[0])
	if saved["password"] != "downloader-password" {
		t.Fatal("password was not saved")
	}
	if _, exists := saved["password_configured"]; exists {
		t.Fatal("client-supplied password marker was persisted")
	}
	update(map[string]any{"proxy_token": "", "api_key": ""})
	saved = proxyTestMap(toSlice(manager.Get()["downloaders"])[0])
	if saved["proxy_token"] != "first-proxy-secret-at-least-32-characters" || saved["api_key"] != "separate-qb-key" {
		t.Fatal("blank token did not preserve distinct credentials")
	}
	if saved["password"] != "downloader-password" {
		t.Fatal("blank password did not preserve saved credential")
	}
	if err := service.UpdateSettings(map[string]any{"downloaders": []any{
		map[string]any{"id": "nas", "password": ""},
		map[string]any{"id": "empty", "password": ""},
	}}); err != nil {
		t.Fatal(err)
	}
	maskedDownloaders := toSlice(service.GetSettingsMasked()["downloaders"])
	if proxyTestMap(maskedDownloaders[0])["password_configured"] != true || proxyTestMap(maskedDownloaders[1])["password_configured"] != false {
		t.Fatal("password configured status did not reflect stored credentials")
	}
	update(map[string]any{"proxy_token": "rotated-secret-at-least-32-characters"})
	if service.DownloaderConfigs()[0]["proxy_token"] != "rotated-secret-at-least-32-characters" {
		t.Fatal("rotation not used internally")
	}
	update(map[string]any{"clear_proxy_token": true, "proxy_token_configured": true})
	saved = proxyTestMap(toSlice(manager.Get()["downloaders"])[0])
	if saved["proxy_token"] != "" || saved["api_key"] != "separate-qb-key" {
		t.Fatal("explicit clear changed wrong secret")
	}
	if _, exists := saved["clear_proxy_token"]; exists {
		t.Fatal("control flag persisted")
	}
	if service.GetSettingsMasked()["downloaders"] == nil {
		t.Fatal("missing settings")
	}
}

func proxyTestMap(value any) map[string]any { return value.(map[string]any) }
