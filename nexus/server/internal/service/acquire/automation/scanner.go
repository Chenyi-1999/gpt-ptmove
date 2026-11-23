package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

// Profile 是扫描所需的无秘密站点声明；Cookie 与订阅地址分别注入。
type Profile struct {
	ID             string   `json:"id"`
	BaseURL        string   `json:"base_url"`
	OfficialGroups []string `json:"official_groups"`
	AnnounceHosts  []string `json:"announce_hosts"`
	Paths          struct {
		Listing  string `json:"listing"`
		Detail   string `json:"detail"`
		Download string `json:"download"`
		Search   string `json:"search"`
	} `json:"paths"`
	Selectors struct {
		Table              string `json:"table"`
		DetailLink         string `json:"detail_link"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		DownloadLink       string `json:"download_link"`
		Tags               string `json:"tags"`
		Peers              string `json:"peers"`
		RestrictionAllowed string `json:"restriction_allowed"`
		RestrictionDenied  string `json:"restriction_denied"`
		SearchEmpty        string `json:"search_empty"`
		SearchResults      string `json:"search_results"`
		Seeders            string `json:"seeders"`
		Leechers           string `json:"leechers"`
		Official           string `json:"official"`
	} `json:"selectors"`
}

// Scanner 只使用 GET 读取站点和种子元信息；不保存文件、不改配额、不调用下载器。
type Scanner struct{ Client *http.Client }

type inspectionStage uint8

const (
	inspectPreview inspectionStage = iota
	inspectDownload
	inspectPublish
	inspectPublishSource
)

// NewScanner 创建有界请求的读取器，使用全局受保护传输层。
func NewScanner() *Scanner { return &Scanner{Client: &http.Client{Timeout: 30 * time.Second}} }

// ProfileFrom 将公共站点声明转成扫描视图，避免持有凭据。
func ProfileFrom(value any) (Profile, error) {
	var p Profile
	raw, err := json.Marshal(value)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}

// Scan 从 RSS 或列表扫描候选，并逐一读取详情、查重证据与完整种子元数据。
func (s *Scanner) Scan(ctx context.Context, source, target Profile, sourceCookie, targetCookie, rss string, settings policy.Settings, excludedIDs ...map[string]bool) ([]policy.Candidate, error) {
	base, zmpt := zmptListing(source)
	// 默认保持 50 条详情请求的上限，较大的月度配置允许读取更多筛选后的条目。
	detailLimit := 50
	if zmpt {
		detailLimit = min(200, max(detailLimit, settings.MonthlyLimit*2))
	}
	seen := map[string]bool{}
	rows := []policy.Candidate{}
	inspected, listedSkips := 0, 0
	consume := func(entries []sourceListingEntry) {
		for _, entry := range entries {
			if ctx.Err() != nil || inspected >= detailLimit {
				break
			}
			if seen[entry.id] {
				continue
			}
			seen[entry.id] = true
			if len(excludedIDs) > 0 && excludedIDs[0][entry.id] {
				continue
			}
			if zmpt && (!entry.flags.official || entry.flags.restricted || entry.flags.episode || entry.flags.downloaded) {
				if listedSkips < 50 {
					seeders := -1
					if entry.seedersKnown {
						seeders = entry.seeders
					}
					c := policy.Candidate{ID: entry.id, Title: entry.title, Seeders: seeders, Leechers: -1}
					applySourceEvidence(&c, entry.flags)
					rows = append(rows, policy.Evaluate(c, settings))
					listedSkips++
				}
				continue
			}
			inspected++
			reportScanProgress(ctx, ScanProgress{Stage: "detail", Current: inspected, Completed: inspected - 1})
			c, err := s.inspect(ctx, source, target, sourceCookie, targetCookie, entry, inspectPreview)
			reportScanProgress(ctx, ScanProgress{Stage: "detail", Completed: inspected})
			if err != nil {
				c.Reasons = []string{"详情或查重读取失败"}
				rows = append(rows, c)
				continue
			}
			rows = append(rows, policy.Evaluate(c, settings))
		}
	}
	if zmpt {
		for page := 0; page < zmptListingPageLimit && inspected < detailLimit; page++ {
			reportScanProgress(ctx, ScanProgress{Stage: "listing", Page: page + 1, Completed: inspected})
			doc, err := s.readListing(ctx, zmptListingURL(base, settings, page), sourceCookie)
			if err != nil {
				return nil, err
			}
			entries := zmptListingEntries(doc, source)
			if len(entries) == 0 {
				break
			}
			before := len(seen)
			consume(entries)
			if len(seen) == before {
				break
			}
		}
	} else {
		reportScanProgress(ctx, ScanProgress{Stage: "listing"})
		entries, err := s.genericSourceEntries(ctx, source, sourceCookie, rss)
		if err != nil {
			return nil, err
		}
		consume(entries)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].DetailURL = candidateDetailURL(source, rows[i].ID)
	}
	policy.Rank(rows, settings)
	return rows, nil
}

// Inspect 读取详情中的明确权限、做种数和完整 torrent。未知结构保持拒绝。
func (s *Scanner) Inspect(ctx context.Context, source, target Profile, sourceCookie, targetCookie, id string) (policy.Candidate, error) {
	return s.inspect(ctx, source, target, sourceCookie, targetCookie, sourceListingEntry{id: id}, inspectDownload)
}

// InspectForPublish 在任务自己的完整下载之后复核源站与目标站，保留已下载标记给调用方。
// 它允许已跟踪任务读取完整证据，仍按相同的官种、分集和禁转规则拒绝候选。
func (s *Scanner) InspectForPublish(ctx context.Context, source, target Profile, sourceCookie, targetCookie, id string) (policy.Candidate, error) {
	return s.inspect(ctx, source, target, sourceCookie, targetCookie, sourceListingEntry{id: id}, inspectPublish)
}

// InspectSourceForPublish 复核批量查重任务的源站资格和种子元数据；调用方持有单独的目标证据。
func (s *Scanner) InspectSourceForPublish(ctx context.Context, source Profile, sourceCookie, id string) (policy.Candidate, error) {
	return s.inspect(ctx, source, Profile{}, sourceCookie, "", sourceListingEntry{id: id}, inspectPublishSource)
}

func (s *Scanner) inspect(ctx context.Context, source, target Profile, sourceCookie, targetCookie string, entry sourceListingEntry, stage inspectionStage) (policy.Candidate, error) {
	c := policy.Candidate{ID: entry.id, Title: entry.title, Seeders: -1, Leechers: -1}
	if entry.seedersKnown {
		c.Seeders = entry.seeders
	}
	listedEntry := entry
	doc, detail, err := s.sourceDetail(ctx, source, sourceCookie, entry.id)
	if err != nil {
		return c, err
	}
	listing, zmpt := zmptListing(source)
	titles := Select(doc, source.Selectors.Title)
	if zmpt {
		if headings := Select(doc, "h1#top"); len(headings) > 0 {
			titles = headings
		}
	}
	if len(titles) > 0 {
		title := strings.TrimSpace(text(titles[0]))
		if zmpt {
			title = zmptTitle(titles[0])
		}
		if title != "" {
			c.Title = title
		}
	}
	c.RestrictionsKnown, c.Allowed = Restrictions(doc, source)
	applySourceEvidence(&c, entry.flags)
	applySourceEvidence(&c, detailSourceEvidence(doc, source, zmpt))
	if zmpt && stage != inspectPreview && c.RestrictionsKnown && c.Allowed && !c.Episode {
		row, rowErr := s.recheckZmptListingEntry(ctx, listing, source, sourceCookie, entry.id, c.Title)
		if rowErr != nil {
			return c, rowErr
		}
		applySourceEvidence(&c, row.flags)
		listedEntry = row
	}
	seedSelector := source.Selectors.Seeders
	if seedSelector == "" {
		seedSelector = source.Selectors.Peers
	}
	if nodes := Select(doc, seedSelector); len(nodes) > 0 {
		match := regexp.MustCompile(`\d+`).FindString(strings.ReplaceAll(text(nodes[0]), ",", ""))
		if match != "" {
			c.Seeders, _ = strconv.Atoi(match)
		}
	}
	if nodes := Select(doc, source.Selectors.Leechers); len(nodes) > 0 {
		match := regexp.MustCompile(`\d+`).FindString(strings.ReplaceAll(text(nodes[0]), ",", ""))
		if match != "" {
			c.Leechers, _ = strconv.Atoi(match)
		}
	}
	nativeSeeders, nativeLeechers := PeerCounts(doc)
	if source.Selectors.Seeders == "" && nativeSeeders >= 0 {
		c.Seeders = nativeSeeders
	}
	if zmpt && listedEntry.seedersKnown {
		c.Seeders = listedEntry.seeders
	}
	if source.Selectors.Leechers == "" && nativeLeechers >= 0 {
		c.Leechers = nativeLeechers
	}
	if !zmpt {
		c.Official = c.Official || len(Select(doc, source.Selectors.Official)) > 0
		for _, group := range source.OfficialGroups {
			if strings.HasSuffix(strings.ToLower(c.Title), "-"+strings.ToLower(strings.TrimSpace(group))) {
				c.Official = true
			}
		}
	}
	checked := c
	ownDownload := stage == inspectPublish || stage == inspectPublishSource
	if ownDownload {
		checked.PreviouslyDownloaded = false
	}
	if len(policy.SourceRejection(checked)) > 0 {
		return c, nil
	}
	data, meta, err := s.sourceTorrent(ctx, source, sourceCookie, entry.id, doc)
	if err != nil {
		return c, err
	}
	c.Torrent = data
	c.Size = meta.Size
	c.InfoHash = meta.InfoHash
	if c.Title == "" {
		c.Title = meta.Name
	}
	if stage == inspectPublishSource {
		return c, nil
	}
	if !ownDownload && IsZmPTSource(source) {
		c.DuplicateKnown, c.Duplicate = s.zmptCrossSeedDuplicate(ctx, source, target, sourceCookie, doc, detail, c.InfoHash)
		return c, nil
	}
	if isNovaHD(target) {
		c.DuplicateKnown, c.Duplicate = s.novaHDDuplicate(ctx, source, target, targetCookie, doc, c)
		return c, nil
	}
	search, err := Endpoint(target, target.Paths.Search, entry.id, c.Title, c.InfoHash)
	if err != nil {
		return c, err
	}
	body, err := s.get(ctx, search, targetCookie, 4<<20)
	if err != nil {
		return c, err
	}
	doc, err = html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return c, err
	}
	c.DuplicateKnown, c.Duplicate = DuplicateEvidence(doc, target)
	return c, nil
}

// TorrentAt 下载指定目标详情页对应的种子，用于核对，绝不上传或编辑。
func (s *Scanner) TorrentAt(ctx context.Context, p Profile, cookie, detail string) ([]byte, string, error) {
	u, err := sameOrigin(p.BaseURL, detail)
	if err != nil {
		return nil, "", err
	}
	id := torrentID(u)
	if id == "" {
		return nil, "", errors.New("目标详情地址缺少种子标识")
	}
	body, err := s.get(ctx, u, cookie, 4<<20)
	if err != nil {
		return nil, "", err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", err
	}
	download, err := Endpoint(p, p.Paths.Download, id, "", "")
	if err != nil {
		return nil, "", err
	}
	if nodes := Select(doc, p.Selectors.DownloadLink); len(nodes) > 0 {
		download, err = sameOrigin(p.BaseURL, attr(nodes[0], "href"))
		if err != nil {
			return nil, "", err
		}
	}
	data, err := s.get(ctx, download, cookie, 8<<20)
	return data, u, err
}

// Endpoint 严格限制声明路径在当前站点同源，避免从正文跳转到其他服务。
func Endpoint(p Profile, path, id, query, hash string) (string, error) {
	if path == "" {
		return "", errors.New("站点缺少自动化路径")
	}
	path = strings.NewReplacer("{id}", url.QueryEscape(id), "{query}", url.QueryEscape(query), "{hash}", url.QueryEscape(hash)).Replace(path)
	return sameOrigin(p.BaseURL, path)
}
func sameOrigin(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return "", errors.New("站点地址无效")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", errors.New("站点路径无效")
	}
	u = b.ResolveReference(u)
	if u.Scheme != b.Scheme || !strings.EqualFold(u.Host, b.Host) || u.User != nil || u.Fragment != "" {
		return "", errors.New("站点链接必须保持同源")
	}
	return u.String(), nil
}
func (s *Scanner) get(ctx context.Context, target, cookie string, limit int64, acceptURL ...func(*url.URL) bool) ([]byte, error) {
	return s.getWithHeaders(ctx, target, cookie, limit, nil, acceptURL...)
}

func (s *Scanner) getWithHeaders(ctx context.Context, target, cookie string, limit int64, headers http.Header, acceptURL ...func(*url.URL) bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("读取地址无效")
	}
	if len(acceptURL) > 0 && !acceptURL[0](req.URL) {
		return nil, errors.New("读取地址的种子标识无效")
	}
	for key, values := range headers {
		req.Header[key] = values
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "PTNexus-Automation/1.0")
	client := *s.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("站点重定向过多")
		}
		if req.URL.Scheme != via[0].URL.Scheme || !strings.EqualFold(req.URL.Host, via[0].URL.Host) || req.URL.User != nil || req.URL.Fragment != "" {
			return errors.New("拒绝跨站重定向")
		}
		if len(acceptURL) > 0 && !acceptURL[0](req.URL) {
			return errors.New("拒绝跳转到其他源站资源")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("站点读取失败，请检查凭据和网络保护设置")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("站点返回状态 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("站点响应超出限制或读取失败")
	}
	return data, nil
}
func torrentID(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	for _, key := range []string{"id", "torrent_id"} {
		if v := u.Query().Get(key); regexp.MustCompile(`^[0-9]+$`).MatchString(v) {
			return v
		}
	}
	m := regexp.MustCompile(`/(?:details|torrent|torrents)/(\d+)(?:/|$)`).FindStringSubmatch(u.Path)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(text(c))
		b.WriteByte(' ')
	}
	return b.String()
}
