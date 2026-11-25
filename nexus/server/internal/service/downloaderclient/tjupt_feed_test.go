package downloaderclient

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	tjuptMockCategory = "北洋园/刷流"
	tjuptMockMarker   = "starpt-tjupt-trial-123456789abcdef012345678"
	tjuptMockAddedOn  = 1795646798
)

func tjuptMockTorrent() ([]byte, string) {
	info := "d6:lengthi1024e4:name8:test.mkve"
	hash := sha1.Sum([]byte(info))
	return []byte("d4:info" + info + "e"), hex.EncodeToString(hash[:])
}

func tjuptMockTorrents(w http.ResponseWriter, hash, state, tags, category string) {
	_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{
		{Hash: hash, State: state, Tags: tags, Category: category, AddedOn: tjuptMockAddedOn},
	})
}

func TestTjuptFeedNativeAdd(t *testing.T) {
	for _, tc := range []struct {
		name            string
		trial           bool
		legacy          bool
		outcome         string
		uploadLimitMBps int
		wantUnconfirmed bool
		wantReadback    bool
	}{
		{name: "qB 5.3.1 stopped, JSON ack", trial: true, wantReadback: true},
		{name: "qB 4.x paused, fixed ack", trial: true, legacy: true, wantReadback: true},
		{name: "qB 5.3.1 normal download", wantReadback: true},
		{name: "positive upload cap", trial: true, uploadLimitMBps: 2, wantReadback: true},
		{name: "upload cap readback mismatch", uploadLimitMBps: 2, outcome: "wrong upload limit", wantUnconfirmed: true, wantReadback: true},
		{name: "upload cap disabled", uploadLimitMBps: 0, wantReadback: true},
		{name: "native ack for another hash", outcome: "wrong ack", wantUnconfirmed: true},
		{name: "legacy negative ack", trial: true, legacy: true, outcome: "bad ack", wantUnconfirmed: true},
		{name: "trial returns downloading", trial: true, outcome: "wrong state", wantUnconfirmed: true, wantReadback: true},
		{name: "readback wrong category", trial: true, outcome: "wrong category", wantUnconfirmed: true, wantReadback: true},
		{name: "readback missing marker", trial: true, outcome: "wrong tags", wantUnconfirmed: true, wantReadback: true},
		{name: "readback wrong hash", trial: true, outcome: "wrong hash", wantUnconfirmed: true, wantReadback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, hash := tjuptMockTorrent()
			otherHash := strings.Repeat("a", 40)
			var posts, reads atomic.Int32
			var replyState = "queuedDL"
			if tc.trial {
				replyState = "stoppedDL"
				if tc.legacy {
					replyState = "pausedDL"
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fake-tjupt-token" {
					t.Error("qB request missing native Bearer authentication")
					http.Error(w, "auth", http.StatusForbidden)
					return
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /api/v2/torrents/info":
					reads.Add(1)
					if q := r.URL.Query(); q.Get("hashes") != hash || q.Has("tag") {
						t.Error("preflight or readback was not exact, unscoped hash lookup")
					}
					if posts.Load() == 0 {
						_, _ = io.WriteString(w, "[]")
						return
					}
					state, category, tags, returnedHash := replyState, tjuptMockCategory, "starpt-tjupt, seeding, "+tjuptMockMarker, hash
					upLimit := int64(tc.uploadLimitMBps * 1024 * 1024)
					switch tc.outcome {
					case "wrong state":
						state = "downloading"
					case "wrong category":
						category = "Other"
					case "wrong tags":
						tags = "starpt-tjupt, seeding"
					case "wrong hash":
						returnedHash = otherHash
					case "wrong upload limit":
						upLimit++
					}
					_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{{Hash: returnedHash, State: state, Tags: tags, Category: category, AddedOn: tjuptMockAddedOn, UpLimit: &upLimit}})
				case "POST /api/v2/torrents/add":
					posts.Add(1)
					pausedFlag := fmt.Sprint(tc.trial)
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Errorf("multipart form: %v", err)
						http.Error(w, "form", http.StatusBadRequest)
						return
					}
					for key, want := range map[string]string{
						"paused": pausedFlag, "stopped": pausedFlag, "skip_checking": "false", "seedMode": "false",
						"category": tjuptMockCategory, "tags": "starpt-tjupt,seeding," + tjuptMockMarker,
					} {
						if got := r.MultipartForm.Value[key]; len(got) != 1 || got[0] != want {
							t.Errorf("%s = %q, want %q", key, got, want)
						}
					}
					wantUpLimit := ""
					if tc.uploadLimitMBps > 0 {
						wantUpLimit = fmt.Sprint(tc.uploadLimitMBps * 1024 * 1024)
					}
					if got := r.MultipartForm.Value["upLimit"]; wantUpLimit == "" && len(got) != 0 || wantUpLimit != "" && (len(got) != 1 || got[0] != wantUpLimit) {
						t.Errorf("upLimit = %q, want %q (empty means omitted)", got, wantUpLimit)
					}
					if r.FormValue("savepath") != "" || r.FormValue("autoTMM") != "" {
						t.Error("plugin should allow the selected category to determine its save path")
					}
					file, header, err := r.FormFile("torrents")
					if err != nil {
						t.Errorf("multipart torrent: %v", err)
						return
					}
					defer file.Close()
					got, err := io.ReadAll(file)
					if err != nil || string(got) != string(meta) || header.Filename != "fixture.torrent" {
						t.Error("multipart upload does not match the synthetic fixture")
					}
					if tc.legacy {
						reply := "Ok."
						if tc.outcome == "bad ack" {
							reply = "Fails."
						}
						_, _ = io.WriteString(w, reply)
						return
					}
					resultHash := hash
					if tc.outcome == "wrong ack" {
						resultHash = otherHash
					}
					_, _ = fmt.Fprintf(w, `{"success_count":1,"failure_count":0,"pending_count":0,"added_torrent_ids":[%q]}`, resultHash)
				default:
					t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-tjupt-token"}
			result, err := d.AddTjuptFeedTorrent(context.Background(), meta, "fixture.torrent", "", tc.trial, []string{"seeding", tjuptMockMarker, "starpt-tjupt"}, tjuptMockCategory, strings.ToUpper(hash), tc.uploadLimitMBps)
			if tc.wantUnconfirmed {
				if !errors.Is(err, ErrTjuptFeedUnconfirmed) || result != nil {
					t.Errorf("result = %+v, err = %v, want unconfirmed", result, err)
				}
			} else if err != nil || result == nil || result.Hash != hash || result.State != replyState || result.AddedOn != tjuptMockAddedOn {
				t.Errorf("result = %+v, err = %v, expected confirmed readback", result, err)
			}
			wantReads := int32(1)
			if tc.wantReadback {
				wantReads++
			}
			if posts.Load() != 1 || reads.Load() != wantReads {
				t.Fatalf("write/read counts = %d/%d, want 1/%d", posts.Load(), reads.Load(), wantReads)
			}
		})
	}
}

func TestTjuptFeedRejectsWritesBeforeDispatch(t *testing.T) {
	meta, hash := tjuptMockTorrent()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/torrents/info" {
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls.Load() == 1 {
			tjuptMockTorrents(w, hash, "downloading", "manual", "")
			return
		}
		_, _ = io.WriteString(w, "[]")
	}))
	defer server.Close()
	d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "unrelated-fixture-token"}
	if _, err := d.AddTjuptFeedTorrent(context.Background(), meta, "fixture.torrent", "", false, nil, "", strings.Repeat("a", 40), 0); err == nil || calls.Load() != 0 {
		t.Fatalf("invalid metainfo hash reached qB: %v", err)
	}
	if _, err := d.AddTjuptFeedTorrent(context.Background(), meta, "fixture.torrent", "", false, nil, "", hash, 0); !errors.Is(err, ErrTjuptFeedAlreadyExists) || calls.Load() != 1 {
		t.Fatalf("manual qB task did not block add: %v", err)
	}
	deny := errors.New("schedule disabled")
	guarded := d.WithEffectGuard(func() error { return deny })
	if _, err := guarded.AddTjuptFeedTorrent(context.Background(), meta, "fixture.torrent", "", false, nil, "", hash, 0); !errors.Is(err, deny) || calls.Load() != 2 {
		t.Fatalf("disabled schedule did not prevent write: %v", err)
	}
}

func TestTjuptFeedResumesOnlyScopedTrial(t *testing.T) {
	for _, tc := range []struct {
		name, state, tags, category, endpoint string
		addedOn                               int64
		progress                              float64
		failWrite                             bool
		wantErr                               error
	}{
		{name: "qB 5.3.1 start", state: "stoppedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, endpoint: "start", addedOn: tjuptMockAddedOn},
		{name: "qB 4.x resume", state: "pausedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, endpoint: "resume", addedOn: tjuptMockAddedOn},
		{name: "wrong marker", state: "stoppedDL", tags: "starpt-tjupt", category: tjuptMockCategory, addedOn: tjuptMockAddedOn, wantErr: ErrTjuptFeedScope},
		{name: "wrong timestamp", state: "stoppedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, addedOn: tjuptMockAddedOn - 5, wantErr: ErrTjuptFeedScope},
		{name: "wrong category", state: "stoppedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: "changed", addedOn: tjuptMockAddedOn, wantErr: ErrTjuptFeedScope},
		{name: "already running", state: "queuedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, addedOn: tjuptMockAddedOn, wantErr: ErrTjuptFeedScope},
		{name: "manually downloaded then stopped", state: "stoppedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, addedOn: tjuptMockAddedOn, progress: 0.25, wantErr: ErrTjuptFeedScope},
		{name: "unknown start result, no fallback", state: "stoppedDL", tags: "starpt-tjupt," + tjuptMockMarker, category: tjuptMockCategory, endpoint: "start", addedOn: tjuptMockAddedOn, failWrite: true, wantErr: ErrTjuptFeedUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, hash := tjuptMockTorrent()
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					if r.URL.Path != "/api/v2/torrents/info" || r.URL.Query().Get("hashes") != hash {
						t.Error("unexpected qB read path")
					}
					state := tc.state
					if writes.Load() != 0 {
						state = "downloading"
					}
					_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{
						{Hash: hash, State: state, Tags: tc.tags, Category: tc.category, AddedOn: tc.addedOn, Progress: &tc.progress},
					})
				case http.MethodPost:
					writes.Add(1)
					if tc.endpoint == "" || r.URL.Path != "/api/v2/torrents/"+tc.endpoint || r.FormValue("hashes") != hash {
						t.Errorf("unexpected qB mutation: %s", r.URL.Path)
					}
					if tc.failWrite {
						w.WriteHeader(http.StatusBadGateway)
					}
				default:
					t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-token"}
			torrent, err := d.ResumeTjuptFeedTorrent(context.Background(), hash, tjuptMockAddedOn, tjuptMockMarker, tjuptMockCategory)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && (torrent == nil || !torrent.TjuptFeedStarted())) {
				t.Fatalf("resume result = %+v, %v, want %v", torrent, err, tc.wantErr)
			}
			var wantWrites int32
			if tc.endpoint != "" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites {
				t.Fatalf("resume writes = %d, want %d", writes.Load(), wantWrites)
			}
		})
	}
}
