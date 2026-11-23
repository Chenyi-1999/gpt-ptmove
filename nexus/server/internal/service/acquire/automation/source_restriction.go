package automation

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"golang.org/x/net/html"
)

var sourceNumericID = regexp.MustCompile(`^[0-9]+$`)

// SourceRestrictionID 将服务端迁移记录中的数字或详情地址归一为站点配置的详情标识。
// 每个非空引用必须是同一条详情；返回的标识可用于重建地址，无需沿用旧查询参数。
func SourceRestrictionID(p Profile, refs ...string) (string, error) {
	id := ""
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		current := ref
		if !sourceNumericID.MatchString(current) {
			safe, err := ValidateDetailURL(p, ref)
			if err != nil {
				return "", err
			}
			current = torrentID(safe)
			endpoint, err := Endpoint(p, p.Paths.Detail, current, "", "")
			if err != nil || !sameSourceDetail(safe, endpoint, current) {
				return "", errors.New("源站详情地址与站点声明不匹配")
			}
		}
		if id != "" && current != id {
			return "", errors.New("源站详情标识不一致")
		}
		id = current
	}
	if id == "" {
		return "", errMissingTorrentID
	}
	return id, nil
}

func sameSourceDetail(link, endpoint, id string) bool {
	got, err := url.Parse(link)
	if err != nil {
		return false
	}
	want, err := url.Parse(endpoint)
	if err != nil || got.EscapedPath() != want.EscapedPath() || torrentID(link) != id {
		return false
	}
	query, err := url.ParseQuery(got.RawQuery)
	if err != nil {
		return false
	}
	for _, key := range []string{"id", "torrent_id"} {
		if values, ok := query[key]; ok && (len(values) != 1 || values[0] != id) {
			return false
		}
	}
	return true
}

// candidateDetailURL 根据公开站点模板生成预览链接，避免转发列表原文或凭据信息。
func candidateDetailURL(p Profile, id string) string {
	detail, err := Endpoint(p, p.Paths.Detail, id, "", "")
	if err != nil || (!strings.HasPrefix(detail, "https://") && !strings.HasPrefix(detail, "http://")) || !sameSourceDetail(detail, detail, id) {
		return ""
	}
	checked, err := ValidateDetailURL(p, detail)
	if err != nil {
		return ""
	}
	parsed, _ := url.Parse(checked)
	for key := range parsed.Query() {
		if strings.Contains(strings.ToLower(key), "key") {
			return ""
		}
	}
	return checked
}

// InspectSourceRestriction 查询真实详情页上的限制。允许时再读取该详情对应的种子，
// 返回其载荷标识以绑定发布内容。整个流程仅发送同源 GET，不写入文件。
func (s *Scanner) InspectSourceRestriction(ctx context.Context, p Profile, cookie, id string) (known, allowed bool, hash string, err error) {
	doc, _, err := s.sourceDetail(ctx, p, cookie, id)
	if err != nil {
		return false, false, "", err
	}
	known, allowed = Restrictions(doc, p)
	if !allowed {
		return known, false, "", nil
	}
	_, meta, err := s.sourceTorrent(ctx, p, cookie, id, doc)
	if err != nil {
		return false, false, "", err
	}
	return true, true, meta.InfoHash, nil
}

func (s *Scanner) sourceDetail(ctx context.Context, p Profile, cookie, id string) (*html.Node, string, error) {
	detail, err := Endpoint(p, p.Paths.Detail, id, "", "")
	if err != nil {
		return nil, "", err
	}
	body, err := s.get(ctx, detail, cookie, 4<<20, func(u *url.URL) bool {
		return sameSourceDetail(u.String(), detail, id)
	})
	if err != nil {
		return nil, detail, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	return doc, detail, err
}

func (s *Scanner) sourceTorrent(ctx context.Context, p Profile, cookie, id string, doc *html.Node) ([]byte, acquirefetch.TorrentMeta, error) {
	endpoint, err := Endpoint(p, p.Paths.Download, id, "", "")
	if err != nil {
		return nil, acquirefetch.TorrentMeta{}, err
	}
	want, _ := url.Parse(endpoint)
	acceptDownload := func(u *url.URL) bool {
		if u.EscapedPath() != want.EscapedPath() {
			return false
		}
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return false
		}
		for key, expected := range want.Query() {
			if len(expected) != 1 || len(values[key]) != 1 || values[key][0] != expected[0] {
				return false
			}
		}
		for _, key := range []string{"id", "torrent_id"} {
			if v, ok := values[key]; ok && (len(v) != 1 || v[0] != id) {
				return false
			}
		}
		return true
	}
	download := endpoint
	if nodes := Select(doc, p.Selectors.DownloadLink); len(nodes) > 0 && attr(nodes[0], "href") != "" {
		download, err = sameOrigin(p.BaseURL, attr(nodes[0], "href"))
		if err != nil {
			return nil, acquirefetch.TorrentMeta{}, err
		}
	}
	data, err := s.get(ctx, download, cookie, 8<<20, acceptDownload)
	if err != nil {
		return nil, acquirefetch.TorrentMeta{}, err
	}
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if err != nil || acquirefetch.ValidateTorrentNetwork(data, p.AnnounceHosts...) != nil {
		return nil, acquirefetch.TorrentMeta{}, errors.New("源种子元数据、私有标记或 Tracker 声明校验失败")
	}
	return data, meta, nil
}
