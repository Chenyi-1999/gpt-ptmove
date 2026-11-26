package repair

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/pt-nexus/server/internal/config"
)

type ptgenTestRoundTripper func(*http.Request) (*http.Response, error)

func (fn ptgenTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func ptgenTestResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestConfiguredPTGenOrderedFallthrough(t *testing.T) {
	sources := []config.PTGenSource{
		{Type: "generic", URL: "https://disabled.example/lookup", Token: "first-fixture"},
		{Type: "universal", URL: "https://slow.example", Token: "second-fixture"},
		{Type: "generic", URL: "https://busy.example/lookup", Token: "third-fixture"},
		{Type: "generic", URL: "https://empty-intro.example/lookup"},
		{Type: "auto", URL: "https://ready.example/api/v2/info", Token: "fifth-fixture"},
		{Type: "generic", URL: "https://unused.example/lookup", Token: "never-sent"},
	}
	order := []string{}
	client := *configuredPTGenClient
	client.Transport = ptgenTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		order = append(order, req.URL.Hostname())
		switch host := req.URL.Hostname(); host {
		case "disabled.example":
			if req.Header.Get("Authorization") != "Bearer first-fixture" || req.Header.Get("X-API-Key") != "" {
				t.Fatalf("first source did not get only its own bearer token")
			}
			if got := req.URL.Query().Get("url"); got != "https://movie.douban.com/subject/123" {
				t.Fatalf("request got a noncanonical media link %q", got)
			}
			return ptgenTestResponse(http.StatusUnauthorized, "{}"), nil
		case "slow.example":
			if req.Header.Get("Authorization") != "" || req.Header.Get("X-API-Key") != "second-fixture" ||
				req.URL.Path != "/api/v2/info" || req.Method != http.MethodPost {
				t.Fatal("second source did not get its Universal request")
			}
			if _, ok := req.Context().Deadline(); !ok {
				t.Fatal("each configured source must have its own timeout")
			}
			return nil, context.DeadlineExceeded
		case "busy.example":
			if req.Header.Get("Authorization") != "Bearer third-fixture" {
				t.Fatal("third source did not get its own bearer token")
			}
			return ptgenTestResponse(http.StatusTooManyRequests, "{}"), nil
		case "empty-intro.example":
			if req.Header.Get("Authorization") != "" || req.Header.Get("X-API-Key") != "" {
				t.Fatal("tokenless source unexpectedly received authentication")
			}
			return ptgenTestResponse(http.StatusOK, `{"format":"[img]https://8.8.8.8/poster.jpg[/img]"}`), nil
		case "ready.example":
			if req.Header.Get("Authorization") != "" || req.Header.Get("X-API-Key") != "fifth-fixture" {
				t.Fatal("fifth source did not get its own API key")
			}
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body["url"] != "https://movie.douban.com/subject/123" || body["format"] != "bbcode" {
				t.Fatalf("unexpected Universal body %v, error %v", body, err)
			}
			return ptgenTestResponse(http.StatusOK, `{"data":{"format":"◎简　　介\n完整的测试简介","imdb_link":"https://www.imdb.com/title/tt456/"}}`), nil
		default:
			t.Fatalf("source queried after target content was available: %s", host)
			return nil, nil
		}
	})
	got, ok := fetchConfiguredPTGen("intro", MovieInfoResult{Douban: "https://movie.douban.com/subject/123/?from=credential-looking"}, sources, &client)
	if !ok || !strings.Contains(got.Intro, "完整的测试简介") || got.IMDb != "https://www.imdb.com/title/tt456" {
		t.Fatalf("ordered lookup not successful: %v %v", got, ok)
	}
	want := []string{"disabled.example", "slow.example", "busy.example", "empty-intro.example", "ready.example"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("unexpected lookup order %v", order)
	}
}

func TestConfiguredPTGenPosterRejectsForbiddenAddressAndRedirect(t *testing.T) {
	sources := []config.PTGenSource{
		{Type: "generic", URL: "https://redirect.example/api", Token: "redirect-fixture"},
		{Type: "generic", URL: "https://blocked.example/api"},
		{Type: "ourbits", URL: "https://ourbits.github.io/PtGen/"},
	}
	var requests []string
	client := *configuredPTGenClient
	client.Transport = ptgenTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.Hostname())
		switch req.URL.Hostname() {
		case "redirect.example":
			resp := ptgenTestResponse(http.StatusTemporaryRedirect, "")
			resp.Header.Set("Location", "https://elsewhere.example/never")
			return resp, nil
		case "blocked.example":
			return ptgenTestResponse(http.StatusOK, `{"format":"[img]https://81.70.24.5/poster.jpg[/img]"}`), nil
		case "ourbits.github.io":
			if req.URL.Path != "/PtGen/imdb/tt456.json" {
				t.Fatalf("unexpected archive path %q", req.URL.Path)
			}
			return ptgenTestResponse(http.StatusOK, `{"poster":"https://8.8.8.8/poster.jpg","introduction":"Archive body"}`), nil
		default:
			t.Fatalf("unexpected source after redirect %q", req.URL.Hostname())
			return nil, nil
		}
	})
	got, ok := fetchConfiguredPTGen("poster", MovieInfoResult{IMDb: "https://www.imdb.com/title/tt456"}, sources, &client)
	if !ok || got.Poster != "[img]https://8.8.8.8/poster.jpg[/img]" {
		t.Fatalf("want public poster after fallback, got %v, %v", got, ok)
	}
	if !reflect.DeepEqual(requests, []string{"redirect.example", "blocked.example", "ourbits.github.io"}) {
		t.Fatalf("unexpected request sequence %v", requests)
	}
}

func TestPTGenSourceOfficialProtocols(t *testing.T) {
	links := MovieInfoResult{Douban: "https://movie.douban.com/subject/123", IMDb: "https://www.imdb.com/title/tt456"}
	ctx := context.Background()

	cspt, err := newPTGenSourceRequest(ctx, "cspt", config.PTGenSource{URL: "https://cspt.top", Token: "only-official"}, links)
	if err != nil || cspt.Method != http.MethodGet || cspt.URL.Path != "/api/ptgen/query/only-official" ||
		cspt.URL.Query().Get("url") != links.Douban || cspt.Header.Get("Authorization") != "" {
		t.Fatal("invalid CSPT request")
	}
	if _, err := newPTGenSourceRequest(ctx, "cspt", config.PTGenSource{URL: "https://example.com", Token: "only-official"}, links); err == nil {
		t.Fatal("official path token could be routed to a different host")
	}

	ourbits, err := newPTGenSourceRequest(ctx, "ourbits", config.PTGenSource{URL: "https://api.ourhelp.club/infogen"}, links)
	if err != nil || ourbits.Method != http.MethodGet || ourbits.URL.Query().Get("site") != "douban" ||
		ourbits.URL.Query().Get("sid") != "123" || ourbits.URL.Query().Has("url") {
		t.Fatal("invalid OurBits dynamic request")
	}
	got, ok := parseConfiguredPTGen([]byte(`{
		"this_title":["Test Film"],"year":"2026","region":["中国大陆"],"genre":["剧情","爱情"],
		"douban_link":"https://movie.douban.com/subject/123/","introduction":"<p>归档测试简介。</p>"
	}`))
	if !ok || !strings.Contains(got.Intro, "◎片　　名　Test Film") || !strings.Contains(got.Intro, "剧情 / 爱情") ||
		!strings.Contains(got.Intro, "归档测试简介。") || strings.Contains(got.Intro, "<p>") {
		t.Fatalf("structured archive intro was not formatted: %+v", got)
	}
}

func TestPTGenDialRejectsLocalAndProtectedAddresses(t *testing.T) {
	for _, address := range []string{
		"81.70.24.5:443", "[::ffff:81.70.24.5]:443", "169.254.169.254:443", "127.0.0.1:443", "10.1.2.3:443",
	} {
		conn, err := dialPTGenSource(context.Background(), "tcp", address)
		if conn != nil {
			conn.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "禁止连接") {
			t.Fatalf("dial did not block forbidden address %s: %v", address, err)
		}
	}
}

func TestConfiguredPTGenDoesNotSendUnrecognizedMediaURL(t *testing.T) {
	client := &http.Client{Transport: ptgenTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		t.Fatal("must not query any provider with unrecognized media URLs")
		return nil, nil
	})}
	_, ok := fetchConfiguredPTGen("intro", MovieInfoResult{IMDb: "https://example.com/private?token=not-sent"},
		[]config.PTGenSource{{Type: "generic", URL: "https://ready.example/api"}}, client)
	if ok {
		t.Fatal("unrecognized media URL should not produce a result")
	}
}

func TestConfiguredMovieLookupMakesNoLegacyCallsWhenItFindsRequestedContent(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = ptgenTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("provider hit must not make legacy lookup %s", req.URL.Hostname())
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	client := *configuredPTGenClient
	lookupCount := 0
	client.Transport = ptgenTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		lookupCount++
		if target := req.URL.Query().Get("url"); target != "https://www.imdb.com/title/tt123" {
			t.Fatalf("should query with the supplied media identifier, got %q", target)
		}
		return ptgenTestResponse(http.StatusOK, `{"format":"◎简　　介\n指定来源简介","douban_link":"https://movie.douban.com/subject/987/"}`), nil
	})
	got, err := fetchMovieInfoUsingClient("intro", "test name", "subtitle", map[string]any{
		"imdb_link": "https://www.imdb.com/title/tt123/",
	}, "", []config.PTGenSource{{Type: "generic", URL: "https://configured.example/info"}}, &client)
	if err != "" || lookupCount != 1 || !strings.Contains(got.Intro, "指定来源简介") ||
		got.IMDb != "https://www.imdb.com/title/tt123" || got.Douban != "https://movie.douban.com/subject/987" {
		t.Fatalf("expected preferred provider result, got count=%d err=%q result=%+v", lookupCount, err, got)
	}
}

func TestConfiguredPTGenSanitizesUntrustedOutput(t *testing.T) {
	for _, response := range []string{
		`{"format":"[b]测试简介[/b] &lt;img src='https://trap.example/leak' onerror='execute()'&gt;\n[url=javascript:execute()]外链[/url]\n[img]https://81.70.24.5/poster.jpg[/img]\n[video]https://trap.example/video[/video]\nhttps://movie.douban.com/subject/123/?token=hidden"}`,
		`{"introduction":"[b]测试简介[/b] &lt;img src='https://trap.example/leak' onerror='execute()'&gt;\n[url=javascript:execute()]外链[/url]\n[img]https://81.70.24.5/poster.jpg[/img]\n[video]https://trap.example/video[/video]\nhttps://movie.douban.com/subject/123/?token=hidden"}`,
	} {
		result, ok := parseConfiguredPTGen([]byte(response))
		if !ok || !strings.Contains(result.Intro, "[b]测试简介[/b]") ||
			!strings.Contains(result.Intro, "https://movie.douban.com/subject/123") || result.Poster != "" {
			t.Fatalf("expected sanitized answer with existing typography and a known media link, got %+v", result)
		}
		for _, blocked := range []string{"<img", "onerror=", "[url", "[img]", "[video]", "trap.example", "81.70.24.5", "?token="} {
			if strings.Contains(result.Intro, blocked) {
				t.Fatalf("configured provider retained potentially active data %q", blocked)
			}
		}
	}
}

func TestConfiguredPTGenErrorBodyCannotSatisfyIntro(t *testing.T) {
	for _, response := range []string{
		`{"code":429,"data":"限流，无法得到资料"}`,
		`{"code":"UNAUTHORIZED","data":{"format":"◎简　　介\n虚假的成功响应"}}`,
		`{"error":{"message":"Unauthorized"},"data":{"format":"◎简　　介\n虚假的成功响应"}}`,
		`{"data":"Unknown service response"}`,
	} {
		result, _ := parseConfiguredPTGen([]byte(response))
		if hasPTGenContent("intro", result) {
			t.Fatalf("an error-shaped response produced an intro: %+v", result)
		}
	}
}
