package downloaderclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

const (
	duplicateTestHash        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	duplicateTestOtherHash   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	duplicateTestTask        = "auto-a4e87f99de0b40b2b9bb21d8b4f57a40"
	duplicateTestTag         = "starpt-" + duplicateTestTask
	duplicateTestDestination = "/nas/duplicates/" + duplicateTestTask
)

func TestAutomationSourceAddMarksOwnership(t *testing.T) {
	var writes atomic.Int32
	server := duplicateTestQB(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			fmt.Fprint(w, "[]")
		case "/api/v2/torrents/add":
			writes.Add(1)
			if err := r.ParseMultipartForm(64 << 10); err != nil {
				t.Errorf("parse source add: %v", err)
			} else if r.FormValue("tags") != duplicateTestTag || r.FormValue("autoTMM") != "false" {
				t.Errorf("source add lacks the ownership tag and safe path mode: %v", r.PostForm)
			}
			fmt.Fprint(w, "Ok.")
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	tracker := "https://127.0.0.1/announce"
	info := "d6:lengthi1e4:name4:test12:piece lengthi1e6:pieces20:012345678901234567897:privatei1ee"
	data := []byte(fmt.Sprintf("d8:announce%d:%s4:info%se", len(tracker), tracker, info))
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if err != nil {
		t.Fatal(err)
	}
	d := duplicateTestDownloader(server.URL).WithEffectGuard(func() error { return nil })
	if err := d.AddSourceTorrent(data, duplicateTestTask+".torrent", "/nas/data", meta.InfoHash); err != nil || writes.Load() != 1 {
		t.Fatalf("source add = %v, writes = %d", err, writes.Load())
	}
}

func TestAutomationDuplicateDeleteRequiresOwnershipAndKeepsFiles(t *testing.T) {
	for _, tt := range []struct {
		name       string
		tags       string
		savePath   string
		guarded    bool
		wantDelete bool
	}{
		{"owned and complete", "other, " + duplicateTestTag, "/nas/data/", true, true},
		{"legacy untagged", "other", "/nas/data", true, false},
		{"similar tag", duplicateTestTag + "-suffix", "/nas/data", true, false},
		{"moved by user", duplicateTestTag, "/nas/other", true, false},
		{"missing authorization", duplicateTestTag, "/nas/data", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var removed atomic.Bool
			server := duplicateTestQB(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					if r.URL.Query().Get("hashes") != duplicateTestHash {
						t.Errorf("unexpected hash filter %s", r.URL.RawQuery)
					}
					if removed.Load() {
						fmt.Fprint(w, "[]")
					} else {
						duplicateTestRespond(w, duplicateTestRow(duplicateTestHash, tt.savePath, tt.savePath+"/movie", "stalledUP", tt.tags))
					}
				case "/api/v2/torrents/delete":
					if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.PostForm.Get("hashes") != duplicateTestHash || r.PostForm.Get("deleteFiles") != "false" {
						t.Errorf("unsafe delete request %s %v %v", r.Method, r.PostForm, err)
					}
					removed.Store(true)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			d := duplicateTestDownloader(server.URL)
			if tt.guarded {
				d = d.WithEffectGuard(func() error { return nil })
			}
			err := d.HandleAutomationDuplicate(duplicateTestHash, duplicateTestTask, "/nas/data", "delete", "")
			if (err == nil) != tt.wantDelete || removed.Load() != tt.wantDelete {
				t.Fatalf("delete = %v, request sent = %v, want success and request %v", err, removed.Load(), tt.wantDelete)
			}
		})
	}
}

func TestAutomationDuplicateMoveIsPendingUntilReadOnlyVerification(t *testing.T) {
	var writes atomic.Int32
	var moved atomic.Bool
	server := duplicateTestQB(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			row := duplicateTestRow(duplicateTestHash, "/nas/data", "/nas/data/movie", "stalledUP", duplicateTestTag)
			if moved.Load() {
				row.SavePath = duplicateTestDestination + "/"
				row.ContentPath = duplicateTestDestination + "/movie"
			} else if writes.Load() > 0 {
				row.State = "moving"
			}
			if r.URL.Query().Get("hashes") == "" {
				duplicateTestRespond(w, row, duplicateTestRow(duplicateTestOtherHash, "/nas", "/nas/other-film", "stalledUP", ""))
			} else {
				duplicateTestRespond(w, row)
			}
		case "/api/v2/torrents/setLocation":
			writes.Add(1)
			if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.PostForm.Get("hashes") != duplicateTestHash || r.PostForm.Get("location") != duplicateTestDestination {
				t.Errorf("unsafe move request %s %v %v", r.Method, r.PostForm, err)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	d := duplicateTestDownloader(server.URL).WithEffectGuard(func() error { return nil })
	err := d.HandleAutomationDuplicate(duplicateTestHash, duplicateTestTask, "/nas/data/", "move", "/nas/duplicates/")
	if !errors.Is(err, ErrAutomationDuplicateCleanupPending) {
		t.Fatalf("move should be pending after qB HTTP acceptance: %v", err)
	}
	done, err := d.AutomationDuplicateCleanupComplete(duplicateTestHash, duplicateTestTask, "move", "/nas/duplicates")
	if err != nil || done {
		t.Fatalf("in-flight move reported complete: done=%v err=%v", done, err)
	}
	moved.Store(true)
	done, err = d.AutomationDuplicateCleanupComplete(duplicateTestHash, duplicateTestTask, "move", "/nas/duplicates")
	if err != nil || !done || writes.Load() != 1 {
		t.Fatalf("move verification = %v, err=%v, writes=%d", done, err, writes.Load())
	}
}

func TestAutomationDuplicateMoveRejectsConflictsAndRevokedLease(t *testing.T) {
	for _, path := range []string{"/nas/data/movie", duplicateTestDestination + "/movie/part01.mkv", ""} {
		t.Run(path, func(t *testing.T) {
			var writes atomic.Int32
			var authorized atomic.Bool
			authorized.Store(true)
			server := duplicateTestQB(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					row := duplicateTestRow(duplicateTestHash, "/nas/data", "/nas/data/movie", "stalledUP", duplicateTestTag)
					if r.URL.Query().Get("hashes") != "" {
						duplicateTestRespond(w, row)
					} else if path != "" {
						duplicateTestRespond(w, row, duplicateTestRow(duplicateTestOtherHash, "/nas", path, "stalledUP", ""))
					} else {
						authorized.Store(false)
						duplicateTestRespond(w, row)
					}
				default:
					writes.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
				}
			})
			d := duplicateTestDownloader(server.URL).WithEffectGuard(func() error {
				if !authorized.Load() {
					return errors.New("revoked")
				}
				return nil
			})
			if err := d.HandleAutomationDuplicate(duplicateTestHash, duplicateTestTask, "/nas/data", "move", "/nas/duplicates"); err == nil || writes.Load() != 0 {
				t.Fatalf("unsafe move = %v, writes = %d", err, writes.Load())
			}
		})
	}
}

func TestAutomationDuplicateInvalidDestination(t *testing.T) {
	for _, input := range []string{"relative", "/", "/nas/data/../other", "//host/share", "C:\\data", "/nas/symlink\x00", "/nas/ data\n", "/nas//data"} {
		if location := automationDuplicateDestination(input, duplicateTestTask); location != "" {
			t.Errorf("accepted invalid path %q as %q", input, location)
		}
	}
}

func duplicateTestQB(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("missing fixture authentication for %s", r.URL.Path)
		}
		if r.URL.Path == "/api/v2/app/version" {
			fmt.Fprint(w, "v5.2.1")
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func duplicateTestDownloader(host string) Downloader {
	return Downloader{Type: "qbittorrent", Host: host, APIKey: "fixture-key"}
}

func duplicateTestRespond(w http.ResponseWriter, rows ...automationDuplicateTorrent) {
	_ = json.NewEncoder(w).Encode(rows)
}

func duplicateTestRow(hash, savePath, contentPath, state, tags string) automationDuplicateTorrent {
	progress, outstanding, autoManaged := float64(1), int64(0), false
	return automationDuplicateTorrent{Hash: strings.ToLower(hash), SavePath: savePath, ContentPath: contentPath, State: state, Tags: tags, Progress: &progress, AmountLeft: &outstanding, AutoManaged: &autoManaged}
}
