package repository

import (
	"errors"

	"gorm.io/gorm"
)

// GetMaintenanceSeed 查找指定站点和 ID 的唯一维护记录。没有记录或标识冲突时返回错误。
func (r *MigrateRepository) GetMaintenanceSeed(torrentID, siteName, hash string) (map[string]any, error) {
	var rows []map[string]any
	query := r.store.DB.Table("seed_parameters").Where("site_name = ? AND torrent_id = ?", siteName, torrentID)
	if hash != "" {
		query = query.Where("LOWER(hash) = ?", hash)
	}
	err := query.Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if len(rows) != 1 {
		return nil, ErrAmbiguousMaintenanceSeed
	}
	return rows[0], nil
}

// ErrAmbiguousMaintenanceSeed 表示站点和种子 ID 对应多条维护记录。
var ErrAmbiguousMaintenanceSeed = errors.New("维护记录不唯一")

// UpdateMaintenanceSeed 仅更新原始记录。若抓取期间字段被修改或记录已删除，返回 false。
func (r *MigrateRepository) UpdateMaintenanceSeed(previous, updates map[string]any) (bool, error) {
	conditions := map[string]any{}
	for _, key := range []string{"hash", "torrent_id", "site_name", "updated_at", "bdinfo_task_id", "screenshot_review_status"} {
		conditions[key] = previous[key]
	}
	for key := range updates {
		conditions[key] = previous[key]
	}
	query := r.store.DB.Table("seed_parameters").Where(conditions)
	if len(updates) == 0 {
		var count int64
		err := query.Count(&count).Error
		return count == 1, err
	}
	result := query.Updates(updates)
	return result.RowsAffected == 1, result.Error
}
