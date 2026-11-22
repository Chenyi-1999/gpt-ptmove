package automation

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

const (
	novaFixtureSourceTitle = "Forest Cartoon 2019 S01 1080p WEB-DL H.264 AAC2.0-ZmWeb"
	novaFixtureOtherTitle  = "Forest Cartoon S1 2019 1080p WEB-DL AVC AAC 2.0-LongWeb"
	novaFixtureQuery       = "forest cartoon"
)

func novaFixturePage(query, results string) string {
	results = strings.ReplaceAll(results, "{{query}}", query)
	return `<html><body><a href="/userdetails.php?id=31">我的账户</a>` +
		`<table class="main"><tr><td><a href="/details.php?id=30">推荐种子</a></td></tr></table>` +
		`<a href="/wiki.php?search=` + query + `&amp;page=1">帮助</a>` +
		`<form action="torrents.php"><input id="searchinput" name="search" value="` + query + `"></form>` + results +
		`</body></html>`
}

func novaFixtureEmpty() string {
	return `<table class="main"><tr><td class="embedded"><h2>搜索结果 "{{query}}"</h2><table><tr><td class="text">` +
		novaHDEmptyNotice + `</td></tr></table></td></tr></table>`
}

func novaFixtureResults(rows string) string {
	return `<table class="torrents"><tr><td class="colhead">种子名称</td></tr>` + rows + `</table>`
}

func novaFixtureRow(href, title string) string {
	return `<tr class="codex-torrent-row"><td class="rowfollow"><table class="torrentname"><tr><td class="embedded"><a href="` +
		href + `" title="` + title + `"><b>` + title + `</b></a></td></tr></table></td><td><a href="/userdetails.php?id=98">发布者</a></td></tr>`
}

func novaFixtureDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestNovaHDResultsRequireExactSearchScopeAndRealTorrentRows(t *testing.T) {
	p := testProfile(novaHDOrigin)
	row := novaFixtureRow("details.php?id=7&amp;hit=1", novaFixtureOtherTitle)
	for _, tc := range []struct {
		name     string
		query    string
		contents string
		ids      string
		complete bool
	}{
		{"known empty with links outside search results", novaFixtureQuery, novaFixtureEmpty(), "", true},
		{"unscoped empty notice with recommendations", novaFixtureQuery, "<p>" + novaHDEmptyNotice + "</p>", "", false},
		{"empty notice belongs to other search", novaFixtureQuery, strings.ReplaceAll(novaFixtureEmpty(), "{{query}}", "different title"), "", false},
		{"search echo mismatch", "wrong search", novaFixtureEmpty(), "", false},
		{"login page", novaFixtureQuery, novaFixtureEmpty() + `<input type="password">`, "", false},
		{"actual result", novaFixtureQuery, novaFixtureResults(row), "7", true},
		{"contradictory result and empty", novaFixtureQuery, novaFixtureResults(row) + novaFixtureEmpty(), "", false},
		{"malformed row remains incomplete", novaFixtureQuery, novaFixtureResults(row + "<tr><td>未知结果</td></tr>"), "7", false},
		{"untrusted result links", novaFixtureQuery, novaFixtureResults(
			novaFixtureRow("userdetails.php?id=7", "个人资料") +
				novaFixtureRow("https://other.example/details.php?id=7", "外站种子") +
				novaFixtureRow("details.php?id=7&amp;id=8", "两个编号") +
				novaFixtureRow("details.php?id=7&amp;passkey=fixture", "带密钥的结果")), "", false},
		{"query relative pager is incomplete", novaFixtureQuery,
			novaFixtureResults(row) + `<p class="nexus-pagination"><a href="?incldead=0&amp;page=1&amp;search=forest+cartoon">下一页</a></p>`, "7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := novaFixtureDoc(t, novaFixturePage(tc.query, tc.contents))
			got := novaHDReadResults(doc, p, novaFixtureQuery)
			ids := make([]string, 0, len(got.hits))
			for _, hit := range got.hits {
				ids = append(ids, hit.id)
			}
			if got.complete != tc.complete || strings.Join(ids, ",") != tc.ids {
				t.Fatalf("results = %+v, expected complete=%v and ids=%q", got, tc.complete, tc.ids)
			}
		})
	}
	var rows strings.Builder
	for id := 1; id <= novaHDResultLimit; id++ {
		rows.WriteString(novaFixtureRow(fmt.Sprintf("details.php?id=%d", id), novaFixtureOtherTitle))
	}
	got := novaHDReadResults(novaFixtureDoc(t, novaFixturePage(novaFixtureQuery, novaFixtureResults(rows.String()))), p, novaFixtureQuery)
	if got.complete || len(got.hits) != novaHDResultLimit {
		t.Fatalf("a page at the result cap cannot prove absence")
	}
}

func TestNovaHDDuplicateRequiresComparablePayloadAndLimitsGetOnlyChecks(t *testing.T) {
	const sourceVideo = 7 * policy.GiB
	source := testProfile("https://zmpt.cc")
	target := testProfile(novaHDOrigin)
	sourceData := novaFixtureTorrent("zmpt.cc", "movie.mkv", "Zm", sourceVideo)
	meta, err := acquirefetch.ParseTorrentMeta([]byte(sourceData))
	if err != nil {
		t.Fatal(err)
	}
	candidate := policy.Candidate{Title: novaFixtureSourceTitle, InfoHash: meta.InfoHash, Size: meta.Size, Torrent: []byte(sourceData)}
	differentTitle := novaFixtureRow("details.php?id=17&amp;hit=1", novaFixtureOtherTitle)
	differentYear := novaFixtureRow("details.php?id=17", strings.Replace(novaFixtureOtherTitle, "2019", "2020", 1))
	sameTitle := novaFixtureRow("details.php?id=17", novaFixtureSourceTitle)
	cappedRows := differentTitle + novaFixtureRow("details.php?id=18", novaFixtureOtherTitle) +
		novaFixtureRow("details.php?id=19", novaFixtureOtherTitle) + novaFixtureRow("details.php?id=20", novaFixtureOtherTitle)
	for _, tc := range []struct {
		name     string
		broad    string
		exact    string
		source   string
		target   string
		wantMeta int
		known    bool
		match    bool
	}{
		{name: "empty with profile and recommendation links", broad: novaFixtureEmpty(), known: true},
		{name: "other group with distinct verified video length", broad: novaFixtureResults(differentTitle),
			target: novaFixtureTorrent("pt.novahd.top", "other.mkv", "Nova", 6*policy.GiB), wantMeta: 1, known: true},
		{name: "same payload with changed infohash", broad: novaFixtureResults(differentTitle),
			target: novaFixtureTorrent("pt.novahd.top", "movie.mkv", "Nova", sourceVideo), wantMeta: 1, known: true, match: true},
		{name: "different title year still checks for the same payload", broad: novaFixtureResults(differentYear),
			target: novaFixtureTorrent("pt.novahd.top", "movie.mkv", "Nova", sourceVideo), wantMeta: 1, known: true, match: true},
		{name: "same-size with mismatched names remains unknown", broad: novaFixtureResults(differentTitle), exact: novaFixtureEmpty(),
			target: novaFixtureTorrent("pt.novahd.top", "other.mkv", "Nova", sourceVideo), wantMeta: 1},
		{name: "extra NFO alone does not prove another edition", broad: novaFixtureResults(differentTitle),
			target: novaFixtureTorrentWithNFO("pt.novahd.top", sourceVideo), wantMeta: 1},
		{name: "same release label with changed sizes remains unknown", broad: novaFixtureResults(sameTitle), exact: novaFixtureEmpty(),
			target: novaFixtureTorrent("pt.novahd.top", "other.mkv", "Nova", 6*policy.GiB), wantMeta: 1},
		{name: "failed payload lookup remains unknown", broad: novaFixtureResults(sameTitle), exact: novaFixtureEmpty(), wantMeta: 1},
		{name: "pagination is not cleared by an exact empty lookup", broad: novaFixtureResults(differentTitle) +
			`<p class="nexus-pagination"><a href="?page=1&amp;search=forest+cartoon">2</a></p>`, exact: novaFixtureEmpty(),
			target: novaFixtureTorrent("pt.novahd.top", "other.mkv", "Nova", 6*policy.GiB), wantMeta: 1},
		{name: "exact fallback confirms an equal payload", broad: "<p>Unknown result markup</p>", exact: novaFixtureResults(sameTitle),
			target: novaFixtureTorrent("pt.novahd.top", "movie.mkv", "Nova", sourceVideo), wantMeta: 1, known: true, match: true},
		{name: "comparison budget cannot prove absence", broad: novaFixtureResults(cappedRows),
			target: novaFixtureTorrent("pt.novahd.top", "other.mkv", "Nova", 6*policy.GiB), wantMeta: novaHDPayloadChecks},
		{name: "verified source hint confirms a target payload", source: `<div id="resultContainer"><a href="https://pt.novahd.top/details.php?id=17">NovaHD</a></div>`,
			target: novaFixtureTorrent("pt.novahd.top", "movie.mkv", "Nova", sourceVideo), wantMeta: 1, known: true, match: true},
		{name: "source hint cannot set a different origin", source: `<div id="resultContainer"><a href="https://other.example/details.php?id=17">NovaHD</a></div>`,
			broad: novaFixtureEmpty(), known: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payloadRequests := 0
			scanner := NewScanner()
			scanner.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "pt.novahd.top" || req.Header.Get("Cookie") != "target-test-session" {
					t.Fatalf("unexpected request scope: %s %s", req.Method, req.URL)
				}
				params := req.URL.Query()
				switch req.URL.Path {
				case "/torrents.php":
					if params.Get("incldead") != "0" || params.Get("search_area") != "0" {
						t.Fatalf("search omitted all-results safeguards: %s", req.URL)
					}
					switch params.Get("search_mode") {
					case "0":
						if params.Get("search") != novaFixtureQuery || tc.broad == "" {
							t.Fatalf("unexpected broad query: %s", req.URL)
						}
						return zmptTestResponse(novaFixturePage(novaFixtureQuery, tc.broad)), nil
					case "2":
						if params.Get("search") != novaFixtureSourceTitle || tc.exact == "" {
							t.Fatalf("unexpected exact query: %s", req.URL)
						}
						return zmptTestResponse(novaFixturePage(novaFixtureSourceTitle, tc.exact)), nil
					}
				case "/details.php":
					return zmptTestResponse(`<a href="/download.php?id=` + params.Get("id") + `">下载种子文件</a>`), nil
				case "/download.php":
					payloadRequests++
					return zmptTestResponse(tc.target), nil
				}
				t.Fatalf("unexpected URL: %s", req.URL)
				return nil, nil
			})
			known, matches := scanner.novaHDDuplicate(context.Background(), source, target, "target-test-session", novaFixtureDoc(t, tc.source), candidate)
			if known != tc.known || matches != tc.match || payloadRequests != tc.wantMeta {
				t.Fatalf("known=%v, duplicate=%v, metadata requests=%d (expected %v, %v, %d)",
					known, matches, payloadRequests, tc.known, tc.match, tc.wantMeta)
			}
		})
	}
}

func TestNovaHDReadOnlyDuplicateCheckUsedByScanAndBothRechecks(t *testing.T) {
	source := testProfile("https://source.example.com")
	source.OfficialGroups = []string{"ZmWeb"}
	target := testProfile(novaHDOrigin)
	scanner := NewScanner()
	searches := 0
	scanner.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("duplicate check attempted a mutation: %s", req.Method)
		}
		switch req.URL.Host {
		case "source.example.com":
			switch req.URL.Path {
			case "/torrents.php":
				return zmptTestResponse(`<a href="/details.php?id=5">种子详情</a>`), nil
			case "/details.php":
				return zmptTestResponse(zmptTestDetail("5", novaFixtureSourceTitle)), nil
			case "/download.php":
				return zmptTestResponse(novaFixtureTorrent("source.example.com", "movie.mkv", "Zm", 7*policy.GiB)), nil
			}
		case "pt.novahd.top":
			if req.URL.Path == "/torrents.php" && req.URL.Query().Get("search") == novaFixtureQuery {
				searches++
				return zmptTestResponse(novaFixturePage(novaFixtureQuery, novaFixtureEmpty())), nil
			}
		}
		t.Fatalf("unexpected request %s", req.URL)
		return nil, nil
	})
	ctx := context.Background()
	rows, err := scanner.Scan(ctx, source, target, "source-session", "target-session", "", policy.Defaults())
	if err != nil || len(rows) != 1 || !rows[0].Eligible {
		t.Fatalf("scan result=%+v error=%v", rows, err)
	}
	for _, inspect := range []func(context.Context, Profile, Profile, string, string, string) (policy.Candidate, error){
		scanner.Inspect, scanner.InspectForPublish,
	} {
		candidate, err := inspect(ctx, source, target, "source-session", "target-session", "5")
		if err != nil || !candidate.DuplicateKnown || candidate.Duplicate {
			t.Fatalf("recheck result=%+v error=%v", candidate, err)
		}
	}
	if searches != 3 {
		t.Fatalf("NovaHD searches = %d, expected 3", searches)
	}
}

func TestZmPTWidgetPrecheckAndNovaHDPublishRecheck(t *testing.T) {
	const sourceID = "5"
	source := testProfile("https://zmpt.cc")
	source.OfficialGroups = []string{"ZmWeb"}
	target := testProfile(novaHDOrigin)
	sourceTorrent := novaFixtureTorrent("zmpt.cc", "movie.mkv", "Zm", 7*policy.GiB)
	targetTorrent := novaFixtureTorrent("pt.novahd.top", "movie.mkv", "Nova", 7*policy.GiB)
	meta, err := acquirefetch.ParseTorrentMeta([]byte(sourceTorrent))
	if err != nil {
		t.Fatal(err)
	}
	widgetLookups, targetSearches := 0, 0
	widgetResponse := `{"code":0,"message":"success","data":[]}`
	scanner := NewScanner()
	scanner.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("inspection attempted a mutation: %s %s", req.Method, req.URL)
		}
		switch req.URL.Host {
		case "zmpt.cc":
			switch req.URL.Path {
			case "/torrents.php":
				if req.URL.Query().Get("tag_id") != "3" {
					t.Fatalf("source listing was not restricted to official torrents: %s", req.URL)
				}
				return zmptTestResponse(`<table class="torrents">` + zmptTestRowWithSeeders(sourceID, novaFixtureSourceTitle, zmptTestTag("官方"), "8") + `</table>`), nil
			case "/details.php":
				return zmptTestResponse(zmptTestDetailWithTorrent(sourceID, novaFixtureSourceTitle, []byte(sourceTorrent))), nil
			case "/download.php":
				return zmptTestResponse(sourceTorrent), nil
			case "/nodeapi/iyuu/getIyuuByInfoHash":
				widgetLookups++
				if req.URL.Query().Get("hash") != meta.InfoHash || req.Header.Get("Cookie") != "source-session" ||
					req.Header.Get("X-Requested-With") != "XMLHttpRequest" || req.Header.Get("Referer") != "https://zmpt.cc/details.php?id="+sourceID {
					t.Fatalf("widget lookup was not bound to the source hash and detail page: %+v", req)
				}
				return zmptTestResponse(widgetResponse), nil
			}
		case "pt.novahd.top":
			switch req.URL.Path {
			case "/torrents.php":
				targetSearches++
				if widgetLookups != 4 {
					t.Fatalf("target search ran outside the publish recheck: widget=%d", widgetLookups)
				}
				return zmptTestResponse(novaFixturePage(novaFixtureQuery,
					novaFixtureResults(novaFixtureRow("details.php?id=17", novaFixtureSourceTitle)))), nil
			case "/details.php":
				return zmptTestResponse(`<a href="/download.php?id=17">download</a>`), nil
			case "/download.php":
				return zmptTestResponse(targetTorrent), nil
			}
		}
		t.Fatalf("unexpected request %s", req.URL)
		return nil, nil
	})
	ctx := context.Background()
	rows, err := scanner.Scan(ctx, source, target, "source-session", "target-session", "", policy.Defaults())
	if err != nil || len(rows) != 1 || !rows[0].Eligible || !rows[0].DuplicateKnown || rows[0].Duplicate {
		t.Fatalf("successful widget miss did not permit the original candidate flow: rows=%+v error=%v", rows, err)
	}
	if widgetLookups != 1 || targetSearches != 0 {
		t.Fatalf("scan did not use only the source widget: widget=%d target=%d", widgetLookups, targetSearches)
	}
	candidate, err := scanner.Inspect(ctx, source, target, "source-session", "target-session", sourceID)
	if err != nil || !candidate.DuplicateKnown || candidate.Duplicate || widgetLookups != 2 || targetSearches != 0 {
		t.Fatalf("predownload check did not use the source widget: candidate=%+v error=%v widget=%d target=%d", candidate, err, widgetLookups, targetSearches)
	}
	widgetResponse = `{"code":-1,"message":"timeout","data":null}`
	candidate, err = scanner.Inspect(ctx, source, target, "source-session", "target-session", sourceID)
	if err != nil || candidate.DuplicateKnown || candidate.Duplicate || widgetLookups != 3 || targetSearches != 0 {
		t.Fatalf("failed widget read did not leave predownload pending: known=%v duplicate=%v error=%v", candidate.DuplicateKnown, candidate.Duplicate, err)
	}
	widgetResponse = `{"code":0,"data":[{"url":"https://pt.novahd.top/details.php?id=17","name":"NovaHD"}]}`
	candidate, err = scanner.Inspect(ctx, source, target, "source-session", "target-session", sourceID)
	if err != nil || !candidate.DuplicateKnown || !candidate.Duplicate || widgetLookups != 4 || targetSearches != 0 {
		t.Fatalf("new widget hit was missed before download: known=%v duplicate=%v error=%v", candidate.DuplicateKnown, candidate.Duplicate, err)
	}
	widgetResponse = `{"code":0,"data":[]}`
	candidate, err = scanner.InspectForPublish(ctx, source, target, "source-session", "target-session", sourceID)
	if err != nil || !candidate.DuplicateKnown || !candidate.Duplicate || targetSearches != 1 {
		t.Fatalf("publish check failed to catch a target duplicate added after download: candidate=%+v error=%v searches=%d", candidate, err, targetSearches)
	}
}

func novaFixtureTorrent(host, name, source string, size int64) string {
	tracker := "https://" + host + "/announce"
	return fmt.Sprintf("d8:announce%d:%s4:infod6:lengthi%de4:name%d:%s12:piece lengthi%de6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1e6:source%d:%see",
		len(tracker), tracker, size, len(name), name, int64(math.MaxInt64), len(source), source)
}

func novaFixtureTorrentWithNFO(host string, videoSize int64) string {
	tracker := "https://" + host + "/announce"
	return fmt.Sprintf("d8:announce%d:%s4:infod5:filesld6:lengthi%de4:pathl9:movie.mkveed6:lengthi1e4:pathl8:info.nfoeee4:name4:pack12:piece lengthi%de6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee",
		len(tracker), tracker, videoSize, int64(math.MaxInt64))
}
