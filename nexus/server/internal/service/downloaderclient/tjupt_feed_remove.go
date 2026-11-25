package downloaderclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
)

// TjuptFeedSeeding 确认任务已下完并在做种，包含未连接下载者的做种状态。
func (torrent TaggedQBTorrent) TjuptFeedSeeding() bool {
	if torrent.Progress == nil || *torrent.Progress != 1 {
		return false
	}
	switch torrent.State {
	case "uploading", "stalledUP", "forcedUP":
		return true
	default:
		return false
	}
}

// TjuptFeedNorthYangYuanTrackers 读取指定哈希的完整 Tracker 列表，核对是否仅有北洋园 Tracker。
func (d Downloader) TjuptFeedNorthYangYuanTrackers(ctx context.Context, hash string) (bool, error) {
	hash, valid := normalizeTjuptFeedHash(hash)
	if !valid {
		return false, ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return false, err
	}
	defer client.client.CloseIdleConnections()
	return client.tjuptFeedNorthYangYuanTrackers(ctx, hash)
}

// RemoveTjuptFeedTorrent 只移除身份与实时状态均符合条件的一条 qB 任务，保留数据文件。
// 写入只发送一次，仅在后续精确查询确认任务已不存在时返回成功。
func (d Downloader) RemoveTjuptFeedTorrent(ctx context.Context, hash string, addedOn int64, marker string, uploadThresholdBytes int64) error {
	hash, valid := normalizeTjuptFeedHash(hash)
	if !valid || addedOn <= 0 || !validTjuptFeedMarker(marker) || uploadThresholdBytes <= 0 {
		return ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	defer client.client.CloseIdleConnections()

	// 先核对全部 Tracker，再获取尽量靠近写入时刻的状态和上传速率。
	trackersMatch, err := client.tjuptFeedNorthYangYuanTrackers(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	if !trackersMatch {
		return ErrTjuptFeedScope
	}
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	if torrent == nil || !torrent.tjuptFeedOwned(addedOn, marker) || !torrent.TjuptFeedSeeding() ||
		torrent.UploadBPS == nil || *torrent.UploadBPS < 0 || *torrent.UploadBPS >= uploadThresholdBytes ||
		torrent.Uploaded == nil || *torrent.Uploaded < 0 ||
		(torrent.Tracker != "" && !tjuptFeedTrackerURL(torrent.Tracker)) {
		return ErrTjuptFeedScope
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return err
	}
	form := url.Values{"hashes": {hash}, "deleteFiles": {"false"}}
	reply, err := client.pluginRequest(ctx, http.MethodPost, "torrents/delete", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if result := strings.TrimSpace(string(reply)); result != "" && result != "Ok." {
		return fmt.Errorf("%w：没有收到 qB 任务移除回执", ErrTjuptFeedUnconfirmed)
	}
	return client.confirmTjuptFeedRemoved(ctx, hash, addedOn, marker)
}

// tjuptFeedOwned 同时验证加入时间、插件标签和单任务归属标记。
func (torrent TaggedQBTorrent) tjuptFeedOwned(addedOn int64, marker string) bool {
	tags := compactStrings(strings.Split(torrent.Tags, ","))
	return torrent.AddedOn == addedOn &&
		slices.Contains(tags, tjuptFeedScopeTag) && slices.Contains(tags, marker)
}

// tjuptFeedNorthYangYuanTrackers 只忽略 qB 明确标记为关闭的内置发现项。
func (client *qbClient) tjuptFeedNorthYangYuanTrackers(ctx context.Context, hash string) (bool, error) {
	params := url.Values{"hash": {hash}}
	body, err := client.pluginRequest(ctx, http.MethodGet, "torrents/trackers?"+params.Encode(), "", nil)
	if err != nil {
		return false, err
	}
	var trackers []struct {
		URL    string `json:"url"`
		Tier   *int   `json:"tier"`
		Status *int   `json:"status"`
	}
	if json.Unmarshal(body, &trackers) != nil || trackers == nil {
		return false, errors.New("qB Tracker 列表无法确认")
	}
	found := false
	for _, tracker := range trackers {
		switch tracker.URL {
		case "** [DHT] **", "** [PeX] **", "** [LSD] **":
			if tracker.Tier == nil || *tracker.Tier != -1 || tracker.Status == nil || *tracker.Status != 0 {
				return false, nil
			}
		default:
			if !tjuptFeedTrackerURL(tracker.URL) {
				return false, nil
			}
			found = true
		}
	}
	return found, nil
}

// tjuptFeedTrackerURL 使用北洋园 RSS 接受的协议和域名范围识别真实 Tracker。
func tjuptFeedTrackerURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Opaque == "" &&
		u.Fragment == "" && !strings.HasPrefix(u.Hostname(), ".") && tjupt.TrackerHost(u.Hostname())
}

// confirmTjuptFeedRemoved 仅轮询读取精确哈希，遇到同哈希的新实例时交由上层核对。
func (client *qbClient) confirmTjuptFeedRemoved(ctx context.Context, hash string, addedOn int64, marker string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		torrent, err := client.tjuptFeedTorrent(ctx, hash)
		if err != nil {
			return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
		}
		if torrent == nil {
			return nil
		}
		if !torrent.tjuptFeedOwned(addedOn, marker) {
			return fmt.Errorf("%w：qB 返回的任务身份已变化", ErrTjuptFeedUnconfirmed)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w：qB 仍返回该任务", ErrTjuptFeedUnconfirmed)
		case <-ticker.C:
		}
	}
}
