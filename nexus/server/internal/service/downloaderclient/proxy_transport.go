package downloaderclient

import (
	"fmt"
	"github.com/pt-nexus/server/internal/platform/netproxy"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var proxyTransportOnce sync.Once
var proxyBaseTransport http.RoundTripper

type proxyAuthTransport struct {
	token string
	base  http.RoundTripper
}

func (transport proxyAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if transport.token != "" {
		clone.Header.Set("Authorization", "Bearer "+transport.token)
	}
	return transport.base.RoundTrip(clone)
}

// proxyHTTPClient 为盒子请求附加独立凭据并禁止重定向；底层保留全局受保护地址校验。
// 参数/返回：超时和当前下载器配置生成客户端；不会复用下载器 API 密钥。
// 失败场景：网络、受保护地址或认证错误由调用方处理；副作用：仅在 Do 时发请求。
func (d Downloader) proxyHTTPClient(timeout time.Duration) *http.Client {
	proxyTransportOnce.Do(func() { proxyBaseTransport = netproxy.ConfigureTransport(&http.Transport{ForceAttemptHTTP2: true}) })
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: proxyAuthTransport{token: strings.TrimSpace(d.ProxyToken), base: proxyBaseTransport}}
}

// ProbeMediaProxy 检查媒体代理认证与路由，不读取媒体或生成截图。
// 参数/返回：使用当前下载器主机和代理端口；返回认证/网络错误。
// 失败场景：认证失败、重定向或协议不兼容；副作用：仅发送无请求体 GET，预期受保护接口返回 405。
func (d Downloader) ProbeMediaProxy() error {
	parsed, err := parseHostURL(d.Host)
	if err != nil {
		return err
	}
	port := d.ProxyPort
	if port <= 0 {
		port = 9090
	}
	endpoint := "http://" + net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port)) + "/api/file/check"
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := d.proxyHTTPClient(10 * time.Second).Do(request)
	if err != nil {
		return fmt.Errorf("媒体代理连接失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("媒体代理认证或路由失败: HTTP %d", response.StatusCode)
	}
	return nil
}
