package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const automationPreviewRecordID = 1

// AutomationPreviewRecord 保存唯一一份网页预览结果和内部请求顺序。
type AutomationPreviewRecord struct {
	ID          uint   `gorm:"primaryKey;autoIncrement:false"`
	StartedAt   int64  `gorm:"not null"`
	PreviewedAt int64  `gorm:"not null"`
	JSON        string `gorm:"not null"`
}

// LatestPreview 读取数据库中最近成功的网页预览，尚未预览时返回 nil。
func (r *AutomationRepository) LatestPreview() (*AutomationPreviewRecord, error) {
	var row AutomationPreviewRecord
	err := r.db.First(&row, automationPreviewRecordID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SavePreview 原子写入成功快照，请求较早时保留当前快照并返回 false。
// 只影响预览单行，判断在数据库内完成，不依赖不同数据库的冲突更新语法。
func (r *AutomationRepository) SavePreview(startedAt, previewedAt int64, snapshot string) (bool, error) {
	var saved bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&AutomationPreviewRecord{ID: automationPreviewRecordID}).Error; err != nil {
			return err
		}
		result := tx.Model(&AutomationPreviewRecord{}).
			Where("id = ? AND started_at < ?", automationPreviewRecordID, startedAt).
			UpdateColumns(map[string]any{"started_at": startedAt, "previewed_at": previewedAt, "json": snapshot})
		saved = result.RowsAffected == 1
		return result.Error
	})
	return saved && err == nil, err
}
