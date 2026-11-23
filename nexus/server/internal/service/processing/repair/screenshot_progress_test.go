package repair

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

type screenshotProgressEvent struct {
	stage            string
	completed, total int
}

func TestScreenshotProgressCountsSuccessfulFramesAndConcurrentUploads(t *testing.T) {
	var events []screenshotProgressEvent
	input := ScreenshotGenerateInput{Progress: func(stage string, completed, total int) {
		events = append(events, screenshotProgressEvent{stage, completed, total})
	}}
	counter := startScreenshotProgress(input, 5)
	for i := 0; i < 3; i++ {
		counter.frameCaptured()
	}
	// An upload can succeed while captures are still running. Its count must not
	// turn the stage into uploading until the capture loop has finished.
	counter.frameUploaded()
	counter.startUploads(3)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.frameUploaded()
		}()
	}
	wg.Wait()
	want := []screenshotProgressEvent{
		{"capturing", 0, 5}, {"capturing", 1, 5}, {"capturing", 2, 5}, {"capturing", 3, 5},
		{"uploading", 1, 3}, {"uploading", 2, 3}, {"uploading", 3, 3},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("progress = %v, want %v", events, want)
	}
}

func TestOpaqueScreenshotProxyProgressUsesResultCounts(t *testing.T) {
	for _, tc := range []struct {
		name, mediaType, body string
		params                map[string]any
		want                  []screenshotProgressEvent
	}{
		{
			name:      "preview",
			mediaType: "screenshot_preview",
			body:      `{"success":true,"preview_candidates":[{},{},{},{},{}]}`,
			want:      []screenshotProgressEvent{{"locating", 0, 0}, {"remote_capture", -1, 12}, {"capturing", 5, 12}},
		},
		{
			name:      "formal upload",
			mediaType: "screenshot_finalize",
			params:    map[string]any{"selected_times": []float64{20, 40, 60, 80, 100}},
			body:      `{"success":true,"bbcode":"[img]https://image.example.com/frame.png[/img]"}`,
			want:      []screenshotProgressEvent{{"locating", 0, 0}, {"remote", -1, 5}, {"uploading", 1, 5}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/media/screenshot" {
					t.Error("unexpected screenshot proxy request")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			payload := map[string]any{"type": tc.mediaType, "downloader_id": "fixture", "save_path": t.TempDir(), "torrent_name": "fixture.mkv"}
			for key, value := range tc.params {
				payload[key] = value
			}
			var events []screenshotProgressEvent
			result, status := MediaValidateEntry(payload, MediaValidateEntryDeps{
				GetRootConfig: func() map[string]any {
					return map[string]any{"downloaders": []any{map[string]any{
						"id": "fixture", "type": "qbittorrent", "host": server.URL, "use_proxy": true,
						"proxy_media_only": true, "proxy_token": "fixture-only", "proxy_port": server.Listener.Addr().(*net.TCPAddr).Port,
					}}}
				},
				ScreenshotProgress: func(stage string, completed, total int) {
					events = append(events, screenshotProgressEvent{stage, completed, total})
				},
			})
			if status != http.StatusOK || result["success"] != true {
				t.Fatalf("media validation returned %d, %v", status, result)
			}
			if !reflect.DeepEqual(events, tc.want) {
				t.Fatalf("progress = %v, want %v", events, tc.want)
			}
		})
	}
}
