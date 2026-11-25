// Package tjupt 提供北洋园 RSS 预览与 H&R 状态的只读接口。
package tjupt

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxRSSBytes = 2 << 20
	maxItems    = 500
)

var hashPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// Client 限定访问北洋园公开 RSS 和官方 H&R 查询 API。
type Client struct{ http *http.Client }

// New 创建禁止自动跳转、限制超时的只读客户端。
func New() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Candidate 表示 RSS 条目的预览结果，未知大小及考核时长保留为空。
type Candidate struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	SizeBytes      *int64 `json:"size_bytes"`
	DetailURL      string `json:"detail_url"`
	AssessmentHour *int   `json:"assessment_hours"`
	Eligible       bool   `json:"eligible"`
	Reason         string `json:"reason"`
}

// Assessment 表示北洋园 API 返回的 H&R 考核及可退种判定。
type Assessment struct {
	Code            *int      `json:"code"`
	Label           string    `json:"label"`
	CanRetire       bool      `json:"can_retire"`
	SeededSeconds   *int64    `json:"seeded_seconds"`
	RequiredSeconds *int64    `json:"required_seconds"`
	LeechedSeconds  *int64    `json:"leeched_seconds"`
	RecordID        int64     `json:"-"`
	TorrentID       int64     `json:"-"`
	AddedAt         time.Time `json:"-"`
}

// ValidHash 检查种子哈希并输出小写十六进制字符串。
func ValidHash(hash string) bool { return hashPattern.MatchString(hash) }

// TrackerHost 识别北洋园站点及子域名。
func TrackerHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "tjupt.org" || strings.HasSuffix(host, ".tjupt.org")
}

// Preview 请求受限的北洋园 RSS 并基于明确列出的文件大小给出 H&R 时长预览。
func (c *Client) Preview(ctx context.Context, rawURL string, maxGiB int) ([]Candidate, error) {
	if maxGiB < 1 || maxGiB > 2000 {
		return nil, errors.New("最大种子体积应在 1 至 2000 GiB 之间")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !validRSSURL(u) {
		return nil, errors.New("RSS 地址必须是北洋园 HTTPS RSS 链接")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("无法构造 RSS 请求")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("北洋园 RSS 请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("北洋园 RSS 响应状态异常")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRSSBytes+1))
	if err != nil || len(body) > maxRSSBytes {
		return nil, errors.New("北洋园 RSS 响应无效或超出大小限制")
	}
	var feed rssFeed
	if xml.Unmarshal(body, &feed) != nil {
		return nil, errors.New("北洋园 RSS 格式无效")
	}
	out := make([]Candidate, 0, min(len(feed.Channel.Items), maxItems))
	seen := make(map[string]bool)
	for _, item := range feed.Channel.Items {
		if len(out) == maxItems {
			break
		}
		id := torrentID(item.Link, item.GUID, item.Enclosure.URL)
		if id == "" || seen[id] {
			continue
		}
		itemTitle := strings.TrimSpace(item.Title)
		if itemTitle == "" {
			continue
		}
		seen[id] = true
		row := Candidate{ID: id, Title: itemTitle, DetailURL: "https://tjupt.org/details.php?id=" + id, Reason: "RSS 未提供可识别的种子大小，暂不推荐"}
		size := explicitSize(item)
		if size != nil {
			hours := requiredHours(*size)
			row.SizeBytes, row.AssessmentHour = size, &hours
			if *size <= int64(maxGiB)<<30 {
				row.Eligible, row.Reason = true, "体积在预览上限内，仍需人工核对 H&R 状态"
			} else {
				row.Reason = "超过预览设置的最大体积"
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// Status 按哈希批量读取官方 H&R 记录；未知或重复记录不可退种。
func (c *Client) Status(ctx context.Context, token string, hashes []string) (map[string]Assessment, error) {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n \t") || len(token) > 1024 {
		return nil, errors.New("H&R API Token 格式无效")
	}
	if len(hashes) == 0 || len(hashes) > 50 {
		return nil, errors.New("H&R 查询须包含 1 至 50 个哈希")
	}
	requested := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		if !ValidHash(h) {
			return nil, errors.New("H&R 查询包含无效哈希")
		}
		requested[strings.ToLower(h)] = true
	}
	encoded, _ := json.Marshal(map[string][]string{"info_hash": hashes})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://tjupt.org/api/v1/hnr.php", strings.NewReader(string(encoded)))
	if err != nil {
		return nil, errors.New("无法构造 H&R 查询")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("北洋园 H&R 查询失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("北洋园 H&R 响应状态异常")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRSSBytes+1))
	if err != nil || len(body) > maxRSSBytes {
		return nil, errors.New("北洋园 H&R 响应无效或超出大小限制")
	}
	var result hnrResponse
	if json.Unmarshal(body, &result) != nil || result.Error || result.Data == nil {
		return nil, errors.New("北洋园 H&R 响应格式无效")
	}
	out := make(map[string]Assessment, len(result.Data))
	seen := make(map[string]bool)
	for _, row := range result.Data {
		h := strings.ToLower(row.Torrent.Hash)
		if !requested[h] {
			continue
		}
		if seen[h] {
			delete(out, h)
			continue
		}
		seen[h] = true
		code := row.Status.Code
		assessment := Assessment{
			Code: code, Label: "站点未返回有效状态", SeededSeconds: row.Statistics.SeedTime,
			RequiredSeconds: row.Status.NeedSeedTime, LeechedSeconds: row.Statistics.LeecTime,
			RecordID: int64(row.ID), TorrentID: int64(row.Torrent.TID), AddedAt: parseAddedAt(row.Timestamps.AddedAt),
		}
		if code != nil {
			assessment.Label = statusLabel(*code)
			assessment.CanRetire = row.ID > 0 && row.Torrent.TID > 0 && explicitlyRetirable(*code)
		}
		out[h] = assessment
	}
	return out, nil
}

func validRSSURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || (u.Hostname() != "tjupt.org" && u.Hostname() != "www.tjupt.org") || u.User != nil || u.Fragment != "" {
		return false
	}
	if port := u.Port(); port != "" && port != "443" {
		return false
	}
	switch u.Path {
	case "/rss.php", "/torrentrss.php":
		return true
	}
	return false
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	SizeTag     string `xml:"size"`
	SizeText    string `xml:"filesize"`
	Description string `xml:"description"`
	Content     string `xml:"encoded"`
	Enclosure   struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
}
type hnrResponse struct {
	Error bool        `json:"error"`
	Data  []hnrRecord `json:"data"`
}
type hnrRecord struct {
	ID      int `json:"id"`
	Torrent struct {
		TID  int    `json:"tid"`
		Hash string `json:"info_hash"`
	} `json:"torrent"`
	Status struct {
		Code         *int   `json:"hnr_status_code"`
		NeedSeedTime *int64 `json:"need_seed_time"`
	} `json:"status"`
	Statistics struct {
		SeedTime *int64 `json:"seed_time"`
		LeecTime *int64 `json:"leech_time"`
	} `json:"statistics"`
	Timestamps struct {
		AddedAt string `json:"added_at"`
	} `json:"timestamps"`
}

func torrentID(values ...string) string {
	for _, value := range values {
		u, err := url.Parse(strings.TrimSpace(value))
		if err != nil || u.User != nil {
			continue
		}
		if u.Host != "" && (!TrackerHost(u.Hostname()) || (u.Scheme != "https" && u.Scheme != "http")) {
			continue
		}
		switch strings.TrimPrefix(u.Path, "/") {
		case "details.php", "download.php":
			id := u.Query().Get("id")
			if len(id) > 0 && len(id) < 20 {
				if number, err := strconv.ParseUint(id, 10, 64); err == nil && number > 0 {
					return strconv.FormatUint(number, 10)
				}
			}
		}
	}
	return ""
}

var sizePattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(GiB|GB|MiB|MB|TiB|TB)`)
var titleSizePattern = regexp.MustCompile(`(?i)\[\s*([0-9]+(?:\.[0-9]+)?\s*(?:GiB|GB|MiB|MB|TiB|TB))\s*\]`)

func explicitSize(item rssItem) *int64 {
	for _, explicit := range []string{item.SizeTag, item.SizeText} {
		if size, err := strconv.ParseInt(strings.TrimSpace(explicit), 10, 64); err == nil && size > 0 {
			return &size
		}
	}
	titleSize := titleSizePattern.FindString(item.Title)
	values := []string{item.SizeTag, item.SizeText, titleSize, item.Description, item.Content}
	for _, value := range values {
		matches := sizePattern.FindStringSubmatch(value)
		if len(matches) != 3 {
			continue
		}
		amount, err := strconv.ParseFloat(matches[1], 64)
		if err != nil || amount <= 0 {
			continue
		}
		unit := strings.ToUpper(matches[2])
		multiplier := float64(1 << 30)
		switch unit {
		case "MIB", "MB":
			multiplier = 1 << 20
		case "TIB", "TB":
			multiplier = 1 << 40
		}
		result := int64(amount * multiplier)
		if result > 0 {
			return &result
		}
	}
	return nil
}

func requiredHours(size int64) int {
	gib := float64(size) / float64(uint64(1)<<30)
	switch {
	case gib <= 10:
		return 24
	case gib <= 20:
		return 48
	case gib <= 30:
		return 72
	case gib <= 40:
		return 96
	case gib <= 50:
		return 120
	default:
		return 168
	}
}
func explicitlyRetirable(code int) bool {
	return code == 20 || code == 21 || code == 40 || code == 41 || code == 42
}
func statusLabel(code int) string {
	switch code {
	case 0:
		return "未触发"
	case 1:
		return "24h等待期"
	case 5, 6:
		return "下载考核中"
	case 10, 11:
		return "做种考核中"
	case 12:
		return "未达标"
	case 20:
		return "已达标"
	case 21:
		return "后达标"
	case 40:
		return "种子被删取消"
	case 41:
		return "未成功下载取消"
	case 42:
		return "管理员取消"
	case 43:
		return "自行放弃"
	case 44:
		return "冗余考核"
	default:
		return fmt.Sprintf("未知状态（%d）", code)
	}
}

func parseAddedAt(value string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		location := time.UTC
		if layout == "2006-01-02 15:04:05" {
			location = time.FixedZone("北洋园", 8*60*60)
		}
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
