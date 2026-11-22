package automation

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

const (
	novaHDOrigin        = "https://pt.novahd.top"
	novaHDSearchPath    = "/torrents.php"
	novaHDEmptyNotice   = "没有种子。请用准确的关键字重试。"
	novaHDResultLimit   = 100
	novaHDPayloadChecks = 3
)

var (
	novaHDYear       = regexp.MustCompile(`^(19|20)[0-9]{2}$`)
	novaHDSeason     = regexp.MustCompile(`^s([0-9]{1,3})(?:e[0-9]{1,4})*$`)
	novaHDResolution = regexp.MustCompile(`^(480|576|720|1080|1440|2160|4320)[pi]$`)
	novaHDGroup      = regexp.MustCompile(`(?i)-([a-z0-9][a-z0-9_-]{1,29})$`)
)

type novaHDHit struct {
	id    string
	title string
}

type novaHDResults struct {
	hits     []novaHDHit
	complete bool
}

type novaHDPayload struct {
	matches      bool
	largestVideo int64
}

type novaHDRelease struct {
	work   string
	season string
	group  string
}

// isNovaHD 仅识别已验证页面结构的 NovaHD HTTPS 来源。
func isNovaHD(p Profile) bool {
	return strings.EqualFold(strings.TrimRight(p.BaseURL, "/"), novaHDOrigin)
}

// novaHDDuplicate 依实际搜索范围及完整载荷判定。未识别页面或未比较完的可能版本
// 保持未知；织梦的辅种链接只用于定位可验证的 NovaHD 种子。
func (s *Scanner) novaHDDuplicate(ctx context.Context, source, target Profile, cookie string, sourceDoc *html.Node, candidate policy.Candidate) (known, duplicate bool) {
	title := strings.TrimSpace(candidate.Title)
	release := novaHDReleaseOf(title)
	if len(title) == 0 || len(title) > 220 || release.work == "" {
		return false, false
	}
	checked := make(map[string]novaHDPayload, novaHDPayloadChecks)
	verify := func(id string) novaHDPayload {
		if evidence, ok := checked[id]; ok {
			return evidence
		}
		if len(checked) >= novaHDPayloadChecks || ctx.Err() != nil {
			return novaHDPayload{}
		}
		evidence := s.novaHDPayload(ctx, target, cookie, id, candidate)
		checked[id] = evidence
		return evidence
	}
	if _, isZmPT := zmptListing(source); isZmPT {
		if id := novaHDHintID(sourceDoc, target); id != "" && verify(id).matches {
			return true, true
		}
	}

	// 按作品名取结果，避免站点未标年份或采用不同的季编号格式时漏查。
	broad := s.novaHDSearch(ctx, target, cookie, release.work, "0")
	exactTitle := novaHDWords(title)
	// 宽搜索不完整时，精确查询可以先确认命中的版本，空结果不足以排除异名种子。
	if !broad.complete {
		exact := s.novaHDSearch(ctx, target, cookie, title, "2")
		for _, hit := range exact.hits {
			if novaHDWords(hit.title) == exactTitle && verify(hit.id).matches {
				return true, true
			}
		}
	}
	sourceVideo, _ := acquirefetch.LargestVideoSizeFromTorrentBytes(candidate.Torrent)
	sort.SliceStable(broad.hits, func(i, j int) bool {
		return novaHDWords(broad.hits[i].title) == exactTitle && novaHDWords(broad.hits[j].title) != exactTitle
	})
	distinct := true
	for _, hit := range broad.hits {
		other := novaHDReleaseOf(hit.title)
		// 年份可能表示首播、上线或发行，不能据此跳过载荷核对。
		if release.work == other.work && knownDifference(release.season, other.season) {
			continue
		}
		evidence := verify(hit.id)
		if evidence.matches {
			return true, true
		}
		// 用最大视频的精确长度排除仅附加样张、字幕或 NFO 导致的总大小变化。
		if !knownDifference(release.group, other.group) || sourceVideo <= 0 || evidence.largestVideo <= 0 || evidence.largestVideo == sourceVideo {
			distinct = false
		}
	}
	return broad.complete && distinct, false
}

// novaHDPayload 读取同一详情对应的私有种子元数据，允许站点写入不同的 source 字段。
func (s *Scanner) novaHDPayload(ctx context.Context, target Profile, cookie, id string, source policy.Candidate) novaHDPayload {
	doc, _, err := s.sourceDetail(ctx, target, cookie, id)
	if err != nil || len(Select(doc, `input[type="password"]`)) != 0 {
		return novaHDPayload{}
	}
	data, meta, err := s.sourceTorrent(ctx, target, cookie, id, doc)
	if err != nil || meta.Size <= 0 {
		return novaHDPayload{}
	}
	largestVideo, _ := acquirefetch.LargestVideoSizeFromTorrentBytes(data)
	return novaHDPayload{
		matches:      meta.InfoHash == source.InfoHash || acquirefetch.CompareTorrentPayload(source.Torrent, data) == nil,
		largestVideo: largestVideo,
	}
}

func knownDifference(left, right string) bool {
	return left != "" && right != "" && left != right
}

func novaHDWords(title string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ")
}

func novaHDReleaseOf(title string) novaHDRelease {
	var release novaHDRelease
	if group := novaHDGroup.FindStringSubmatch(strings.TrimSpace(title)); len(group) > 0 {
		release.group = strings.ToLower(group[1])
		title = title[:strings.LastIndex(title, group[0])]
	}
	tokens := strings.Fields(novaHDWords(title))
	releaseAt := len(tokens)
	multiSeason := false
	for i, token := range tokens {
		if i == 0 {
			continue // 片名可直接以年份或数字开头。
		}
		switch {
		case novaHDYear.MatchString(token), novaHDResolution.MatchString(token):
		default:
			season := novaHDSeason.FindStringSubmatch(token)
			if len(season) == 0 {
				continue
			}
			normalized := strings.TrimLeft(season[1], "0")
			if release.season == "" {
				release.season = normalized
			} else if release.season != normalized {
				multiSeason = true
			}
		}
		releaseAt = min(releaseAt, i)
	}
	release.work = strings.Join(tokens[:releaseAt], " ")
	if multiSeason {
		release.season = ""
	}
	return release
}

// novaHDHintID 只接受织梦辅种区中实际存在的绝对 NovaHD 详情链接。
func novaHDHintID(doc *html.Node, target Profile) string {
	for _, link := range Select(doc, "#resultContainer a[href]") {
		ref := strings.TrimSpace(attr(link, "href"))
		u, err := url.Parse(ref)
		if err != nil || !u.IsAbs() {
			continue
		}
		if id := novaHDTorrentID(target, ref); id != "" {
			return id
		}
	}
	return ""
}

// novaHDSearch 查询标题、包括断种，拒绝路径、条件或搜索词被跳转改写的响应。
func (s *Scanner) novaHDSearch(ctx context.Context, p Profile, cookie, query, mode string) novaHDResults {
	if query == "" || ctx.Err() != nil {
		return novaHDResults{}
	}
	values := url.Values{
		"search":      {query},
		"search_area": {"0"},
		"search_mode": {mode},
		"incldead":    {"0"},
	}
	search, err := sameOrigin(p.BaseURL, novaHDSearchPath+"?"+values.Encode())
	if err != nil {
		return novaHDResults{}
	}
	body, err := s.get(ctx, search, cookie, 4<<20, func(u *url.URL) bool {
		got, err := url.ParseQuery(u.RawQuery)
		return err == nil && u.EscapedPath() == novaHDSearchPath && got.Encode() == values.Encode()
	})
	if err != nil {
		return novaHDResults{}
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return novaHDResults{}
	}
	return novaHDReadResults(doc, p, query)
}

// novaHDReadResults 仅解析真实结果表与已知空结果通知。登录页、冲突页面和超出范围的结果保持未知。
func novaHDReadResults(doc *html.Node, p Profile, query string) novaHDResults {
	inputs := Select(doc, `input#searchinput[name="search"]`)
	if len(inputs) != 1 || strings.TrimSpace(attr(inputs[0], "value")) != query || len(Select(doc, `input[type="password"]`)) != 0 {
		return novaHDResults{}
	}
	empty := novaHDHasEmptyNotice(doc, query)
	tables := Select(doc, "table.torrents")
	if len(tables) == 0 {
		return novaHDResults{complete: empty && !novaHDPaginated(doc, p, query)}
	}
	if len(tables) != 1 || empty {
		return novaHDResults{}
	}

	rows := Select(tables[0], "table.torrents > tbody > tr")
	result := novaHDResults{complete: !novaHDPaginated(doc, p, query)}
	seen := make(map[string]bool)
	for _, row := range rows {
		if len(Select(row, "table.torrents > tbody > tr > td.colhead, table.torrents > thead > tr > th")) != 0 {
			continue
		}
		hit := novaHDReadHit(row, p)
		if hit.id == "" || seen[hit.id] {
			result.complete = false
			continue
		}
		seen[hit.id] = true
		result.hits = append(result.hits, hit)
	}
	result.complete = result.complete && len(result.hits) > 0 && len(result.hits) < novaHDResultLimit
	return result
}

// novaHDHasEmptyNotice 将空结果提示限定为对应搜索标题下的同一结果容器。
func novaHDHasEmptyNotice(doc *html.Node, query string) bool {
	expected := `搜索结果 "` + strings.Join(strings.Fields(query), " ") + `"`
	for _, heading := range Select(doc, "td.embedded > h2") {
		if strings.Join(strings.Fields(text(heading)), " ") != expected {
			continue
		}
		for _, notice := range Select(heading.Parent, "td.embedded > table > tbody > tr > td.text") {
			table := notice.Parent.Parent.Parent
			if table.Parent == heading.Parent && strings.Join(strings.Fields(text(notice)), " ") == novaHDEmptyNotice {
				return true
			}
		}
	}
	return false
}

// novaHDReadHit 只识别当前结果行种子名称单元中的实际种子详情，忽略其他列中的链接。
func novaHDReadHit(row *html.Node, p Profile) novaHDHit {
	var hit novaHDHit
	for _, link := range Select(row, "table.torrentname a[title][href]") {
		id := novaHDTorrentID(p, attr(link, "href"))
		title := strings.TrimSpace(attr(link, "title"))
		if id == "" || title == "" {
			continue
		}
		current := novaHDHit{id: id, title: title}
		if hit.id != "" && hit != current {
			return novaHDHit{}
		}
		hit = current
	}
	return hit
}

func novaHDTorrentID(p Profile, href string) string {
	ref := strings.TrimSpace(href)
	safe, err := ValidateDetailURL(p, ref)
	if err != nil {
		return ""
	}
	u, err := url.Parse(safe)
	if err != nil || u.EscapedPath() != "/details.php" {
		return ""
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["id"]) != 1 || len(query["torrent_id"]) != 0 {
		return ""
	}
	id, err := strconv.ParseUint(query["id"][0], 10, 63)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != query["id"][0] {
		return ""
	}
	match, err := SourceRestrictionID(p, safe)
	if err != nil {
		return ""
	}
	return match
}

func novaHDPaginated(doc *html.Node, p Profile, query string) bool {
	for _, a := range Select(doc, `p.nexus-pagination a[href*="page="]`) {
		safe, err := sameOrigin(strings.TrimRight(p.BaseURL, "/")+novaHDSearchPath, strings.TrimSpace(attr(a, "href")))
		if err != nil {
			return true
		}
		u, err := url.Parse(safe)
		if err != nil || u.EscapedPath() != novaHDSearchPath || u.Query().Get("search") != query {
			return true
		}
		if page, err := strconv.Atoi(u.Query().Get("page")); err != nil || page != 0 {
			return true
		}
	}
	return false
}
