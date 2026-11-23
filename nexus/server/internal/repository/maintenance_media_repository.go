package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errInvalidMaintenanceMediaUpdate = errors.New("无效的维护媒体更新字段")

// FindMaintenanceMediaDownloader 返回该哈希在列表选中下载器上的有效缓存归属。
// 未指定下载器时匹配最新记录。无匹配时返回错误，不能直接采用请求中的下载器。
func (r *MigrateRepository) FindMaintenanceMediaDownloader(hash, selectedID string) (string, error) {
	query := r.store.DB.Table("torrents").Where("hash = ? AND (is_hidden = 0 OR is_hidden IS NULL) AND COALESCE(state, '') <> ?", hash, "不存在")
	if selectedID != "" {
		query = query.Where("downloader_id = ?", selectedID)
	}
	var rows []string
	if err := query.Order("last_seen DESC, downloader_id ASC").Limit(1).Pluck("downloader_id", &rows).Error; err != nil {
		return "", err
	}
	if len(rows) != 1 || strings.TrimSpace(rows[0]) == "" {
		return "", errors.New("下载器缓存中没有这条种子")
	}
	return strings.TrimSpace(rows[0]), nil
}

// UpdateMaintenanceMedia 在目标媒体字段及其审核状态仍与读取时一致时，保存缺失资料。
// 参数/返回：hash、种子 ID、站点定位记录；expected 是采集前字段快照；updates 是单类媒体字段；成功匹配返回 true。
// 失败场景：字段组合无效返回错误；行不存在或内容已被人工修改时返回 false。
// 副作用：只更新截图或 MediaInfo 字段、相应状态与更新时间，不创建记录。
func (r *MigrateRepository) UpdateMaintenanceMedia(hash, torrentID, siteName string, expected, updates map[string]any) (bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil || hash == "" || torrentID == "" || siteName == "" {
		return false, errInvalidMaintenanceMediaUpdate
	}

	keys, err := maintenanceMediaCASFields(expected, updates)
	if err != nil {
		return false, err
	}

	query := r.store.DB.Table("seed_parameters").
		Where("hash = ? AND torrent_id = ? AND site_name = ?", hash, torrentID, siteName)
	for _, key := range keys {
		value := expected[key]
		if key == "bdinfo_task_id" {
			if value == nil {
				query = query.Where("bdinfo_task_id IS NULL")
			} else {
				query = query.Where("bdinfo_task_id = ?", value)
			}
			continue
		}
		if value == nil {
			value = ""
		}
		query = query.Where(fmt.Sprintf("COALESCE(%s, '') = ?", key), value)
	}

	values := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["updated_at"] = time.Now().Format("2006-01-02 15:04:05")
	result := query.Updates(values)
	return result.RowsAffected == 1, result.Error
}

func maintenanceMediaCASFields(expected, updates map[string]any) ([]string, error) {
	screenshotUpdate := len(updates) == 2 && hasKeys(updates, "screenshots", "screenshot_review_status")
	mediainfoUpdate := len(updates) == 2 && hasKeys(updates, "mediainfo", "mediainfo_status")
	if screenshotUpdate && hasKeys(expected, "screenshots", "screenshot_review_status") {
		if hasOnlyKeys(expected, "screenshots", "screenshot_review_status") {
			return []string{"screenshots", "screenshot_review_status"}, nil
		}
	}
	if mediainfoUpdate && hasKeys(expected, "mediainfo", "mediainfo_status", "bdinfo_task_id") {
		base := []string{"mediainfo", "mediainfo_status", "bdinfo_task_id"}
		hasDescription := hasKeys(expected, "statement", "body")
		if hasDescription {
			base = append(base, "statement", "body")
		}
		if hasOnlyKeys(expected, base...) {
			return base, nil
		}
	}
	return nil, errInvalidMaintenanceMediaUpdate
}

func hasKeys(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func hasOnlyKeys(values map[string]any, allowed ...string) bool {
	if len(values) != len(allowed) {
		return false
	}
	return hasKeys(values, allowed...)
}
