package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrAutomationPluginDisabled blocks new automated work while keeping existing evidence available.
var ErrAutomationPluginDisabled = errors.New("自动转种插件已停用")

const automationPluginID = 1

// AutomationPluginControl is persisted independently of the existing rule and scheduling settings.
type AutomationPluginControl struct {
	ID      uint `gorm:"primaryKey"`
	Enabled bool `gorm:"not null"`
}

func (r *AutomationRepository) initPluginControl() error {
	// Existing installations retain their rule behavior. Default rules and schedules still start off.
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&AutomationPluginControl{
		ID: automationPluginID, Enabled: true,
	}).Error
}

func (r *AutomationRepository) PluginEnabled() (bool, error) {
	return automationPluginEnabled(r.db)
}

func (r *AutomationRepository) SetPluginEnabled(enabled bool) error {
	return r.db.Model(&AutomationPluginControl{}).Where("id = ?", automationPluginID).Update("enabled", enabled).Error
}

func automationPluginEnabled(db *gorm.DB) (bool, error) {
	var control AutomationPluginControl
	if err := db.First(&control, automationPluginID).Error; err != nil {
		return false, err
	}
	return control.Enabled, nil
}

// lockAutomationPlugin serializes durable side-effect authorization with enable/disable.
// A caller must use a short database transaction and perform network requests after it commits.
func lockAutomationPlugin(tx *gorm.DB) error {
	enabled, err := automationPluginEnabled(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
	if err != nil {
		return err
	}
	if !enabled {
		return ErrAutomationPluginDisabled
	}
	return nil
}

func automationPluginEnabledQuery(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true}).Model(&AutomationPluginControl{}).
		Select("id").Where("id = ? AND enabled = ?", automationPluginID, true)
}

// These states already have a publish result or an explicit reconciliation request. The
// worker may verify evidence while stopped; it checks the plugin again before adding to qB.
var automationReconciliationStates = []string{"published", "seeding", "reconciling"}

// RestoreUnsentAutomationPublish is only for the worker whose upload guard rejected
// the first write. The task and its native queue must both still own the attempt.
func (r *AutomationRepository) RestoreUnsentAutomationPublish(queueID int64, beganAt, now time.Time) error {
	const message = "自动转种插件已停用，上传未发送，等待恢复"
	return r.db.Transaction(func(tx *gorm.DB) error {
		task := tx.Model(&AutomationTask{}).
			Where("queue_id = ? AND state = ? AND publish_attempted = ? AND updated_at = ?", queueID, "publishing", true, beganAt.UnixMilli()).
			Updates(map[string]any{"state": "queued_publish", "publish_attempted": false, "publish_month": "", "message": message, "updated_at": now.UnixMilli()})
		if task.Error != nil {
			return task.Error
		}
		if task.RowsAffected != 1 {
			return ErrAutomationFence
		}
		queue := tx.Model(&PublishQueueTask{}).
			Where("id = ? AND publish_trigger = ? AND status = ?", queueID, "automation", PublishQueueStatusRunning).
			Updates(map[string]any{"status": PublishQueueStatusQueued, "started_at": nil, "finished_at": nil, "next_run_at": nil,
				"last_error": message, "last_result": "", "updated_at": now.Format(PublishQueueTimeLayout)})
		if queue.Error != nil {
			return queue.Error
		}
		if queue.RowsAffected != 1 {
			return ErrAutomationFence
		}
		return nil
	})
}
