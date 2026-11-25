package downloaderclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

const tjuptFeedScopeTag = "starpt-tjupt"

// ErrTjuptFeedAlreadyExists 表示提交前发现 qB 内已有此哈希，没有加种。
var ErrTjuptFeedAlreadyExists = errors.New("qB 中已存在此种子")

// ErrTjuptFeedUnconfirmed 表示写入结果未知，须先核对 qB，禁止盲目重试。
var ErrTjuptFeedUnconfirmed = errors.New("qB 写入结果未确认")

// ErrTjuptFeedScope 表示任务身份或当前状态不符合条件，未发出写操作。
var ErrTjuptFeedScope = errors.New("qB 任务身份或当前状态不符合操作条件")

// TjuptFeedTorrent 按完整哈希只读查询 qB，包含手工任务。已确认不存在时返回 nil。
func (d Downloader) TjuptFeedTorrent(ctx context.Context, hash string) (*TaggedQBTorrent, error) {
	hash, valid := normalizeTjuptFeedHash(hash)
	if !valid {
		return nil, errors.New("北洋园种子哈希无效")
	}
	client, err := d.newPluginQB()
	if err != nil {
		return nil, err
	}
	defer client.client.CloseIdleConnections()
	return client.tjuptFeedTorrent(ctx, hash)
}

// AddTjuptFeedTorrent 通过原生 qB API 提交单个已验证的北洋园种子。试运行以停止状态入队。
// 投种前核对哈希，投种后要求对应添加回执及哈希、状态、标签、分类的读回一致。
func (d Downloader) AddTjuptFeedTorrent(ctx context.Context, data []byte, filename, savePath string, paused bool, tags []string, category, hash string) (*TaggedQBTorrent, error) {
	hash, valid := normalizeTjuptFeedHash(hash)
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if !valid || err != nil || meta.InfoHash != hash {
		return nil, errors.New("种子文件与北洋园任务哈希不一致")
	}
	tags = compactStrings(append([]string{tjuptFeedScopeTag}, tags...))
	for _, tag := range tags {
		if strings.ContainsRune(tag, ',') || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, errors.New("qB 标签不能包含逗号或控制字符")
		}
	}
	category = strings.TrimSpace(category)
	fields := map[string]string{
		"paused":        strconv.FormatBool(paused), // qB 4.x
		"stopped":       strconv.FormatBool(paused), // qB 5.x
		"skip_checking": "false",
		"seedMode":      "false",
		"tags":          strings.Join(tags, ","),
		"category":      category,
	}
	if savePath = strings.TrimSpace(savePath); savePath != "" {
		fields["savepath"] = savePath
		fields["autoTMM"] = "false"
	}
	if strings.TrimSpace(filename) == "" {
		filename = hash + ".torrent"
	}
	body, contentType, err := qbTorrentMultipart(data, filename, fields)
	if err != nil {
		return nil, errors.New("未能编码 qB 投种请求")
	}
	client, err := d.newPluginQB()
	if err != nil {
		return nil, err
	}
	defer client.client.CloseIdleConnections()
	existing, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrTjuptFeedAlreadyExists
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return nil, err
	}
	reply, err := client.pluginRequest(ctx, http.MethodPost, "torrents/add", contentType, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTjuptFeedUnconfirmed, err)
	}
	if !tjuptFeedAddAcknowledged(reply, hash) {
		return nil, fmt.Errorf("%w: 没有收到对应哈希的添加回执", ErrTjuptFeedUnconfirmed)
	}
	return client.confirmTjuptFeedTorrent(ctx, hash, category, tags, 0, paused)
}

// ResumeTjuptFeedTorrent 仅启动添加时间、试运行标记、分类均匹配的停止任务。
// 使用对应版本的原生启动接口并只发送一次，随后以只读查询确认状态。
func (d Downloader) ResumeTjuptFeedTorrent(ctx context.Context, hash string, addedOn int64, marker, category string) (*TaggedQBTorrent, error) {
	hash, valid := normalizeTjuptFeedHash(hash)
	if !valid || addedOn <= 0 || !validTjuptFeedMarker(marker) {
		return nil, ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return nil, err
	}
	defer client.client.CloseIdleConnections()
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return nil, err
	}
	category = strings.TrimSpace(category)
	tags := []string{tjuptFeedScopeTag, marker}
	if torrent == nil || !torrent.matchesTjuptFeedScope(category, tags, addedOn) || !torrent.TjuptFeedUntouched() {
		return nil, ErrTjuptFeedScope
	}
	endpoint := "torrents/start"
	if torrent.State == "pausedDL" || torrent.State == "pausedUP" {
		endpoint = "torrents/resume"
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return nil, err
	}
	form := url.Values{"hashes": {hash}}
	reply, err := client.pluginRequest(ctx, http.MethodPost, endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTjuptFeedUnconfirmed, err)
	}
	if result := strings.TrimSpace(string(reply)); result != "" && result != "Ok." {
		return nil, fmt.Errorf("%w: 没有收到 qB 启动回执", ErrTjuptFeedUnconfirmed)
	}
	return client.confirmTjuptFeedTorrent(ctx, hash, category, tags, addedOn, false)
}

// TjuptFeedStopped 识别 qB 两代接口明确停止的任务状态。
func (torrent TaggedQBTorrent) TjuptFeedStopped() bool {
	switch torrent.State {
	case "stoppedDL", "stoppedUP", "pausedDL", "pausedUP":
		return true
	default:
		return false
	}
}

// TjuptFeedUntouched 判断 qB 是否确认任务停在下载前且进度为零。
func (torrent TaggedQBTorrent) TjuptFeedUntouched() bool {
	return (torrent.State == "stoppedDL" || torrent.State == "pausedDL") &&
		torrent.Progress != nil && *torrent.Progress == 0
}

// TjuptFeedStarted 识别 qB 已开始处理的状态，包括排队和校验。
func (torrent TaggedQBTorrent) TjuptFeedStarted() bool {
	switch torrent.State {
	case "allocating", "checkingDL", "checkingUP", "downloading", "forcedDL", "forcedMetaDL", "forcedUP", "metaDL",
		"queuedDL", "queuedUP", "stalledDL", "stalledUP", "uploading":
		return true
	default:
		return false
	}
}

func normalizeTjuptFeedHash(hash string) (string, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	return hash, len(hash) == 40 && isTjuptFeedHex(hash)
}

// validTjuptFeedMarker 校验插件生成的任务归属标记。
func validTjuptFeedMarker(marker string) bool {
	id, ok := strings.CutPrefix(marker, "starpt-tjupt-trial-")
	return ok && len(id) == 24 && isTjuptFeedHex(id)
}

func isTjuptFeedHex(text string) bool {
	_, err := hex.DecodeString(text)
	return err == nil
}

func checkTjuptFeedEffect(ctx context.Context, guard func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard != nil {
		return guard()
	}
	return nil
}

// tjuptFeedTorrent 查询时不附加标签过滤，避免把已有的手工任务当成可添加状态。
func (client *qbClient) tjuptFeedTorrent(ctx context.Context, hash string) (*TaggedQBTorrent, error) {
	query := url.Values{"hashes": {hash}, "limit": {"2"}}
	body, err := client.pluginRequest(ctx, http.MethodGet, "torrents/info?"+query.Encode(), "", nil)
	if err != nil {
		return nil, err
	}
	var rows []TaggedQBTorrent
	if json.Unmarshal(body, &rows) != nil || rows == nil || len(rows) > 1 ||
		(len(rows) == 1 && !strings.EqualFold(rows[0].Hash, hash)) {
		return nil, errors.New("qB 未确认哈希查询结果")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// tjuptFeedAddAcknowledged 校验新版 JSON 添加回执，兼容旧版固定文本回执。
func tjuptFeedAddAcknowledged(body []byte, hash string) bool {
	if strings.TrimSpace(string(body)) == "Ok." {
		return true
	}
	var result struct {
		SuccessCount    int      `json:"success_count"`
		FailureCount    *int     `json:"failure_count"`
		PendingCount    *int     `json:"pending_count"`
		AddedTorrentIDs []string `json:"added_torrent_ids"`
	}
	return json.Unmarshal(body, &result) == nil && result.SuccessCount == 1 && result.FailureCount != nil &&
		*result.FailureCount == 0 && result.PendingCount != nil && *result.PendingCount == 0 &&
		len(result.AddedTorrentIDs) == 1 && strings.EqualFold(result.AddedTorrentIDs[0], hash)
}

func (torrent TaggedQBTorrent) matchesTjuptFeedScope(category string, tags []string, addedOn int64) bool {
	if torrent.AddedOn <= 0 || (addedOn != 0 && torrent.AddedOn != addedOn) || torrent.Category != category {
		return false
	}
	actualTags := compactStrings(strings.Split(torrent.Tags, ","))
	for _, tag := range tags {
		if !slices.Contains(actualTags, tag) {
			return false
		}
	}
	return true
}

// confirmTjuptFeedTorrent 允许 qB 短时间同步新增状态，仅重复读取，不重新发送写请求。
func (client *qbClient) confirmTjuptFeedTorrent(ctx context.Context, hash, category string, tags []string, addedOn int64, paused bool) (*TaggedQBTorrent, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		torrent, err := client.tjuptFeedTorrent(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTjuptFeedUnconfirmed, err)
		}
		if torrent != nil {
			if !torrent.matchesTjuptFeedScope(category, tags, addedOn) {
				return nil, fmt.Errorf("%w: qB 返回的种子标签、分类或添加时间不符", ErrTjuptFeedUnconfirmed)
			}
			if (paused && torrent.TjuptFeedStopped()) || (!paused && torrent.TjuptFeedStarted()) {
				return torrent, nil
			}
			if paused && torrent.TjuptFeedStarted() {
				return nil, fmt.Errorf("%w: qB 没有以停止状态添加试运行种子", ErrTjuptFeedUnconfirmed)
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: qB 未返回预期的种子状态", ErrTjuptFeedUnconfirmed)
		case <-ticker.C:
		}
	}
}
