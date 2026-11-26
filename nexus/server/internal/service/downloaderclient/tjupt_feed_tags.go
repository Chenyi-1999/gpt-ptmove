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
)

// EnsureTjuptFeedDisplayTag 将已确认的唯一北洋园任务切换为统一显示标签。
func (d Downloader) EnsureTjuptFeedDisplayTag(ctx context.Context, hash string, addedOn int64, marker string) error {
	hash, ok := normalizeTjuptFeedHash(hash)
	if !ok || addedOn <= 0 || !validTjuptFeedMarker(marker) {
		return ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	defer client.client.CloseIdleConnections()
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	tags := []string(nil)
	if torrent == nil || torrent.AddedOn != addedOn {
		return ErrTjuptFeedScope
	}
	tags = compactStrings(strings.Split(torrent.Tags, ","))
	if !slices.Contains(tags, marker) {
		return ErrTjuptFeedScope
	}
	if slices.Contains(tags, TjuptFeedDisplayTag) {
		return nil
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return err
	}
	form := url.Values{"hashes": {hash}, "tags": {TjuptFeedDisplayTag}}
	reply, err := client.pluginRequest(ctx, http.MethodPost, "torrents/addTags", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if result := strings.TrimSpace(string(reply)); result != "" && result != "Ok." {
		return fmt.Errorf("%w：qB 未确认标签更新", ErrTjuptFeedUnconfirmed)
	}
	return client.confirmTjuptFeedTags(ctx, hash, addedOn, marker, TjuptFeedDisplayTag, true)
}

// RemoveTjuptFeedLegacyTags 清理准确匹配任务上的临时标记与旧分类标签。
func (d Downloader) RemoveTjuptFeedLegacyTags(ctx context.Context, hash string, addedOn int64, marker string) error {
	hash, ok := normalizeTjuptFeedHash(hash)
	if !ok || addedOn <= 0 || !validTjuptFeedMarker(marker) {
		return ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	defer client.client.CloseIdleConnections()
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	if torrent == nil || torrent.AddedOn != addedOn {
		return ErrTjuptFeedScope
	}
	actual := compactStrings(strings.Split(torrent.Tags, ","))
	if !slices.Contains(actual, TjuptFeedDisplayTag) {
		return ErrTjuptFeedScope
	}
	remove := make([]string, 0, 2)
	for _, tag := range []string{marker, tjuptFeedScopeTag} {
		if slices.Contains(actual, tag) {
			remove = append(remove, tag)
		}
	}
	if len(remove) == 0 {
		return nil
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return err
	}
	form := url.Values{"hashes": {hash}, "tags": {strings.Join(remove, ",")}}
	reply, err := client.pluginRequest(ctx, http.MethodPost, "torrents/removeTags", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if result := strings.TrimSpace(string(reply)); result != "" && result != "Ok." {
		return fmt.Errorf("%w：qB 未确认标签清理", ErrTjuptFeedUnconfirmed)
	}
	return client.confirmTjuptFeedRemoval(ctx, hash, addedOn, marker)
}

// PruneEmptyTjuptFeedMarker 仅删除经 qB 标签过滤确认无种子的旧任务标签。
func (d Downloader) PruneEmptyTjuptFeedMarker(ctx context.Context, marker string) error {
	if !validTjuptFeedMarker(marker) {
		return ErrTjuptFeedScope
	}
	client, err := d.newPluginQB()
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	defer client.client.CloseIdleConnections()
	rows, err := client.tjuptFeedTorrentRows(ctx, url.Values{"tag": {marker}}, tjuptFeedHashBatchSize)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	if len(rows) != 0 {
		return ErrTjuptFeedScope
	}
	knownTags, err := client.tjuptFeedTags(ctx)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedScope, err)
	}
	if !slices.Contains(knownTags, marker) {
		return nil
	}
	if err := checkTjuptFeedEffect(ctx, d.effectGuard); err != nil {
		return err
	}
	form := url.Values{"tags": {marker}}
	reply, err := client.pluginRequest(ctx, http.MethodPost, "torrents/deleteTags", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if result := strings.TrimSpace(string(reply)); result != "" && result != "Ok." {
		return fmt.Errorf("%w：qB 未确认空标签清理", ErrTjuptFeedUnconfirmed)
	}
	knownTags, err = client.tjuptFeedTags(ctx)
	if err != nil || slices.Contains(knownTags, marker) {
		return ErrTjuptFeedUnconfirmed
	}
	return nil
}

func (client *qbClient) tjuptFeedTags(ctx context.Context) ([]string, error) {
	body, err := client.pluginRequest(ctx, http.MethodGet, "torrents/tags", "", nil)
	if err != nil {
		return nil, err
	}
	var tags []string
	if json.Unmarshal(body, &tags) != nil || tags == nil {
		return nil, errors.New("qB 未确认标签列表")
	}
	return tags, nil
}

func (client *qbClient) confirmTjuptFeedTags(ctx context.Context, hash string, addedOn int64, marker, tag string, present bool) error {
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if torrent == nil || torrent.AddedOn != addedOn {
		return ErrTjuptFeedUnconfirmed
	}
	tags := compactStrings(strings.Split(torrent.Tags, ","))
	if slices.Contains(tags, tag) != present {
		return ErrTjuptFeedUnconfirmed
	}
	if marker != TjuptFeedDisplayTag && slices.Contains(tags, marker) != (tag != marker) {
		return ErrTjuptFeedUnconfirmed
	}
	return nil
}

func (client *qbClient) confirmTjuptFeedRemoval(ctx context.Context, hash string, addedOn int64, marker string) error {
	torrent, err := client.tjuptFeedTorrent(ctx, hash)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrTjuptFeedUnconfirmed, err)
	}
	if torrent == nil || torrent.AddedOn != addedOn {
		return ErrTjuptFeedUnconfirmed
	}
	tags := compactStrings(strings.Split(torrent.Tags, ","))
	if !slices.Contains(tags, TjuptFeedDisplayTag) || slices.Contains(tags, marker) || slices.Contains(tags, tjuptFeedScopeTag) {
		return ErrTjuptFeedUnconfirmed
	}
	return nil
}
