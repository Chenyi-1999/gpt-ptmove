package repair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/platform/netproxy"
)

const (
	ptgenSourceTimeout = 6 * time.Second
	maxPTGenSourceBody = 2 << 20
)

var ptgenBBCodeTag = regexp.MustCompile(`(?i)\[/?[a-z][a-z0-9_-]*(?:=[^\]\r\n]*)?\]`)
var ptgenIntroRawURL = regexp.MustCompile(`https?://[^\s\[\]<>"']+`)
var ptgenAngleBrackets = strings.NewReplacer("<", "＜", ">", "＞")

var configuredPTGenClient = &http.Client{
	Timeout: ptgenSourceTimeout,
	// Configured providers own their credentials. Never follow their redirects.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport: &http.Transport{
		DialContext:            dialPTGenSource,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    4 * time.Second,
		MaxIdleConns:           16,
		IdleConnTimeout:        30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	},
}

func fetchConfiguredPTGen(mediaType string, links MovieInfoResult, sources []config.PTGenSource, client *http.Client) (MovieInfoResult, bool) {
	links = knownPTGenMediaLinks(links)
	if !hasPTGenMediaLink(links) {
		return MovieInfoResult{}, false
	}
	for index, source := range sources {
		kind, err := config.ResolvePTGenType(source.Type, source.URL)
		if err != nil {
			logx.Warnf(ptgenLogModule, "已跳过无效 PTGen 配置 来源=%d", index+1)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), ptgenSourceTimeout)
		result, err := readConfiguredPTGen(ctx, client, kind, source, links, mediaType)
		cancel()
		if err != nil {
			logx.Warnf(ptgenLogModule, "PTGen 来源未返回可用资料 来源=%d 类型=%s 原因=%s", index+1, kind, err)
			continue
		}
		if hasPTGenContent(mediaType, result) {
			logx.Infof(ptgenLogModule, "PTGen 自定义来源命中 来源=%d 类型=%s 资料=%s", index+1, kind, mediaType)
			return result, true
		}
		logx.Infof(ptgenLogModule, "PTGen 来源缺少目标资料 来源=%d 类型=%s 资料=%s", index+1, kind, mediaType)
	}
	return MovieInfoResult{}, false
}

func knownPTGenMediaLinks(links MovieInfoResult) MovieInfoResult {
	// Never pass values outside of known movie link hosts, even if an old record contains them.
	return MovieInfoResult{
		Douban: knownPTGenLink(links.Douban, reDoubanLink),
		IMDb:   knownPTGenLink(links.IMDb, reIMDbLink),
		TMDb:   knownPTGenLink(links.TMDb, reTMDbLink),
	}
}

func hasPTGenMediaLink(links MovieInfoResult) bool {
	links = knownPTGenMediaLinks(links)
	return firstNonEmpty(links.Douban, links.TMDb, links.IMDb) != ""
}

func hasPTGenContent(mediaType string, result MovieInfoResult) bool {
	switch mediaType {
	case "poster":
		return strings.TrimSpace(result.Poster) != ""
	case "intro":
		return strings.TrimSpace(result.Intro) != ""
	default:
		return false
	}
}

func readConfiguredPTGen(ctx context.Context, client *http.Client, kind string, source config.PTGenSource, links MovieInfoResult, mediaType string) (MovieInfoResult, error) {
	req, err := newPTGenSourceRequest(ctx, kind, source, links)
	if err != nil {
		return MovieInfoResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		// net/http errors may contain a full URL, including the CSPT credential.
		return MovieInfoResult{}, errors.New("连接失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MovieInfoResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPTGenSourceBody+1))
	if err != nil || len(body) > maxPTGenSourceBody {
		return MovieInfoResult{}, errors.New("响应读取失败或过大")
	}
	result, ok := parseConfiguredPTGen(body)
	if !ok {
		return MovieInfoResult{}, errors.New("响应格式无效")
	}
	// A configured provider also controls the returned image URL. Keep protected
	// and local addresses away from the client and downstream image handling.
	if mediaType == "poster" && result.Poster != "" {
		image, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(result.Poster, "[img]"), "[/img]"))
		if err != nil {
			return MovieInfoResult{}, errors.New("海报地址无效")
		}
		if _, err := resolvePTGenAddresses(ctx, image.Hostname()); err != nil {
			return MovieInfoResult{}, errors.New("海报地址不可用")
		}
	}
	return result, nil
}

func newPTGenSourceRequest(ctx context.Context, kind string, source config.PTGenSource, links MovieInfoResult) (*http.Request, error) {
	endpoint, err := url.Parse(source.URL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("接口地址无效")
	}
	resource := firstNonEmpty(links.Douban, links.TMDb, links.IMDb)
	query := endpoint.Query()
	token := strings.TrimSpace(source.Token)
	method := http.MethodGet
	var body io.Reader
	tokenHeader, tokenValue := "Authorization", "Bearer "+token
	switch kind {
	case "cspt":
		if !strings.EqualFold(endpoint.Hostname(), "cspt.top") || (endpoint.Port() != "" && endpoint.Port() != "443") ||
			(strings.TrimRight(endpoint.Path, "/") != "" && strings.TrimRight(endpoint.Path, "/") != "/api/ptgen/query") {
			return nil, errors.New("财神接口地址无效")
		}
		if token == "" || token == "." || token == ".." || strings.ContainsAny(token, "/\\\r\n") {
			return nil, errors.New("财神令牌缺失或格式无效")
		}
		endpoint.Path = "/api/ptgen/query/" + token
		endpoint.RawPath = "/api/ptgen/query/" + url.PathEscape(token)
		query.Set("url", resource)
		tokenHeader = ""
	case "universal":
		if endpoint.Path == "" || endpoint.Path == "/" {
			endpoint.Path = "/api/v2/info"
			endpoint.RawPath = ""
		}
		method = http.MethodPost
		raw, _ := json.Marshal(map[string]string{"url": resource, "format": "bbcode"})
		body = bytes.NewReader(raw)
		tokenHeader, tokenValue = "X-API-Key", token
	case "ourbits":
		site, sid := "douban", extractDoubanID(links.Douban)
		if sid == "" {
			site, sid = "imdb", extractIMDbID(links.IMDb)
		}
		if sid == "" {
			return nil, errors.New("缺少豆瓣或 IMDb 编号")
		}
		if path.Base(endpoint.Path) == "infogen" {
			query.Set("site", site)
			query.Set("sid", sid)
		} else {
			// Static PtGen Archive returns structured JSON at /site/sid.json.
			endpoint = endpoint.JoinPath(site, sid+".json")
		}
	case "generic":
		query.Set("url", resource)
	default:
		return nil, errors.New("接口类型无效")
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, errors.New("无法建立请求")
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9")
	req.Header.Set("User-Agent", "STARPT PTGen")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" && tokenHeader != "" {
		req.Header.Set(tokenHeader, tokenValue)
	}
	return req, nil
}

func resolvePTGenAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	ip, err := netip.ParseAddr(host)
	var addresses []netip.Addr
	if err == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("域名解析失败")
		}
	}
	for _, addr := range addresses {
		if !config.IsPTGenPublicAddress(addr) {
			return nil, errors.New("接口解析到了禁止连接的地址")
		}
	}
	return addresses, nil
}

func dialPTGenSource(ctx context.Context, network, endpoint string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, errors.New("接口地址无效")
	}
	addresses, err := resolvePTGenAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addresses {
		// Dial the validated numeric address without resolving the host again.
		conn, err := netproxy.ProtectedDialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("目标连接失败")
}

func knownPTGenLink(raw string, pattern *regexp.Regexp) string {
	return strings.TrimRight(pattern.FindString(raw), "/")
}

func parseConfiguredPTGen(raw []byte) (MovieInfoResult, bool) {
	body := strings.TrimSpace(string(raw))
	var result MovieInfoResult
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err == nil {
		data, _ := root["data"].(map[string]any)
		if ptgenReportedError(root) || ptgenReportedError(data) {
			return result, false
		}
		format := firstNonEmpty(
			pickFirstString(root, "format", "content"),
			pickFirstString(data, "format", "content"),
		)
		// Some legacy services send their formatted output directly as
		// data. A bare data string could also be an unstructured error.
		if legacyData := pickFirstString(root, "data"); format == "" && looksLikePTGenFormat(legacyData) {
			format = legacyData
		}
		result = parseConfiguredPTGenFormat(format,
			firstNonEmpty(pickFirstString(root, "imdb_link", "imdb"), pickFirstString(data, "imdb_link", "imdb")),
			firstNonEmpty(pickFirstString(root, "douban_link", "douban"), pickFirstString(data, "douban_link", "douban")),
			firstNonEmpty(pickFirstString(root, "tmdb_link", "tmdb"), pickFirstString(data, "tmdb_link", "tmdb")),
		)
		if result.Poster == "" {
			result.Poster = firstNonEmpty(
				ptgenPosterBBCode(pickFirstString(data, "poster", "poster_url", "posterUrl")),
				ptgenPosterBBCode(pickFirstString(root, "poster", "poster_url", "posterUrl")),
			)
		}
		if result.Intro == "" {
			result.Intro = firstNonEmpty(structuredPTGenIntro(data), structuredPTGenIntro(root))
		}
		return result, true
	}
	if strings.HasPrefix(body, "<") || !looksLikePTGenFormat(body) {
		return result, false
	}
	return parseConfiguredPTGenFormat(body, "", "", ""), true
}

func looksLikePTGenFormat(value string) bool {
	return reImgBBCode.MatchString(value) || strings.ContainsAny(value, "◎❁")
}

func ptgenReportedError(record map[string]any) bool {
	if success, ok := record["success"].(bool); ok && !success {
		return true
	}
	for _, key := range []string{"ret", "code"} {
		switch code := record[key].(type) {
		case float64:
			if code != 0 && (code < 200 || code >= 300) {
				return true
			}
		case string:
			if code = strings.ToLower(strings.TrimSpace(code)); code != "" && code != "0" && code != "ok" && code != "success" &&
				(numericToInt(code) < 200 || numericToInt(code) >= 300) {
				return true
			}
		}
	}
	switch value := record["error"].(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case bool:
		return value
	default:
		return true
	}
}

func parseConfiguredPTGenFormat(format, imdb, douban, tmdb string) MovieInfoResult {
	// Keep ordered image candidates and ignore malformed / unrelated schemes.
	_, intro, _, _, _ := parseFormatContent(format, "", "", "")
	result := MovieInfoResult{
		Intro:  safeConfiguredPTGenIntro(intro),
		IMDb:   firstNonEmpty(knownPTGenLink(imdb, reIMDbLink), knownPTGenLink(format, reIMDbLink)),
		Douban: firstNonEmpty(knownPTGenLink(douban, reDoubanLink), knownPTGenLink(format, reDoubanLink)),
		TMDb:   firstNonEmpty(knownPTGenLink(tmdb, reTMDbLink), knownPTGenLink(format, reTMDbLink)),
	}
	for _, match := range reImgBBCode.FindAllStringSubmatch(format, -1) {
		if result.Poster = ptgenPosterBBCode(match[1]); result.Poster != "" {
			break
		}
	}
	return result
}

func ptgenPosterBBCode(raw string) string {
	link, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (link.Scheme != "http" && link.Scheme != "https") ||
		link.Hostname() == "" || link.User != nil || strings.ContainsAny(raw, "[]\r\n") {
		return ""
	}
	if address, err := netip.ParseAddr(link.Hostname()); err == nil && !config.IsPTGenPublicAddress(address) {
		return ""
	}
	return "[img]" + link.String() + "[/img]"
}

func structuredPTGenIntro(data map[string]any) string {
	summary := safeConfiguredPTGenIntro(pickFirstString(data, "introduction", "description", "summary", "overview"))
	if summary == "" {
		return ""
	}
	var fields []string
	for _, field := range []struct{ label, key string }{
		{"译　　名", "trans_title"}, {"片　　名", "this_title"}, {"年　　代", "year"},
		{"产　　地", "region"}, {"类　　型", "genre"}, {"语　　言", "language"},
		{"上映日期", "playdate"}, {"片　　长", "duration"}, {"集　　数", "episodes"},
		{"IMDb评分", "imdb_rating"}, {"豆瓣评分", "douban_rating"},
	} {
		if value := ptgenMetadataText(data[field.key]); value != "" {
			fields = append(fields, "◎"+field.label+"　"+value)
		}
	}
	for _, field := range []struct {
		label, key string
		pattern    *regexp.Regexp
	}{
		{"IMDb链接", "imdb_link", reIMDbLink},
		{"豆瓣链接", "douban_link", reDoubanLink},
		{"TMDB链接", "tmdb_link", reTMDbLink},
	} {
		if link := knownPTGenLink(pickFirstString(data, field.key), field.pattern); link != "" {
			fields = append(fields, "◎"+field.label+"　"+link)
		}
	}
	fields = append(fields, "", "◎简　　介", "", summary)
	return safeConfiguredPTGenIntro(strings.Join(fields, "\n"))
}

func safeConfiguredPTGenIntro(raw string) string {
	text := html.UnescapeString(raw)
	text = reImgBBCode.ReplaceAllString(text, "")
	text = reBreakLineTag.ReplaceAllString(text, "\n")
	text = reAnyHTMLTag.ReplaceAllString(text, "")
	text = ptgenAngleBrackets.Replace(text)
	text = ptgenBBCodeTag.ReplaceAllStringFunc(text, func(tag string) string {
		switch strings.ToLower(tag) {
		case "[b]", "[/b]", "[i]", "[/i]", "[u]", "[/u]", "[quote]", "[/quote]":
			return tag
		default:
			return ""
		}
	})
	text = ptgenIntroRawURL.ReplaceAllStringFunc(text, func(link string) string {
		// Sources can return links to their media records. Other returned
		// URLs do not belong in the automatically generated description.
		return firstNonEmpty(knownPTGenLink(link, reDoubanLink), knownPTGenLink(link, reIMDbLink), knownPTGenLink(link, reTMDbLink))
	})
	return strings.TrimSpace(text)
}

func ptgenMetadataText(value any) string {
	switch item := value.(type) {
	case string:
		return strings.Join(strings.Fields(item), " ")
	case []any:
		items := make([]string, 0, len(item))
		for _, v := range item {
			if text, ok := v.(string); ok && strings.TrimSpace(text) != "" {
				items = append(items, strings.Join(strings.Fields(text), " "))
			}
		}
		return strings.Join(items, " / ")
	default:
		return ""
	}
}
