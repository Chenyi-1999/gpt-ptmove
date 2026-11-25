package tjupt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestRSSPreviewAndAllowlist(t *testing.T) {
	c := New()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(200, `<rss><channel><item><title>Film</title><link>https://tjupt.org/details.php?id=42</link><description>Size 9.8 GiB</description><enclosure length="99999999999"/></item><item><title>unknown</title><link>https://tjupt.org/details.php?id=43</link><enclosure length="100000"/></item></channel></rss>`), nil
	})
	rows, err := c.Preview(context.Background(), "https://tjupt.org/rss.php?passkey=hidden", 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("Preview = %v, %v", rows, err)
	}
	if !rows[0].Eligible || rows[0].AssessmentHour == nil || *rows[0].AssessmentHour != 24 || rows[0].DetailURL != "https://tjupt.org/details.php?id=42" {
		t.Fatalf("bad candidate: %+v", rows[0])
	}
	if rows[1].SizeBytes != nil || rows[1].Eligible {
		t.Fatalf("enclosure length treated as file size: %+v", rows[1])
	}
	for _, bad := range []string{"http://tjupt.org/rss.php", "https://example.org/rss.php", "https://tjupt.org.evil/rss.php", "https://tjupt.org/details.php", "https://user@tjupt.org/rss.php"} {
		if _, err := c.Preview(context.Background(), bad, 10); err == nil {
			t.Fatalf("accepted URL %s", bad)
		}
	}
}

func TestHNRStatusRequiresExplicitSafeStatusAndIdentity(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	c := New()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		return response(200, `{"data":[{"id":1,"torrent":{"tid":2,"info_hash":"`+hash+`"},"status":{"hnr_status_code":20,"need_seed_time":86400},"statistics":{"seed_time":3,"leech_time":0},"timestamps":{"added_at":"2024-01-02 03:04:05"}},{"id":3,"torrent":{"tid":4,"info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"status":{"hnr_status_code":1}}]}`), nil
	})
	rows, err := c.Status(context.Background(), "secret-token", []string{hash})
	if err != nil || !rows[hash].CanRetire || rows[hash].AddedAt.IsZero() || rows[hash].AddedAt.Location().String() != "北洋园" {
		t.Fatalf("status = %+v, %v", rows, err)
	}
	if rows[hash].RecordID != 1 || rows[hash].TorrentID != 2 {
		t.Fatalf("考核记录必须保留站点内部标识: %#v", rows[hash])
	}
	for _, code := range []string{"0", "1", "5", "6", "10", "11", "12", "43", "44", "999"} {
		body := `{"data":[{"id":1,"torrent":{"tid":2,"info_hash":"` + hash + `"},"status":{"hnr_status_code":` + code + `}}]}`
		c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		rows, err = c.Status(context.Background(), "t", []string{hash})
		if err != nil || rows[hash].CanRetire {
			t.Fatalf("unsafe code %s was allowed", code)
		}
	}
}

func TestHNRDuplicatesMalformedAndAddedAt(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	for _, body := range []string{`{`, `{"data":[{"id":1,"torrent":{"tid":2,"info_hash":"` + hash + `"},"status":{"hnr_status_code":20}},{"id":2,"torrent":{"tid":3,"info_hash":"` + hash + `"},"status":{"hnr_status_code":20}}]}`} {
		c := New()
		c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		rows, err := c.Status(context.Background(), "t", []string{hash})
		if err == nil && rows[hash].CanRetire {
			t.Fatal("malformed or ambiguous response allowed retirement")
		}
	}
	if got := parseAddedAt("2024-01-02T03:04:05Z"); !got.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatal(got)
	}
}

func TestHashAndHost(t *testing.T) {
	if !ValidHash(strings.Repeat("a", 40)) || ValidHash(strings.Repeat("a", 39)) || !TrackerHost("a.tjupt.org") || TrackerHost("tjupt.org.evil") {
		t.Fatal("validation mismatch")
	}
}

func TestAutoCandidatesAndDownloadValidatePayload(t *testing.T) {
	rss := `<rss><channel><item><title>[9.8 GiB] Film</title><link>https://tjupt.org/details.php?id=42</link><guid>https://tjupt.org/torrents.php?id=42</guid><enclosure url="https://tjupt.org/download.php?id=42&amp;passkey=secret" length="99999999"/></item><item><title>external</title><link>https://tjupt.org/details.php?id=43</link><guid>https://tjupt.org/torrents.php?id=43</guid><enclosure url="https://evil.test/download.php?id=43&amp;passkey=secret"/></item></channel></rss>`
	valid := `d8:announce34:https://tracker.tjupt.org/announce4:infod6:lengthi5e4:name4:test6:pieces20:abcdefghijklmnopqrstee`
	if _, err := parseTorrent([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	c := New()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/rss.php" {
			return response(200, rss), nil
		}
		if r.URL.Path == "/download.php" {
			if r.URL.Query().Get("passkey") != "secret" || r.URL.Query().Get("id") != "42" || r.URL.Query().Get("linktype") != "" {
				t.Fatal("download request did not preserve a scoped feed link")
			}
			return response(200, valid), nil
		}
		t.Fatalf("unexpected route %s", r.URL)
		return nil, nil
	})
	rows, err := c.AutoCandidates(context.Background(), "https://tjupt.org/rss.php?passkey=feed-secret")
	if err != nil || len(rows) != 1 {
		t.Fatalf("AutoCandidates = %v, %v", rows, err)
	}
	if strings.Contains(rows[0].Title, "secret") || rows[0].download == "" {
		t.Fatalf("candidate leaks or lacks private link: %+v", rows[0])
	}
	encoded, _ := json.Marshal(rows[0])
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("candidate JSON exposes the RSS passkey")
	}
	torrent, err := c.DownloadCandidate(context.Background(), rows[0])
	if err != nil || torrent.Hash == "" || torrent.SizeBytes != 5 || len(torrent.Data) == 0 {
		t.Fatalf("download = %+v, %v", torrent, err)
	}
}

func TestDownloadedTorrentRejectsForeignTrackerAndInvalidLink(t *testing.T) {
	bad := `d8:announce26:https://evil.test/announce4:infod6:lengthi5e4:name4:test6:pieces20:abcdefghijklmnopqrstee`
	c := New()
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, bad), nil })
	candidate := AutoCandidate{ID: "42", download: "https://tjupt.org/download.php?id=42&passkey=secret&linktype=dl"}
	if _, err := c.DownloadCandidate(context.Background(), candidate); err == nil {
		t.Fatal("accepted foreign announce")
	}
	for _, bad := range []string{
		"https://tjupt.org/download.php?id=43&passkey=secret",
		"https://tjupt.org/download.php?id=42",
		"http://tjupt.org/download.php?id=42&passkey=secret",
		"https://tjupt.org/download.php?id=42&passkey=secret&linktype=upload",
		"https://tjupt.org/download.php?id=42&passkey=secret&next=https://evil.test",
	} {
		candidate.download = bad
		if _, err := c.DownloadCandidate(context.Background(), candidate); err == nil {
			t.Fatalf("accepted an invalid candidate URL %q", bad)
		}
	}
}
