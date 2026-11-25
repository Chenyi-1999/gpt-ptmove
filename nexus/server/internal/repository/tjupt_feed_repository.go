package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TjuptFeedControl 保存直连 RSS 的设置、加密凭据与执行租约。
type TjuptFeedControl struct {
	ID                      int    `gorm:"primaryKey;autoIncrement:false"`
	Policy                  string `gorm:"type:text"`
	RSS                     []byte `json:"-"`
	HNR                     []byte `json:"-"`
	Enabled                 bool
	UseSiteUploadLimit      bool  `gorm:"not null;default:false"`
	SiteUploadLimitRevision int64 `gorm:"not null;default:0"`
	IntervalMinutes         int
	Revision                int64
	Lease                   string
	LeaseUntil              int64
	Fence                   int64
	LastRun                 int64
	LastCleanup             int64 `gorm:"not null;default:0"`
	LastMessage             string
}

// TjuptFeedTask 保存投种前的身份、容量及投种后的回执。归档任务仍参与去重。
type TjuptFeedTask struct {
	Hash       string `gorm:"primaryKey;size:40"`
	SourceID   string `gorm:"uniqueIndex;size:24;not null"`
	Scope      string `gorm:"index:idx_tjupt_feed_scope_state;size:64"`
	State      string `gorm:"index:idx_tjupt_feed_scope_state;size:24"`
	Marker     string `gorm:"size:64"`
	TrialScope string `gorm:"size:64"`
	SizeBytes  int64
	AddedOn    int64
	ReservedAt int64
	Cleanup    TjuptFeedCleanupObservation `gorm:"embedded;embeddedPrefix:cleanup_"`
}

// TjuptFeedCleanupObservation 保存低速采样与相隔缓存期的站点确认。
type TjuptFeedCleanupObservation struct {
	Scope       string `gorm:"size:64;not null;default:''"`
	RunAt       int64  `gorm:"not null;default:0"`
	SampledAt   int64  `gorm:"not null;default:0"`
	Uploaded    int64  `gorm:"not null;default:0"`
	LowSince    int64  `gorm:"not null;default:0"`
	CheckedAt   int64  `gorm:"not null;default:0"`
	ConfirmedAt int64  `gorm:"not null;default:0"`
	RecordID    int64  `gorm:"not null;default:0"`
}

const (
	// TjuptFeedAdding 表示已预留容量、尚未取得写入回执。
	TjuptFeedAdding = "adding"
	// TjuptFeedStopped 表示插件已确认试运行任务以停止状态添加。
	TjuptFeedStopped = "stopped"
	// TjuptFeedStarted 表示已确认添加或继续运行。
	TjuptFeedStarted = "started"
	// TjuptFeedResuming 表示已尝试继续运行，未确认时保留回执且不重试。
	TjuptFeedResuming = "resuming"
	// TjuptFeedRemoving 表示删除请求已预留或结果待核对，禁止自动重试。
	TjuptFeedRemoving = "removing"
	// TjuptFeedGone 表示原任务已不在下载器，释放容量并保留去重记录。
	TjuptFeedGone = "gone"
)

// ErrTjuptFeedLease 表示执行权或启停状态已变更。
var ErrTjuptFeedLease = errors.New("北洋园 RSS 执行权已变更")

// TjuptFeedRepository 管理直连 RSS 的独立设置与投种台账。
type TjuptFeedRepository struct {
	db            *gorm.DB
	legacyControl bool
	legacyTasks   bool
}

// NewTjuptFeedRepository 初始化存储，保留已存在的设置和试运行回执。
func NewTjuptFeedRepository(db *gorm.DB, policy string, interval int) (*TjuptFeedRepository, error) {
	if err := db.AutoMigrate(&TjuptFeedControl{}, &TjuptFeedTask{}); err != nil {
		return nil, err
	}
	row := TjuptFeedControl{ID: 1, Policy: policy, IntervalMinutes: interval}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	return &TjuptFeedRepository{
		db: db, legacyControl: db.Migrator().HasTable(&TjuptAutomationControl{}),
		legacyTasks: db.Migrator().HasTable(&TjuptAutomationTask{}),
	}, nil
}

// Control 读取插件保存的状态。
func (r *TjuptFeedRepository) Control(ctx context.Context) (TjuptFeedControl, error) {
	var row TjuptFeedControl
	err := r.db.WithContext(ctx).Take(&row, 1).Error
	return row, err
}

// SiteUploadLimit 只读取站点设置中的北洋园上传限速，缺失或重复记录均返回错误。
func (r *TjuptFeedRepository) SiteUploadLimit(ctx context.Context) (int, error) {
	var rows []struct{ SpeedLimit *int }
	err := r.db.WithContext(ctx).Table("sites").Select("speed_limit").
		Where("site = ?", "tjupt").Limit(2).Find(&rows).Error
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 || rows[0].SpeedLimit == nil || *rows[0].SpeedLimit < 0 {
		return 0, errors.New("北洋园站点上传限速不明确")
	}
	return *rows[0].SpeedLimit, nil
}

// SetSiteUploadLimit 独立更新上传限速开关，保留其他配置和退种观察版本。
func (r *TjuptFeedRepository) SetSiteUploadLimit(ctx context.Context, revision int64, enabled bool) (bool, error) {
	result := r.db.WithContext(ctx).Model(&TjuptFeedControl{}).
		Where("id = ? AND site_upload_limit_revision = ?", 1, revision).
		Updates(map[string]any{
			"use_site_upload_limit":      enabled,
			"site_upload_limit_revision": gorm.Expr("site_upload_limit_revision + 1"),
		})
	return result.RowsAffected == 1, result.Error
}

// Save 保存停止状态下的设置，读写之间发生启停或设置变更时拒绝覆盖。
func (r *TjuptFeedRepository) Save(ctx context.Context, row TjuptFeedControl, now int64) (bool, error) {
	result := r.db.WithContext(ctx).Model(&TjuptFeedControl{}).
		Where("id = ? AND revision = ? AND enabled = ? AND lease_until <= ?", 1, row.Revision, false, now).
		Updates(map[string]any{
			"policy": row.Policy, "rss": row.RSS, "hnr": row.HNR, "interval_minutes": row.IntervalMinutes,
			"revision": gorm.Expr("revision + 1"),
		})
	return result.RowsAffected == 1, result.Error
}

// SetEnabled 启用需要空闲租约。停止立即阻止后续写请求，即使本轮尚在收尾。
func (r *TjuptFeedRepository) SetEnabled(ctx context.Context, revision int64, enabled bool, now int64) (bool, error) {
	query := r.db.WithContext(ctx).Model(&TjuptFeedControl{}).Where("id = ?", 1)
	updates := map[string]any{"enabled": enabled, "revision": gorm.Expr("revision + 1")}
	if enabled {
		query = query.Where("revision = ? AND lease_until <= ?", revision, now)
		updates["last_run"] = int64(0)
		updates["last_cleanup"] = int64(0)
	}
	result := query.Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// Claim 在规则到期或手工试运行时领取租约，分别记录本轮实际处理的阶段。
func (r *TjuptFeedRepository) Claim(ctx context.Context, expected TjuptFeedControl, lease string, trial, fetchRSS, sampleCleanup bool, now, until int64) (bool, error) {
	query := r.db.WithContext(ctx).Model(&TjuptFeedControl{}).
		Where("id = ? AND revision = ? AND enabled = ? AND lease_until <= ?", 1, expected.Revision, !trial, now).
		Where("last_run = ? AND last_cleanup = ?", expected.LastRun, expected.LastCleanup)
	updates := map[string]any{"lease": lease, "lease_until": until}
	if fetchRSS {
		updates["last_run"] = now
	}
	if sampleCleanup {
		updates["last_cleanup"] = now
	}
	result := query.Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// Verify 在外部写请求发出前确认运行模式、租约以及投种时的限速开关版本。
func (r *TjuptFeedRepository) Verify(ctx context.Context, lease string, trial bool, now int64, uploadLimitRevision *int64) error {
	var count int64
	query := r.db.WithContext(ctx).Model(&TjuptFeedControl{}).
		Where("id = ? AND lease = ? AND lease_until > ? AND enabled = ?", 1, lease, now, !trial)
	if uploadLimitRevision != nil {
		query = query.Where("site_upload_limit_revision = ?", *uploadLimitRevision)
	}
	err := query.Count(&count).Error
	if err == nil && count != 1 {
		return ErrTjuptFeedLease
	}
	return err
}

// Finish 仅由当前租约持有人保存本轮摘要并释放执行权。
func (r *TjuptFeedRepository) Finish(ctx context.Context, lease, message string) error {
	return r.db.WithContext(ctx).Model(&TjuptFeedControl{}).Where("id = ? AND lease = ?", 1, lease).
		Updates(map[string]any{"lease": "", "lease_until": 0, "last_message": message}).Error
}

// LegacyControl 查询旧飞牛模式，仅用于继承默认设置及避免两个模式同时运行。
func (r *TjuptFeedRepository) LegacyControl(ctx context.Context) (TjuptAutomationControl, error) {
	if !r.legacyControl {
		return TjuptAutomationControl{}, nil
	}
	return readLegacyTjuptControl(ctx, r.db)
}

// ReadLegacyTjuptControl 在没有旧版台账的安装中返回未启用状态。
func ReadLegacyTjuptControl(ctx context.Context, db *gorm.DB) (TjuptAutomationControl, error) {
	if !db.Migrator().HasTable(&TjuptAutomationControl{}) {
		return TjuptAutomationControl{}, nil
	}
	return readLegacyTjuptControl(ctx, db)
}

func readLegacyTjuptControl(ctx context.Context, db *gorm.DB) (TjuptAutomationControl, error) {
	var row TjuptAutomationControl
	err := db.WithContext(ctx).Take(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return row, err
}

// LiveTasks 返回当前下载器内占用容量的登记任务，包括尚未确认的操作。
func (r *TjuptFeedRepository) LiveTasks(ctx context.Context, scope string) ([]TjuptFeedTask, error) {
	var rows []TjuptFeedTask
	err := r.db.WithContext(ctx).Where("scope = ? AND state <> ?", scope, TjuptFeedGone).
		Order("source_id").Find(&rows).Error
	return rows, err
}

// SeenSource 查询曾经登记的来源种子，并保留旧版台账的来源去重行为。
func (r *TjuptFeedRepository) SeenSource(ctx context.Context, id string) (bool, error) {
	var count int64
	db := r.db.WithContext(ctx)
	err := db.Model(&TjuptFeedTask{}).Where("source_id = ?", id).Count(&count).Error
	if err != nil || count > 0 || !r.legacyTasks {
		return count > 0, err
	}
	err = db.Model(&TjuptAutomationTask{}).Where("source_id = ?", id).Count(&count).Error
	return count > 0, err
}

// Reserve 在核对租约和容量后登记候选，来源 ID 和哈希均不能重复。任务数上限为零时只限制总容量。
func (r *TjuptFeedRepository) Reserve(ctx context.Context, lease string, now int64, task TjuptFeedTask, limitBytes int64, activeLimit int) (bool, error) {
	var inserted bool
	err := r.inLease(ctx, lease, now, func(tx *gorm.DB) error {
		if r.legacyTasks {
			var count int64
			err := tx.Model(&TjuptAutomationTask{}).Where("hash = ? OR source_id = ?", task.Hash, task.SourceID).Count(&count).Error
			if err != nil || count > 0 {
				return err
			}
		}
		var occupied struct{ Count, Bytes int64 }
		err := tx.Model(&TjuptFeedTask{}).Where("scope = ? AND state <> ?", task.Scope, TjuptFeedGone).
			Select("COUNT(*) AS count, COALESCE(SUM(size_bytes), 0) AS bytes").Scan(&occupied).Error
		if err != nil || (activeLimit != 0 && occupied.Count >= int64(activeLimit)) || task.SizeBytes > limitBytes-occupied.Bytes {
			return err
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&task)
		inserted = result.RowsAffected == 1
		return result.Error
	})
	return inserted, err
}

// UpdateTask 保存可验证状态，过期执行者不得改变台账。
func (r *TjuptFeedRepository) UpdateTask(ctx context.Context, lease string, now int64, task TjuptFeedTask) error {
	return r.inLease(ctx, lease, now, func(tx *gorm.DB) error {
		return tx.Model(&TjuptFeedTask{}).Where("hash = ?", task.Hash).
			Updates(map[string]any{
				"state": task.State, "added_on": task.AddedOn,
				"cleanup_scope": task.Cleanup.Scope, "cleanup_run_at": task.Cleanup.RunAt,
				"cleanup_sampled_at": task.Cleanup.SampledAt, "cleanup_uploaded": task.Cleanup.Uploaded,
				"cleanup_low_since": task.Cleanup.LowSince, "cleanup_checked_at": task.Cleanup.CheckedAt,
				"cleanup_confirmed_at": task.Cleanup.ConfirmedAt, "cleanup_record_id": task.Cleanup.RecordID,
			}).Error
	})
}

// ReleaseAdd 只释放已知没有发出投种写请求的容量预留。
func (r *TjuptFeedRepository) ReleaseAdd(ctx context.Context, lease string, now int64, hash string) error {
	return r.inLease(ctx, lease, now, func(tx *gorm.DB) error {
		return tx.Where("hash = ? AND state = ?", hash, TjuptFeedAdding).Delete(&TjuptFeedTask{}).Error
	})
}

func (r *TjuptFeedRepository) inLease(ctx context.Context, lease string, now int64, apply func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&TjuptFeedControl{}).Where("id = ? AND lease = ? AND lease_until > ?", 1, lease, now).
			UpdateColumn("fence", gorm.Expr("fence + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTjuptFeedLease
		}
		return apply(tx)
	})
}
