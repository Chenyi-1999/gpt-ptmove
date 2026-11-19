package automation

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var errMissingTorrentID = errors.New("详情地址缺少种子标识")
var restrictedTerms = []string{
	"禁转", "禁轉", "禁止转载", "禁止轉載", "严禁转载", "嚴禁轉載", "谢绝转载", "謝絕轉載",
	"禁止转发", "禁止轉發", "禁止转种", "禁止轉種", "限转", "限轉", "仅限本站", "僅限本站",
	"do not repost", "no repost", "do not reupload", "do not re-upload", "reupload forbidden",
	"do not cross-seed", "no cross-seeding", "cross-seeding prohibited",
}

// Restrictions 将全页限制优先于任意自定义允许选择器，未知页面结构保持拒绝。
func Restrictions(doc *html.Node, p Profile) (bool, bool) {
	plain := strings.ToLower(text(doc))
	badges := Select(doc, "[title], [alt], [aria-label]")
	for _, term := range restrictedTerms {
		if strings.Contains(plain, term) {
			return true, false
		}
		for _, badge := range badges {
			for _, key := range []string{"title", "alt", "aria-label"} {
				if strings.Contains(strings.ToLower(attr(badge, key)), term) {
					return true, false
				}
			}
		}
	}
	if len(Select(doc, p.Selectors.RestrictionDenied)) > 0 {
		return true, false
	}
	if len(Select(doc, `input[type="password"]`)) > 0 {
		return false, false
	}
	if len(Select(doc, p.Selectors.RestrictionAllowed)) > 0 {
		return true, true
	}
	recognized := len(Select(doc, "#kdescr, #description, .torrent-description")) > 0 && len(Select(doc, `a[href*="download.php"], a[href$=".torrent"]`)) > 0 && len(Select(doc, "h1, #top, .torrent-title")) > 0
	metadata := false
	for _, term := range []string{"基本信息", "基本資訊", "种子信息", "種子資訊", "副标题", "副標題", "文件列表", "檔案列表", "type", "size"} {
		if strings.Contains(plain, term) {
			metadata = true
			break
		}
	}
	return recognized && metadata, recognized && metadata
}

// DuplicateEvidence 要求明确结果行或肯定的空结果提示，登录页和互相矛盾的页面保持未知。
func DuplicateEvidence(doc *html.Node, p Profile) (bool, bool) {
	if len(Select(doc, `input[type="password"]`)) > 0 {
		return false, false
	}
	present := len(Select(doc, p.Selectors.SearchResults)) > 0
	empty := len(Select(doc, p.Selectors.SearchEmpty)) > 0
	if p.Selectors.SearchResults == "" {
		present = len(Select(doc, `a[href*="details.php?id="], a[href*="/torrents/"]`)) > 0
	}
	if p.Selectors.SearchEmpty == "" {
		plain := strings.ToLower(text(doc))
		for _, term := range []string{"没有种子", "沒有種子", "没有找到", "沒有找到", "未找到相关", "no torrents found", "no torrent found", "nothing found", "no results found", "没有搜索到", "沒有搜索到", "没有符合条件", "沒有符合條件"} {
			if strings.Contains(plain, term) {
				empty = true
				break
			}
		}
	}
	return present != empty, present
}

// PeerCounts 只从明确的做种与下载人数标签中读取，避免把总连接数当成做种数。
func PeerCounts(doc *html.Node) (int, int) {
	plain := strings.ToLower(text(doc))
	read := func(pattern string) int {
		m := regexp.MustCompile(pattern).FindStringSubmatch(plain)
		if len(m) < 2 {
			return -1
		}
		v, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		return v
	}
	return read(`(?:做种者|做種者|做种数|做種數|seeders)[^0-9]{0,20}([0-9,]+)`), read(`(?:下载者|下載者|下载数|下載數|leechers)[^0-9]{0,20}([0-9,]+)`)
}

// ValidateDetailURL 离线验证核对地址属于冻结目标站，且包含明确种子标识。
func ValidateDetailURL(p Profile, detail string) (string, error) {
	u, err := sameOrigin(p.BaseURL, detail)
	if err != nil {
		return "", err
	}
	if torrentID(u) == "" {
		return "", errMissingTorrentID
	}
	parsed, _ := url.Parse(u)
	for key := range parsed.Query() {
		for _, secret := range []string{"passkey", "token", "secret", "password", "cookie", "auth"} {
			if strings.Contains(strings.ToLower(key), secret) {
				return "", errors.New("详情地址禁止包含凭据")
			}
		}
	}
	return u, nil
}
