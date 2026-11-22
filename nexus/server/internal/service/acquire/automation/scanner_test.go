package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
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
	inspectedTitles := map[string]string{"500": "New smaller release", "501": "New larger release", "502": "Above Exact Bounds", "503": "Already on Target", "504": "Below Exact Bounds"}
	sizes := map[string]int64{"500": 13 * policy.GiB / 2, "501": 53 * policy.GiB / 8, "502": 55 * policy.GiB / 8, "503": 51 * policy.GiB / 8, "504": 49 * policy.GiB / 8}
	var requests []string
	widgetRequests, targetRequests := 0, 0
	knownHashes := map[string]string{}
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
			targetRequests++
			t.Fatalf("candidate scan used target search before publish: %s", req.URL)
		case req.URL.Host == "zmpt.cc" && req.URL.Path == "/nodeapi/iyuu/getIyuuByInfoHash":
			widgetRequests++
			if req.Header.Get("X-Requested-With") != "XMLHttpRequest" || knownHashes[req.URL.Query().Get("hash")] == "" {
				t.Fatalf("widget request did not use a known source infohash and XHR header: %s", req.URL)
			}
			if knownHashes[req.URL.Query().Get("hash")] == "Already on Target" {
				body = `{"code":0,"message":"success","data":[{"url":"https://target.example.com/details.php?id=123","name":"Target","icon":"https://icons.example.com/target.png"}]}`
			} else {
				body = `{"code":0,"message":"success","data":[]}`
			}
		case req.URL.Path == "/torrents.php":
			q := req.URL.Query()
			for key, want := range map[string]string{"tag_id": "3", "sort": "5", "type": "asc", "size_begin": "6", "size_end": "7", "seeders_begin": "3", "leechers_begin": "1", "incldead": "1"} {
				if q.Get(key) != want {
					t.Errorf("listing query %s = %q, want %q", key, q.Get(key), want)
				}
			}
			var rows strings.Builder
			switch q.Get("page") {
			case "0":
				rows.WriteString(zmptTestRowWithSeeders("1", "Pinned-ZmWeb", `<div>官方</div>`, "9"))
				rows.WriteString(zmptTestRow("2", "Restricted", official+zmptTestTag("禁转")))
				rows.WriteString(zmptTestRow("3", "Single Episode", official+zmptTestTag("分集")))
				rows.WriteString(zmptTestRow("4", "Previously downloaded and removed from client", official+zmptTestBar("inactivity 100%")))
				for n := 10; n < 65; n++ {
					rows.WriteString(zmptTestRow(fmt.Sprint(n), "Previous download", official+zmptTestBar("seeding 100%")))
				}
			case "1":
				rows.WriteString(zmptTestRowWithSeeders("501", "New larger release", official, "13"))
				rows.WriteString(zmptTestRowWithSeeders("500", "New smaller release", official+`<img title="审核通过" style="color: green">`, "8"))
				rows.WriteString(zmptTestRow("502", "Above Exact Bounds", official))
				rows.WriteString(zmptTestRow("503", "Already on Target", official))
				rows.WriteString(zmptTestRow("504", "Below Exact Bounds", official))
			}
			body = `<a href="torrents.php?tag_id=3">官方</a><table class="torrents">` + rows.String() + `</table>`
		case req.URL.Path == "/details.php" && sizes[id] > 0:
			body = zmptTestDetailWithTorrent(id, inspectedTitles[id], []byte(zmptTestTorrent(sizes[id])))
		case req.URL.Path == "/download.php" && sizes[id] > 0:
			body = zmptTestTorrent(sizes[id])
			meta, err := acquirefetch.ParseTorrentMeta([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			knownHashes[meta.InfoHash] = inspectedTitles[id]
		default:
			t.Fatalf("read excluded or unexpected URL %s", req.URL)
		}
		return zmptTestResponse(body), nil
	})
	rows, err := s.Scan(context.Background(), source, target, "source-cookie-fixture", "target-cookie-fixture", "https://zmpt.cc/rss.php", settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 55 || !rows[0].Eligible || rows[0].ID != "500" || rows[0].Seeders != 8 || !rows[1].Eligible || rows[1].ID != "501" || rows[1].Seeders != 13 {
		t.Fatalf("expected both eligible rows in size order after the skipped first page, got %d rows, first %#v", len(rows), rows[:min(2, len(rows))])
	}
	byID := map[string]policy.Candidate{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["1"].Official || byID["1"].Eligible || !byID["2"].RestrictionsKnown || byID["2"].Allowed || !byID["3"].Episode || !byID["4"].PreviouslyDownloaded || !byID["10"].PreviouslyDownloaded || byID["502"].Eligible || !byID["503"].Duplicate || byID["503"].Eligible {
		t.Fatalf("ineligible rows bypassed: %+v", byID)
	}
	for _, id := range []string{"502", "504"} {
		if candidate := byID[id]; candidate.Eligible || !strings.Contains(strings.Join(candidate.Reasons, " "), "完整种子体积不符合规则") {
			t.Fatalf("exact size bounds not enforced for %s: %+v", id, candidate)
		}
	}
	if n := len(requests); n != 18 || widgetRequests != 5 || targetRequests != 0 {
		t.Fatalf("visited %d pages/details/torrents/widget lookups, widget=%d target=%d, want 18/5/0: %v", n, widgetRequests, targetRequests, requests)
	}
}

func TestZmptRereadsAccountProgressAtExecutionAndPublish(t *testing.T) {
	source := testProfile("https://zmpt.cc")
	target := testProfile("https://target.example.com")
	var progress = "inactivity 0%"
	var matchID = "500"
	var detailsTitle = "Synthetic ZmPT Release-ZmWeb"
	var listingSeeders = "17"
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
			body = `<h1>Unrelated header</h1>` + zmptTestDetailWithTorrent("500", detailsTitle+`<span>[ 免费 ] 剩余时间： 1天</span>`, []byte(zmptTestTorrent(policy.GiB)))
		case req.URL.Path == "/torrents.php":
			seenSearch = req.URL.Query().Get("search")
			if req.URL.Query().Get("tag_id") != "3" {
				t.Fatal("live verification failed to restrict to official results")
			}
			body = `<table class="torrents">` + zmptTestRowWithSeeders(matchID, detailsTitle, zmptTestTag("官方")+zmptTestBar(progress), listingSeeders) + `</table>`
		case req.URL.Path == "/download.php":
			torrentReads++
			body = zmptTestTorrent(policy.GiB)
		case req.URL.Path == "/nodeapi/iyuu/getIyuuByInfoHash":
			if req.URL.Query().Get("hash") == "" || req.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Fatalf("publish flow omitted the source widget hash lookup: %s", req.URL)
			}
			body = `{"code":0,"message":"success","data":[]}`
		default:
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return zmptTestResponse(body), nil
	})
	ctx := context.Background()
	c, err := s.Inspect(ctx, source, target, "source-cookie-fixture", "target-cookie-fixture", "500")
	if err != nil || !c.Official || !c.PreviouslyDownloaded || c.Seeders != 17 || len(c.Torrent) != 0 || seenSearch != detailsTitle || torrentReads != 0 || targetReads != 0 {
		t.Fatalf("new download did not yield to previous account activity: %+v, %v (%d,%d), search=%q", c, err, torrentReads, targetReads, seenSearch)
	}
	progress = "leeching 35.50%"
	c, err = s.InspectForPublish(ctx, source, target, "source-cookie-fixture", "target-cookie-fixture", "500")
	if err != nil || !c.PreviouslyDownloaded || c.Seeders != 17 || len(c.Torrent) == 0 || !c.DuplicateKnown || c.Duplicate || torrentReads != 1 || targetReads != 1 {
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
	return zmptTestRowWithSeeders(id, title, badges, "8")
}

func zmptTestRowWithSeeders(id, title, badges, seeders string) string {
	return zmptTestRowStart(id, title, badges) + `<td><a href="details.php?id=` + id + `&amp;hit=1#seeders">` + seeders + `</a></td></tr>`
}

func zmptTestRowNoSeeders(id, title, badges string) string {
	return zmptTestRowStart(id, title, badges) + `</tr>`
}

func zmptTestRowStart(id, title, badges string) string {
	return `<tr><td class="rowfollow"><table class="torrentname"><tr><td class="embedded"><a href="details.php?id=` + id + `&amp;hit=1"><b>` + title + `</b></a>` + badges + `</td></tr></table></td>`
}

func TestZmptListingSeedersUseMatchingSeedersAnchor(t *testing.T) {
	source := testProfile("https://zmpt.cc")
	tests := []struct {
		name  string
		row   string
		want  int
		known bool
	}{
		{name: "matching row", row: `<table class="torrents">` + zmptTestRowWithSeeders("1", "one", "", "13") + `</table>`, want: 13, known: true},
		{name: "zero is a known count", row: `<table class="torrents">` + zmptTestRowWithSeeders("1", "one", "", "0") + `</table>`, want: 0, known: true},
		{name: "grouped count", row: `<table class="torrents">` + zmptTestRowWithSeeders("1", "one", "", "1,234") + `</table>`, want: 1234, known: true},
		{name: "peer links and other torrent ignored", row: `<table class="torrents"><tr><td><a href="details.php?id=2#seeders">99</a></td><td><table class="torrentname"><tr><td class="embedded"><a href="details.php?id=1"><b>one</b></a></td></tr></table><a href="details.php?id=1&amp;hit=1#seeders">7</a><a href="details.php?id=1#leechers">22</a><a href="details.php?id=1#completed">250</a></td></tr></table>`, want: 7, known: true},
		{name: "invalid count falls back", row: `<table class="torrents">` + zmptTestRowWithSeeders("1", "one", "", "unknown") + `</table>`, known: false},
		{name: "malformed grouping falls back", row: `<table class="torrents">` + zmptTestRowWithSeeders("1", "one", "", "12,,4") + `</table>`, known: false},
		{name: "conflicting counts fall back", row: `<table class="torrents"><tr><td><table class="torrentname"><tr><td class="embedded"><a href="details.php?id=1"><b>one</b></a></td></tr></table><a href="details.php?id=1#seeders">7</a><a href="details.php?id=1&amp;hit=1#seeders">8</a></td></tr></table>`, known: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(test.row))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range zmptListingEntries(doc, source) {
				if entry.id == "1" {
					seeders, known := entry.seeders, entry.seedersKnown
					if seeders != test.want || known != test.known {
						t.Fatalf("seeders = %d, known %v, want %d, %v", seeders, known, test.want, test.known)
					}
					return
				}
			}
			t.Fatal("matching listing row was not parsed")
		})
	}
}

func TestZmptInspectFallsBackToDetailSeedersWhenListingCountIsMissing(t *testing.T) {
	source := testProfile("https://zmpt.cc")
	target := testProfile("https://target.example.com")
	var requests []string
	s := NewScanner()
	s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Host+req.URL.Path)
		var body string
		switch {
		case req.URL.Path == "/details.php":
			body = zmptTestDetailWithTorrent("1", "Fallback-ZmWeb", []byte(zmptTestTorrent(policy.GiB)))
		case req.URL.Path == "/torrents.php":
			body = `<table class="torrents">` + zmptTestRowNoSeeders("1", "Fallback-ZmWeb", zmptTestTag("官方")) + `</table>`
		case req.URL.Path == "/download.php":
			body = zmptTestTorrent(policy.GiB)
		case req.URL.Path == "/nodeapi/iyuu/getIyuuByInfoHash":
			body = `{"code":0,"message":"success","data":[]}`
		default:
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return zmptTestResponse(body), nil
	})
	candidate, err := s.Inspect(context.Background(), source, target, "source-cookie-fixture", "target-cookie-fixture", "1")
	if err != nil || candidate.Seeders != 8 || len(candidate.Torrent) == 0 || !candidate.DuplicateKnown || candidate.Duplicate {
		t.Fatalf("detail peer count was not used as the missing-list fallback: %+v, %v", candidate, err)
	}
	if len(requests) != 4 {
		t.Fatalf("unexpected requests: %v", requests)
	}
}

func zmptTestDetail(id, title string) string {
	return zmptTestDetailWithTorrent(id, title, []byte(zmptTestTorrent(policy.GiB)))
}

func zmptTestDetailWithTorrent(id, title string, torrent []byte) string {
	meta, err := acquirefetch.ParseTorrentMeta(torrent)
	if err != nil {
		panic(err)
	}
	return `<h1 id="top">` + title + `</h1><span id="hashID">` + meta.InfoHash + `</span><div id="resultContainer"></div><div id="kdescr">影片介绍</div><table><tr><td>基本信息 做种者: 8 下载者: 2</td></tr></table><a href="/download.php?id=` + id + `">download</a>`
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
