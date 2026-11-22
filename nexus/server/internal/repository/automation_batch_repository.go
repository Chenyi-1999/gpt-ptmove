package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var automationBatchActiveStates = []string{
	"reserved", "downloading", AutomationStateWaitingBatch, "preparing", AutomationStateWaitingMedia,
	"ready", "queued_publish", "publishing", AutomationStateDuplicateCleanup,
}

const automationBatchRetryDelay = 2 * time.Minute

// BatchExcludedSourceIDs 让续批跳过已有下载与当月已经分配的源 ID，避免耗尽详情读取额度。
func (r *AutomationRepository) BatchExcludedSourceIDs(source, target, month string) (map[string]bool, error) {
	var ids []string
	err := r.db.Model(&AutomationTask{}).Distinct("source_id").Where("source = ?", source).
		Where("((target = ? AND month = ?) OR download_attempted = ? OR publish_attempted = ? OR state IN ?)", target, month, true, true, automationDownloadStates).
		Pluck("source_id", &ids).Error
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]bool, len(ids))
	for _, id := range ids {
		excluded[id] = true
	}
	return excluded, nil
}

// BatchRuns 返回正在下载、查重或发布的已登记扫描。
func (r *AutomationRepository) BatchRuns() ([]AutomationRun, error) {
	rows := []AutomationRun{}
	err := r.db.Where("status = ?", "batch_active").
		Order("created_at ASC").Order("id ASC").Find(&rows).Error
	return rows, err
}

// ReservedForCurrentBatch 恢复扫描中断前已持久预留的任务数，已结束批次以累计已选数为界。
func (r *AutomationRepository) ReservedForCurrentBatch(run AutomationRun) (int, error) {
	var total int64
	err := r.db.Model(&AutomationTask{}).Where("run_id = ? AND month = ? AND batch_check = ?", run.ID, run.Month, true).Count(&total).Error
	return max(0, int(total)-run.Selected), err
}

// BatchCleanupPending 等待本批查重结论入库，以及重复项首次处理之后再发布其余种子。
func (r *AutomationRepository) BatchCleanupPending(runID string, now time.Time) (bool, error) {
	var pending int64
	err := r.db.Model(&AutomationTask{}).Where("run_id = ? AND batch_check = ?", runID, true).
		Where("((state = ? AND (duplicate_cleanup_attempted IS NULL OR duplicate_cleanup_attempted = ?)) OR (state = ? AND lease_until > ?))",
			AutomationStateDuplicateCleanup, false, AutomationStateWaitingBatch, now.UnixMilli()).Count(&pending).Error
	return pending > 0, err
}

// WaitingBatchTasks 只在该批下载阶段结束时返回已确认完整的任务。未知任务按批次剩余成员统一退避。
func (r *AutomationRepository) WaitingBatchTasks(runID string, now time.Time) ([]AutomationTask, error) {
	if err := automationBatchDownloadsFinished(r.db, runID); err != nil {
		if errors.Is(err, ErrAutomationFence) {
			return nil, nil
		}
		return nil, err
	}
	var rows []AutomationTask
	err := r.db.Where("run_id = ? AND batch_check = ? AND state = ?", runID, true, AutomationStateWaitingBatch).
		Order("created_at ASC").Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, task := range rows {
		if task.BatchRetryAt > now.UnixMilli() || task.LeaseUntil > now.UnixMilli() {
			return nil, nil
		}
	}
	return rows, nil
}

// automationBatchDownloadsFinished 检查本批仍可能完成下载的任务。下载前待人工核对的任务保留名额。
func automationBatchDownloadsFinished(db *gorm.DB, runID string) error {
	var active int64
	if err := db.Model(&AutomationTask{}).Where("run_id = ? AND batch_check = ? AND state IN ?", runID, true, []string{"reserved", "downloading"}).Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return ErrAutomationFence
	}
	return nil
}

// ClaimBatchTasks 原子领取已完整下载的批次，校验任务集合并禁止两个工作者同时查询。
func (r *AutomationRepository) ClaimBatchTasks(tasks []AutomationTask, token string, now time.Time) ([]AutomationTask, error) {
	if len(tasks) == 0 || token == "" || tasks[0].RunID == "" {
		return nil, ErrAutomationFence
	}
	runID := tasks[0].RunID
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.RunID != runID || task.Month != tasks[0].Month || task.Source != tasks[0].Source || task.Target != tasks[0].Target {
			return nil, ErrAutomationFence
		}
		ids = append(ids, task.ID)
	}
	var claimed []AutomationTask
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var run AutomationRun
		res := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", runID, "batch_active").Take(&run)
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return ErrAutomationFence
		}
		if res.Error != nil {
			return res.Error
		}
		if run.Month != tasks[0].Month {
			return ErrAutomationFence
		}
		if err := automationBatchDownloadsFinished(tx, runID); err != nil {
			return err
		}

		var waiting int64
		if err := tx.Model(&AutomationTask{}).Where("run_id = ? AND batch_check = ? AND state = ?", runID, true, AutomationStateWaitingBatch).Count(&waiting).Error; err != nil {
			return err
		}
		if waiting != int64(len(ids)) {
			return ErrAutomationFence
		}
		res = tx.Model(&AutomationTask{}).
			Where("id IN ? AND run_id = ? AND month = ? AND source = ? AND target = ? AND batch_check = ? AND state = ? AND download_attempted = ? AND publish_attempted = ? AND queue_id IS NULL",
				ids, runID, run.Month, tasks[0].Source, tasks[0].Target, true, AutomationStateWaitingBatch, true, false).
			Where("lease_until <= ? AND (batch_retry_at IS NULL OR batch_retry_at <= ?)", now.UnixMilli(), now.UnixMilli()).
			Updates(map[string]any{"lease_token": token, "lease_until": now.Add(15 * time.Minute).UnixMilli()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(ids)) {
			return ErrAutomationFence
		}
		return tx.Where("id IN ?", ids).Order("created_at ASC").Order("id ASC").Find(&claimed).Error
	})
	return claimed, err
}

// ResolveBatchTask 保存受租约约束的查重结论。空结果指针代表未知并继续等待。
func (r *AutomationRepository) ResolveBatchTask(task AutomationTask, duplicate *bool, now time.Time) error {
	if task.State != AutomationStateWaitingBatch || !task.BatchCheck || task.LeaseToken == "" {
		return ErrAutomationFence
	}
	values := map[string]any{"lease_token": "", "lease_until": 0, "updated_at": now.UnixMilli()}
	switch {
	case duplicate == nil:
		values["message"] = "IYUU 批量查重待核对，将自动重试"
		values["batch_retry_at"] = now.Add(automationBatchRetryDelay).UnixMilli()
	case *duplicate:
		values["state"] = AutomationStateDuplicateCleanup
		values["message"] = "IYUU 确认目标已有对应种子，等待重复处理"
		values["batch_checked_at"] = now.UnixMilli()
		values["batch_retry_at"] = 0
		values["batch_duplicate"] = true
	default:
		values["state"] = "preparing"
		values["message"] = "IYUU 批量查询未发现目标重复，准备发布资料"
		values["batch_checked_at"] = now.UnixMilli()
		values["batch_retry_at"] = 0
		values["batch_duplicate"] = false
	}
	res := r.db.Model(&AutomationTask{}).
		Where("id = ? AND run_id = ? AND info_hash = ? AND batch_check = ? AND state = ? AND lease_token = ? AND lease_until > ? AND download_attempted = ? AND publish_attempted = ? AND queue_id IS NULL",
			task.ID, task.RunID, task.InfoHash, true, AutomationStateWaitingBatch, task.LeaseToken, now.UnixMilli(), true, false).
		Updates(values)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrAutomationFence
	}
	return nil
}

// ResumeBatchRun 在没有可继续推进的本批任务时将同一持久扫描重新排队。
// 待人工复查的任务继续占用其名额，其他空闲名额可以进入下一批。
func (r *AutomationRepository) ResumeBatchRun(run AutomationRun, now time.Time) (bool, error) {
	if run.ID == "" || run.Status != "batch_active" {
		return false, ErrAutomationFence
	}
	values := map[string]any{
		"status": "queued", "finished_at": 0, "lease_token": "", "lease_until": 0,
		"message": "上一批已处理，等待扫描剩余候选",
	}
	if !run.Execute {
		values["status"] = "completed"
		values["finished_at"] = now.UnixMilli()
		values["message"] = "批次任务已处理"
	}
	active := r.db.Model(&AutomationTask{}).Select("id").
		Where("run_id = ? AND batch_check = ? AND state IN ?", run.ID, true, automationBatchActiveStates).
		Where("(state <> ? OR duplicate_cleanup_attempted IS NULL OR duplicate_cleanup_attempted = ?)", AutomationStateDuplicateCleanup, false)
	res := r.db.Model(&AutomationRun{}).
		Where("id = ? AND month = ? AND status = ? AND execute = ? AND lease_until <= ?", run.ID, run.Month, "batch_active", run.Execute, now.UnixMilli()).
		Where("NOT EXISTS (?)", active).
		Updates(values)
	return run.Execute && res.RowsAffected == 1, res.Error
}
