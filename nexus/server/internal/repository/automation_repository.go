package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAutomationFence     = errors.New("自动任务租约或状态已改变")
	ErrAutomationQuota     = errors.New("本月次数或流量预算不足")
	ErrAutomationDuplicate = errors.New("该源种子已预留、已下载、已发布或结果待核对")
)

// AutomationSettingsRecord 保存模块设置；凭据仍由原生站点和下载器配置管理。
type AutomationSettingsRecord struct {
	ID        uint   `gorm:"primaryKey"`
	JSON      string `gorm:"type:text"`
	UpdatedAt int64
}

// AutomationRun 保存一次手动或月度扫描的规则快照与结果。
type AutomationRun struct {
	ID             string  `json:"id" gorm:"primaryKey;size:100"`
	ScheduleKey    *string `json:"schedule_key,omitempty" gorm:"uniqueIndex;size:100"`
	Month          string  `json:"month"`
	Status         string  `json:"status"`
	Snapshot       string  `json:"-" gorm:"type:text"`
	Message        string  `json:"message"`
	CreatedAt      int64   `json:"created_at"`
	FinishedAt     int64   `json:"finished_at"`
	Selected       int     `json:"selected"`
	CandidatesJSON string  `json:"-" gorm:"type:text"`
	Execute        bool    `json:"execute"`
	Explicit       bool    `json:"explicit"`
	Scheduled      bool    `json:"scheduled"`
	LeaseToken     string  `json:"-"`
	LeaseUntil     int64   `json:"-"`
}

// AutomationQuota 保存上海月份的保守预留账本；开始下载后不自动释放。
type AutomationQuota struct {
	Month string `json:"month" gorm:"primaryKey;size:7"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// AutomationTask 保存自动任务状态与冻结的非秘密站点快照。发布尝试后禁止再次上传。
type AutomationTask struct {
	ID                string `json:"id" gorm:"primaryKey;size:100"`
	RunID             string `json:"run_id"`
	Month             string `json:"month" gorm:"uniqueIndex:automation_route_month,priority:4;size:7"`
	Source            string `json:"source" gorm:"uniqueIndex:automation_route_month,priority:1;size:100"`
	Target            string `json:"target" gorm:"uniqueIndex:automation_route_month,priority:2;size:100"`
	SourceID          string `json:"source_id" gorm:"uniqueIndex:automation_route_month,priority:3;size:150"`
	Title             string `json:"title"`
	InfoHash          string `json:"info_hash" gorm:"index;size:40"`
	Size              int64  `json:"size"`
	Seeders           int    `json:"seeders"`
	Official          bool   `json:"official"`
	State             string `json:"state" gorm:"index;size:32"`
	Message           string `json:"message"`
	Snapshot          string `json:"-" gorm:"type:text"`
	DownloaderID      string `json:"downloader_id"`
	SavePath          string `json:"save_path"`
	TorrentPath       string `json:"-"`
	QueueID           *int64 `json:"queue_id,omitempty" gorm:"uniqueIndex"`
	PublishAttempted  bool   `json:"publish_attempted"`
	DownloadAttempted bool   `json:"download_attempted"`
	Explicit          bool   `json:"explicit"`
	TargetTorrentPath string `json:"-"`
	TargetHash        string `json:"target_hash"`
	ResultURL         string `json:"result_url"`
	ResultJSON        string `json:"-" gorm:"type:text"`
	LeaseToken        string `json:"-"`
	LeaseUntil        int64  `json:"-"`
	LastPolledAt      int64  `json:"-" gorm:"index"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// AutomationRepository 提供原子配额、租约和原生队列交接；不执行网络操作。
type AutomationRepository struct{ db *gorm.DB }

// NewAutomationRepository 初始化新增的自动化表；失败不启动工作线程。
func NewAutomationRepository(store *Store) (*AutomationRepository, error) {
	if store == nil || store.DB == nil {
		return nil, errors.New("自动化数据库未初始化")
	}
	r := &AutomationRepository{db: store.DB}
	if err := r.db.AutoMigrate(&AutomationSettingsRecord{}, &AutomationRun{}, &AutomationQuota{}, &AutomationTask{}); err != nil {
		return nil, err
	}
	return r, nil
}

// Settings 读取独立设置，未配置时返回空字符串。
func (r *AutomationRepository) Settings() (string, error) {
	var row AutomationSettingsRecord
	err := r.db.First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return row.JSON, err
}

// SaveSettings 原子保存设置，关闭模块时收回尚未产生副作用的执行授权。
func (r *AutomationRepository) SaveSettings(value string, disabled bool, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&AutomationSettingsRecord{ID: 1, JSON: value, UpdatedAt: now.UnixMilli()}).Error; err != nil {
			return err
		}
		if disabled {
			if err := tx.Model(&AutomationRun{}).Where("status IN ?", []string{"queued", "scanning"}).Updates(map[string]any{"execute": false, "explicit": false}).Error; err != nil {
				return err
			}
			return tx.Model(&AutomationTask{}).Where("state IN ?", []string{"reserved", "downloading", "preparing", "ready", "published", "seeding", "reconciling", "needs_review"}).Updates(map[string]any{"explicit": false}).Error
		}
		return nil
	})
}

// CreateRun 插入扫描记录，月度调度键保证重启后同月只扫描一次。
func (r *AutomationRepository) CreateRun(run *AutomationRun) error { return r.db.Create(run).Error }

// FinishRun 保存扫描实际结果，不记录凭据或原始站点正文。
func (r *AutomationRepository) FinishRun(id, status, message string, selected int, now time.Time) error {
	return r.db.Model(&AutomationRun{}).Where("id = ?", id).Updates(map[string]any{"status": status, "message": message, "selected": selected, "finished_at": now.UnixMilli()}).Error
}

// automationDownloadStates 包含已开始下载或已进入其后阶段的旧任务，兼容缺少下载标记的记录。
var automationDownloadStates = []string{
	"downloading", "preparing", "ready", "queued_publish", "publishing",
	"published", "seeding", "succeeded", "reconciling", "needs_review",
}

// automationSourceIdentity 按同源标识或规范化信息哈希匹配载荷，不受目标站或月份影响。
func automationSourceIdentity(db *gorm.DB, source, sourceID, hash string) *gorm.DB {
	query := db.Model(&AutomationTask{})
	if hash == "" {
		return query.Where("source = ? AND source_id = ?", source, sourceID)
	}
	return query.Where("((source = ? AND source_id = ?) OR LOWER(info_hash) = ?)", source, sourceID, strings.ToLower(hash))
}

// HasDownloadHistory 检查同一载荷的自动下载历史。重查已有任务时可排除任务自身。
// 无记录返回 false；查询失败交由上层拒绝执行。此查询不更改任务或配额。
func (r *AutomationRepository) HasDownloadHistory(source, sourceID, hash, exceptTaskID string) (bool, error) {
	query := automationSourceIdentity(r.db, source, sourceID, hash).
		Where("(download_attempted = ? OR publish_attempted = ? OR state IN ?)", true, true, automationDownloadStates)
	if exceptTaskID != "" {
		query = query.Where("id <> ?", exceptTaskID)
	}
	var existing int64
	err := query.Count(&existing).Error
	return existing > 0, err
}

// Reserve 在同一事务内创建任务并条件扣减月度次数与字节预算。
func (r *AutomationRepository) Reserve(task *AutomationTask, maxCount int, maxBytes int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if task.Size <= 0 || task.Size > maxBytes || maxCount <= 0 {
			return ErrAutomationQuota
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&AutomationQuota{Month: task.Month}).Error; err != nil {
			return err
		}
		updated := tx.Model(&AutomationQuota{}).Where("month = ? AND count < ? AND bytes <= ?", task.Month, maxCount, maxBytes-task.Size).Updates(map[string]any{"count": gorm.Expr("count + 1"), "bytes": gorm.Expr("bytes + ?", task.Size)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrAutomationQuota
		}
		// 在当月配额行串行化写入后重读，命中时回滚本次配额扣减。
		var duplicate AutomationTask
		err := automationSourceIdentity(tx, task.Source, task.SourceID, task.InfoHash).
			Where("(download_attempted = ? OR publish_attempted = ? OR state IN ? OR state = ? OR (source = ? AND target = ? AND month = ?))",
				true, true, automationDownloadStates, "reserved", task.Source, task.Target, task.Month).
			Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Take(&duplicate).Error
		if err == nil {
			return ErrAutomationDuplicate
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(task).Error
	})
}

// Tasks 返回最近任务及当前月份配额，不包含快照中的内部字段。
func (r *AutomationRepository) Tasks() ([]AutomationTask, error) {
	rows := []AutomationTask{}
	err := r.db.Order("created_at DESC").Limit(500).Find(&rows).Error
	return rows, err
}

// Runs 返回最近扫描历史。
func (r *AutomationRepository) Runs() ([]AutomationRun, error) {
	rows := []AutomationRun{}
	err := r.db.Order("created_at DESC").Limit(100).Find(&rows).Error
	return rows, err
}

// Quota 返回月份用量，空账本视为零。
func (r *AutomationRepository) Quota(month string) (AutomationQuota, error) {
	row := AutomationQuota{Month: month}
	err := r.db.Where("month = ?", month).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return row, err
}

// Task 按标识读取持久任务。
func (r *AutomationRepository) Task(id string) (AutomationTask, error) {
	var row AutomationTask
	err := r.db.First(&row, "id = ?", id).Error
	return row, err
}

// Claim 通过状态和过期时间比较交换领取任务；过期工作者无法推进状态。
func (r *AutomationRepository) Claim(id, token string, now time.Time) (AutomationTask, error) {
	result := r.db.Model(&AutomationTask{}).Where("id = ? AND state IN ? AND lease_until <= ?", id, []string{"reserved", "downloading", "preparing", "ready", "published", "seeding", "reconciling"}, now.UnixMilli()).Updates(map[string]any{"lease_token": token, "lease_until": now.Add(15 * time.Minute).UnixMilli()})
	if result.Error != nil {
		return AutomationTask{}, result.Error
	}
	if result.RowsAffected != 1 {
		return AutomationTask{}, ErrAutomationFence
	}
	return r.Task(id)
}

// Advance 匹配租约和预期状态后保存检查点，租约过期时拒绝写入。
func (r *AutomationRepository) Advance(id, token, expected string, values map[string]any, now time.Time) error {
	values["updated_at"] = now.UnixMilli()
	result := r.db.Model(&AutomationTask{}).Where("id = ? AND lease_token = ? AND lease_until > ? AND state = ?", id, token, now.UnixMilli(), expected).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// Release 仅释放自己持有的租约，允许下次轮询继续检查下载进度。
func (r *AutomationRepository) Release(id, token string) error {
	return r.db.Model(&AutomationTask{}).Where("id = ? AND lease_token = ?", id, token).Updates(map[string]any{"lease_token": "", "lease_until": 0}).Error
}

// Cancel 取消尚未开始上传的任务；仅完全未触碰的任务退还预留。
func (r *AutomationRepository) Cancel(id string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var row AutomationTask
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.PublishAttempted || row.State == "succeeded" {
			return errors.New("发布已尝试，必须先核对结果")
		}
		if row.State == "cancelled" {
			return nil
		}
		result := tx.Model(&AutomationTask{}).Where("id = ? AND state = ? AND publish_attempted = ?", id, row.State, false).Updates(map[string]any{"state": "cancelled", "message": "用户取消；已有下载保持原状", "lease_token": "", "lease_until": 0, "updated_at": now.UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAutomationFence
		}
		if row.QueueID != nil {
			if err := tx.Model(&PublishQueueTask{}).Where("id = ? AND status = ?", *row.QueueID, PublishQueueStatusQueued).Updates(map[string]any{"status": PublishQueueStatusCancelled}).Error; err != nil {
				return err
			}
		}
		if !row.DownloadAttempted && row.QueueID == nil {
			return tx.Model(&AutomationQuota{}).Where("month = ? AND count > 0 AND bytes >= ?", row.Month, row.Size).Updates(map[string]any{"count": gorm.Expr("count - 1"), "bytes": gorm.Expr("bytes - ?", row.Size)}).Error
		}
		return nil
	})
}

// Retry 仅恢复未尝试上传的失败任务，沿用原来的配额与下载副作用标记。
func (r *AutomationRepository) Retry(id string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var task AutomationTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", id).Error; err != nil {
			return err
		}
		if task.State != "failed" || task.PublishAttempted {
			return ErrAutomationFence
		}
		next := "reserved"
		if task.DownloadAttempted {
			next = "downloading"
		}
		if task.QueueID != nil {
			q := tx.Model(&PublishQueueTask{}).Where("id = ? AND status = ?", *task.QueueID, PublishQueueStatusFailed).Updates(map[string]any{"status": PublishQueueStatusQueued, "next_run_at": nil, "started_at": nil, "finished_at": nil})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrAutomationFence
			}
			next = "queued_publish"
		}
		result := tx.Model(&AutomationTask{}).Where("id = ? AND state = ? AND publish_attempted = ?", id, "failed", false).Updates(map[string]any{"state": next, "explicit": true, "message": "用户重试，沿用原有预留", "lease_token": "", "lease_until": 0, "updated_at": now.UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAutomationFence
		}
		return nil
	})
} // Enqueue 将原生队列行与任务唯一关联放在同一事务；交接失败全部回滚。
func (r *AutomationRepository) Enqueue(task AutomationTask, queue *PublishQueueTask, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var row AutomationTask
		if err := tx.First(&row, "id = ?", task.ID).Error; err != nil {
			return err
		}
		if row.QueueID != nil {
			return ErrAutomationFence
		}
		if err := tx.Create(queue).Error; err != nil {
			return err
		}
		result := tx.Model(&AutomationTask{}).Where("id = ? AND state = ? AND lease_token = ? AND lease_until > ? AND queue_id IS NULL AND publish_attempted = ?", task.ID, "ready", task.LeaseToken, now.UnixMilli(), false).Updates(map[string]any{"queue_id": queue.ID, "state": "queued_publish", "lease_token": "", "lease_until": 0, "updated_at": now.UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAutomationFence
		}
		return nil
	})
}

// BeginPublish 在调用原生发布前持久化尝试标记。失效配置、重复领取、取消均拒绝。
func (r *AutomationRepository) BeginPublish(queueID int64, now time.Time) error {
	return r.BeginPublishPrepared(queueID, now, nil)
}

// BeginPublishPrepared 在同一授权事务中准备当前发布字段，再持久化不可回退的尝试标记。
// prepare 只能通过 tx 读写本地数据库；失败时字段更新与尝试标记一同回滚，禁止执行外部动作。
func (r *AutomationRepository) BeginPublishPrepared(queueID int64, now time.Time, prepare func(tx *gorm.DB, task AutomationTask) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var settings AutomationSettingsRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, 1).Error; err != nil {
			return err
		}
		var flags struct {
			Enabled     bool `json:"enabled"`
			AutoPublish bool `json:"auto_publish"`
		}
		if json.Unmarshal([]byte(settings.JSON), &flags) != nil || !flags.Enabled || !flags.AutoPublish {
			return errors.New("模块或自动发布已关闭")
		}
		var task AutomationTask
		if err := tx.First(&task, "queue_id = ?", queueID).Error; err != nil {
			return err
		}
		if task.State != "queued_publish" || task.PublishAttempted {
			return ErrAutomationFence
		}
		var n int64
		if err := tx.Model(&AutomationTask{}).Where("id <> ? AND source = ? AND target = ? AND (source_id = ? OR info_hash = ?) AND publish_attempted = ?", task.ID, task.Source, task.Target, task.SourceID, task.InfoHash, true).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrAutomationDuplicate
		}
		if prepare != nil {
			if err := prepare(tx, task); err != nil {
				return err
			}
		}
		result := tx.Model(&AutomationTask{}).Where("queue_id = ? AND state = ? AND publish_attempted = ?", queueID, "queued_publish", false).Updates(map[string]any{"publish_attempted": true, "state": "publishing", "updated_at": now.UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAutomationFence
		}
		return nil
	})
}

// PublishOutcome 先保存永久结果，再允许原生队列完成或清理。未知结果不允许重试。
func (r *AutomationRepository) PublishOutcome(queueID int64, state, url, result string, now time.Time) error {
	return r.db.Model(&AutomationTask{}).Where("queue_id = ? AND publish_attempted = ?", queueID, true).Updates(map[string]any{"state": state, "result_url": url, "result_json": result, "message": "发布结果已持久化；只有校验目标种子后才能完成", "updated_at": now.UnixMilli()}).Error
}

// RecoverPublishing 将进程中断遗留的上传状态隔离，绝不重新进入上传路径。
func (r *AutomationRepository) RecoverPublishing(now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&AutomationTask{}).Where("state = ?", "publishing").Updates(map[string]any{"state": "needs_review", "message": "发布进程中断，需提供目标详情地址核对", "updated_at": now.UnixMilli()}).Error; err != nil {
			return err
		}
		uncertain := tx.Model(&AutomationTask{}).Select("queue_id").Where("publish_attempted = ? AND queue_id IS NOT NULL", true)
		if err := tx.Model(&PublishQueueTask{}).Where("publish_trigger = ? AND status = ? AND id IN (?)", "automation", PublishQueueStatusRunning, uncertain).Updates(map[string]any{"status": PublishQueueStatusFailed, "last_error": "自动化发布进程中断，禁止重试"}).Error; err != nil {
			return err
		}
		untouched := tx.Model(&AutomationTask{}).Select("queue_id").Where("state = ? AND publish_attempted = ? AND queue_id IS NOT NULL", "queued_publish", false)
		if err := tx.Model(&PublishQueueTask{}).Where("publish_trigger = ? AND status = ? AND id IN (?)", "automation", PublishQueueStatusRunning, untouched).Updates(map[string]any{"status": PublishQueueStatusQueued, "started_at": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&PublishQueueTask{}).Where("publish_trigger = ? AND status = ?", "automation", PublishQueueStatusRunning).Updates(map[string]any{"status": PublishQueueStatusFailed, "last_error": "缺少自动化关联，已隔离"}).Error
	})
}

// Reconcile 保存已验证的目标做种证据，不触发上传也不释放预留。
func (r *AutomationRepository) Reconcile(id, url, result string, now time.Time) error {
	res := r.db.Model(&AutomationTask{}).Where("id = ? AND state IN ?", id, []string{"needs_review", "published", "seeding"}).Updates(map[string]any{"state": "succeeded", "result_url": url, "result_json": result, "message": "已核对目标种子并确认完整做种", "updated_at": now.UnixMilli()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// TaskForQueue 按唯一关联读取自动任务，供原生队列执行安全校验。
func (r *AutomationRepository) TaskForQueue(queueID int64) (AutomationTask, error) {
	var row AutomationTask
	err := r.db.First(&row, "queue_id = ?", queueID).Error
	return row, err
}

// MarkSeeding 保存目标哈希及补种尝试边界，后续轮询只检查不会反复强制重校验。
func (r *AutomationRepository) MarkSeeding(id, hash, url string, now time.Time) error {
	res := r.db.Model(&AutomationTask{}).Where("id = ? AND state IN ?", id, []string{"needs_review", "published", "seeding"}).Updates(map[string]any{"state": "seeding", "target_hash": hash, "result_url": url, "updated_at": now.UnixMilli()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// FailBeforePublish 保存尚未上传时的拒绝原因，保留预留与队列关联以供审计。
func (r *AutomationRepository) FailBeforePublish(queueID int64, message string, now time.Time) error {
	return r.db.Model(&AutomationTask{}).Where("queue_id = ? AND publish_attempted = ? AND state = ?", queueID, false, "queued_publish").Updates(map[string]any{"state": "failed", "message": message, "updated_at": now.UnixMilli()}).Error
}

// RequestReconcile 仅登记目标详情和显式核对授权；不访问站点、下载器或文件。
func (r *AutomationRepository) RequestReconcile(id, url string, now time.Time) error {
	result := r.db.Model(&AutomationTask{}).Where("id = ? AND state IN ?", id, []string{"needs_review", "published", "seeding", "reconciling"}).Updates(map[string]any{"state": "reconciling", "result_url": url, "explicit": true, "message": "已登记目标核对，等待后台仅修复做种", "lease_token": "", "lease_until": 0, "updated_at": now.UnixMilli()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// ClaimRun 领取或恢复已登记的扫描，不在数据库事务中执行任何网络请求。
func (r *AutomationRepository) ClaimRun(id, token string, now time.Time) (AutomationRun, error) {
	res := r.db.Model(&AutomationRun{}).Where("id = ? AND status IN ? AND lease_until <= ?", id, []string{"queued", "scanning"}, now.UnixMilli()).Updates(map[string]any{"status": "scanning", "lease_token": token, "lease_until": now.Add(15 * time.Minute).UnixMilli()})
	if res.Error != nil {
		return AutomationRun{}, res.Error
	}
	if res.RowsAffected != 1 {
		return AutomationRun{}, ErrAutomationFence
	}
	var row AutomationRun
	err := r.db.First(&row, "id = ?", id).Error
	return row, err
}

// FinishRunLeased 只有当前扫描工作者能保存候选与扫描结论。
func (r *AutomationRepository) FinishRunLeased(run AutomationRun, status, message, candidates string, selected int, now time.Time) error {
	res := r.db.Model(&AutomationRun{}).Where("id = ? AND lease_token = ? AND lease_until > ? AND status = ?", run.ID, run.LeaseToken, now.UnixMilli(), "scanning").Updates(map[string]any{"status": status, "message": message, "candidates_json": candidates, "selected": selected, "finished_at": now.UnixMilli(), "lease_token": "", "lease_until": 0})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// ReserveForRun 将扫描租约围栏与配额预留包含在同一事务，过期扫描不会产生新任务。
func (r *AutomationRepository) ReserveForRun(task *AutomationTask, maxCount int, maxBytes int64, runID, token string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var run AutomationRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND lease_token = ? AND lease_until > ? AND status = ? AND execute = ?", runID, token, now.UnixMilli(), "scanning", true).Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAutomationFence
			}
			return err
		}
		return (&AutomationRepository{db: tx}).Reserve(task, maxCount, maxBytes)
	})
}

// PendingRuns 返回最早的未完成扫描意图，避免新历史记录饿死旧任务。
func (r *AutomationRepository) PendingRuns() ([]AutomationRun, error) {
	rows := []AutomationRun{}
	err := r.db.Where("status IN ?", []string{"queued", "scanning"}).Order("created_at ASC").Limit(10).Find(&rows).Error
	return rows, err
}

// WorkTasks 只读取可推进任务，避免终态历史挤出待执行任务。
func (r *AutomationRepository) WorkTasks() ([]AutomationTask, error) {
	rows := []AutomationTask{}
	err := r.db.Where("state IN ?", []string{"reserved", "downloading", "preparing", "ready", "published", "seeding", "reconciling"}).Order("last_polled_at ASC").Order("created_at ASC").Order("id ASC").Limit(50).Find(&rows).Error
	return rows, err
}

// TouchPoll 为每条被轮询的记录推进公平游标；禁用、领取失败、网络等待均不会饿死后续任务。
func (r *AutomationRepository) TouchPoll(id string, now time.Time) error {
	return r.db.Model(&AutomationTask{}).Where("id = ?", id).Update("last_polled_at", now.UnixMilli()).Error
}

// CheckPublishEvidence 拒绝对已有上传或不确定证据的同源载荷再次发布；当前受信自动队列可排除自身。
func (r *AutomationRepository) CheckPublishEvidence(sources, sourceIDs, hashes, targets []string, allowedQueueID int64) error {
	if len(targets) == 0 {
		return errors.New("目标站身份不明确，无法核对自动发布账本")
	}
	identity := r.db.Where("1 = 0")
	if len(hashes) > 0 {
		identity = identity.Or("LOWER(info_hash) IN ?", hashes)
	}
	if len(sources) > 0 && len(sourceIDs) > 0 {
		identity = identity.Or("source IN ? AND source_id IN ?", sources, sourceIDs)
	}
	query := r.db.Model(&AutomationTask{}).Where("target IN ?", targets).Where(identity).Where("publish_attempted = ? OR state IN ?", true, []string{"needs_review", "publishing", "published", "seeding", "succeeded", "reconciling"})
	if allowedQueueID > 0 {
		query = query.Where("queue_id IS NULL OR queue_id <> ?", allowedQueueID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return errors.New("自动发布账本读取失败，已停止上传")
	}
	if count > 0 {
		return errors.New("该载荷在目标站已有自动发布或待核对证据，禁止再次上传；请到自动化任务中人工核对目标详情")
	}
	return nil
}
