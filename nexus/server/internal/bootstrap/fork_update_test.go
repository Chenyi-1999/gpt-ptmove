package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestForkUpdateRoutesReportReleaseVersion(t *testing.T) {
	engine := gin.New()
	registerForkUpdateRoutes(engine)

	for _, path := range []string{"/update/local-version", "/update/check", "/update/changelog"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			var response struct {
				LocalVersion  string `json:"local_version"`
				RemoteVersion string `json:"remote_version"`
				HasUpdate     bool   `json:"has_update"`
				UpdateControl struct {
					DisableUpdate bool `json:"disable_update"`
				} `json:"update_control"`
				History []struct {
					Version string `json:"version"`
				} `json:"history"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			switch path {
			case "/update/local-version":
				if response.LocalVersion != forkVersion {
					t.Fatalf("local version = %q, want %q", response.LocalVersion, forkVersion)
				}
			case "/update/check":
				if response.LocalVersion != forkVersion || response.RemoteVersion != forkVersion || response.HasUpdate || !response.UpdateControl.DisableUpdate {
					t.Fatalf("unexpected offline update metadata: %+v", response)
				}
			case "/update/changelog":
				if len(response.History) < 2 || response.History[0].Version != forkVersion || response.History[0].Version == response.History[1].Version {
					t.Fatalf("unexpected release history: %+v", response.History)
				}
			}
		})
	}
	for _, path := range []string{"/update/pull", "/update/install"} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusConflict {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusConflict)
		}
	}
}
