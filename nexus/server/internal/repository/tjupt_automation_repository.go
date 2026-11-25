package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TjuptAutomationControl 保存自动化设置、调度租约和最近执行结果。
type TjuptAutomationControl struct {
	ID              int `gorm:"primaryKey;autoIncrement:false"`
	Policy          string
	Enabled         bool
	IntervalMinutes int
	Lease           string
	LeaseUntil      int64
	Fence           int64
	LastRun         int64
	LastMessage     string
}

// TjuptAutomationTask 仅追踪本插件登记的种子、磁盘预留和低速观察。
type TjuptAutomationTask struct {
	Hash         string `gorm:"primaryKey;size:40"`
	SourceID     string `gorm:"uniqueIndex;size:24;not null"`
	DownloaderID string
	SaveRoot     string
	Title        string
	SizeBytes    int64
	AddedOn      int64
	State        string `gorm:"index"`
	PolicyStamp  string
	LowSince     int64
	LastSample   int64
	LastUploaded int64
	UpdatedAt    time.Time
}

// ErrTjuptAutomationBusy 表示设置更新与执行租约冲突。
var ErrTjuptAutomationBusy = errors.New("北洋园正在执行本轮任务，请稍后保存设置")

// ErrTjuptAutomationScope 表示仍在管理的种子与新下载器或目录不一致。
var ErrTjuptAutomationScope = errors.New("仍有占用额度的任务，请恢复原飞牛下载器与保存目录")

// ErrTjuptAutomationLeaseLost 表示原执行者的租约已失效。
var ErrTjuptAutomationLeaseLost = errors.New("北洋园执行租约已失效")

// TjuptAutomationRepository 管理北洋园自动投种和清理的持久状态。
type TjuptAutomationRepository struct{ db *gorm.DB }

// NewTjuptAutomationRepository 初始化持久状态。已有配置不会被默认值替换。
func NewTjuptAutomationRepository(db *gorm.DB, defaultPolicy string, interval int) (*TjuptAutomationRepository, error) {
	if err := db.AutoMigrate(&TjuptAutomationControl{}, &TjuptAutomationTask{}); err != nil {
		return nil, err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&TjuptAutomationControl{
		ID: 1, Policy: defaultPolicy, IntervalMinutes: interval,
	}).Error; err != nil {
		return nil, err
	}
	return &TjuptAutomationRepository{db: db}, nil
}

// Control 读取当前设置和调度状态。
func (r *TjuptAutomationRepository) Control(ctx context.Context) (TjuptAutomationControl, error) {
	var row TjuptAutomationControl
	err := r.db.WithContext(ctx).Take(&row, 1).Error
	return row, err
}

// SaveControl 在无执行租约时更新策略。执行中的规则保持一致。
func (r *TjuptAutomationRepository) SaveControl(ctx context.Context, policy string, enabled bool, interval int, now int64, downloaderID, saveRoot string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&TjuptAutomationControl{}).Where("id = ? AND lease_until <= ?", 1, now).
			UpdateColumn("fence", gorm.Expr("fence + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTjuptAutomationBusy
		}
		if enabled {
			var count int64
			if err := tx.Model(&TjuptAutomationTask{}).
				Where("state <> ? AND (downloader_id <> ? OR save_root <> ?)", "deleted", downloaderID, saveRoot).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrTjuptAutomationScope
			}
		}
		return tx.Model(&TjuptAutomationControl{}).Where("id = ?", 1).
			Updates(map[string]any{"policy": policy, "enabled": enabled, "interval_minutes": interval, "last_run": 0}).Error
	})
}

// Claim 原子领取当前策略的到期执行权，过期旧租约无法控制新的一轮。
func (r *TjuptAutomationRepository) Claim(ctx context.Context, policy, token string, now, until int64) (bool, error) {
	result := r.db.WithContext(ctx).Model(&TjuptAutomationControl{}).
		Where("id = ? AND enabled = ? AND policy = ? AND lease_until <= ? AND (last_run = 0 OR last_run <= ? - interval_minutes * 60)", 1, true, policy, now, now).
		Updates(map[string]any{"lease": token, "lease_until": until, "last_run": now})
	return result.RowsAffected == 1, result.Error
}

// VerifyLease 核对执行租约仍属于当前运行者，校验失败不发送外部写操作。
func (r *TjuptAutomationRepository) VerifyLease(ctx context.Context, token string, now int64) error {
	_, err := r.LeaseUntil(ctx, token, now)
	return err
}

// LeaseUntil 返回有效租约的最晚截止时间，供外部写请求限定自身授权时效。
func (r *TjuptAutomationRepository) LeaseUntil(ctx context.Context, token string, now int64) (int64, error) {
	var control TjuptAutomationControl
	err := r.db.WithContext(ctx).Select("lease_until").
		Where("id = ? AND lease = ? AND lease_until > ?", 1, token, now).Take(&control).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrTjuptAutomationLeaseLost
	}
	return control.LeaseUntil, err
}

// Finish 释放自己的租约并更新运行结果。
func (r *TjuptAutomationRepository) Finish(ctx context.Context, token, message string) error {
	return r.db.WithContext(ctx).Model(&TjuptAutomationControl{}).Where("id = ? AND lease = ?", 1, token).
		Updates(map[string]any{"lease": "", "lease_until": 0, "last_message": message}).Error
}

// HoldUntilExpiry 记录外部请求结果不明确，并保留租约直到已领单的操作无法继续执行。
func (r *TjuptAutomationRepository) HoldUntilExpiry(ctx context.Context, token, message string) error {
	return r.db.WithContext(ctx).Model(&TjuptAutomationControl{}).Where("id = ? AND lease = ?", 1, token).
		Update("last_message", message).Error
}

// LiveTasks 返回仍占用额度的已登记任务，包括待确认的投种和删除。
func (r *TjuptAutomationRepository) LiveTasks(ctx context.Context) ([]TjuptAutomationTask, error) {
	items := make([]TjuptAutomationTask, 0)
	err := r.db.WithContext(ctx).Where("state <> ?", "deleted").Order("updated_at DESC").Find(&items).Error
	return items, err
}

// SeenSource 查询该站点种子标识是否曾被插件登记。
func (r *TjuptAutomationRepository) SeenSource(ctx context.Context, id string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&TjuptAutomationTask{}).Where("source_id = ?", id).Count(&count).Error
	return count > 0, err
}

// Reserve 在同一事务锁定调度行、核对总量与并发上限，再登记唯一候选。
func (r *TjuptAutomationRepository) Reserve(ctx context.Context, task *TjuptAutomationTask, token string, now int64, totalBytes int64, activeLimit int) (bool, error) {
	inserted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTjuptControl(tx, token, now); err != nil {
			return err
		}
		var occupied struct {
			Count int64
			Bytes int64
		}
		if err := tx.Model(&TjuptAutomationTask{}).Where("state <> ?", "deleted").
			Select("COUNT(*) AS count, COALESCE(SUM(size_bytes), 0) AS bytes").Scan(&occupied).Error; err != nil {
			return err
		}
		if occupied.Count >= int64(activeLimit) || task.SizeBytes > totalBytes-occupied.Bytes {
			return nil
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(task)
		inserted = result.RowsAffected == 1
		return result.Error
	})
	return inserted, err
}

// UpdateTask 保存当前租约内的任务状态和观察窗口。
func (r *TjuptAutomationRepository) UpdateTask(ctx context.Context, task TjuptAutomationTask, token string, now int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTjuptControl(tx, token, now); err != nil {
			return err
		}
		task.UpdatedAt = time.Unix(now, 0)
		return tx.Model(&TjuptAutomationTask{}).Where("hash = ?", task.Hash).
			Select("AddedOn", "State", "PolicyStamp", "LowSince", "LastSample", "LastUploaded", "UpdatedAt").Updates(&task).Error
	})
}

// ReleaseUnappliedAdd 只撤销有明确写前拒绝回执的投种预留。
func (r *TjuptAutomationRepository) ReleaseUnappliedAdd(ctx context.Context, hash, token string, now int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTjuptControl(tx, token, now); err != nil {
			return err
		}
		return tx.Where("hash = ? AND state = ? AND added_on = ?", hash, "adding", 0).Delete(&TjuptAutomationTask{}).Error
	})
}

func lockTjuptControl(tx *gorm.DB, token string, now int64) error {
	result := tx.Model(&TjuptAutomationControl{}).Where("id = ? AND lease = ? AND lease_until > ?", 1, token, now).
		UpdateColumn("fence", gorm.Expr("fence + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTjuptAutomationLeaseLost
	}
	return nil
}
