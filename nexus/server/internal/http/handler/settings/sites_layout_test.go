package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/config"
	settingsservice "github.com/pt-nexus/server/internal/service/settings"
)

func TestDashboardLayoutHTTP(t *testing.T) {
	t.Setenv("AUTH_PASSWORD", "layout-test")
	manager, err := config.NewManager(config.RuntimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(settingsservice.NewSettingsService(manager), nil, nil, nil)
	app := gin.New()
	home := "/api/ui_settings/home_layout"
	sites := "/api/ui_settings/sites_layout"
	app.GET(home, handler.GetHomeLayoutSettings)
	app.POST(home, handler.SaveHomeLayoutSettings)
	app.GET(sites, handler.GetSitesLayoutSettings)
	app.POST(sites, handler.SaveSitesLayoutSettings)

	call := func(method, path, body string, wantStatus int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		app.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.Code, wantStatus, response.Body.String())
		}
		return response
	}
	checkHidden := func(path string, wantHidden ...string) {
		t.Helper()
		response := call(http.MethodGet, path, "", http.StatusOK)
		var layout settingsservice.DashboardLayoutSettings
		if err := json.Unmarshal(response.Body.Bytes(), &layout); err != nil {
			t.Fatal(err)
		}
		if layout.Hidden == nil || strings.Join(layout.Hidden, ",") != strings.Join(wantHidden, ",") {
			t.Fatalf("GET %s: hidden = %#v, want %v", path, layout.Hidden, wantHidden)
		}
	}

	checkHidden(home)
	checkHidden(sites)
	call(http.MethodPost, home, `{"columns":1,"order":["traffic"],"hidden":["tasks"]}`, http.StatusOK)
	call(http.MethodPost, sites, `{"columns":2,"order":[],"hidden":["local-scan"]}`, http.StatusOK)
	call(http.MethodPost, sites, `{"columns":1,"order":["local-scan"]}`, http.StatusOK)
	checkHidden(home, "tasks")
	checkHidden(sites, "local-scan")
	call(http.MethodPost, sites, `{"columns":1,"order":[],"hidden":[]}`, http.StatusOK)
	checkHidden(sites)
	call(http.MethodPost, home, `{"columns":3,"order":[]}`, http.StatusBadRequest)
	call(http.MethodPost, home, `{"columns":1,"order":[],"hidden":"tasks"}`, http.StatusBadRequest)
	call(http.MethodPost, home, `{"columns":1,"hidden":[]}`, http.StatusBadRequest)
	checkHidden(home, "tasks")
}
