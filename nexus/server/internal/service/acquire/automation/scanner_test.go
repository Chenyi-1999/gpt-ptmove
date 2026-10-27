package automation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func testProfile(base string) Profile {
	p := Profile{BaseURL: base}
	p.Paths.Listing = "/torrents.php"
	p.Paths.Detail = "/details.php?id={id}"
	p.Paths.Download = "/download.php?id={id}"
	p.Paths.Search = "/torrents.php?search={query}"
	p.Selectors.Title = "h1"
	p.Selectors.DetailLink = `a[href*="details.php?id="]`
	p.Selectors.DownloadLink = `a[href*="download.php"]`
	return p
}
func TestReadOnlyScanMetadataAndExplicitEmptyEvidence(t *testing.T) {
	source := testProfile("https://source.example.com")
	target := testProfile("https://target.example.com")
	requests := 0
	s := NewScanner()
	s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != "GET" {
			t.Fatalf("preview performed mutation %s", req.Method)
		}
		body := ""
		switch {
		case req.URL.Host == "target.example.com":
			body = "<html><p>没有找到相关种子</p></html>"
		case req.URL.Path == "/details.php":
			body = `<h1>Movie-GROUP</h1><div id="kdescr">影片介绍</div><table><tr><td>基本信息 做种者: 5 下载者: 2</td></tr></table><a href="/download.php?id=1">download</a>`
		case req.URL.Path == "/download.php":
			body = "d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name4:test12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee"
		default:
			body = `<table><a href="/details.php?id=1">Movie</a></table>`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	settings := policy.Defaults()
	rows, err := s.Scan(context.Background(), source, target, "source-secret", "target-secret", "", settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Eligible || rows[0].Size != policy.GiB || rows[0].Seeders != 5 || rows[0].Leechers != 2 {
		t.Fatalf("unexpected candidates %+v", rows)
	}
	if requests != 4 {
		t.Fatalf("requests=%d", requests)
	}
	raw, _ := json.Marshal(rows)
	for _, secret := range []string{"source-secret", "target-secret", "d4:info", "DetailURL"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("preview leaked internal data %s", secret)
		}
	}
}
func TestGlobalRestrictionOverridesCustomAllow(t *testing.T) {
	p := testProfile("https://source.example.com")
	p.Selectors.RestrictionAllowed = "#allow"
	for _, term := range []string{"禁转", "禁止轉載", "Do not reupload"} {
		doc, _ := html.Parse(strings.NewReader(`<div id="allow">允许</div><div class="outside">` + term + `</div>`))
		known, allowed := Restrictions(doc, p)
		if !known || allowed {
			t.Fatalf("global restriction missed %q", term)
		}
	}
}
func TestUnknownPagesAndConflictingDuplicateEvidenceFailClosed(t *testing.T) {
	p := testProfile("https://target.example.com")
	for _, body := range []string{`<h1>Sign in</h1><input type="password">`, `<h1>unexpected maintenance page</h1>`, `<p>没有找到相关种子</p><a href="details.php?id=1">existing</a>`} {
		doc, _ := html.Parse(strings.NewReader(body))
		known, _ := DuplicateEvidence(doc, p)
		if known {
			t.Fatalf("unknown or contradictory page accepted: %s", body)
		}
	}
	doc, _ := html.Parse(strings.NewReader(`<h1>detail</h1>`))
	known, allowed := Restrictions(doc, p)
	if known || allowed {
		t.Fatal("unrecognized detail granted transfer")
	}
}
func TestCSSSubsetAndUnknownSyntax(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<table class="torrents"><tr><td><a class="detail official" href="details.php?id=3">link</a></td></tr></table>`))
	if n := len(Select(doc, `table.torrents > tbody tr a.detail[href*="id="]:not(.hidden)`)); n != 1 {
		t.Fatalf("selector got %d", n)
	}
	if len(Select(doc, `a:unsupported(foo)`)) != 0 {
		t.Fatal("unknown selector broadened match")
	}
	if len(Select(doc, "")) != 0 {
		t.Fatal("empty selector matched")
	}
}
func TestReconcileURLRejectsOtherOriginAndSecrets(t *testing.T) {
	p := testProfile("https://target.example.com")
	for _, u := range []string{"https://source.example.com/details.php?id=1", "https://target.example.com/details.php?id=1&passkey=secret", "https://target.example.com/login.php"} {
		if _, err := ValidateDetailURL(p, u); err == nil {
			t.Fatalf("unsafe reconcile URL accepted: %s", u)
		}
	}
}
