package downloaderclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pt-nexus/server/internal/platform/netproxy"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Downloader struct {
	effectGuard    func() error
	ID             string
	Name           string
	Type           string
	Host           string
	Username       string
	Password       string
	APIKey         string
	Enabled        bool
	UseProxy       bool
	ProxyPort      int
	ProxyToken     string
	ProxyMediaOnly bool
}

// TrafficStats 统一描述下载器实时速度与累计流量统计。
type TrafficStats struct {
	DownloadSpeed int64
	UploadSpeed   int64
	TotalDownload int64
	TotalUpload   int64
	Version       string
}

// AddTorrentOptions 定义添加种子时的附加选项。
type AddTorrentOptions struct {
	Paused          bool
	Tags            []string
	Category        string
	UploadLimitMBps int
}

func FromConfig(root map[string]any, downloaderID string) (Downloader, error) {
	downloadersRaw, ok := root["downloaders"]
	if !ok {
		return Downloader{}, fmt.Errorf("配置中不存在 downloaders")
	}

	list := toSlice(downloadersRaw)
	for _, item := range list {
		downloader := toMap(item)
		if strings.TrimSpace(toString(downloader["id"], "")) != downloaderID {
			continue
		}
		result := Downloader{
			ID:             toString(downloader["id"], ""),
			Name:           toString(downloader["name"], ""),
			Type:           strings.ToLower(strings.TrimSpace(toString(downloader["type"], ""))),
			Host:           normalizeHost(toString(downloader["host"], "")),
			Username:       toString(downloader["username"], ""),
			Password:       toString(downloader["password"], ""),
			APIKey:         strings.TrimSpace(toString(downloader["api_key"], "")),
			Enabled:        toBool(downloader["enabled"], true),
			UseProxy:       toBool(downloader["use_proxy"], false),
			ProxyPort:      toInt(downloader["proxy_port"], 9090),
			ProxyToken:     strings.TrimSpace(toString(downloader["proxy_token"], "")),
			ProxyMediaOnly: toBool(downloader["proxy_media_only"], false),
		}
		if result.ID == "" || result.Type == "" || result.Host == "" {
			return Downloader{}, fmt.Errorf("下载器配置不完整: id=%s", downloaderID)
		}
		if !result.Enabled {
			return Downloader{}, fmt.Errorf("下载器未启用: %s", result.Name)
		}
		if result.Type != "qbittorrent" && result.Type != "transmission" {
			return Downloader{}, fmt.Errorf("不支持的下载器类型: %s", result.Type)
		}
		return result, nil
	}
	return Downloader{}, fmt.Errorf("未找到下载器配置: %s", downloaderID)
}

// FetchTrafficStats 连接下载器并返回当前速度与累计流量。
// 参数/返回：根据 Downloader.Type 自动选择 qBittorrent 或 Transmission 协议，返回统一结构体。
// 失败场景：连接失败、认证失败、响应格式异常时返回错误。
// 副作用：会向下载器发起网络请求获取实时统计。
func (d Downloader) FetchTrafficStats() (TrafficStats, error) {
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return TrafficStats{}, err
		}
		if err := client.Login(); err != nil {
			return TrafficStats{}, err
		}

		mainDataBody, err := client.Get("sync/maindata", nil)
		if err != nil {
			return TrafficStats{}, err
		}
		mainData := map[string]any{}
		if err := json.Unmarshal(mainDataBody, &mainData); err != nil {
			return TrafficStats{}, fmt.Errorf("qB 主数据解析失败: %w", err)
		}
		serverState := toMap(mainData["server_state"])

		version := ""
		versionBody, err := client.Get("app/version", nil)
		if err == nil {
			version = strings.TrimSpace(string(versionBody))
		}

		return TrafficStats{
			DownloadSpeed: toInt64Any(serverState["dl_info_speed"]),
			UploadSpeed:   toInt64Any(serverState["up_info_speed"]),
			TotalDownload: toInt64Any(serverState["alltime_dl"]),
			TotalUpload:   toInt64Any(serverState["alltime_ul"]),
			Version:       version,
		}, nil
	case "transmission":
		client := newTransmissionClient(d)
		response, err := client.Call("session-stats", map[string]any{})
		if err != nil {
			return TrafficStats{}, err
		}
		arguments := toMap(response["arguments"])
		cumulativeStats := toMap(arguments["cumulative-stats"])
		if len(cumulativeStats) == 0 {
			cumulativeStats = toMap(arguments["cumulative_stats"])
		}

		version := ""
		sessionResponse, sessionErr := client.Call("session-get", map[string]any{})
		if sessionErr == nil {
			sessionArguments := toMap(sessionResponse["arguments"])
			version = strings.TrimSpace(toString(sessionArguments["version"], ""))
		}

		return TrafficStats{
			DownloadSpeed: toInt64Any(arguments["downloadSpeed"]),
			UploadSpeed:   toInt64Any(arguments["uploadSpeed"]),
			TotalDownload: toInt64Any(cumulativeStats["downloadedBytes"]),
			TotalUpload:   toInt64Any(cumulativeStats["uploadedBytes"]),
			Version:       version,
		}, nil
	default:
		return TrafficStats{}, fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

// FetchProxyTrafficStats 通过盒子代理获取 qBittorrent 的速度与累计流量。
// 参数/返回：proxyPort 为空时使用 9090，成功返回代理汇总结构。
// 失败场景：代理地址解析失败、代理请求失败、返回为空或格式非法时返回错误。
// 副作用：会向 proxy 服务发起 HTTP 请求。
func (d Downloader) FetchProxyTrafficStats(proxyPort int) (TrafficStats, error) {
	if d.ProxyMediaOnly {
		return d.FetchTrafficStats()
	}
	if d.Type != "qbittorrent" {
		return TrafficStats{}, fmt.Errorf("代理统计仅支持 qBittorrent: %s", d.Type)
	}
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return TrafficStats{}, fmt.Errorf("解析 host 失败: %w", err)
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return TrafficStats{}, errors.New("无法解析代理地址")
	}

	originPort := parsedURL.Port()
	if strings.TrimSpace(originPort) == "" {
		originPort = "8080"
	}
	proxyDownloaderConfig := map[string]any{
		"id":       d.ID,
		"type":     d.Type,
		"host":     "http://127.0.0.1:" + originPort,
		"username": d.Username,
		"password": d.Password,
	}
	payload, err := json.Marshal([]map[string]any{proxyDownloaderConfig})
	if err != nil {
		return TrafficStats{}, fmt.Errorf("构造代理请求失败: %w", err)
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/stats/server", proxyIP, proxyPort)
	req, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payload))
	if err != nil {
		return TrafficStats{}, fmt.Errorf("创建代理请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := d.proxyHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return TrafficStats{}, fmt.Errorf("代理请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TrafficStats{}, fmt.Errorf("代理返回异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	statsData := make([]map[string]any, 0)
	if err := json.Unmarshal(body, &statsData); err != nil {
		return TrafficStats{}, fmt.Errorf("解析代理响应失败: %w", err)
	}
	if len(statsData) == 0 {
		return TrafficStats{}, errors.New("代理返回空统计信息")
	}

	row := statsData[0]
	return TrafficStats{
		DownloadSpeed: toInt64Any(row["download_speed"]),
		UploadSpeed:   toInt64Any(row["upload_speed"]),
		TotalDownload: toInt64Any(row["total_download"]),
		TotalUpload:   toInt64Any(row["total_upload"]),
		Version:       strings.TrimSpace(toString(row["version"], "")),
	}, nil
}

// FetchVersion 连接下载器并返回版本号字符串。
// 参数/返回：根据 Downloader.Type 自动走 qBittorrent 或 Transmission 协议，成功返回版本号。
// 失败场景：连接失败、认证失败、RPC 返回异常或类型不支持时返回错误。
// 副作用：会向下载器发起网络请求用于登录与查询版本信息。
func (d Downloader) FetchVersion() (string, error) {
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return "", err
		}
		if err := client.Login(); err != nil {
			return "", err
		}
		body, err := client.Get("app/version", nil)
		if err != nil {
			return "", err
		}
		version := strings.TrimSpace(string(body))
		if version == "" {
			version = "未知"
		}
		return version, nil
	case "transmission":
		client := newTransmissionClient(d)
		response, err := client.Call("session-get", map[string]any{})
		if err != nil {
			return "", err
		}
		arguments := toMap(response["arguments"])
		version := strings.TrimSpace(toString(arguments["version"], ""))
		if version == "" {
			version = "未知"
		}
		return version, nil
	default:
		return "", fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

func (d Downloader) AddTorrentURL(torrentURL, savePath string, paused bool, tags []string) error {
	return d.AddTorrentURLWithOptions(torrentURL, savePath, AddTorrentOptions{
		Paused: paused,
		Tags:   tags,
	})
}

// AddTorrentURLWithOptions 按 URL 添加种子，并支持附加参数（标签/暂停/上传限速）。
func (d Downloader) AddTorrentURLWithOptions(torrentURL, savePath string, options AddTorrentOptions) error {
	torrentURL = strings.TrimSpace(torrentURL)
	if torrentURL == "" {
		return errors.New("torrent URL 不能为空")
	}
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return err
		}
		if err := client.Login(); err != nil {
			return err
		}
		values := url.Values{}
		values.Set("urls", torrentURL)
		if strings.TrimSpace(savePath) != "" {
			values.Set("savepath", savePath)
		}
		values.Set("paused", strconv.FormatBool(options.Paused))
		values.Set("skip_checking", "true")
		if len(options.Tags) > 0 {
			values.Set("tags", strings.Join(compactStrings(options.Tags), ","))
		}
		if category := strings.TrimSpace(options.Category); category != "" {
			values.Set("category", category)
		}
		if limitBytes := normalizeUploadLimitBytes(options.UploadLimitMBps); limitBytes > 0 {
			values.Set("upLimit", strconv.Itoa(limitBytes))
		}
		_, err = client.PostForm("torrents/add", values)
		return err
	case "transmission":
		client := newTransmissionClient(d)
		args := map[string]any{"filename": torrentURL}
		if strings.TrimSpace(savePath) != "" {
			args["download-dir"] = savePath
		}
		args["paused"] = options.Paused
		if len(options.Tags) > 0 {
			args["labels"] = compactStrings(options.Tags)
		}
		response, err := client.Call("torrent-add", args)
		if err != nil {
			return err
		}
		if options.UploadLimitMBps > 0 {
			applyTransmissionUploadLimit(client, response, options.UploadLimitMBps)
		}
		return nil
	default:
		return fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

func (d Downloader) AddTorrentData(content []byte, fileName, savePath string, paused bool, tags []string) error {
	return d.AddTorrentDataWithOptions(content, fileName, savePath, AddTorrentOptions{
		Paused: paused,
		Tags:   tags,
	})
}

// AddTorrentDataWithOptions 按种子字节添加任务，并支持附加参数（标签/暂停/上传限速）。
func (d Downloader) AddTorrentDataWithOptions(content []byte, fileName, savePath string, options AddTorrentOptions) error {
	if len(content) == 0 {
		return errors.New("torrent 内容为空")
	}
	if fileName == "" {
		fileName = fmt.Sprintf("pt-nexus-%d.torrent", time.Now().UnixNano())
	}
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return err
		}
		if err := client.Login(); err != nil {
			return err
		}
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("torrents", filepath.Base(fileName))
		if err != nil {
			return err
		}
		if _, err := part.Write(content); err != nil {
			return err
		}
		if strings.TrimSpace(savePath) != "" {
			_ = writer.WriteField("savepath", savePath)
		}
		_ = writer.WriteField("paused", strconv.FormatBool(options.Paused))
		_ = writer.WriteField("skip_checking", "true")
		if len(options.Tags) > 0 {
			_ = writer.WriteField("tags", strings.Join(compactStrings(options.Tags), ","))
		}
		if category := strings.TrimSpace(options.Category); category != "" {
			_ = writer.WriteField("category", category)
		}
		if limitBytes := normalizeUploadLimitBytes(options.UploadLimitMBps); limitBytes > 0 {
			_ = writer.WriteField("upLimit", strconv.Itoa(limitBytes))
		}
		if err := writer.Close(); err != nil {
			return err
		}
		_, err = client.PostMultipart("torrents/add", body, writer.FormDataContentType())
		return err
	case "transmission":
		client := newTransmissionClient(d)
		args := map[string]any{
			"metainfo": base64.StdEncoding.EncodeToString(content),
			"paused":   options.Paused,
		}
		if strings.TrimSpace(savePath) != "" {
			args["download-dir"] = savePath
		}
		if len(options.Tags) > 0 {
			args["labels"] = compactStrings(options.Tags)
		}
		response, err := client.Call("torrent-add", args)
		if err != nil {
			return err
		}
		if options.UploadLimitMBps > 0 {
			applyTransmissionUploadLimit(client, response, options.UploadLimitMBps)
		}
		return nil
	default:
		return fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

func (d Downloader) AddTorrentFile(filePath, savePath string, paused bool, tags []string) error {
	return d.AddTorrentFileWithOptions(filePath, savePath, AddTorrentOptions{
		Paused: paused,
		Tags:   tags,
	})
}

// AddTorrentFileWithOptions 按本地种子文件添加任务，并支持附加参数（标签/暂停/上传限速）。
func (d Downloader) AddTorrentFileWithOptions(filePath, savePath string, options AddTorrentOptions) error {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return errors.New("torrent 文件路径为空")
	}
	content, err := os.ReadFile(trimmed)
	if err != nil {
		return err
	}
	return d.AddTorrentDataWithOptions(content, filepath.Base(trimmed), savePath, options)
}

func normalizeUploadLimitBytes(uploadLimitMBps int) int {
	if uploadLimitMBps <= 0 {
		return 0
	}
	return uploadLimitMBps * 1024 * 1024
}

func normalizeUploadLimitKBps(uploadLimitMBps int) int {
	if uploadLimitMBps <= 0 {
		return 0
	}
	return uploadLimitMBps * 1024
}

func applyTransmissionUploadLimit(client *transmissionClient, addResponse map[string]any, uploadLimitMBps int) {
	if client == nil || uploadLimitMBps <= 0 {
		return
	}
	ids := extractTransmissionAddedIDs(addResponse)
	if len(ids) == 0 {
		return
	}
	limitKBps := normalizeUploadLimitKBps(uploadLimitMBps)
	if limitKBps <= 0 {
		return
	}
	_, _ = client.Call("torrent-set", map[string]any{
		"ids":           ids,
		"uploadLimited": true,
		"uploadLimit":   limitKBps,
	})
}

func extractTransmissionAddedIDs(addResponse map[string]any) []int {
	if len(addResponse) == 0 {
		return nil
	}
	arguments := toMap(addResponse["arguments"])
	if len(arguments) == 0 {
		return nil
	}
	ids := make([]int, 0, 2)
	for _, key := range []string{"torrent-added", "torrent-duplicate"} {
		record := toMap(arguments[key])
		if len(record) == 0 {
			continue
		}
		id := toInt(record["id"], -1)
		if id >= 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func (d Downloader) Pause(hashes []string) error {
	hashes = compactStrings(hashes)
	if len(hashes) == 0 {
		return errors.New("hash 列表为空")
	}
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return err
		}
		if err := client.Login(); err != nil {
			return err
		}
		values := url.Values{}
		values.Set("hashes", strings.Join(hashes, "|"))
		_, err = client.PostForm("torrents/pause", values)
		return err
	case "transmission":
		client := newTransmissionClient(d)
		_, err := client.Call("torrent-stop", map[string]any{"ids": hashes})
		return err
	default:
		return fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

func (d Downloader) ExportTorrent(hash string) ([]byte, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, errors.New("hash 不能为空")
	}
	switch d.Type {
	case "qbittorrent":
		client, err := newQBClient(d)
		if err != nil {
			return nil, err
		}
		if err := client.Login(); err != nil {
			return nil, err
		}
		params := url.Values{}
		params.Set("hash", hash)
		return client.Get("torrents/export", params)
	case "transmission":
		client := newTransmissionClient(d)
		response, err := client.Call("torrent-get", map[string]any{
			"ids":    []string{hash},
			"fields": []string{"hashString", "name", "torrentFile"},
		})
		if err != nil {
			return nil, err
		}
		args := toMap(response["arguments"])
		torrents := toSlice(args["torrents"])
		if len(torrents) == 0 {
			return nil, fmt.Errorf("Transmission 未找到 hash=%s", hash)
		}
		row := toMap(torrents[0])
		torrentFile := strings.TrimSpace(toString(row["torrentFile"], ""))
		if torrentFile == "" {
			return nil, fmt.Errorf("Transmission 未返回 torrentFile 路径")
		}
		content, readErr := os.ReadFile(torrentFile)
		if readErr != nil {
			return nil, fmt.Errorf("读取 Transmission torrentFile 失败: %w", readErr)
		}
		if len(content) == 0 {
			return nil, fmt.Errorf("Transmission torrentFile 内容为空")
		}
		return content, nil
	default:
		return nil, fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

type qbClient struct {
	effectGuard func() error
	host        string
	username    string
	password    string
	client      *http.Client
	isLoggedIn  bool
	apiKey      string
}

func newQBClient(d Downloader) (*qbClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &qbClient{
		effectGuard: d.effectGuard,
		host:        strings.TrimSuffix(normalizeHost(d.Host), "/"),
		username:    d.Username,
		password:    d.Password,
		apiKey:      d.APIKey,
		client: &http.Client{
			Jar:           jar,
			Timeout:       90 * time.Second,
			Transport:     NewBearerTransport(d.APIKey, netproxy.ConfigureTransport(&http.Transport{ForceAttemptHTTP2: true})),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *qbClient) Login() error {
	if c.isLoggedIn {
		return nil
	}
	if c.apiKey != "" {
		if _, err := c.Get("app/version", nil); err != nil {
			return errors.New("qB API Token认证失败，未回退密码")
		}
		c.isLoggedIn = true
		return nil
	}
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)
	req, err := http.NewRequest(http.MethodPost, c.host+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.host)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("qB 登录失败: HTTP %d", resp.StatusCode)
	}
	// Python 版本使用 qbittorrentapi，部分 Windows/代理环境下 auth/login 会返回 204。
	// 这里兼容 200/204 + SID cookie 判定，避免把可用连接误判为失败。
	loginResp := strings.TrimSpace(string(body))
	hasSIDCookie := qbHasSIDCookie(resp, c.host, c.client.Jar)
	if loginResp == "Ok." || loginResp == "Ok" || hasSIDCookie || (resp.StatusCode == http.StatusNoContent && loginResp == "") {
		c.isLoggedIn = true
		return nil
	}
	// 兜底：部分网关会返回非标准登录响应，但会话实际上已建立。
	if _, err := c.Get("app/version", nil); err == nil {
		c.isLoggedIn = true
		return nil
	}
	if loginResp == "" {
		return fmt.Errorf("qB 登录失败: 响应内容为空")
	}
	if strings.EqualFold(loginResp, "fails.") {
		return fmt.Errorf("qB 登录失败: 认证失败")
	}
	if !hasSIDCookie {
		return errors.New("qB 登录失败，请检查凭据")
	}
	c.isLoggedIn = true
	return nil
}

func qbHasSIDCookie(resp *http.Response, host string, jar http.CookieJar) bool {
	if resp != nil {
		for _, cookie := range resp.Cookies() {
			if qbIsSessionCookie(cookie) {
				return true
			}
		}
	}
	if jar == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(host))
	if err != nil || parsed == nil {
		return false
	}
	for _, cookie := range jar.Cookies(parsed) {
		if qbIsSessionCookie(cookie) {
			return true
		}
	}
	return false
}

func qbIsSessionCookie(cookie *http.Cookie) bool {
	if cookie == nil {
		return false
	}
	if strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	name := strings.ToUpper(strings.TrimSpace(cookie.Name))
	return name == "SID" || strings.HasPrefix(name, "QBT_SID_")
}

func (c *qbClient) PostForm(endpoint string, values url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.host+"/api/v2/"+endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.host)
	if c.effectGuard != nil {
		if err := c.effectGuard(); err != nil {
			return nil, err
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qB 请求失败: HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func (c *qbClient) PostMultipart(endpoint string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.host+"/api/v2/"+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Referer", c.host)
	if c.effectGuard != nil {
		if err := c.effectGuard(); err != nil {
			return nil, err
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qB 请求失败: HTTP %d", resp.StatusCode)
	}
	return respBody, nil
}

func (c *qbClient) Get(endpoint string, params url.Values) ([]byte, error) {
	fullURL := c.host + "/api/v2/" + endpoint
	if params != nil {
		fullURL += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", c.host)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qB 请求失败: HTTP %d", resp.StatusCode)
	}
	return body, nil
}

type transmissionClient struct {
	host      string
	username  string
	password  string
	sessionID string
	client    *http.Client
}

func newTransmissionClient(d Downloader) *transmissionClient {
	normalized := normalizeHost(d.Host)
	parsed, err := url.Parse(normalized)
	host := normalized
	if err == nil && parsed.Host != "" {
		host = parsed.Scheme + "://" + parsed.Host
	}
	return &transmissionClient{
		host:     strings.TrimSuffix(host, "/"),
		username: d.Username,
		password: d.Password,
		client: &http.Client{
			Timeout:       90 * time.Second,
			Transport:     netproxy.ConfigureTransport(&http.Transport{ForceAttemptHTTP2: true}),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *transmissionClient) rpcURL() string {
	return c.host + "/transmission/rpc"
}

func (c *transmissionClient) Call(method string, args map[string]any) (map[string]any, error) {
	payload := map[string]any{"method": method, "arguments": args}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, c.rpcURL(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", c.sessionID)
		}
		if c.username != "" {
			req.SetBasicAuth(c.username, c.password)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusConflict {
			c.sessionID = strings.TrimSpace(resp.Header.Get("X-Transmission-Session-Id"))
			if c.sessionID == "" {
				return nil, errors.New("Transmission 返回 409 但未提供 Session ID")
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Transmission RPC HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}

		parsed := map[string]any{}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, fmt.Errorf("Transmission 响应解析失败: %w", err)
		}
		if result := toString(parsed["result"], ""); result != "success" {
			return nil, fmt.Errorf("Transmission RPC 失败: %s", result)
		}
		return parsed, nil
	}

	return nil, errors.New("Transmission 会话协商失败")
}

func normalizeHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "http://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func compactStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func toMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	if typed, ok := value.(map[string]interface{}); ok {
		result := map[string]any{}
		for key, item := range typed {
			result[key] = item
		}
		return result
	}
	return map[string]any{}
}

func toSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return []any{}
	}
}

func toString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return fallback
		}
		return text
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" || text == "<nil>" {
			return fallback
		}
		return text
	}
}

func toBool(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		s := strings.ToLower(strings.TrimSpace(typed))
		if s == "" {
			return fallback
		}
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return fallback
	}
}

func toInt64Any(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return int64(^uint64(0) >> 1)
		}
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err == nil {
			return parsed
		}
		floatParsed, floatErr := strconv.ParseFloat(trimmed, 64)
		if floatErr == nil {
			return int64(floatParsed)
		}
		return 0
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
		floatParsed, floatErr := typed.Float64()
		if floatErr == nil {
			return int64(floatParsed)
		}
		return 0
	default:
		return 0
	}
}

func toInt(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		if typed > uint64(^uint(0)) {
			return fallback
		}
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return fallback
		}
		return int(parsed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func parseHostURL(hostValue string) (*url.URL, error) {
	normalized := strings.TrimSpace(hostValue)
	if normalized == "" {
		return nil, errors.New("host 为空")
	}
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "http://" + normalized
	}
	return url.Parse(normalized)
}

func extractHostFallback(hostValue string) string {
	value := strings.TrimSpace(hostValue)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, "://"); index >= 0 {
		value = value[index+3:]
	}
	if index := strings.Index(value, "/"); index >= 0 {
		value = value[:index]
	}
	if index := strings.Index(value, ":"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

// AutomationTorrentStatus 是自动化完整下载检查所需的下载器快照，不包含凭据。
type AutomationTorrentStatus struct {
	Found                       bool
	Hash, State, SavePath, Name string
	ContentPath                 string
	Progress                    float64
	AmountLeft, Downloaded      int64
}

// AutomationTracker 表示qB返回的单个Tracker状态。
type AutomationTracker struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
}

// WithEffectGuard 返回附带即时授权检查的副本；每次qB写请求发送前重新核验。
// guard拒绝时不发请求，不更改原配置，其他普通下载器操作不受影响。
func (d Downloader) WithEffectGuard(guard func() error) Downloader { d.effectGuard = guard; return d }

type bearerTransport struct {
	key  string
	base http.RoundTripper
}

// NewBearerTransport 为qB请求附加独立API Token，错误不返回认证头或响应原文。
// key为空时保持Cookie会话；仅构造包装器，不发请求。
func NewBearerTransport(key string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &bearerTransport{key: key, base: base}
}
func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header = req.Header.Clone()
	if t.key != "" {
		copy.Header.Set("Authorization", "Bearer "+t.key)
	}
	return t.base.RoundTrip(copy)
}
func (d Downloader) automationQB() (*qbClient, error) {
	if d.Type != "qbittorrent" {
		return nil, errors.New("自动下载需要直连qBittorrent")
	}
	// UseProxy 是原生媒体盒子代理开关；自动化qB请求始终按Host直连，不能因此禁用NAS媒体能力。
	c, err := newQBClient(d)
	if err != nil {
		return nil, err
	}
	if err = c.Login(); err != nil {
		return nil, errors.New("qB认证或连接失败")
	}
	return c, nil
}

// TorrentStatus 按预先计算的hash查询；未找到返回Found=false，查询失败返回错误。
// 副作用仅为qB版本认证和读取种子状态，不添加或修改种子。
func (d Downloader) TorrentStatus(hash string) (AutomationTorrentStatus, error) {
	c, err := d.automationQB()
	if err != nil {
		return AutomationTorrentStatus{}, err
	}
	return automationStatus(c, hash)
}
func automationStatus(c *qbClient, hash string) (AutomationTorrentStatus, error) {
	body, err := c.Get("torrents/info", url.Values{"hashes": {hash}})
	if err != nil {
		return AutomationTorrentStatus{}, errors.New("读取下载状态失败")
	}
	var rows []map[string]any
	if json.Unmarshal(body, &rows) != nil || rows == nil || len(rows) > 1 {
		return AutomationTorrentStatus{}, errors.New("下载状态响应无效")
	}
	if len(rows) == 0 {
		return AutomationTorrentStatus{}, nil
	}
	row := rows[0]
	if !strings.EqualFold(toString(row["hash"], ""), hash) {
		return AutomationTorrentStatus{}, errors.New("下载状态响应的哈希与查询不一致")
	}
	progress, _ := row["progress"].(float64)
	return AutomationTorrentStatus{
		Found: true, Hash: hash, State: toString(row["state"], ""),
		SavePath: toString(row["save_path"], ""), Name: toString(row["name"], ""),
		ContentPath: toString(row["content_path"], ""), Progress: progress,
		AmountLeft: automationAmountLeft(row), Downloaded: toInt64Any(row["downloaded"]),
	}, nil
}

// TorrentFilesComplete 确认每个文件均选择且完整；空列表或未知字段返回false。
// 参数为hash；副作用仅为qB文件列表查询。
func (d Downloader) TorrentFilesComplete(hash string) (bool, error) {
	c, err := d.automationQB()
	if err != nil {
		return false, err
	}
	body, err := c.Get("torrents/files", url.Values{"hash": {hash}})
	if err != nil {
		return false, errors.New("读取下载文件列表失败")
	}
	var files []map[string]any
	if json.Unmarshal(body, &files) != nil || len(files) == 0 {
		return false, errors.New("下载文件列表未知")
	}
	for _, file := range files {
		p, ok := file["progress"].(float64)
		priority, known := file["priority"]
		if !ok || p < 1 || !known || toInt64Any(priority) == 0 {
			return false, nil
		}
	}
	return true, nil
}
func automationValidate(data []byte, expected string) error {
	if err := acquirefetch.ValidateTorrentNetwork(data); err != nil {
		return err
	}
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if err != nil || !strings.EqualFold(meta.InfoHash, expected) {
		return errors.New("种子hash与任务快照不一致")
	}
	urls, err := acquirefetch.TorrentNetworkURLs(data)
	if err != nil {
		return err
	}
	for _, raw := range urls {
		if err := netproxy.ValidateTargetURL(context.Background(), raw); err != nil {
			return err
		}
	}
	return nil
}
func automationAdd(c *qbClient, data []byte, name, path string, paused bool) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("torrents", filepath.Base(name))
	if err != nil {
		return err
	}
	if _, err = part.Write(data); err != nil {
		return err
	}
	for key, val := range map[string]string{"savepath": path, "paused": strconv.FormatBool(paused), "stopped": strconv.FormatBool(paused), "skip_checking": "false", "autoTMM": "false"} {
		if err = writer.WriteField(key, val); err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	response, err := c.PostMultipart("torrents/add", &body, writer.FormDataContentType())
	if err != nil {
		return errors.New("qB加种结果未知，必须按hash核实，不会盲目重发")
	}
	if strings.TrimSpace(string(response)) != "Ok." && strings.TrimSpace(string(response)) != "" {
		return errors.New("qB未确认接受种子")
	}
	return nil
}

// AddSourceTorrent 按已验证hash添加源站私有种子，强制实际下载和校验。
// 若哈希已存在则返回 ErrAutomationSourceExists；加种响应未知时调用者需查询哈希恢复。
func (d Downloader) AddSourceTorrent(data []byte, fileName, savePath, expectedHash string) error {
	if err := automationValidate(data, expectedHash); err != nil {
		return err
	}
	c, err := d.automationQB()
	if err != nil {
		return err
	}
	status, err := automationStatus(c, expectedHash)
	if err != nil {
		return err
	}
	if status.Found {
		return ErrAutomationSourceExists
	}
	return automationAdd(c, data, fileName, savePath, false)
}

// ErrAutomationSourceExists 表示准备提交前发现源种已存在，未向下载器提交添加请求。
var ErrAutomationSourceExists = errors.New("源种子已存在于下载器")

// Trackers 读取Tracker状态供上层按目标站精确域名确认做种，不修改任何Tracker。
func (d Downloader) Trackers(hash string) ([]AutomationTracker, error) {
	c, err := d.automationQB()
	if err != nil {
		return nil, err
	}
	body, err := c.Get("torrents/trackers", url.Values{"hash": {hash}})
	if err != nil {
		return nil, errors.New("读取Tracker状态失败")
	}
	var result []AutomationTracker
	if json.Unmarshal(body, &result) != nil {
		return nil, errors.New("Tracker状态无效")
	}
	return result, nil
}

// EnsureTargetSeeding 只对已验证目标种子添加Tracker/补种；不执行站点上传。
// 新种暂停加入后完整校验；后续调用等待校验结束再恢复。网络错误保留上层发布证据。
func (d Downloader) EnsureTargetSeeding(data []byte, fileName, savePath, expectedHash string) error {
	if err := automationValidate(data, expectedHash); err != nil {
		return err
	}
	c, err := d.automationQB()
	if err != nil {
		return err
	}
	status, err := automationStatus(c, expectedHash)
	if err != nil {
		return err
	}
	if status.Found && strings.TrimRight(status.SavePath, "/\\") != strings.TrimRight(savePath, "/\\") {
		return errors.New("目标hash位于其他保存目录，禁止修改现有任务")
	}
	if !status.Found {
		if err = automationAdd(c, data, fileName, savePath, true); err != nil {
			return err
		}
		_, err = c.PostForm("torrents/recheck", url.Values{"hashes": {expectedHash}})
		return err
	}
	urls, _ := acquirefetch.TorrentNetworkURLs(data)
	existing, err := d.Trackers(expectedHash)
	if err != nil {
		return err
	}
	missing := []string{}
	for _, raw := range urls {
		found := false
		for _, tracker := range existing {
			if tracker.URL == raw {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, raw)
		}
	}
	if len(missing) > 0 {
		if _, err = c.PostForm("torrents/addTrackers", url.Values{"hash": {expectedHash}, "urls": {strings.Join(missing, "\n")}}); err != nil {
			return err
		}
	}
	if strings.HasPrefix(status.State, "checking") || status.State == "allocating" || status.Progress < 1 || status.AmountLeft != 0 {
		return nil
	}
	complete, err := d.TorrentFilesComplete(expectedHash)
	if err != nil || !complete {
		return err
	}
	version, err := c.Get("app/version", nil)
	if err != nil {
		return err
	}
	endpoint := "torrents/start"
	if strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(string(version)), "v"), "4.") {
		endpoint = "torrents/resume"
	}
	_, err = c.PostForm(endpoint, url.Values{"hashes": {expectedHash}})
	return err
}

// FreeSpace 仅在目标路径与qB默认保存目录一致时报告剩余空间，其他挂载点拒绝猜测。
// 参数为空时使用默认目录；副作用为读取qB配置和主状态。
func (d Downloader) FreeSpace(savePath string) (int64, error) {
	c, err := d.automationQB()
	if err != nil {
		return 0, err
	}
	body, err := c.Get("app/preferences", nil)
	if err != nil {
		return 0, err
	}
	prefs := map[string]any{}
	if json.Unmarshal(body, &prefs) != nil {
		return 0, errors.New("qB默认路径未知")
	}
	configured := toString(prefs["save_path"], "")
	if savePath != "" && strings.TrimRight(savePath, "/\\") != strings.TrimRight(configured, "/\\") {
		return 0, errors.New("qB无法证明指定目录的剩余空间；只支持默认目录检查")
	}
	body, err = c.Get("sync/maindata", nil)
	if err != nil {
		return 0, err
	}
	root := map[string]any{}
	if json.Unmarshal(body, &root) != nil {
		return 0, errors.New("qB空间响应无效")
	}
	state := toMap(root["server_state"])
	value, ok := state["free_space_on_disk"]
	if !ok {
		return 0, errors.New("qB未返回剩余空间")
	}
	return toInt64Any(value), nil
}

func automationAmountLeft(row map[string]any) int64 {
	value, ok := row["amount_left"]
	if !ok {
		return -1
	}
	return toInt64Any(value)
}
