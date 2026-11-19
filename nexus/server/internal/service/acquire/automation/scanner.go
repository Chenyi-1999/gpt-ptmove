package automation

import (
	"context"
	"encoding/json"
	"encoding/xml"
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

// Scan 从 RSS 或列表扫描候选，并逐一读取详情、目标查重与完整种子元数据。
func (s *Scanner) Scan(ctx context.Context, source, target Profile, sourceCookie, targetCookie, rss string, settings policy.Settings) ([]policy.Candidate, error) {
	var ids []string
	if rss != "" {
		safeRSS, rssErr := sameOrigin(source.BaseURL, rss)
		if rssErr != nil {
			return nil, rssErr
		}
		body, err := s.get(ctx, safeRSS, sourceCookie, 4<<20)
		if err != nil {
			return nil, err
		}
		var feed struct {
			Channel struct {
				Items []struct {
					Link      string `xml:"link"`
					GUID      string `xml:"guid"`
					Enclosure struct {
						URL string `xml:"url,attr"`
					} `xml:"enclosure"`
				} `xml:"item"`
			} `xml:"channel"`
		}
		if xml.Unmarshal(body, &feed) != nil {
			return nil, errors.New("RSS 格式无效")
		}
		for _, item := range feed.Channel.Items {
			for _, link := range []string{item.Link, item.GUID, item.Enclosure.URL} {
				if id := torrentID(link); id != "" {
					ids = append(ids, id)
					break
				}
			}
		}
	} else {
		listing, err := Endpoint(source, source.Paths.Listing, "", "", "")
		if err != nil {
			return nil, err
		}
		body, err := s.get(ctx, listing, sourceCookie, 4<<20)
		if err != nil {
			return nil, err
		}
		doc, err := html.Parse(strings.NewReader(string(body)))
		if err != nil {
			return nil, errors.New("源站列表无法解析")
		}
		selector := source.Selectors.DetailLink
		if selector == "" {
			return nil, errors.New("源站未配置详情链接选择器")
		}
		for _, n := range Select(doc, selector) {
			if id := torrentID(attr(n, "href")); id != "" {
				ids = append(ids, id)
			}
		}
	}
	seen := map[string]bool{}
	rows := []policy.Candidate{}
	for _, id := range ids {
		if len(rows) >= 50 {
			break
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		c, err := s.Inspect(ctx, source, target, sourceCookie, targetCookie, id)
		if err != nil {
			c.ID = id
			c.Reasons = []string{"详情或查重读取失败"}
			c.Eligible = false
			rows = append(rows, c)
			continue
		}
		rows = append(rows, policy.Evaluate(c, settings))
	}
	policy.Rank(rows, settings)
	return rows, nil
}

// Inspect 读取详情中的明确权限、做种数和完整 torrent。未知结构保持拒绝。
func (s *Scanner) Inspect(ctx context.Context, source, target Profile, sourceCookie, targetCookie, id string) (policy.Candidate, error) {
	c := policy.Candidate{ID: id, Seeders: -1, Leechers: -1}
	doc, detail, err := s.sourceDetail(ctx, source, sourceCookie, id)
	c.DetailURL = detail
	if err != nil {
		return c, err
	}
	if nodes := Select(doc, source.Selectors.Title); len(nodes) > 0 {
		c.Title = strings.TrimSpace(text(nodes[0]))
	}
	c.RestrictionsKnown, c.Allowed = Restrictions(doc, source)
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
	if source.Selectors.Leechers == "" && nativeLeechers >= 0 {
		c.Leechers = nativeLeechers
	}
	c.Official = len(Select(doc, source.Selectors.Official)) > 0
	for _, group := range source.OfficialGroups {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(c.Title)), "-"+strings.ToLower(group)) {
			c.Official = true
		}
	}
	data, meta, err := s.sourceTorrent(ctx, source, sourceCookie, id, doc)
	if err != nil {
		return c, err
	}
	c.Torrent = data
	c.Size = meta.Size
	c.InfoHash = meta.InfoHash
	if c.Title == "" {
		c.Title = meta.Name
	}
	search, err := Endpoint(target, target.Paths.Search, id, c.Title, c.InfoHash)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("读取地址无效")
	}
	if len(acceptURL) > 0 && !acceptURL[0](req.URL) {
		return nil, errors.New("读取地址的种子标识无效")
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
