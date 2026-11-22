package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	zmptCrossSeedPath    = "/nodeapi/iyuu/getIyuuByInfoHash"
	zmptNoCrossSeedMatch = "查询辅种信息失败！Error: 查询失败: IYUU API Error: 未查询到可辅种数据"
)

// IsZmPTSource 仅识别已验证辅种接口的织梦 HTTPS 地址。
func IsZmPTSource(source Profile) bool {
	u, err := url.Parse(source.BaseURL)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "zmpt.cc") && u.User == nil
}

// zmptCrossSeedDuplicate reads the same list that populates the empty 辅种 cell on page load.
func (s *Scanner) zmptCrossSeedDuplicate(ctx context.Context, source, target Profile, sourceCookie string, doc *html.Node, detail, infoHash string) (known, duplicate bool) {
	hashes := Select(doc, "#hashID")
	if len(Select(doc, "#resultContainer")) != 1 || len(hashes) != 1 || !strings.EqualFold(strings.TrimSpace(text(hashes[0])), infoHash) {
		return false, false
	}
	query := url.Values{"hash": {infoHash}}.Encode()
	lookup, err := sameOrigin(source.BaseURL, zmptCrossSeedPath+"?"+query)
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, err := s.getWithHeaders(ctx, lookup, sourceCookie, 256<<10, http.Header{
		"Accept":           {"application/json"},
		"Referer":          {detail},
		"X-Requested-With": {"XMLHttpRequest"},
	}, func(next *url.URL) bool {
		return next.EscapedPath() == zmptCrossSeedPath && next.RawQuery == query
	})
	if err != nil {
		return false, false
	}
	return zmptCrossSeedEvidence(body, target.BaseURL)
}

func zmptCrossSeedEvidence(body []byte, targetURL string) (known, duplicate bool) {
	targetHost := crossSeedSiteHost(targetURL)
	if targetHost == "" {
		return false, false
	}
	var response struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Code == nil {
		return false, false
	}
	// The API also wraps an explicit no-match response in code -1 and a null payload.
	if *response.Code == -1 && response.Message == zmptNoCrossSeedMatch && bytes.Equal(response.Data, []byte("null")) {
		return true, false
	}
	var sites []struct {
		URL string `json:"url"`
	}
	// A nil slice also covers a missing or null data field. Only a completed array is evidence.
	if *response.Code != 0 || json.Unmarshal(response.Data, &sites) != nil || sites == nil {
		return false, false
	}
	for _, site := range sites {
		host := crossSeedSiteHost(site.URL)
		if host == "" {
			return false, false
		}
		duplicate = duplicate || host == targetHost
	}
	return true, duplicate
}

func crossSeedSiteHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}
