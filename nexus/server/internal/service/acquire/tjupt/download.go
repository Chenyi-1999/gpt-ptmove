package tjupt

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxTorrentBytes = 512 << 10

// AutoCandidate 保存 RSS 候选及仅由 RSS 解析得到的私有下载链接。
type AutoCandidate struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
	download  string
}

// Torrent 返回已校验的 v1 种子哈希、载荷体积和原始文件。
type Torrent struct {
	Hash      string
	SizeBytes int64
	Data      []byte
}

// AutoCandidates 解析北洋园 RSS 中指向站内下载端点的种子。
func (c *Client) AutoCandidates(ctx context.Context, rss string) ([]AutoCandidate, error) {
	feedURL, err := url.Parse(rss)
	if err != nil || !validRSSURL(feedURL) {
		return nil, errors.New("RSS 地址必须是北洋园 HTTPS RSS 链接")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL.String(), nil)
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
	rows := make([]AutoCandidate, 0, min(len(feed.Channel.Items), maxItems))
	seen := make(map[string]bool)
	for _, item := range feed.Channel.Items {
		id := torrentID(item.Link, item.GUID, item.Enclosure.URL)
		link := safeDownloadLink(item, id)
		if id == "" || link == "" || seen[id] || strings.TrimSpace(item.Title) == "" {
			continue
		}
		seen[id] = true
		rows = append(rows, AutoCandidate{ID: id, Title: strings.TrimSpace(item.Title), SizeBytes: explicitSize(item), download: link})
		if len(rows) == maxItems {
			break
		}
	}
	return rows, nil
}

// DownloadCandidate 下载 RSS 已验证的站内种子并检查 v1 哈希及 Tracker。
func (c *Client) DownloadCandidate(ctx context.Context, candidate AutoCandidate) (Torrent, error) {
	if candidate.ID == "" || candidate.download == "" {
		return Torrent{}, errors.New("候选缺少有效站内下载链接")
	}
	u, err := url.Parse(candidate.download)
	if err != nil || !safeDownloadURL(u, candidate.ID) {
		return Torrent{}, errors.New("候选下载链接无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Torrent{}, errors.New("无法构造种子下载请求")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Torrent{}, errors.New("北洋园种子下载失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Torrent{}, errors.New("北洋园种子下载响应状态异常")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes+1))
	if err != nil || len(data) > maxTorrentBytes {
		return Torrent{}, errors.New("种子文件无效或超出 512 KiB 限制")
	}
	meta, err := parseTorrent(data)
	if err != nil {
		return Torrent{}, errors.New("种子元数据无效或 Tracker 不属于北洋园")
	}
	return Torrent{Hash: meta.Hash, SizeBytes: meta.Size, Data: data}, nil
}

func safeDownloadLink(item rssItem, id string) string {
	if id == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(item.Enclosure.URL))
	if err != nil || !safeDownloadURL(u, id) {
		return ""
	}
	query := url.Values{"id": {id}, "passkey": {u.Query().Get("passkey")}}
	if u.Query().Get("linktype") == "dl" {
		query.Set("linktype", "dl")
	}
	return "https://" + u.Hostname() + "/download.php?" + query.Encode()
}

func safeDownloadURL(u *url.URL, id string) bool {
	if u == nil || u.Scheme != "https" || (u.Hostname() != "tjupt.org" && u.Hostname() != "www.tjupt.org") || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || u.Path != "/download.php" {
		return false
	}
	query := u.Query()
	if query.Get("id") != id || query.Get("passkey") == "" {
		return false
	}
	if linkType := query.Get("linktype"); linkType != "" && linkType != "dl" {
		return false
	}
	for key := range query {
		if key != "id" && key != "passkey" && key != "linktype" {
			return false
		}
	}
	return true
}

type torrentMeta struct {
	Hash string
	Size int64
}

func parseTorrent(data []byte) (torrentMeta, error) {
	root, err := decodeBencode(data)
	if err != nil {
		return torrentMeta{}, err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return torrentMeta{}, errors.New("torrent root is not a dictionary")
	}
	announce, ok := dict["announce"].([]byte)
	if !ok || !allowedTracker(string(announce)) {
		return torrentMeta{}, errors.New("invalid announce")
	}
	if rows, exists := dict["announce-list"]; exists && !allTrackersAllowed(rows) {
		return torrentMeta{}, errors.New("invalid announce list")
	}
	info, ok := dict["info"].(map[string]any)
	if !ok || info["meta version"] != nil {
		return torrentMeta{}, errors.New("not a v1 torrent")
	}
	if _, ok := info["pieces"].([]byte); !ok {
		return torrentMeta{}, errors.New("missing v1 pieces")
	}
	start, end, err := infoValueRange(data)
	if err != nil {
		return torrentMeta{}, err
	}
	size, err := payloadSize(info)
	if err != nil {
		return torrentMeta{}, err
	}
	sum := sha1.Sum(data[start:end])
	return torrentMeta{Hash: hex.EncodeToString(sum[:]), Size: size}, nil
}

func allowedTracker(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && TrackerHost(u.Hostname())
}
func allTrackersAllowed(value any) bool {
	rows, ok := value.([]any)
	if !ok || len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		tier, ok := row.([]any)
		if !ok || len(tier) == 0 {
			return false
		}
		for _, entry := range tier {
			tracker, ok := entry.([]byte)
			if !ok || !allowedTracker(string(tracker)) {
				return false
			}
		}
	}
	return true
}
func payloadSize(info map[string]any) (int64, error) {
	if length, ok := info["length"].(int64); ok && length >= 0 {
		return length, nil
	}
	files, ok := info["files"].([]any)
	if !ok || len(files) == 0 {
		return 0, errors.New("missing payload files")
	}
	var total int64
	for _, row := range files {
		file, ok := row.(map[string]any)
		if !ok {
			return 0, errors.New("invalid file entry")
		}
		length, ok := file["length"].(int64)
		if !ok || length < 0 || total > int64(^uint64(0)>>1)-length {
			return 0, errors.New("invalid payload size")
		}
		total += length
	}
	return total, nil
}

// bdecode 仅解码经过长度限制的 bencode 元数据。
type bdecode struct {
	data               []byte
	pos                int
	infoStart, infoEnd int
}

func decodeBencode(data []byte) (any, error) {
	d := bdecode{data: data}
	v, err := d.value(0)
	if err != nil || d.pos != len(data) {
		return nil, errors.New("malformed bencode")
	}
	return v, nil
}
func (d *bdecode) value(depth int) (any, error) {
	if depth > 32 || d.pos >= len(d.data) {
		return nil, errors.New("bencode limit")
	}
	switch d.data[d.pos] {
	case 'i':
		d.pos++
		end := bytesIndex(d.data, d.pos, 'e')
		if end < 0 {
			return nil, errors.New("invalid integer")
		}
		n, err := strconv.ParseInt(string(d.data[d.pos:end]), 10, 64)
		if err != nil {
			return nil, err
		}
		d.pos = end + 1
		return n, nil
	case 'l':
		d.pos++
		var list []any
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
			if len(list) > 10000 {
				return nil, errors.New("list too long")
			}
		}
		if d.pos >= len(d.data) {
			return nil, errors.New("unterminated list")
		}
		d.pos++
		return list, nil
	case 'd':
		d.pos++
		result := make(map[string]any)
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			key, err := d.bytes()
			if err != nil {
				return nil, err
			}
			name := string(key)
			start := d.pos
			value, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			if name == "info" {
				d.infoStart = start
				d.infoEnd = d.pos
			}
			result[name] = value
			if len(result) > 10000 {
				return nil, errors.New("dictionary too large")
			}
		}
		if d.pos >= len(d.data) {
			return nil, errors.New("unterminated dictionary")
		}
		d.pos++
		return result, nil
	default:
		return d.bytes()
	}
}
func (d *bdecode) bytes() ([]byte, error) {
	colon := bytesIndex(d.data, d.pos, ':')
	if colon < 0 {
		return nil, errors.New("invalid string")
	}
	n, err := strconv.Atoi(string(d.data[d.pos:colon]))
	if err != nil || n < 0 || n > maxTorrentBytes || colon+1+n > len(d.data) {
		return nil, errors.New("string length invalid")
	}
	d.pos = colon + 1
	out := d.data[d.pos : d.pos+n]
	d.pos += n
	return out, nil
}
func bytesIndex(data []byte, start int, target byte) int {
	for i := start; i < len(data); i++ {
		if data[i] == target {
			return i
		}
	}
	return -1
}
func infoValueRange(data []byte) (int, int, error) {
	d := bdecode{data: data}
	_, err := d.value(0)
	if err != nil || d.pos != len(data) || d.infoEnd <= d.infoStart {
		return 0, 0, fmt.Errorf("invalid info dictionary")
	}
	return d.infoStart, d.infoEnd, nil
}
