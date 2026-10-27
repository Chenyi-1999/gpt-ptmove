package uploader

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type automationUploadTransport func(*http.Request) (*http.Response, error)

func (f automationUploadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestUploadEffectGuardRunsImmediatelyBeforePOST 验证关闭开关后真实写请求未发送，恢复授权后一次调用只有一个POST。
func TestUploadEffectGuardRunsImmediatelyBeforePOST(t *testing.T) {
	previous := uploadHTTPClientFactory
	t.Cleanup(func() { uploadHTTPClientFactory = previous })
	requests := 0
	guards := 0
	enabled := false
	stopped := errors.New("module stopped")
	uploadHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: automationUploadTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != "POST" {
				t.Fatalf("unexpected method %s", req.Method)
			}
			return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Location": []string{"/details.php?id=123"}}, Body: io.NopCloser(strings.NewReader("uploaded"))}, nil
		})}
	}
	guard := func() error {
		guards++
		if !enabled {
			return stopped
		}
		return nil
	}
	_, _, _, err := TryUploadTorrent("https://target.example.com/takeupload.php", "https://target.example.com", "fixture-cookie", "file", []byte("fixture-torrent"), "task.torrent", map[string]string{"name": "fixture"}, guard)
	if !errors.Is(err, stopped) || requests != 0 || guards != 1 {
		t.Fatalf("guard did not fence POST requests=%d guards=%d err=%v", requests, guards, err)
	}
	enabled = true
	if _, _, _, err = TryUploadTorrent("https://target.example.com/takeupload.php", "https://target.example.com", "fixture-cookie", "file", []byte("fixture-torrent"), "task.torrent", map[string]string{"name": "fixture"}, guard); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || guards != 2 {
		t.Fatalf("unexpected write count requests=%d guards=%d", requests, guards)
	}
}
