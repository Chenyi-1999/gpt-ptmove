package torrentdata

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	iyuuAPIBase        = "https://2025.iyuu.cn"
	iyuuClientVersion  = "8.2.0"
	iyuuRequestTimeout = 120 * time.Second
	iyuuResponseLimit  = 16 << 20
	iyuuBatchLimit     = 200
)

const iyuuLogModule = "IYUU查询"

type iyuuSite struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	BaseURL  string `json:"base_url"`
	Site     string `json:"site"`
}

type iyuuClient struct {
	httpClient *http.Client

	baseURL string

	mu              sync.Mutex
	lastRequestTime time.Time
	rateLimitDelay  time.Duration
}

type iyuuAPIError struct {
	code      int64
	message   string
	emptyData bool
}

func (e *iyuuAPIError) Error() string {
	return fmt.Sprintf("IYUU API 错误: %s (代码: %d)", e.message, e.code)
}

// newIYUUClient 创建 IYUU API 客户端。
// 参数/返回：无输入；返回可复用的客户端实例。
// 失败场景：无。
// 副作用：无（仅初始化内存对象）。
func newIYUUClient() *iyuuClient {
	return &iyuuClient{
		httpClient: &http.Client{
			Timeout: iyuuRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:        iyuuAPIBase,
		rateLimitDelay: 5 * time.Second,
	}
}

func (c *iyuuClient) doRequest(ctx context.Context, method string, rawURL string, token string, headers map[string]string, body io.Reader) (map[string]any, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("IYUU Token 未配置")
	}

	c.mu.Lock()
	now := time.Now()
	wait := c.lastRequestTime.Add(c.rateLimitDelay).Sub(now)
	c.lastRequestTime = now.Add(max(wait, 0))
	c.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Token", token)
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("IYUU API HTTP错误 status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(limited)))
	}

	var parsed map[string]any
	data, err := io.ReadAll(io.LimitReader(resp.Body, iyuuResponseLimit+1))
	if err != nil || len(data) > iyuuResponseLimit {
		return nil, errors.New("IYUU API 读取响应失败或响应超过大小限制")
	}
	if err = json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("IYUU API 响应JSON解析失败: %w", err)
	}
	code, ok := iyuuInteger(parsed["code"])
	if !ok {
		return nil, errors.New("IYUU API 响应缺少有效状态码")
	}
	if code != 0 {
		msg := strings.TrimSpace(stringValue(parsed["msg"], "未知 API 错误"))
		list, hasList := parsed["data"].([]any)
		return nil, &iyuuAPIError{code: code, message: msg, emptyData: hasList && len(list) == 0}
	}

	return parsed, nil
}

// iyuuInteger 只接受 JSON 可无损表示的非负整数，避免错误的站点 ID 被截断。
func iyuuInteger(value any) (int64, bool) {
	n, ok := value.(float64)
	if !ok || n < 0 || n > 1<<53-1 || n != float64(int64(n)) {
		return 0, false
	}
	return int64(n), true
}

func sha1Hex(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// getSupportedSites 获取 IYUU 支持的可辅种站点列表。
// 参数/返回：token 为 IYUU Token；返回站点列表。
// 失败场景：网络错误、Token 无效、API 返回错误。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) getSupportedSites(ctx context.Context, token string) ([]iyuuSite, error) {
	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/sites/index"
	resp, err := c.doRequest(ctx, http.MethodGet, rawURL, token, nil, nil)
	if err != nil {
		return nil, err
	}
	data, _ := resp["data"].(map[string]any)
	rawSites, _ := data["sites"].([]any)
	if len(rawSites) == 0 {
		return nil, errors.New("未能获取到可辅种站点列表，请检查 Token 或 IYUU 服务状态")
	}

	sites := make([]iyuuSite, 0, len(rawSites))
	for _, raw := range rawSites {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, known := iyuuInteger(m["id"])
		if !known || id == 0 {
			continue
		}
		site := iyuuSite{
			ID:       id,
			Nickname: strings.TrimSpace(stringValue(m["nickname"], "")),
			BaseURL:  strings.TrimSpace(stringValue(m["base_url"], "")),
			Site:     strings.TrimSpace(stringValue(m["site"], "")),
		}
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		return nil, errors.New("IYUU 站点列表为空（解析失败）")
	}
	return sites, nil
}

// reportExisting 上报现有站点列表并获取 sid_sha1。
// 参数/返回：sidList 为 IYUU 站点ID列表；返回 sid_sha1。
// 失败场景：网络错误、API 返回错误、sid_sha1 缺失。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) reportExisting(ctx context.Context, token string, sidList []int64) (string, error) {
	if len(sidList) == 0 {
		return "", errors.New("sid_list 为空，无法生成 sid_sha1")
	}
	payload := map[string]any{"sid_list": sidList}
	bodyBytes, _ := json.Marshal(payload)
	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/sites/reportExisting"
	resp, err := c.doRequest(ctx, http.MethodPost, rawURL, token, map[string]string{"Content-Type": "application/json"}, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	data, _ := resp["data"].(map[string]any)
	sidSha1 := strings.TrimSpace(stringValue(data["sid_sha1"], ""))
	if sidSha1 == "" {
		return "", errors.New("未能从 IYUU API 获取 sid_sha1")
	}
	return sidSha1, nil
}

// queryCrossSeedBatch 批量查询多个 infohash 的辅种信息（单次请求传入 hash 数组）。
// 参数/返回：hashes 为 infohash 列表（忽略空值，统一转小写去重）；返回 hash->torrentList 的映射。
// 失败场景：网络错误、API 返回错误（如 Token 无效等）。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) queryCrossSeedBatch(ctx context.Context, token string, hashes []string, sidSha1 string) (map[string][]map[string]any, error) {
	unique := make([]string, 0, len(hashes))
	seen := map[string]struct{}{}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		unique = append(unique, h)
	}
	if len(unique) == 0 {
		return map[string][]map[string]any{}, nil
	}
	resp, err := c.requestCrossSeedBatch(ctx, token, unique, sidSha1)
	if err != nil {
		// 对齐 Python：400/未查询到数据 视为无结果，不作为硬错误。
		msg := err.Error()
		if strings.Contains(msg, "400") || strings.Contains(msg, "未查询到可辅种数据") {
			result := map[string][]map[string]any{}
			for _, h := range unique {
				result[h] = []map[string]any{}
			}
			return result, nil
		}
		return nil, err
	}
	data, _ := resp["data"].(map[string]any)
	result := map[string][]map[string]any{}
	for _, h := range unique {
		entry, _ := data[h].(map[string]any)
		rawTorrents, _ := entry["torrent"].([]any)
		items := make([]map[string]any, 0, len(rawTorrents))
		for _, raw := range rawTorrents {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, m)
		}
		result[h] = items
	}
	return result, nil
}

// requestCrossSeedBatch 发起一次批量辅种查询并保留 IYUU 原始结果结构。
func (c *iyuuClient) requestCrossSeedBatch(ctx context.Context, token string, hashes []string, sidSha1 string) (map[string]any, error) {
	if strings.TrimSpace(sidSha1) == "" {
		return nil, errors.New("sid_sha1 为空，无法查询辅种信息")
	}

	hashesJSON, _ := json.Marshal(hashes)
	hashesJSONStr := string(hashesJSON)
	form := url.Values{}
	form.Set("hash", hashesJSONStr)
	form.Set("sha1", sha1Hex(hashesJSONStr))
	form.Set("sid_sha1", sidSha1)
	form.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	form.Set("version", iyuuClientVersion)

	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/index/index"
	return c.doRequest(ctx, http.MethodPost, rawURL, token, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, strings.NewReader(form.Encode()))
}

// queryDuplicates 解析目标站 SID 后发送一次哈希批查询，逐个返回可确认的查重结果。
func (c *iyuuClient) queryDuplicates(ctx context.Context, token, targetURL string, hashes []string) (map[string]bool, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("IYUU Token 未配置")
	}
	targetHost := iyuuSiteHost(targetURL)
	if targetHost == "" {
		return nil, errors.New("IYUU 查重的目标站地址无效")
	}
	unique := make([]string, 0, min(len(hashes), iyuuBatchLimit))
	seen := make(map[string]bool)
	for _, hash := range hashes {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if validIYUUHash(hash) && !seen[hash] {
			unique = append(unique, hash)
			seen[hash] = true
		}
	}
	result := make(map[string]bool, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	if len(unique) > iyuuBatchLimit {
		return nil, fmt.Errorf("IYUU 每批最多查询 %d 个种子", iyuuBatchLimit)
	}

	sites, err := c.getSupportedSites(ctx, token)
	if err != nil {
		return nil, errors.New("无法读取 IYUU 支持的站点，请检查 Token 和站点绑定")
	}
	targetSID, siteIDs := iyuuTargetSite(sites, targetHost)
	if targetSID == 0 {
		return nil, errors.New("IYUU 站点列表中无法唯一匹配目标站域名")
	}
	sidSha1, err := c.reportExisting(ctx, token, siteIDs)
	if err != nil || !validIYUUHash(sidSha1) {
		return nil, errors.New("IYUU 未返回有效的站点查询签名")
	}
	response, err := c.requestCrossSeedBatch(ctx, token, unique, sidSha1)
	if err != nil {
		var apiErr *iyuuAPIError
		if errors.As(err, &apiErr) && apiErr.code == 400 && apiErr.message == "未查询到可辅种数据" && apiErr.emptyData {
			for _, hash := range unique {
				result[hash] = false
			}
			return result, nil
		}
		return nil, errors.New("IYUU 批量查重失败，请核对 Token 或稍后重试")
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, errors.New("IYUU 批量查重结果格式异常")
	}
	for _, hash := range unique {
		if known, duplicate := iyuuDuplicateEvidence(data[hash], targetSID); known {
			result[hash] = duplicate
		}
	}
	return result, nil
}

// iyuuTargetSite 只接受唯一匹配目标域名的 SID，报告全部受支持站点以保留其他站的查重证据。
func iyuuTargetSite(sites []iyuuSite, targetHost string) (int64, []int64) {
	var targetSID int64
	ids := make([]int64, 0, len(sites))
	seen := make(map[int64]string, len(sites))
	for _, site := range sites {
		host := iyuuSiteHost(site.BaseURL)
		if previous, exists := seen[site.ID]; exists && previous != host {
			return 0, nil
		}
		if _, exists := seen[site.ID]; !exists {
			ids = append(ids, site.ID)
			seen[site.ID] = host
		}
		if host == targetHost {
			if targetSID != 0 && targetSID != site.ID {
				return 0, nil
			}
			targetSID = site.ID
		}
	}
	slices.Sort(ids)
	return targetSID, ids
}

func iyuuSiteHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

func validIYUUHash(hash string) bool {
	if len(hash) != sha1.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

// iyuuDuplicateEvidence 将结构完整的 torrent 数组转为查重结论，缺项或坏行保持未知。
func iyuuDuplicateEvidence(value any, targetSID int64) (known, duplicate bool) {
	entry, _ := value.(map[string]any)
	rows, ok := entry["torrent"].([]any)
	if !ok {
		return false, false
	}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		sid, validSID := iyuuInteger(row["sid"])
		torrentID, validID := iyuuInteger(row["torrent_id"])
		hash, _ := row["info_hash"].(string)
		if !validSID || sid == 0 || !validID || torrentID == 0 || !validIYUUHash(hash) {
			return false, false
		}
		duplicate = duplicate || sid == targetSID
	}
	return true, duplicate
}

func iyuuInfof(format string, args ...any) {
	logx.Infof(iyuuLogModule, format, args...)
}

func iyuuWarnf(format string, args ...any) {
	logx.Warnf(iyuuLogModule, format, args...)
}

func iyuuErrorf(format string, args ...any) {
	logx.Errorf(iyuuLogModule, format, args...)
}
