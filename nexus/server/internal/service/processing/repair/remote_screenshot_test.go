package repair

import (
	"encoding/json"
	"fmt"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	shared "github.com/pt-nexus/server/internal/service/processing/shared"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMissingAndInvalidScreenshotsUseRemoteRepair(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid", "partial", "unknown-subtitle", "inspection-failure", "upload-failure", "interactive-failure"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/image/") {
					w.Header().Set("Content-Type", "image/png")
					fmt.Fprint(w, "fixture")
					return
				}
				if r.URL.Path == "/bad.png" {
					http.NotFound(w, r)
					return
				}
				if r.Header.Get("Authorization") != "Bearer proxy-secret" {
					t.Error("remote repair missed proxy auth")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				mode, _ := request["mode"].(string)
				calls = append(calls, mode)
				if mode == "inspect" && (scenario == "inspection-failure" || scenario == "interactive-failure") {
					http.Error(w, "probe failed", 500)
					return
				}
				if mode == "inspect" {
					state := "confirmed_chinese"
					if scenario == "unknown-subtitle" {
						state = "usable_but_unconfirmed"
					}
					fmt.Fprintf(w, `{"success":true,"subtitle_state":%q}`, state)
					return
				}
				if scenario == "upload-failure" {
					http.Error(w, "upload failed", 500)
					return
				}
				fmt.Fprint(w, `{"success":true,"bbcode":"[img]https://img99.pixhost.to/images/1/a.png[/img]\n[img]https://img99.pixhost.to/images/1/b.png[/img]\n[img]https://img99.pixhost.to/images/1/c.png[/img]"}`)
			}))
			defer server.Close()
			_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			number, _ := strconv.Atoi(port)
			root := map[string]any{"downloaders": []any{map[string]any{"id": "nas", "type": "qbittorrent", "host": server.URL, "use_proxy": true, "proxy_media_only": true, "proxy_port": number, "proxy_token": "proxy-secret", "enabled": true}}}
			screens := ""
			if scenario == "invalid" || scenario == "upload-failure" {
				screens = "[img]" + server.URL + "/bad.png[/img]"
			}
			if scenario == "partial" {
				screens = "[img]" + server.URL + "/image/valid.png[/img]"
			}
			review := parser.ReviewExtractedData{Screens: screens}
			mode := "background"
			if scenario == "interactive-failure" {
				mode = "interactive"
			}
			status, preview := repairScreenshotsDuringFetch("test", "/media", "nas", "Movie.mkv", "Movie", mode, &review, FetchRepairDeps{RootConfig: root})
			if len(calls) == 0 || calls[0] != "inspect" {
				t.Fatalf("missing remote inspection: %v", calls)
			}
			pending := scenario == "unknown-subtitle" || scenario == "inspection-failure" || scenario == "upload-failure" || scenario == "interactive-failure"
			if shared.NeedsScreenshotManualReview(status) != pending {
				t.Fatalf("unsafe review status %q scenario %s", status, scenario)
			}
			if scenario == "interactive-failure" {
				if len(calls) != 1 || !preview {
					t.Fatalf("interactive failure continued captures %v", calls)
				}
				return
			}
			if scenario == "partial" && !strings.Contains(review.Screens, "/image/valid.png") {
				t.Fatal("discarded valid original image")
			}
			if scenario == "upload-failure" && review.Screens != "" {
				t.Fatal("preserved invalid image")
			}
			if scenario != "upload-failure" && !strings.Contains(review.Screens, "img99.pixhost.to") {
				t.Fatalf("missing full image %s", review.Screens)
			}
		})
	}
}

func TestEnoughValidScreenshotsHaveNoCaptureSideEffects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, "fixture")
	}))
	defer server.Close()
	review := parser.ReviewExtractedData{Screens: ToBBCodeImages([]string{server.URL + "/a.png", server.URL + "/b.png", server.URL + "/c.png"})}
	status, preview := repairScreenshotsDuringFetch("test", "/media", "nas", "Movie.mkv", "Movie", "background", &review, FetchRepairDeps{})
	if status != shared.ScreenshotReviewStatusNone || preview || calls.Load() != 3 {
		t.Fatalf("valid image repair side effects status=%s preview=%t calls=%d", status, preview, calls.Load())
	}
}

func TestEmptyPendingRepairClearsInvalidScreenshotsInParallelMerge(t *testing.T) {
	original := "[img]https://example.invalid/dead.png[/img]"
	pending := screenshotsRepairResult{ScreenshotReviewStatus: shared.ScreenshotReviewStatusPending}
	if got := mergeRepairedScreenshots(original, pending); got != "" {
		t.Fatal("parallel merge restored known-invalid screenshots")
	}
	// An uncompleted goroutine's zero result must preserve the draft rather than discard it.
	if got := mergeRepairedScreenshots(original, screenshotsRepairResult{}); got != original {
		t.Fatal("zero result discarded draft")
	}
	valid := screenshotsRepairResult{Screens: "[img]https://img99.pixhost.to/images/1/new.png[/img]"}
	if got := mergeRepairedScreenshots(original, valid); got != valid.Screens {
		t.Fatal("parallel merge lost repaired image")
	}
}
