package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

func TestExecutePublishCannotBypassDownloaderGuard(t *testing.T) {
	previous := checkPublishDownloaderGate
	calledWith := ""
	checkPublishDownloaderGate = func(downloaderID string) (bool, string) {
		calledWith = downloaderID
		return false, "达到数量限制"
	}
	t.Cleanup(func() { checkPublishDownloaderGate = previous })

	result, status := ExecutePublish(PublishExecutionInput{
		TargetSite:        "target",
		TargetInfo:        map[string]any{"nickname": "Target"},
		UploadData:        map[string]any{"title": "Example"},
		SourceRestriction: SourceRestriction{Status: SourceRestrictionAllowed},
		Payload: map[string]any{
			"savePath":     "/downloads",
			"downloaderId": "qb-main",
		},
	}, PublishExecutionDeps{})

	if status != 200 || result["limit_reached"] != true {
		t.Fatalf("unexpected result: status=%d result=%#v", status, result)
	}
	if calledWith != "qb-main" {
		t.Fatalf("guard called with %q", calledWith)
	}
}

func TestPublishChecksSourceBeforePublisherAndFollowUp(t *testing.T) {
	previousGate, previousPublish := checkPublishDownloaderGate, publishToTarget
	t.Cleanup(func() {
		checkPublishDownloaderGate, publishToTarget = previousGate, previousPublish
	})
	checkPublishDownloaderGate = func(string) (bool, string) { return true, "" }
	raw := []byte("d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name4:test12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee")
	meta, err := acquirefetch.ParseTorrentMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.torrent")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, authorization, hash, status string
	}{
		{"denied", SourceRestrictionDenied, meta.InfoHash, SourceRestrictionDenied},
		{"missing source", "", meta.InfoHash, SourceRestrictionUnknown},
		{"mismatched bytes", SourceRestrictionAllowed, "wrong-hash", SourceRestrictionUnknown},
		{"matched bytes", SourceRestrictionAllowed, meta.InfoHash, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			uploads, adds := 0, 0
			publishToTarget = func(_ map[string]any, _ map[string]any, _ string, actual []byte, _ string, _ func(string) (string, error), _ ...func() error) (string, string, string, bool, map[string]string, error) {
				uploads++
				if !bytes.Equal(raw, actual) {
					t.Fatal("publisher did not receive the exact bytes checked")
				}
				return "https://target.example.com/details.php?id=2", "", "", true, nil, nil
			}
			input := PublishExecutionInput{
				TargetSite:        "target",
				TargetInfo:        map[string]any{"base_url": "https://target.example.com", "nickname": "target"},
				UploadData:        map[string]any{"tags": []string{}, "source_restriction_status": "allowed"},
				TorrentPath:       path,
				SourceTorrentHash: test.hash,
				SourceRestriction: SourceRestriction{Status: test.authorization},
				Payload: map[string]any{
					"savePath": "/downloads", "downloaderId": "test", "auto_add_to_downloader": true,
					"auto_update_existing_torrent": test.status != "", "source_restriction_status": "allowed",
					"_ptnexus_before_upload": "ignored",
				},
			}
			result, status := ExecutePublish(input, PublishExecutionDeps{AddToDownloader: func(map[string]any) (map[string]any, int) {
				adds++
				return map[string]any{"success": true}, 200
			}})
			if test.status != "" {
				if status != 409 || result["source_restriction_status"] != test.status || result["pre_check"] != true || uploads != 0 || adds != 0 {
					t.Fatalf("status=%d uploads=%d adds=%d result=%v", status, uploads, adds, result)
				}
			} else if status != 200 || result["is_existing_torrent"] != true || uploads != 1 || adds != 1 {
				t.Fatalf("status=%d uploads=%d adds=%d result=%v", status, uploads, adds, result)
			}
		})
	}
}
