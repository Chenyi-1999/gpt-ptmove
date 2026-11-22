package automation

import (
	"context"
	"encoding/xml"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"golang.org/x/net/html"
)

const zmptListingPageLimit = 8

var zmptPeerCount = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)$`)

type sourceListingEntry struct {
	id           string
	title        string
	flags        sourcePolicyEvidence
	seeders      int
	seedersKnown bool
}

// zmptListing 确认织梦实站及已验证的列表路径，其他站点继续使用声明的列表或 RSS。
func zmptListing(source Profile) (*url.URL, bool) {
	listing, err := Endpoint(source, source.Paths.Listing, "", "", "")
	if err != nil {
		return nil, false
	}
	u, err := url.Parse(listing)
	return u, err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "zmpt.cc") && u.EscapedPath() == "/torrents.php"
}

// zmptListingURL 先搜索体积较小的官种，再按完整种子的字节数核对体积区间。
func zmptListingURL(base *url.URL, settings policy.Settings, page int) string {
	u := *base
	query := u.Query()
	query.Set("tag_id", "3")
	// 站点接受整数 GB；向外取整后由完整种子字节数再核对精确边界。
	query.Set("size_begin", strconv.Itoa(int(math.Floor(settings.MinSizeGiB))))
	query.Set("size_end", strconv.Itoa(int(math.Ceil(settings.MaxSizeGiB))))
	query.Set("sort", "5")
	query.Set("type", "asc")
	if settings.MinSeeders > 0 {
		query.Set("incldead", "1")
		query.Set("seeders_begin", strconv.Itoa(settings.MinSeeders))
	}
	if settings.MinLeechers > 0 {
		query.Set("leechers_begin", strconv.Itoa(settings.MinLeechers))
	}
	query.Set("page", strconv.Itoa(page))
	u.RawQuery = query.Encode()
	return u.String()
}

// recheckZmptListingEntry 按当前详情的标题重新读取织梦账号自己的种子行。
// 只接受同一数字标识的候选；标题查不到或账号页面无法验证时终止执行。
func (s *Scanner) recheckZmptListingEntry(ctx context.Context, base *url.URL, source Profile, cookie, id, title string) (sourceListingEntry, error) {
	if title == "" {
		return sourceListingEntry{}, errors.New("源站下载历史检查缺少种子标题")
	}
	u := *base
	query := url.Values{"tag_id": {"3"}, "search": {title}, "search_area": {"0"}, "search_mode": {"0"}}
	u.RawQuery = query.Encode()
	doc, err := s.readListing(ctx, u.String(), cookie)
	if err != nil {
		return sourceListingEntry{}, err
	}
	for _, row := range zmptListingEntries(doc, source) {
		if row.id == id {
			return row, nil
		}
	}
	return sourceListingEntry{}, errors.New("源站官种或账号下载记录无法重新核对")
}

// zmptTitle 提取详情主标题，忽略嵌套的促销和剩余时间小部件。
func zmptTitle(heading *html.Node) string {
	var title strings.Builder
	for child := heading.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			title.WriteString(child.Data)
		}
	}
	if value := strings.TrimSpace(title.String()); value != "" {
		return value
	}
	return strings.TrimSpace(text(heading))
}

// readListing 读取列表并拒绝跳转到同源的其他页面，避免登录或错误页误判。
func (s *Scanner) readListing(ctx context.Context, listing, cookie string) (*html.Node, error) {
	u, err := url.Parse(listing)
	if err != nil {
		return nil, errors.New("源站列表地址无效")
	}
	body, err := s.get(ctx, listing, cookie, 4<<20, func(next *url.URL) bool {
		return next.EscapedPath() == u.EscapedPath() && next.Query().Encode() == u.Query().Encode()
	})
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil || len(Select(doc, `input[type="password"]`)) > 0 {
		return nil, errors.New("源站列表无法解析或要求重新登录")
	}
	return doc, nil
}

// zmptListingEntries 只从实际种子列表行读取账号进度及标签。置顶条目仍需逐条校验。
func zmptListingEntries(doc *html.Node, source Profile) []sourceListingEntry {
	rows := Select(doc, "table.torrents > tbody > tr, table.torrents > tr")
	entries := make([]sourceListingEntry, 0, len(rows))
	for _, row := range rows {
		for _, link := range Select(row, `table.torrentname a[href*="details.php?id="]`) {
			id, err := SourceRestrictionID(source, attr(link, "href"))
			if err != nil {
				continue
			}
			title := strings.TrimSpace(text(link))
			if labels := Select(link, "b"); len(labels) > 0 {
				title = strings.TrimSpace(text(labels[0]))
			}
			seeders, seedersKnown := zmptListingSeeders(row, source, id)
			entries = append(entries, sourceListingEntry{
				id:           id,
				title:        title,
				flags:        listingSourceEvidence(row, source),
				seeders:      seeders,
				seedersKnown: seedersKnown,
			})
			break
		}
	}
	return entries
}

// zmptListingSeeders 提取当前种子行中指向同一详情 ID 的做种人数链接。
// 只接受 #seeders 锚点和纯数字文本，0 也是有效值，歧义数据留给详情页回退。
func zmptListingSeeders(row *html.Node, source Profile, id string) (int, bool) {
	seeders := 0
	known := false
	for _, link := range Select(row, `a[href*="#seeders"]`) {
		u, err := url.Parse(attr(link, "href"))
		if err != nil || !strings.EqualFold(u.Fragment, "seeders") {
			continue
		}
		u.Fragment = ""
		linkID, err := SourceRestrictionID(source, u.String())
		if err != nil || linkID != id {
			continue
		}
		value := strings.TrimSpace(text(link))
		if !zmptPeerCount.MatchString(value) {
			continue
		}
		count, err := strconv.Atoi(strings.ReplaceAll(value, ",", ""))
		if err != nil {
			continue
		}
		if known && count != seeders {
			return 0, false
		}
		seeders, known = count, true
	}
	return seeders, known
}

// genericSourceEntries 兼容非织梦源站的订阅和自定义列表，只保留种子标识。
func (s *Scanner) genericSourceEntries(ctx context.Context, source Profile, cookie, rss string) ([]sourceListingEntry, error) {
	if rss == "" {
		listing, err := Endpoint(source, source.Paths.Listing, "", "", "")
		if err != nil {
			return nil, err
		}
		doc, err := s.readListing(ctx, listing, cookie)
		if err != nil {
			return nil, err
		}
		if source.Selectors.DetailLink == "" {
			return nil, errors.New("源站未配置详情链接选择器")
		}
		entries := []sourceListingEntry{}
		for _, n := range Select(doc, source.Selectors.DetailLink) {
			if id := torrentID(attr(n, "href")); id != "" {
				entries = append(entries, sourceListingEntry{id: id})
			}
		}
		return entries, nil
	}
	safeRSS, err := sameOrigin(source.BaseURL, rss)
	if err != nil {
		return nil, err
	}
	body, err := s.get(ctx, safeRSS, cookie, 4<<20)
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
	entries := []sourceListingEntry{}
	for _, item := range feed.Channel.Items {
		for _, link := range []string{item.Link, item.GUID, item.Enclosure.URL} {
			if id := torrentID(link); id != "" {
				entries = append(entries, sourceListingEntry{id: id})
				break
			}
		}
	}
	return entries, nil
}
