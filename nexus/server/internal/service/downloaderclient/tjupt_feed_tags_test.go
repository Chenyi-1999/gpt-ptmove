package downloaderclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTjuptDisplayTagAddKeepsLegacyCallersCompatible(t *testing.T) {
	_, hash := tjuptMockTorrent()
	for _, tc := range []struct {
		name string
		tags []string
		want string
	}{
		{name: "legacy", tags: []string{tjuptMockMarker}, want: "starpt-tjupt," + tjuptMockMarker},
		{name: "public", tags: []string{TjuptFeedDisplayTag, tjuptMockMarker, "custom"}, want: TjuptFeedDisplayTag + ",custom," + tjuptMockMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var added atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					if added.Load() == 0 {
						_, _ = w.Write([]byte("[]"))
						return
					}
					_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{{Hash: hash, Tags: tc.want, AddedOn: tjuptMockAddedOn, State: "downloading"}})
				case "/api/v2/torrents/tags":
					_ = json.NewEncoder(w).Encode([]string{tjuptMockMarker})
				case "/api/v2/torrents/add":
					added.Add(1)
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					got := compactStrings(strings.Split(r.FormValue("tags"), ","))
					if !sameStrings(got, strings.Split(tc.want, ",")) {
						t.Errorf("added tags = %q", got)
					}
					w.Write([]byte("Ok."))
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			}))
			defer server.Close()
			data, _ := tjuptMockTorrent()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-test-api-key"}
			_, err := d.AddTjuptFeedTorrent(context.Background(), data, "x.torrent", "", false, tc.tags, "", hash, 0)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTjuptDisplayTagOwnershipAndMigration(t *testing.T) {
	_, hash := tjuptMockTorrent()
	for _, tc := range []struct {
		name, tags, marker string
		want               bool
	}{
		{name: "common without legacy scope", tags: "北洋园," + tjuptMockMarker, marker: "北洋园", want: true},
		{name: "old marker with common", tags: "北洋园," + tjuptMockMarker, marker: tjuptMockMarker, want: true},
		{name: "old marker with legacy", tags: "starpt-tjupt," + tjuptMockMarker, marker: tjuptMockMarker, want: true},
		{name: "legacy marker without either scope", tags: tjuptMockMarker, marker: tjuptMockMarker},
	} {
		row := TaggedQBTorrent{Hash: hash, AddedOn: tjuptMockAddedOn, Tags: tc.tags, Category: tjuptMockCategory}
		if got := row.tjuptFeedOwned(tjuptMockAddedOn, tc.marker); got != tc.want {
			t.Errorf("%s ownership = %v", tc.name, got)
		}
		resumeTags := []string{tc.marker}
		if tc.marker == tjuptMockMarker {
			resumeTags = append(resumeTags, tjuptFeedScopeTag)
		}
		if got := row.matchesTjuptFeedScope(tjuptMockCategory, resumeTags, tjuptMockAddedOn); got != tc.want {
			t.Errorf("%s resume scope = %v", tc.name, got)
		}
	}

	var currentTags = "starpt-tjupt," + tjuptMockMarker + ",custom"
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			_ = json.NewEncoder(w).Encode([]TaggedQBTorrent{{Hash: hash, AddedOn: tjuptMockAddedOn, Tags: currentTags}})
		case "/api/v2/torrents/tags":
			_ = json.NewEncoder(w).Encode([]string{tjuptMockMarker})
		case "/api/v2/torrents/addTags":
			posts.Add(1)
			_ = r.ParseForm()
			if r.Form.Get("tags") != "北洋园" {
				t.Error("unexpected add tag")
			}
			currentTags += ",北洋园"
			w.Write([]byte("Ok."))
		case "/api/v2/torrents/removeTags":
			posts.Add(1)
			_ = r.ParseForm()
			if r.Form.Get("tags") != tjuptMockMarker+",starpt-tjupt" {
				t.Errorf("unexpected removal %q", r.Form.Get("tags"))
			}
			currentTags = "北洋园,custom"
			w.Write([]byte("Ok."))
		default:
			t.Errorf("unexpected URL %s", r.URL)
		}
	}))
	defer server.Close()
	d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-test-api-key"}
	if err := d.EnsureTjuptFeedDisplayTag(context.Background(), hash, tjuptMockAddedOn, tjuptMockMarker); err != nil {
		t.Fatal(err)
	}
	if currentTags != "starpt-tjupt,"+tjuptMockMarker+",custom,北洋园" {
		t.Fatalf("tags after ensure = %s", currentTags)
	}
	if err := d.RemoveTjuptFeedLegacyTags(context.Background(), hash, tjuptMockAddedOn, tjuptMockMarker); err != nil {
		t.Fatal(err)
	}
	if currentTags != "北洋园,custom" || posts.Load() != 2 {
		t.Fatalf("migration result tags=%s writes=%d", currentTags, posts.Load())
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for _, value := range left {
		if !slices.Contains(right, value) {
			return false
		}
	}
	return true
}

func TestPruneTjuptMarkerRequiresEmptyReadbackAndGuard(t *testing.T) {
	marker := tjuptMockMarker
	for _, tc := range []struct {
		name, rows string
		guard      error
		wantWrites int32
	}{
		{name: "empty marker", rows: "[]", wantWrites: 1},
		{name: "marker still in use", rows: "[{}]"},
		{name: "query unknown", rows: "null"},
		{name: "guard denied", rows: "[]", guard: errors.New("fenced")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/torrents/info":
					reads.Add(1)
					if r.URL.Query().Get("tag") != marker {
						t.Error("query not scoped to marker")
					}
					w.Write([]byte(tc.rows))
				case "/api/v2/torrents/tags":
					if writes.Load() == 0 {
						_ = json.NewEncoder(w).Encode([]string{marker})
					} else {
						_ = json.NewEncoder(w).Encode([]string{})
					}
				case "/api/v2/torrents/deleteTags":
					writes.Add(1)
					_ = r.ParseForm()
					if r.Form.Get("tags") != marker {
						t.Error("delete tag not exact")
					}
					w.Write([]byte("Ok."))
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "fake-test-api-key"}.WithEffectGuard(func() error { return tc.guard })
			err := d.PruneEmptyTjuptFeedMarker(context.Background(), marker)
			if writes.Load() != tc.wantWrites {
				t.Errorf("writes=%d", writes.Load())
			}
			if (tc.guard != nil && !errors.Is(err, tc.guard)) || (tc.rows != "[]" && err == nil) || (tc.rows == "[]" && tc.guard == nil && err != nil) {
				t.Fatalf("prune result %v", err)
			}
			if tc.wantWrites == 1 && reads.Load() != 1 {
				t.Errorf("tag-filter reads=%d", reads.Load())
			}
		})
	}
}
