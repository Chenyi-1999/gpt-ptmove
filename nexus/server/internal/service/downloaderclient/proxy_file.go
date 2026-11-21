package downloaderclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// CheckMediaPathByProxy 确认媒体容器可读取 qB 返回的单一路径，不修改文件或生成媒体资料。
func (d Downloader) CheckMediaPathByProxy(ctx context.Context, remotePath string) (bool, error) {
	host, err := parseHostURL(d.Host)
	if err != nil || host.Hostname() == "" {
		return false, errors.New("媒体代理地址无效")
	}
	port := d.ProxyPort
	if port <= 0 {
		port = 9090
	}
	endpoint := "http://" + net.JoinHostPort(host.Hostname(), strconv.Itoa(port)) + "/api/file/check"
	body, err := json.Marshal(map[string]string{"remote_path": remotePath})
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.proxyHTTPClient(20 * time.Second).Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	var result struct {
		Success bool  `json:"success"`
		Exists  *bool `json:"exists"`
	}
	if response.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result) != nil ||
		!result.Success || result.Exists == nil {
		return false, errors.New("媒体代理未返回有效的文件检查结果")
	}
	return *result.Exists, nil
}
