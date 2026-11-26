package downloaderclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTjuptFeedRemoveNativeKeepsFiles(t *testing.T) {
	_, hash := tjuptMockTorrent()
	denied := errors.New("lease revoked")
	progress, partial := 1.0, .99
	upload, threshold, total := int64(20), int64(100), int64(1234)
	goodTrackers := `[{"url":"** [DHT] **","tier":-1,"status":0},{"url":"** [PeX] **","tier":-1,"status":0},{"url":"** [LSD] **","tier":-1,"status":0},{"url":"https://tracker.tjupt.org/announce","tier":0,"status":2}]`
	for _, tc := range []struct {
		name     string
		marker   string
		trackers string
		info     string
		mutate   func(*TaggedQBTorrent)
		result   string
		guard    error
		wantErr  error
		wantPost int32
	}{
		{name: "one native removal and exact missing readback", wantPost: 1},
		{name: "public label marker", marker: TjuptFeedDisplayTag, mutate: func(row *TaggedQBTorrent) { row.Tags = TjuptFeedDisplayTag }, wantPost: 1},
		{name: "public label wrong timestamp", marker: TjuptFeedDisplayTag, mutate: func(row *TaggedQBTorrent) { row.Tags = TjuptFeedDisplayTag; row.AddedOn++ }, wantErr: ErrTjuptFeedScope},
		{name: "same hash readded after deletion request", result: "readded", wantPost: 1, wantErr: ErrTjuptFeedUnconfirmed},
		{name: "malformed readback", result: "invalid", wantPost: 1, wantErr: ErrTjuptFeedUnconfirmed},
		{name: "HTTP failure after write", result: "uncertain", wantPost: 1, wantErr: ErrTjuptFeedUnconfirmed},
		{name: "false success write response", result: "unexpected body", wantPost: 1, wantErr: ErrTjuptFeedUnconfirmed},
		{name: "revoked lease", guard: denied, wantErr: denied},
		{name: "duplicate exact hash from response", info: "duplicate", wantErr: ErrTjuptFeedScope},
		{name: "null info response", info: "null", wantErr: ErrTjuptFeedScope},
		{name: "hash not returned", info: "missing", wantErr: ErrTjuptFeedScope},
		{name: "unauthorized info query", info: "denied", wantErr: ErrTjuptFeedScope},
		{name: "other returned hash", mutate: func(row *TaggedQBTorrent) { row.Hash = strings.Repeat("0", 40) }, wantErr: ErrTjuptFeedScope},
		{name: "different task marker", mutate: func(row *TaggedQBTorrent) { row.Tags = "starpt-tjupt" }, wantErr: ErrTjuptFeedScope},
		{name: "no plugin label", mutate: func(row *TaggedQBTorrent) { row.Tags = tjuptMockMarker }, wantErr: ErrTjuptFeedScope},
		{name: "newer add date", mutate: func(row *TaggedQBTorrent) { row.AddedOn++ }, wantErr: ErrTjuptFeedScope},
		{name: "at speed threshold", mutate: func(row *TaggedQBTorrent) { row.UploadBPS = &threshold }, wantErr: ErrTjuptFeedScope},
		{name: "unknown upload speed", mutate: func(row *TaggedQBTorrent) { row.UploadBPS = nil }, wantErr: ErrTjuptFeedScope},
		{name: "unknown uploaded count", mutate: func(row *TaggedQBTorrent) { row.Uploaded = nil }, wantErr: ErrTjuptFeedScope},
		{name: "incomplete", mutate: func(row *TaggedQBTorrent) { row.Progress = &partial }, wantErr: ErrTjuptFeedScope},
		{name: "paused", mutate: func(row *TaggedQBTorrent) { row.State = "stoppedUP" }, wantErr: ErrTjuptFeedScope},
		{name: "current tracker outside site", mutate: func(row *TaggedQBTorrent) { row.Tracker = "https://other.test/announce" }, wantErr: ErrTjuptFeedScope},
		{name: "full tracker list changes", trackers: `[{"url":"https://tjupt.org/announce"},{"url":"https://tjupt.org.other.test/announce"}]`, wantErr: ErrTjuptFeedScope},
		{name: "tracker list only synthetic", trackers: `[{"url":"** [DHT] **","tier":-1,"status":0}]`, wantErr: ErrTjuptFeedScope},
		{name: "enabled discovery", trackers: `[{"url":"** [PeX] **","tier":-1,"status":2},{"url":"https://tjupt.org/announce"}]`, wantErr: ErrTjuptFeedScope},
		{name: "ambiguous discovery", trackers: `[{"url":"** [PeX] **","tier":-1},{"url":"https://tjupt.org/announce"}]`, wantErr: ErrTjuptFeedScope},
		{name: "unknown tracker list", trackers: "null", wantErr: ErrTjuptFeedScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := TaggedQBTorrent{
				Hash: hash, State: "stalledUP", AddedOn: tjuptMockAddedOn,
				Progress: &progress, UploadBPS: &upload, Uploaded: &total,
				Tags: "chosen, starpt-tjupt, " + tjuptMockMarker,
			}
			if tc.mutate != nil {
				tc.mutate(&row)
			}
			var writes, infoReads, trackerReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer fake-test-api-key" {
					t.Errorf("missing synthetic bearer authorization")
				}
				switch req.URL.Path {
				case "/api/v2/torrents/trackers":
					trackerReads.Add(1)
					if req.Method != http.MethodGet || req.URL.Query().Get("hash") != hash {
						t.Errorf("tracker lookup must match exact hash: %s %s", req.Method, req.URL)
					}
					response := tc.trackers
					if response == "" {
						response = goodTrackers
					}
					_, _ = io.WriteString(w, response)
				case "/api/v2/torrents/info":
					infoReads.Add(1)
					query := req.URL.Query()
					if req.Method != http.MethodGet || query.Get("hashes") != hash || query.Has("tag") || query.Get("limit") != "2" {
						t.Errorf("info must look up the exact hash without filtering by tag: %s %s", req.Method, req.URL)
					}
					if writes.Load() != 0 {
						switch tc.result {
						case "readded":
							newer := row
							newer.AddedOn++
							_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{newer})
						case "invalid":
							_, _ = io.WriteString(w, "null")
						default:
							_, _ = io.WriteString(w, "[]")
						}
						return
					}
					switch tc.info {
					case "duplicate":
						_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{row, row})
					case "denied":
						w.WriteHeader(http.StatusForbidden)
					case "null":
						_, _ = io.WriteString(w, "null")
					case "missing":
						_, _ = io.WriteString(w, "[]")
					default:
						_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{row})
					}
				case "/api/v2/torrents/delete":
					writes.Add(1)
					formErr := req.ParseForm()
					if req.Method != http.MethodPost || formErr != nil || req.PostForm.Get("hashes") != hash ||
						req.PostForm.Get("deleteFiles") != "false" || len(req.PostForm) != 2 {
						t.Errorf("delete must preserve files and affect exactly one hash: %s %+v %v", req.Method, req.PostForm, formErr)
					}
					if trackerReads.Load() != 1 || infoReads.Load() != 1 {
						t.Errorf("delete attempted without native tracker and torrent readbacks")
					}
					switch tc.result {
					case "uncertain":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "unexpected body":
						_, _ = io.WriteString(w, "unexpected")
					default:
						w.WriteHeader(http.StatusNoContent)
					}
				default:
					t.Errorf("unexpected operation: %s %s", req.Method, req.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-test-api-key"}
			client = client.WithEffectGuard(func() error { return tc.guard })
			marker := tc.marker
			if marker == "" {
				marker = tjuptMockMarker
			}
			err := client.RemoveTjuptFeedTorrent(context.Background(), strings.ToUpper(hash), tjuptMockAddedOn, marker, threshold)
			if !errors.Is(err, tc.wantErr) || writes.Load() != tc.wantPost {
				t.Fatalf("got error %v and %d writes, want %v and %d writes", err, writes.Load(), tc.wantErr, tc.wantPost)
			}
			if err == nil && infoReads.Load() != 2 {
				t.Fatalf("expected an exact absent readback after the only POST, got %d reads", infoReads.Load())
			}
		})
	}
}
