package downloaderclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTjuptFeedTorrentsBatchesExactHashes(t *testing.T) {
	rows := tjuptFeedBatchFixture(130)
	hashes := make([]string, len(rows))
	for i, row := range rows {
		hashes[i] = strings.ToUpper(row.Hash)
	}
	const missing = 70
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		batch := int(calls.Add(1)) - 1
		query := r.URL.Query()
		first := batch * tjuptFeedHashBatchSize
		last := min(first+tjuptFeedHashBatchSize, len(rows))
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/torrents/info" ||
			first >= len(rows) || len(query) != 1 || r.Header.Get("Authorization") != "Bearer mock-qb" {
			t.Errorf("unexpected qB request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := query.Get("hashes"); got != strings.ToLower(strings.Join(hashes[first:last], "|")) {
			t.Error("qB did not receive the exact requested batch")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		matches := make([]TaggedQBTorrent, 0, last-first)
		for i := last - 1; i >= first; i-- {
			if i == missing {
				continue
			}
			row := rows[i]
			row.Hash = strings.ToUpper(row.Hash)
			matches = append(matches, row)
		}
		_ = json.NewEncoder(w).Encode(matches)
	}))
	defer server.Close()
	d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "mock-qb"}
	got, err := d.TjuptFeedTorrents(context.Background(), hashes)
	if err != nil || calls.Load() != 3 || len(got) != len(rows)-1 {
		t.Fatalf("batch lookup returned %d rows and %d requests, error %v", len(got), calls.Load(), err)
	}
	for i, want := range rows {
		row, found := got[want.Hash]
		if (i != missing) != found || (found && (!strings.EqualFold(row.Hash, want.Hash) || row.State != want.State)) {
			t.Fatalf("unexpected qB result for %s", want.Hash)
		}
	}
}

func TestTjuptFeedTorrentsRejectsInvalidSnapshots(t *testing.T) {
	rows := tjuptFeedBatchFixture(tjuptFeedHashBatchSize + 2)
	hashes := make([]string, len(rows))
	for i, row := range rows {
		hashes[i] = row.Hash
	}
	late := hashes[len(hashes)-1]
	for _, tc := range []struct {
		name, reply string
	}{
		{"null", "null"},
		{"malformed", "["},
		{"nonarray", "{}"},
		{"missing hash", "[{}]"},
		{"unknown hash", `[{"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`},
		{"hash from earlier batch", fmt.Sprintf(`[{"hash":%q}]`, hashes[0])},
		{"duplicate case-insensitive hash", fmt.Sprintf(`[{"hash":%q},{"hash":%q}]`, late, strings.ToUpper(late))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					_ = json.NewEncoder(w).Encode(rows[:1])
					return
				}
				_, _ = io.WriteString(w, tc.reply)
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "mock-qb"}
			got, err := d.TjuptFeedTorrents(context.Background(), hashes)
			if err == nil || got != nil || calls.Load() != 2 {
				t.Fatalf("invalid later batch returned %d rows in %d calls, error %v", len(got), calls.Load(), err)
			}
		})
	}
}

func TestTjuptFeedTorrentsRejectsInvalidInputBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "mock-qb"}
	hash := strings.Repeat("a", 40)
	for _, hashes := range [][]string{{hash, "bad"}, {hash, strings.ToUpper(hash)}} {
		if got, err := d.TjuptFeedTorrents(context.Background(), hashes); err == nil || got != nil {
			t.Fatal("invalid input was accepted")
		}
	}
	if got, err := d.TjuptFeedTorrents(context.Background(), nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty lookup returned %v, error %v", got, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("input validation made %d network requests", calls.Load())
	}
}

func TestTjuptFeedTaggedTorrentsPagesIncludingLegacyOffset(t *testing.T) {
	for _, count := range []int{0, 64, 129} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			rows := tjuptFeedBatchFixture(count)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset := (int(calls.Add(1)) - 1) * tjuptFeedHashBatchSize
				q := r.URL.Query()
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/torrents/info" ||
					q.Get("tag") != "starpt-tjupt" || q.Get("sort") != "hash" || q.Get("limit") != "65" ||
					q.Get("offset") != strconv.Itoa(offset) || len(q) != 4 {
					t.Errorf("unexpected qB pagination query %s %s", r.Method, r.URL.String())
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if offset >= len(rows) {
					offset = 0 // qB 4.6.7 returns the first page for an offset at the end.
				}
				_ = json.NewEncoder(w).Encode(rows[offset:min(offset+tjuptFeedHashBatchSize+1, len(rows))])
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "mock-qb"}
			got, err := d.TjuptFeedTaggedTorrents(context.Background(), "starpt-tjupt")
			wantCalls := max(1, (count+tjuptFeedHashBatchSize-1)/tjuptFeedHashBatchSize)
			if err != nil || !slices.Equal(got, rows) || int(calls.Load()) != wantCalls {
				t.Fatalf("tagged lookup returned %d rows in %d calls, want %d rows in %d calls, error %v",
					len(got), calls.Load(), count, wantCalls, err)
			}
		})
	}
}

func TestTjuptFeedTaggedTorrentsRejectsInconsistentPages(t *testing.T) {
	for _, kind := range []string{"repeated page", "shifted page", "duplicate hash", "wrong tag", "null page"} {
		t.Run(kind, func(t *testing.T) {
			rows := tjuptFeedBatchFixture(tjuptFeedHashBatchSize + 2)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					_ = json.NewEncoder(w).Encode(rows[:tjuptFeedHashBatchSize+1])
					return
				}
				page := slices.Clone(rows[tjuptFeedHashBatchSize:])
				switch kind {
				case "repeated page":
					page = rows[:tjuptFeedHashBatchSize+1]
				case "shifted page":
					page = page[1:]
				case "duplicate hash":
					page[1].Hash = strings.ToUpper(page[0].Hash)
				case "wrong tag":
					page[1].Tags = "not-starpt-tjupt"
				case "null page":
					page = nil
				}
				_ = json.NewEncoder(w).Encode(page)
			}))
			defer server.Close()
			d := Downloader{Type: "qbittorrent", Enabled: true, Host: server.URL, APIKey: "mock-qb"}
			got, err := d.TjuptFeedTaggedTorrents(context.Background(), "starpt-tjupt")
			if err == nil || got != nil || calls.Load() != 2 {
				t.Fatalf("invalid pagination returned %d rows in %d calls, error %v", len(got), calls.Load(), err)
			}
		})
	}
}

func tjuptFeedBatchFixture(count int) []TaggedQBTorrent {
	rows := make([]TaggedQBTorrent, count)
	for i := range rows {
		rows[i] = TaggedQBTorrent{
			Hash: fmt.Sprintf("%040x", i+1), State: "uploading", Tags: "manual, starpt-tjupt",
		}
	}
	return rows
}
