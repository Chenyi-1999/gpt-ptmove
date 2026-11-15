package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	"gorm.io/gorm"
)

// automationQueueEnabled 让自动化队列独立于原生手动队列开关，但必须同时启用模块和自动发布。
func (s *MigrateService) automationQueueEnabled() bool {
	if s == nil || s.automation == nil {
		return false
	}
	v, err := s.automation.settings(s.automation.repo)
	return err == nil && v.Enabled && v.AutoPublish
}

// beginAutomationPublish 在上传授权事务内重读审核状态，只刷新截图字段并返回实际发送的同一份数据。
// 审核、队列或尝试围栏失败则整体回滚；不访问网络，不刷新冻结的路由和其他发布字段。
func (s *MigrateService) beginAutomationPublish(queue repository.PublishQueueTask) (map[string]any, error) {
	var upload map[string]any
	err := s.automation.repo.BeginPublishPrepared(queue.ID, s.automation.now(), func(tx *gorm.DB, task repository.AutomationTask) error {
		var snapshot automationSnapshot
		if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
			return errors.New("自动任务快照损坏")
		}
		store := &repository.Store{DB: tx}
		current := &MigrateService{repo: repository.NewMigrateRepository(store)}
		row, err := current.automationSeedForPublish(task, snapshot)
		if err != nil {
			return err
		}
		if json.Unmarshal([]byte(queue.UploadDataJSON), &upload) != nil || upload == nil {
			return errors.New("自动发布字段快照无效")
		}
		screenshots := processingshared.ToString(row["screenshots"], "")
		upload["screenshots"] = screenshots
		upload["screenshot_review_status"] = row["screenshot_review_status"]
		if intro, ok := upload["intro"].(map[string]any); ok {
			intro["screenshots"] = screenshots
		}
		if complete, ok := upload["complete_publish_params"].(map[string]any); ok {
			if intro, ok := complete["intro"].(map[string]any); ok {
				intro["screenshots"] = screenshots
			}
		}
		raw, err := json.Marshal(upload)
		if err != nil {
			return err
		}
		updated := tx.Model(&repository.PublishQueueTask{}).Where("id = ? AND status = ? AND upload_data_json = ?", queue.ID, repository.PublishQueueStatusRunning, queue.UploadDataJSON).Updates(map[string]any{"upload_data_json": string(raw), "updated_at": s.automation.now().Format(repository.PublishQueueTimeLayout)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return repository.ErrAutomationFence
		}
		return nil
	})
	return upload, err
}

// prepareAutomationQueue 在任何上传标记前重新校验站点冻结、完整下载、限制和目标缺失。
func (s *MigrateService) prepareAutomationQueue(queue repository.PublishQueueTask) error {
	if !s.automationQueueEnabled() {
		return errors.New("模块或自动发布已关闭")
	}
	a := s.automation
	task, err := a.repo.TaskForQueue(queue.ID)
	if err != nil {
		return err
	}
	if task.State != "queued_publish" || task.PublishAttempted {
		return errors.New("自动任务已停止或已尝试上传")
	}
	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return errors.New("自动任务快照损坏")
	}
	if err = a.validateFrozen(snapshot); err != nil {
		return err
	}
	if _, err = s.automationSeedForPublish(task, snapshot); err != nil {
		return err
	}
	d, err := a.downloader(task.DownloaderID)
	if err != nil {
		return err
	}
	status, err := d.TorrentStatus(task.InfoHash)
	if err != nil || !status.Found || status.Progress < 1 || status.AmountLeft != 0 || !sameAutomationSavePath(status.SavePath, task.SavePath) {
		return errors.New("源种子完整状态未确认")
	}
	complete, err := d.TorrentFilesComplete(task.InfoHash)
	if err != nil || !complete {
		return errors.New("源种子并非全部文件完成")
	}
	sp, _ := acquireautomation.ProfileFrom(snapshot.Source)
	tp, _ := acquireautomation.ProfileFrom(snapshot.Target)
	sourceCookie, err := a.profiles.Cookie(task.Source)
	if err != nil {
		return errors.New("源凭据不可用")
	}
	targetCookie, err := a.profiles.Cookie(task.Target)
	if err != nil {
		return errors.New("目标凭据不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	candidate, err := a.scanner.Inspect(ctx, sp, tp, sourceCookie, targetCookie, task.SourceID)
	if err != nil {
		return errors.New("发布前只读检查失败")
	}
	candidate = policy.Evaluate(candidate, snapshot.Settings)
	if !candidate.Eligible || candidate.InfoHash != task.InfoHash || candidate.Size != task.Size {
		return errors.New("发布前权限、查重或载荷检查未通过")
	}
	return nil
}

// automationPublishOutcome 将任何上传尝试结果先写永久账本，绝不进入原生自动重试。
func (s *MigrateService) automationPublishOutcome(queue repository.PublishQueueTask, result map[string]any, status int, text string) error {
	if s.automation == nil {
		return errors.New("自动化服务未初始化")
	}
	state := "needs_review"
	url := processString(result["url"])
	success, _ := result["success"].(bool)
	existing, _ := result["is_existing_torrent"].(bool)
	if success && status < 400 && !existing && url != "" {
		state = "published"
	}
	return s.automation.repo.PublishOutcome(queue.ID, state, url, text, s.automation.now())
}

// automationSafeResult 只保存可公开的结构化发布证据，避免复制原生结果内的下载 passkey 或正文日志。
func (s *MigrateService) automationSafeResult(queue repository.PublishQueueTask, result map[string]any) map[string]any {
	safe := map[string]any{"success": result["success"], "is_existing_torrent": result["is_existing_torrent"], "message": "自动发布已尝试，完整结果以原生日志和目标核对为准"}
	if s.automation == nil {
		return safe
	}
	task, err := s.automation.repo.TaskForQueue(queue.ID)
	if err != nil {
		return safe
	}
	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return safe
	}
	p, _ := acquireautomation.ProfileFrom(snapshot.Target)
	if u, err := acquireautomation.ValidateDetailURL(p, processString(result["url"])); err == nil {
		safe["url"] = u
	}
	return safe
}
