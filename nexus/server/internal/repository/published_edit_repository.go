package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PublishedEditAttempt 记录一次已确认的外站编辑请求与提交前后的资料。
type PublishedEditAttempt struct {
	ID           uint64 `gorm:"primaryKey"`
	PublishLogID uint64
	TargetID     string `gorm:"size:32;uniqueIndex:idx_published_edit_target_revision,priority:1"`
	Revision     string `gorm:"size:64;uniqueIndex:idx_published_edit_target_revision,priority:2"`
	Status       string `gorm:"size:16"`
	BeforeJSON   string `gorm:"type:text"`
	AfterJSON    string `gorm:"type:text"`
	CreatedAt    int64
	UpdatedAt    int64
}

// PublishedEditRepository 持久化提交记录，限制同一目标和版本最多发送一次。
type PublishedEditRepository struct {
	db *gorm.DB
}

// NewPublishedEditRepository 创建编辑记录表并返回仓储。
func NewPublishedEditRepository(store *Store) (*PublishedEditRepository, error) {
	if store == nil || store.DB == nil {
		return nil, errors.New("published edit store unavailable")
	}
	if err := store.DB.AutoMigrate(&PublishedEditAttempt{}); err != nil {
		return nil, err
	}
	return &PublishedEditRepository{db: store.DB}, nil
}

// Reserve 先持久化请求及差异，再允许发送外站 POST。冲突只返回未获取。
func (r *PublishedEditRepository) Reserve(ctx context.Context, attempt *PublishedEditAttempt) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(attempt)
	return result.RowsAffected == 1, result.Error
}

// Complete 更新请求的回读结果。
func (r *PublishedEditRepository) Complete(ctx context.Context, id uint64, status string) error {
	result := r.db.WithContext(ctx).Model(&PublishedEditAttempt{}).Where("id = ? AND status = ?", id, "pending").Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("published edit attempt not pending")
	}
	return nil
}

// PublishedMediaHashes 按发布日志的源种子 ID 和站点查询去重的媒体哈希。
func (r *MigrateRepository) PublishedMediaHashes(torrentID, sourceSite string) ([]string, error) {
	var hashes []string
	err := r.store.DB.Table("seed_parameters").
		Where("torrent_id = ? AND (LOWER(site_name) = ? OR LOWER(nickname) = ?)", torrentID, sourceSite, sourceSite).
		Distinct("hash").Limit(2).Pluck("hash", &hashes).Error
	return hashes, err
}
