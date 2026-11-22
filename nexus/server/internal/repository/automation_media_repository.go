package repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertAutomationSeed 在自动任务租约内保存首份原生草稿；同主键的既有草稿保持原状。
func (r *MigrateRepository) InsertAutomationSeed(task AutomationTask, site string, record map[string]any, now time.Time) error {
	if record["hash"] != task.InfoHash || record["torrent_id"] != task.SourceID || record["site_name"] != site {
		return ErrAutomationFence
	}
	return r.withAutomationMediaTask(task, now, func(tx *gorm.DB) error {
		return tx.Table("seed_parameters").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "hash"}, {Name: "torrent_id"}, {Name: "site_name"}},
			DoNothing: true,
		}).Create(record).Error
	})
}

// AutomationMediaUpdate 定义客户端回填的单项资料及其读取时的原始字段。
type AutomationMediaUpdate struct {
	Tags, PreviousValue, PreviousStatus string
	Screenshots, MediaInfo              *string
	Description                         *PreparedMediaDescription
}

// UpdateAutomationSeedMedia 只在同一任务租约、载荷和字段仍有效时写入媒体；并发人工编辑保持原样。
func (r *MigrateRepository) UpdateAutomationSeedMedia(task AutomationTask, site string, input AutomationMediaUpdate, now time.Time) error {
	return r.withAutomationMediaTask(task, now, func(tx *gorm.DB) error {
		row := tx.Table("seed_parameters").
			Where("hash = ? AND torrent_id = ? AND site_name = ?", task.InfoHash, task.SourceID, site).
			Where("COALESCE(tags, '') = ?", input.Tags)
		values := map[string]any{"updated_at": now.Format("2006-01-02 15:04:05")}
		switch {
		case input.Screenshots != nil && input.MediaInfo == nil:
			row = row.Where("COALESCE(screenshots, '') = ? AND screenshot_review_status = ?", input.PreviousValue, input.PreviousStatus)
			values["screenshots"] = *input.Screenshots
		case input.MediaInfo != nil && input.Screenshots == nil:
			row = row.Where("COALESCE(mediainfo, '') = ? AND COALESCE(mediainfo_status, '') = ?", input.PreviousValue, input.PreviousStatus)
			if input.Description != nil {
				row = row.Where("COALESCE(statement, '') = ? AND COALESCE(body, '') = ?", input.Description.Statement, input.Description.Body)
			}
			values["mediainfo"], values["mediainfo_status"], values["bdinfo_error"] = *input.MediaInfo, "completed", ""
		default:
			return ErrAutomationFence
		}
		result := row.Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAutomationFence
		}
		return nil
	})
}

func (r *MigrateRepository) withAutomationMediaTask(task AutomationTask, now time.Time, work func(*gorm.DB) error) error {
	if task.LeaseToken == "" || (task.State != "preparing" && task.State != AutomationStateWaitingMedia) {
		return ErrAutomationFence
	}
	return r.store.DB.Transaction(func(tx *gorm.DB) error {
		var current AutomationTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND lease_token = ? AND lease_until > ? AND state = ? AND download_attempted = ? AND publish_attempted = ?",
				task.ID, task.LeaseToken, now.UnixMilli(), task.State, true, false).
			Take(&current).Error; err != nil {
			return err
		}
		if current.InfoHash != task.InfoHash || current.SourceID != task.SourceID || current.Source != task.Source ||
			current.DownloaderID != task.DownloaderID || current.SavePath != task.SavePath {
			return ErrAutomationFence
		}
		return work(tx)
	})
}
