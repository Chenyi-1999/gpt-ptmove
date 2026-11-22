package automation

import (
	"context"
	"encoding/json"
	"fmt"
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
	source.OfficialGroups = []string{"GROUP"}
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

func TestZmptOfficialListingBoundsPagingAndEarlyExclusions(t *testing.T) {
	source := testProfile("https://zmpt.cc")
	source.OfficialGroups = []string{"ZmWeb"}
	source.AnnounceHosts = []string{"zmpt.cc"}
	target := testProfile("https://target.example.com")
	settings := policy.Defaults()
	settings.MinSizeGiB, settings.MaxSizeGiB = 6.25, 6.75
	settings.MonthlyLimit, settings.MinLeechers = 60, 1
	official := zmptTestTag("官方")
	inspectedTitles := map[string]string{"500": "New smaller release", "501": "New larger release", "502": "Out of Exact Bounds", "503": "Already on Target"}
	var requests []string
	s := NewScanner()
	s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Host+req.URL.RequestURI())
		if req.Method != http.MethodGet {
			t.Fatalf("preview performed mutation %s", req.Method)
		}
		id := req.URL.Query().Get("id")
		var body string
		switch {
		case req.URL.Host == "target.example.com":
			if req.URL.Query().Get("search") == "Already on Target" {
				body = `<a href="details.php?id=99">existing</a>`
			} else {
				body = `<p>没有找到相关种子</p>`
			}
		case req.URL.Path == "/torrents.php":
			q := req.URL.Query()
			for key, want := range map[string]string{"tag_id": "3", "sort": "5", "type": "desc", "size_begin": "6", "size_end": "7", "seeders_begin": "3", "leechers_begin": "1", "incldead": "1"} {
				if q.Get(key) != want {
					t.Errorf("listing query %s = %q, want %q", key, q.Get(key), want)
				}
			}
			var rows strings.Builder
			switch q.Get("page") {
			case "0":
				rows.WriteString(zmptTestRow("1", "Pinned-ZmWeb", `<div>官方</div>`))
				rows.WriteString(zmptTestRow("2", "Restricted", official+zmptTestTag("禁转")))
				rows.WriteString(zmptTestRow("3", "Single Episode", official+zmptTestTag("分集")))
				rows.WriteString(zmptTestRow("4", "Previously downloaded and removed from client", official+zmptTestBar("inactivity 100%")))
				for n := 10; n < 65; n++ {
					rows.WriteString(zmptTestRow(fmt.Sprint(n), "Previous download", official+zmptTestBar("seeding 100%")))
				}
			case "1":
				rows.WriteString(zmptTestRow("500", "New smaller release", official+`<img title="审核通过" style="color: green">`))
				rows.WriteString(zmptTestRow("501", "New larger release", official))
				rows.WriteString(zmptTestRow("502", "Out of Exact Bounds", official))
				rows.WriteString(zmptTestRow("503", "Already on Target", official))
			}
			body = `<a href="torrents.php?tag_id=3">官方</a><table class="torrents">` + rows.String() + `</table>`
		case req.URL.Path == "/details.php" && id >= "500" && id <= "503":
			body = zmptTestDetail(id, inspectedTitles[id])
		case req.URL.Path == "/download.php" && id >= "500" && id <= "503":
			size := map[string]int64{"500": 13 * policy.GiB / 2, "501": 53 * policy.GiB / 8, "502": 55 * policy.GiB / 8, "503": 13 * policy.GiB / 2}[id]
			body = zmptTestTorrent(size)
		default:
			t.Fatalf("read excluded or unexpected URL %s", req.URL)
		}
		return zmptTestResponse(body), nil
	})
	rows, err := s.Scan(context.Background(), source, target, "source-cookie-fixture", "target-cookie-fixture", "https://zmpt.cc/rss.php", settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 54 || !rows[0].Eligible || rows[0].ID != "501" || !rows[1].Eligible || rows[1].ID != "500" {
		t.Fatalf("expected both eligible rows in size order after the skipped first page, got %d rows, first %#v", len(rows), rows[:min(2, len(rows))])
	}
	byID := map[string]policy.Candidate{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["1"].Official || byID["1"].Eligible || !byID["2"].RestrictionsKnown || byID["2"].Allowed || !byID["3"].Episode || !byID["4"].PreviouslyDownloaded || !byID["10"].PreviouslyDownloaded || byID["502"].Eligible || !byID["503"].Duplicate || byID["503"].Eligible {
		t.Fatalf("ineligible rows bypassed: %+v", byID)
	}
	if n := len(requests); n != 15 {
		t.Fatalf("visited %d pages/details/torrents/target searches, want 15: %v", n, requests)
	}
}

func TestZmptRereadsAccountProgressAtExecutionAndPublish(t *testing.T) {
	source := testProfile("https://zmpt.cc")
	target := testProfile("https://target.example.com")
	var progress = "inactivity 0%"
	var matchID = "500"
	var detailsTitle = "Synthetic ZmPT Release-ZmWeb"
	var seenSearch string
	torrentReads, targetReads := 0, 0
	s := NewScanner()
	s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.URL.Host == "target.example.com":
			targetReads++
			body = `<p>没有找到相关种子</p>`
		case req.URL.Path == "/details.php":
			body = `<h1>Unrelated header</h1>` + zmptTestDetail("500", detailsTitle+`<span>[ 免费 ] 剩余时间： 1天</span>`)
		case req.URL.Path == "/torrents.php":
			seenSearch = req.URL.Query().Get("search")
			if req.URL.Query().Get("tag_id") != "3" {
				t.Fatal("live verification failed to restrict to official results")
			}
			body = `<table class="torrents">` + zmptTestRow(matchID, detailsTitle, zmptTestTag("官方")+zmptTestBar(progress)) + `</table>`
		case req.URL.Path == "/download.php":
			torrentReads++
			body = zmptTestTorrent(policy.GiB)
		default:
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return zmptTestResponse(body), nil
	})
	ctx := context.Background()
	c, err := s.Inspect(ctx, source, target, "source-cookie-fixture", "target-cookie-fixture", "500")
	if err != nil || !c.Official || !c.PreviouslyDownloaded || len(c.Torrent) != 0 || seenSearch != detailsTitle || torrentReads != 0 || targetReads != 0 {
		t.Fatalf("new download did not yield to previous account activity: %+v, %v (%d,%d), search=%q", c, err, torrentReads, targetReads, seenSearch)
	}
	progress = "leeching 35.50%"
	c, err = s.InspectForPublish(ctx, source, target, "source-cookie-fixture", "target-cookie-fixture", "500")
	if err != nil || !c.PreviouslyDownloaded || len(c.Torrent) == 0 || !c.DuplicateKnown || c.Duplicate || torrentReads != 1 || targetReads != 1 {
		t.Fatalf("own download could not refresh full publish evidence: %+v, %v (%d,%d)", c, err, torrentReads, targetReads)
	}
	matchID = "501"
	if _, err := s.Inspect(ctx, source, target, "source-cookie-fixture", "target-cookie-fixture", "500"); err == nil || torrentReads != 1 || targetReads != 1 {
		t.Fatal("same-title different source ID was accepted as download history evidence")
	}
}

func TestGenericOfficialBadgeIsBoundToDetailTagRow(t *testing.T) {
	source := testProfile("https://source.example.com")
	source.Selectors.Tags = ".tag"
	for _, test := range []struct {
		name     string
		body     string
		official bool
	}{
		{"navigation badge", `<nav><a class="tag" href="torrents.php?tag_id=3">官方</a></nav>`, false},
		{"description imitation", `<div id="kdescr"><table><tr><td class="rowhead">标签</td><td><span class="tag">官方</span></td></tr></table></div>`, false},
		{"filter form row", `<form><table><tr><th>Tags</th><td><span class="tag">官方</span></td></tr></table></form>`, false},
		{"detail tag row", `<table><tr><td class="rowhead">标签：</td><td><span class="tag">官方</span></td></tr></table>`, true},
		{"unclassed stock badge", `<table><tr><th>Tags</th><td>` + zmptTestTag("官方") + `</td></tr></table>`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := detailSourceEvidence(doc, source, false).official; got != test.official {
				t.Fatalf("official evidence = %v, want %v", got, test.official)
			}
		})
	}
}

func zmptTestTag(label string) string {
	return `<span style="background-color:blue;color:white;border-radius:2px;" title="标签">` + label + `</span>`
}

func zmptTestBar(title string) string {
	status, width, _ := strings.Cut(title, " ")
	color := map[string]string{"seeding": "green", "inactivity": "#aaa", "leeching": "blue"}[status]
	return `<div style="padding: 1px;margin-top: 2px;border: 1px solid #838383" title="` + title + `"><div style="width: ` + width + `;background-color: ` + color + `;height: 2px"></div></div>`
}

func zmptTestRow(id, title, badges string) string {
	return `<tr><td class="rowfollow"><table class="torrentname"><tr><td class="embedded"><a href="details.php?id=` + id + `&amp;hit=1"><b>` + title + `</b></a>` + badges + `</td></tr></table></td><td>999</td></tr>`
}

func zmptTestDetail(id, title string) string {
	return `<h1 id="top">` + title + `</h1><div id="kdescr">影片介绍</div><table><tr><td>基本信息 做种者: 8 下载者: 2</td></tr></table><a href="/download.php?id=` + id + `">download</a>`
}

func zmptTestTorrent(size int64) string {
	tracker := "https://zmpt.cc/announce"
	return fmt.Sprintf("d8:announce%d:%s4:infod6:lengthi%de4:name4:test12:piece lengthi%de6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee", len(tracker), tracker, size, size)
}

func zmptTestResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
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
