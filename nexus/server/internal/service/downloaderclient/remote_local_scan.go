package downloaderclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pt-nexus/server/internal/platform/netproxy"
)

const (
	remoteScanRequestLimit   = 64 << 10
	remoteScanReferenceLimit = 32 << 20
	remoteScanResultLimit    = 2 << 20
	remoteScanEntryLimit     = 20_000
	remoteScanHTTPTimeout    = 45 * time.Second
)

// RemoteDirectoryScanRequest 保存扫描目录及其直属文件名。Expected 用于查缺失，Referenced 用于保护所有被引用的文件。
type RemoteDirectoryScanRequest struct {
	RemotePath string   `json:"remote_path"`
	Expected   []string `json:"expected"`
	Referenced []string `json:"referenced"`
}

// RemoteDirectoryFile 是客户端返回的孤立文件元数据。
type RemoteDirectoryFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// RemoteDirectoryScanResult 表示已完整核验的目录结果。
type RemoteDirectoryScanResult struct {
	TotalItems int
	Missing    []string
	Orphaned   []RemoteDirectoryFile
}

// RemoteScanReference 是下载器清单中用于核对实际内容位置的字段。
type RemoteScanReference struct {
	Hash        string `json:"hash"`
	SavePath    string `json:"save_path"`
	ContentPath string `json:"content_path"`
}

// ScanMediaDirectoryByProxy 通过现有媒体领单链路扫描一个目录，只接受完整且与请求范围相符的结果。
// 请求携带目录与必要的文件名，客户端不返回文件内容。
func (d Downloader) ScanMediaDirectoryByProxy(ctx context.Context, scan RemoteDirectoryScanRequest) (RemoteDirectoryScanResult, error) {
	host, err := parseHostURL(d.Host)
	if err != nil || host.Hostname() == "" || host.User != nil || !d.UseProxy || strings.TrimSpace(d.ProxyToken) == "" {
		return RemoteDirectoryScanResult{}, errors.New("远程媒体客户端连接未配置完整")
	}
	directory, ok := remoteScanPath(scan.RemotePath)
	if !ok || directory == "/" {
		return RemoteDirectoryScanResult{}, errors.New("远程扫描目录无效")
	}
	scan.RemotePath = directory
	expected, err := remoteScanNameSet(scan.Expected)
	if err != nil {
		return RemoteDirectoryScanResult{}, err
	}
	references, err := remoteScanNameSet(scan.Referenced)
	if err != nil {
		return RemoteDirectoryScanResult{}, err
	}
	for name := range expected {
		if _, referenced := references[name]; !referenced {
			return RemoteDirectoryScanResult{}, errors.New("远程扫描请求缺少待核验文件的任务引用")
		}
	}
	if scan.Expected == nil {
		scan.Expected = []string{}
	}
	if scan.Referenced == nil {
		scan.Referenced = []string{}
	}
	body, err := json.Marshal(scan)
	if err != nil || len(body) > remoteScanRequestLimit {
		return RemoteDirectoryScanResult{}, errors.New("远程扫描请求超出大小限制")
	}

	port := d.ProxyPort
	if port <= 0 {
		port = 9090
	}
	endpoint := "http://" + net.JoinHostPort(host.Hostname(), strconv.Itoa(port)) + "/api/file/scan"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return RemoteDirectoryScanResult{}, errors.New("远程媒体客户端地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.proxyHTTPClient(remoteScanHTTPTimeout).Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return RemoteDirectoryScanResult{}, ctx.Err()
		}
		return RemoteDirectoryScanResult{}, errors.New("远程客户端未返回完整扫描结果")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RemoteDirectoryScanResult{}, remoteDirectoryStatusError(response.StatusCode)
	}
	data, err := readRemoteScanBody(response.Body, remoteScanResultLimit)
	if err != nil {
		return RemoteDirectoryScanResult{}, errors.New("远程扫描结果无法读取或超出大小限制")
	}
	return parseRemoteDirectoryResult(data, expected, references)
}

// ListRemoteScanReferences 通过领单客户端专用只读接口获取不受页面筛选影响的完整清单。
// 认证只使用已配置的令牌。客户端在本机裁剪 qB 响应，只回传判断文件引用所需的字段。
func (d Downloader) ListRemoteScanReferences(ctx context.Context) ([]RemoteScanReference, error) {
	if !d.UseProxy || d.Type != "qbittorrent" || strings.TrimSpace(d.APIKey) == "" {
		return nil, errors.New("未配置远程 qB 只读令牌，无法核对全部下载任务")
	}
	host, err := parseHostURL(d.Host)
	if err != nil || host.Hostname() == "" || host.User != nil || host.Opaque != "" ||
		host.RawQuery != "" || host.ForceQuery || host.Fragment != "" ||
		(host.Scheme != "http" && host.Scheme != "https") {
		return nil, errors.New("远程 qB 地址无效")
	}
	endpoint := host.JoinPath("api/local-scan/references")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("远程 qB 地址无效")
	}
	request.Header.Set("Referer", host.String())
	transport := netproxy.ConfigureShortLivedTransport(&http.Transport{ForceAttemptHTTP2: true})
	client := &http.Client{
		Transport:     NewBearerTransport(strings.TrimSpace(d.APIKey), transport),
		Timeout:       remoteScanHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("远程 qB 清单不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, errors.New("远程 qB 清单未获授权或领单组件需更新")
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return nil, errors.New("远程 qB 通道不支持只读清单，请更新领单组件")
		case http.StatusServiceUnavailable, http.StatusTooManyRequests:
			return nil, errors.New("远程客户端离线或繁忙，无法核对全部下载任务")
		}
		return nil, fmt.Errorf("远程 qB 清单不可用（HTTP %d）", response.StatusCode)
	}
	data, err := readRemoteScanBody(response.Body, remoteScanReferenceLimit)
	if err != nil {
		return nil, errors.New("远程 qB 清单无法读取或超出大小限制")
	}
	return parseRemoteScanReferences(data)
}

func parseRemoteScanReferences(data []byte) ([]RemoteScanReference, error) {
	invalid := errors.New("远程 qB 清单不完整或格式无效")
	var rows []RemoteScanReference
	if !decodeRemoteScanJSON(data, &rows) || rows == nil {
		return nil, invalid
	}
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		row := &rows[i]
		row.Hash = strings.ToLower(row.Hash)
		if len(row.Hash) != 40 && len(row.Hash) != 64 {
			return nil, invalid
		}
		if _, err := hex.DecodeString(row.Hash); err != nil {
			return nil, invalid
		}
		if _, exists := seen[row.Hash]; exists {
			return nil, invalid
		}
		seen[row.Hash] = struct{}{}
		savePath, saveValid := remoteScanPath(row.SavePath)
		contentPath, contentValid := remoteScanPath(row.ContentPath)
		if !saveValid || !contentValid {
			return nil, invalid
		}
		row.SavePath, row.ContentPath = savePath, contentPath
	}
	return rows, nil
}

func parseRemoteDirectoryResult(data []byte, expected, references map[string]struct{}) (RemoteDirectoryScanResult, error) {
	invalid := errors.New("远程客户端未返回有效的完整扫描结果")
	var wire struct {
		Success    bool     `json:"success"`
		Complete   bool     `json:"complete"`
		TotalItems *int     `json:"total_items"`
		Missing    []string `json:"missing"`
		Orphaned   []struct {
			Name string `json:"name"`
			Size *int64 `json:"size"`
		} `json:"orphaned"`
	}
	if !decodeRemoteScanJSON(data, &wire) || !wire.Success || !wire.Complete ||
		wire.TotalItems == nil || *wire.TotalItems < 0 || *wire.TotalItems > remoteScanEntryLimit ||
		wire.Missing == nil || wire.Orphaned == nil ||
		len(expected)-len(wire.Missing)+len(wire.Orphaned) > *wire.TotalItems {
		return RemoteDirectoryScanResult{}, invalid
	}
	seen := make(map[string]struct{}, len(wire.Missing)+len(wire.Orphaned))
	for _, name := range wire.Missing {
		if _, ok := expected[name]; !ok {
			return RemoteDirectoryScanResult{}, invalid
		}
		if _, exists := seen[name]; exists {
			return RemoteDirectoryScanResult{}, invalid
		}
		seen[name] = struct{}{}
	}
	orphaned := make([]RemoteDirectoryFile, 0, len(wire.Orphaned))
	for _, file := range wire.Orphaned {
		if !validRemoteScanName(file.Name) || file.Size == nil || *file.Size < 0 {
			return RemoteDirectoryScanResult{}, invalid
		}
		if _, referenced := references[file.Name]; referenced {
			return RemoteDirectoryScanResult{}, invalid
		}
		if _, exists := seen[file.Name]; exists {
			return RemoteDirectoryScanResult{}, invalid
		}
		seen[file.Name] = struct{}{}
		orphaned = append(orphaned, RemoteDirectoryFile{Name: file.Name, Size: *file.Size})
	}
	return RemoteDirectoryScanResult{TotalItems: *wire.TotalItems, Missing: wire.Missing, Orphaned: orphaned}, nil
}

func decodeRemoteScanJSON(data []byte, result any) bool {
	if !utf8.Valid(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(result) == nil && decoder.Decode(new(json.RawMessage)) == io.EOF
}

func readRemoteScanBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("响应读取失败或超出大小限制")
	}
	return data, nil
}

func remoteDirectoryStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("扫描目录未获授权或远程组件需更新")
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return errors.New("远程组件不支持目录扫描，请更新领单与媒体组件")
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return errors.New("远程客户端离线或繁忙")
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return errors.New("远程客户端未在时限内返回完整扫描结果")
	case http.StatusRequestEntityTooLarge:
		return errors.New("远程扫描请求超出大小限制")
	case http.StatusUnprocessableEntity:
		return errors.New("远程目录不可访问或扫描未完成")
	default:
		return fmt.Errorf("远程目录未核验（HTTP %d）", status)
	}
}

// remoteScanPath 按 POSIX 规则校验远程下载路径，不受服务端系统影响。
func remoteScanPath(value string) (string, bool) {
	if !utf8.ValidString(value) || !path.IsAbs(value) || strings.HasPrefix(value, "//") ||
		strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n") || strings.Contains(value, "://") {
		return "", false
	}
	if value != "/" {
		value = strings.TrimRight(value, "/")
	}
	return value, path.Clean(value) == value
}

func validRemoteScanName(name string) bool {
	return utf8.ValidString(name) && name != "" && len(name) <= 255 && name != "." && name != ".." &&
		!strings.ContainsAny(name, "/\\\x00\r\n")
}

func remoteScanNameSet(names []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validRemoteScanName(name) {
			return nil, errors.New("远程扫描请求包含无效文件名")
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("远程扫描请求包含重复文件名")
		}
		result[name] = struct{}{}
	}
	return result, nil
}
