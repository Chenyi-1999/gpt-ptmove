package downloaderclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TorrentSnapshot 表示从下载器读取的一条标准化种子快照。
// 参数/返回：用于跨下载器统一字段，供上层服务写入 torrents/torrent_upload_stats。
// 失败场景：无直接失败场景，具体失败由 FetchTorrents 返回。
// 副作用：无副作用，仅承载数据。
type TorrentSnapshot struct {
	Hash           string
	Name           string
	SavePath       string
	Size           int64
	Progress       float64
	State          string
	Trackers       []string
	Comment        string
	Group          string
	Category       string
	Tags           []string
	InventoryState string
	Seeders        int64
	Uploaded       int64
}

// FetchTorrents 拉取下载器当前全部种子并归一化为统一结构。
// 参数/返回：根据下载器类型自动走 qBittorrent 或 Transmission，返回种子快照列表。
// 失败场景：连接失败、认证失败、RPC 异常、响应解析失败时返回错误。
// 副作用：会向下载器发起网络请求。
func (d Downloader) FetchTorrents() ([]TorrentSnapshot, error) {
	return d.fetchTorrents(90 * time.Second)
}

// FetchInventoryTorrents 为插件统计读取下载器当前快照，每次 HTTP 调用最多等待 12 秒。
// 参数/返回：无参数；成功返回归一化种子快照，失败返回认证或下载器错误。
// 失败场景：下载器不可用、认证失败或接口响应无效。
// 副作用：每种下载器仅读取一次种子列表。
func (d Downloader) FetchInventoryTorrents() ([]TorrentSnapshot, error) {
	return d.fetchTorrents(12 * time.Second)
}

func (d Downloader) fetchTorrents(timeout time.Duration) ([]TorrentSnapshot, error) {
	switch d.Type {
	case "qbittorrent":
		return d.fetchQBTorrents(timeout)
	case "transmission":
		return d.fetchTransmissionTorrents(timeout)
	default:
		return nil, fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

func (d Downloader) fetchQBTorrents(timeout time.Duration) ([]TorrentSnapshot, error) {
	client, err := newQBClient(d)
	if err != nil {
		return nil, err
	}
	client.client.Timeout = timeout
	if err := client.Login(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("filter", "all")
	body, err := client.Get("torrents/info", params)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0)
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("qB 种子列表解析失败: %w", err)
	}
	if rows == nil {
		return nil, fmt.Errorf("qB 未返回种子列表")
	}

	result := make([]TorrentSnapshot, 0, len(rows))
	for _, row := range rows {
		hash := strings.TrimSpace(toString(row["hash"], ""))
		name := strings.TrimSpace(toString(row["name"], ""))
		if hash == "" || name == "" {
			continue
		}

		trackers := make([]string, 0, 2)
		if tracker := strings.TrimSpace(toString(row["tracker"], "")); tracker != "" {
			trackers = append(trackers, tracker)
		}

		category := strings.TrimSpace(toString(row["category"], ""))
		tags := parseTorrentTags(row["tags"])
		group := torrentGroup(category, tags)

		progress := normalizeProgressPercent(row["progress"])
		seeders := toInt64Any(row["num_seeds"])
		if seeders <= 0 {
			seeders = toInt64Any(row["num_complete"])
		}

		result = append(result, TorrentSnapshot{
			Hash:           hash,
			Name:           name,
			SavePath:       strings.TrimSpace(toString(row["save_path"], "")),
			Size:           toInt64Any(row["size"]),
			Progress:       progress,
			State:          formatTorrentState(toString(row["state"], "")),
			Trackers:       trackers,
			Comment:        strings.TrimSpace(toString(row["comment"], "")),
			Group:          group,
			Category:       category,
			Tags:           tags,
			InventoryState: normalizeInventoryState(toString(row["state"], "")),
			Seeders:        seeders,
			Uploaded:       toInt64Any(row["uploaded"]),
		})
	}

	return result, nil
}

func (d Downloader) fetchTransmissionTorrents(timeout time.Duration) ([]TorrentSnapshot, error) {
	client := newTransmissionClient(d)
	client.client.Timeout = timeout
	response, err := client.Call("torrent-get", map[string]any{
		"fields": []string{
			"hashString",
			"name",
			"downloadDir",
			"totalSize",
			"sizeWhenDone",
			"status",
			"error",
			"comment",
			"trackers",
			"trackerStats",
			"percentDone",
			"uploadedEver",
			"peersGettingFromUs",
			"labels",
		},
	})
	if err != nil {
		return nil, err
	}

	args := toMap(response["arguments"])
	torrents, ok := args["torrents"].([]any)
	if !ok || torrents == nil {
		return nil, fmt.Errorf("Transmission 未返回种子列表")
	}
	result := make([]TorrentSnapshot, 0, len(torrents))

	for _, raw := range torrents {
		row := toMap(raw)
		hash := strings.TrimSpace(toString(row["hashString"], ""))
		name := strings.TrimSpace(toString(row["name"], ""))
		if hash == "" || name == "" {
			continue
		}

		size := toInt64Any(row["sizeWhenDone"])
		if size <= 0 {
			size = toInt64Any(row["totalSize"])
		}

		trackers := extractTransmissionTrackers(row)
		tags := parseTorrentTags(row["labels"])
		group := firstString(tags)
		statusText := transmissionStatusText(int(toInt64Any(row["status"])))
		inventoryState := normalizeInventoryState(statusText)
		if toInt64Any(row["error"]) > 0 {
			inventoryState = "error"
		}

		result = append(result, TorrentSnapshot{
			Hash:           hash,
			Name:           name,
			SavePath:       strings.TrimSpace(toString(row["downloadDir"], "")),
			Size:           size,
			Progress:       normalizeProgressPercent(row["percentDone"]),
			State:          formatTorrentState(statusText),
			Trackers:       trackers,
			Comment:        strings.TrimSpace(toString(row["comment"], "")),
			Group:          group,
			Tags:           tags,
			InventoryState: inventoryState,
			Seeders:        toInt64Any(row["peersGettingFromUs"]),
			Uploaded:       toInt64Any(row["uploadedEver"]),
		})
	}

	return result, nil
}

func parseTorrentTags(raw any) []string {
	var values []string
	switch typed := raw.(type) {
	case string:
		values = strings.Split(typed, ",")
	case []string:
		values = typed
	case []any:
		values = make([]string, 0, len(typed))
		for _, value := range typed {
			values = append(values, toString(value, ""))
		}
	}

	tags := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func torrentGroup(category string, tags []string) string {
	if value := strings.TrimSpace(category); value != "" {
		return value
	}
	return firstString(tags)
}

func normalizeInventoryState(state string) string {
	state = strings.ToLower(strings.TrimSpace(state))
	switch {
	case state == "", state == "unknown":
		return "other"
	case strings.Contains(state, "error"), strings.Contains(state, "missing"):
		return "error"
	case strings.Contains(state, "check"):
		return "checking"
	case strings.Contains(state, "queue"):
		return "queued"
	case strings.Contains(state, "download"), strings.Contains(state, "metadl"), strings.Contains(state, "forceddl"), state == "stalleddl", strings.Contains(state, "allocat"):
		return "downloading"
	case strings.Contains(state, "upload"), strings.Contains(state, "seed"), state == "stalledup", state == "forcedup":
		return "seeding"
	case strings.Contains(state, "pause"), strings.Contains(state, "stop"), state == "stalled":
		return "paused"
	default:
		return "other"
	}
}

func extractTransmissionTrackers(row map[string]any) []string {
	result := make([]string, 0, 4)
	seen := map[string]struct{}{}

	appendTracker := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	for _, item := range toSlice(row["trackers"]) {
		tracker := toMap(item)
		appendTracker(toString(tracker["announce"], ""))
		appendTracker(toString(tracker["host"], ""))
	}
	for _, item := range toSlice(row["trackerStats"]) {
		tracker := toMap(item)
		appendTracker(toString(tracker["announce"], ""))
		appendTracker(toString(tracker["host"], ""))
	}

	return result
}

func normalizeProgressPercent(value any) float64 {
	raw := toFloat64(value)
	if raw <= 0 {
		return 0
	}
	if raw <= 1 {
		raw *= 100
	}
	if raw < 0 {
		return 0
	}
	if raw > 100 {
		return 100
	}
	return raw
}

func toFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
		return 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		parsed, err := json.Number(trimmed).Float64()
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func transmissionStatusText(status int) string {
	switch status {
	case 0:
		return "stopped"
	case 1:
		return "check_wait"
	case 2:
		return "checking"
	case 3:
		return "queued"
	case 4:
		return "downloading"
	case 5:
		return "queued"
	case 6:
		return "seeding"
	default:
		return fmt.Sprintf("status_%d", status)
	}
}

func formatTorrentState(state string) string {
	stateLower := strings.ToLower(strings.TrimSpace(state))
	stateMap := map[string]string{
		"downloading":   "下载中",
		"forceddl":      "下载中",
		"uploading":     "做种中",
		"stalledup":     "做种中",
		"forcedup":      "做种中",
		"seed":          "做种中",
		"seeding":       "做种中",
		"paused":        "暂停",
		"stopped":       "暂停",
		"stalleddl":     "暂停",
		"check_wait":    "校验中",
		"checking":      "校验中",
		"check":         "校验中",
		"error":         "错误",
		"missingfiles":  "文件丢失",
		"moving":        "移动中",
		"allocating":    "分配空间",
		"queued":        "队列",
		"queuedup":      "队列",
		"queueddl":      "队列",
		"meta":          "下载中",
		"stalled":       "暂停",
		"upnp":          "暂停",
		"downloadingup": "下载中",
	}
	for key, value := range stateMap {
		if strings.Contains(stateLower, key) {
			return value
		}
	}
	if strings.TrimSpace(state) == "" {
		return "未知"
	}
	runes := []rune(state)
	if len(runes) == 0 {
		return "未知"
	}
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}
