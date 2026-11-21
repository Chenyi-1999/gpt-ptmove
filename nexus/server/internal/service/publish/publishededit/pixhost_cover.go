package publishededit

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/netproxy"
	"golang.org/x/net/html"
)

const maxPixhostShowPage = 512 << 10

var pixhostCoverGroup = regexp.MustCompile("^[0-9]{1,20}$")

var pixhostLocalNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

var pixhostCoverClient = &http.Client{
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		DialContext:           dialPixhostCover,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          12,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type pixhostCoverAddress struct {
	url   *url.URL
	kind  string
	group string
	name  string
}

// ResolvePixhostCover 找出 Pixhost 展示页或图链对应、可从 NovaHD 加载的原图地址。
func ResolvePixhostCover(ctx context.Context, raw string) (string, error) {
	return resolvePixhostCover(ctx, pixhostCoverClient, raw)
}

func resolvePixhostCover(ctx context.Context, client *http.Client, raw string) (string, error) {
	address, ok := parsePixhostCoverAddress(raw)
	if !ok {
		return "", fmt.Errorf("%w：请粘贴 Pixhost 的 HTTPS 展示页或图片链接", ErrInvalidChange)
	}
	if address.kind == "images" && pixhostOriginalAvailable(ctx, client, address) {
		return address.url.String(), nil
	}
	original, err := pixhostShowOriginal(ctx, client, address)
	if err != nil || !pixhostOriginalAvailable(ctx, client, original) {
		return "", ErrCoverUnavailable
	}
	return original.url.String(), nil
}

func parsePixhostCoverAddress(raw string) (pixhostCoverAddress, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "[]\\<>\"' \t\r\n\x00") {
		return pixhostCoverAddress{}, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil ||
		u.Host != u.Hostname() || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return pixhostCoverAddress{}, false
	}
	u.Host = strings.ToLower(u.Host)
	parts := strings.Split(u.Path, "/")
	if len(parts) != 4 || parts[0] != "" || !pixhostCoverGroup.MatchString(parts[2]) || parts[3] == "" ||
		parts[3] == "." || parts[3] == ".." || strings.ContainsAny(parts[3], "[]\\<>\"'#?\t\r\n\x00") {
		return pixhostCoverAddress{}, false
	}
	for _, char := range parts[3] {
		if char < 32 || char == 127 {
			return pixhostCoverAddress{}, false
		}
	}
	switch parts[1] {
	case "show":
		if u.Host != "pixhost.to" {
			return pixhostCoverAddress{}, false
		}
	case "images", "thumbs":
		if !isPixhostCoverHost(u.Host) {
			return pixhostCoverAddress{}, false
		}
	default:
		return pixhostCoverAddress{}, false
	}
	// 统一百分号编码，避免将经过编码的分隔符写入简介。
	u.RawPath = ""
	return pixhostCoverAddress{url: u, kind: parts[1], group: parts[2], name: parts[3]}, true
}

func isPixhostCoverHost(host string) bool {
	return host == "pixhost.to" || pixhostAssetHost.MatchString(host)
}

func (address pixhostCoverAddress) showURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "pixhost.to", Path: "/show/" + address.group + "/" + address.name}
}

func pixhostShowOriginal(ctx context.Context, client *http.Client, address pixhostCoverAddress) (pixhostCoverAddress, error) {
	pageURL := address.showURL()
	req, err := pixhostRequest(ctx, pageURL, "text/html", "")
	if err != nil {
		return pixhostCoverAddress{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return pixhostCoverAddress{}, ErrCoverUnavailable
	}
	defer response.Body.Close()
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || contentType != "text/html" {
		return pixhostCoverAddress{}, ErrCoverUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxPixhostShowPage+1))
	if err != nil || len(data) > maxPixhostShowPage {
		return pixhostCoverAddress{}, ErrCoverUnavailable
	}
	// 只提取 Pixhost 展示页原图区的 src，并对解析后的地址重新校验。
	tokenizer := html.NewTokenizer(strings.NewReader(string(data)))
	var original pixhostCoverAddress
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data != "img" {
			continue
		}
		var id, src string
		for _, attribute := range token.Attr {
			switch attribute.Key {
			case "id":
				id = attribute.Val
			case "src":
				src = attribute.Val
			}
		}
		if id != "image" {
			continue
		}
		link, err := pageURL.Parse(src)
		if err != nil || original.url != nil {
			return pixhostCoverAddress{}, ErrCoverUnavailable
		}
		candidate, ok := parsePixhostCoverAddress(link.String())
		if !ok || candidate.kind != "images" || candidate.group != address.group {
			return pixhostCoverAddress{}, ErrCoverUnavailable
		}
		original = candidate
	}
	if original.url == nil || tokenizer.Err() != io.EOF {
		return pixhostCoverAddress{}, ErrCoverUnavailable
	}
	return original, nil
}

func pixhostOriginalAvailable(ctx context.Context, client *http.Client, address pixhostCoverAddress) bool {
	if address.kind != "images" {
		return false
	}
	req, err := pixhostRequest(ctx, address.url, "image/webp,image/png,image/jpeg,image/gif", novaOrigin+"/")
	if err != nil {
		return false
	}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK {
		return false
	}
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return false
	}
	head, err := io.ReadAll(io.LimitReader(response.Body, 512))
	return err == nil && http.DetectContentType(head) == contentType
}

func pixhostRequest(ctx context.Context, endpoint *url.URL, accept, referer string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "Mozilla/5.0 AppleWebKit/537.36")
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	return request, nil
}

// dialPixhostCover 拒绝非公网目标，连接时使用已检查的具体地址，避免重新解析域名。
func dialPixhostCover(ctx context.Context, network, endpoint string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || port != "443" || !isPixhostCoverHost(strings.ToLower(host)) {
		return nil, ErrCoverUnavailable
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrCoverUnavailable
	}
	for _, addr := range addresses {
		if !isPublicPixhostIP(addr) {
			return nil, ErrCoverUnavailable
		}
	}
	for _, addr := range addresses {
		conn, err := netproxy.ProtectedDialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, ErrCoverUnavailable
}

func isPublicPixhostIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address == netip.AddrFrom4([4]byte{81, 70, 24, 5}) {
		return false
	}
	for _, prefix := range pixhostLocalNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
