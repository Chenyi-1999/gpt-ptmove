package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit opt-in runs only synthetic media and a mocked official Pixhost transport.
func TestCaptureIntegration(t *testing.T) {
	if os.Getenv("PTMOVE_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set PTMOVE_CAPTURE_INTEGRATION=1 for installed-tool integration")
	}
	root := t.TempDir()
	video := filepath.Join(root, "Synthetic.S01E01.mkv")
	output, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24", "-t", "16", "-c:v", "mpeg4", video).CombinedOutput()
	if err != nil {
		t.Fatalf("synthetic video: %v %s", err, output)
	}
	duration, err := getVideoDuration(video)
	if err != nil || duration < 15 {
		t.Fatalf("duration=%v err=%v", duration, err)
	}
	imagePath := filepath.Join(root, "capture.png")
	if err := takeScreenshot(video, imagePath, 5, 0); err != nil {
		t.Fatal(err)
	}
	optimized := filepath.Join(root, "optimized.png")
	if err := convertPngToOptimizedPng(imagePath, optimized); err != nil {
		t.Fatal(err)
	}
	posts := 0
	image, err := uploadPixhostImage(optimized, mockPixhostClient(t, &posts))
	if err != nil || posts != 1 || !strings.Contains(image.ImageURL, "img99") {
		t.Fatalf("fake upload: %v", err)
	}
	raw, _ := json.Marshal([]string{root})
	policy, err := loadMediaPolicy(fixtureToken, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"remote_path": video, "mode": "preview", "preview_count": 5})
	req := httptest.NewRequest("POST", "/api/media/screenshot", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+fixtureToken)
	rec := httptest.NewRecorder()
	newMediaHandler(policy).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview status=%d: %s", rec.Code, rec.Body.String())
	}
	var result ScreenshotResponse
	if json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.PreviewCandidates) != 5 || result.BBCode != "" {
		t.Fatalf("invalid preview side effects: %s", rec.Body.String())
	}
}
