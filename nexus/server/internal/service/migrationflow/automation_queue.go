package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
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

// beginAutomationPublish 在上传授权事务内重读审核状态，刷新媒体资料并返回实际发送的同一份数据。
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
		upload["mediainfo"] = processingshared.ToString(row["mediainfo"], "")
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

// automationTargetCheckState 提取唯一由目标查重引起的等待或重复结果。
func automationTargetCheckState(candidate policy.Candidate, task repository.AutomationTask, settings policy.Settings) string {
	if (candidate.DuplicateKnown && !candidate.Duplicate) || candidate.InfoHash != task.InfoHash || candidate.Size != task.Size {
		return ""
	}
	state := repository.AutomationStateWaitingTarget
	if candidate.DuplicateKnown {
		state = repository.AutomationStateAlreadyExists
	}
	// 仅评估其余规则；调用方绝不以此副本下载或发种。
	candidate.DuplicateKnown, candidate.Duplicate = true, false
	if !policy.Evaluate(candidate, settings).Eligible {
		return ""
	}
	return state
}

type automationTargetCheckError struct{ state string }

func (e *automationTargetCheckError) Error() string {
	if e.state == repository.AutomationStateAlreadyExists {
		return "发布前确认目标已有种子"
	}
	return "发布前无法确认目标站查重结果"
}

// prepareAutomationQueue 在任何上传标记前重新校验站点冻结、完整下载、限制和目标缺失。
func (s *MigrateService) prepareAutomationQueue(queue repository.PublishQueueTask, nativeContext publishworkflow.Context) error {
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
	if !task.DownloadAttempted {
		return errors.New("未确认本任务发起下载，已停止自动发布")
	}
	if err = a.checkTaskDownloadHistory(context.Background(), task); err != nil {
		return errors.New(err.Error() + "，已停止自动发布")
	}
	sp, err := acquireautomation.ProfileFrom(snapshot.Source)
	if err != nil {
		return errors.New("源站配置不可用，无法确认转种权限")
	}
	id, err := acquireautomation.SourceRestrictionID(sp, nativeContext.TorrentID, nativeContext.SourceDetailURL)
	nativeSite := strings.TrimSpace(nativeContext.SiteName)
	if err != nil || id != task.SourceID ||
		(nativeSite != snapshot.Source.NativeCode() && nativeSite != snapshot.Source.NativeName()) ||
		!strings.EqualFold(nativeContext.Hash, task.InfoHash) {
		return errors.New("自动发布上下文与冻结的源站种子不一致")
	}
	if err := verifyAutomationPublishTorrent(nativeContext.OriginalTorrentPath, task); err != nil {
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
	sourceCookie, err := a.profiles.Cookie(task.Source)
	if err != nil {
		return errors.New("源凭据不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var candidate policy.Candidate
	if task.BatchCheck {
		if task.BatchCheckedAt <= 0 || task.BatchDuplicate || !usesIYUUBatches(snapshot) {
			return errors.New("缺少本批 IYUU 查重通过记录")
		}
		candidate, err = a.scanner.InspectSourceForPublish(ctx, sp, sourceCookie, task.SourceID)
		candidate.DuplicateKnown = true
	} else {
		tp, _ := acquireautomation.ProfileFrom(snapshot.Target)
		targetCookie, cookieErr := a.profiles.Cookie(task.Target)
		if cookieErr != nil {
			return errors.New("目标凭据不可用")
		}
		candidate, err = a.scanner.InspectForPublish(ctx, sp, tp, sourceCookie, targetCookie, task.SourceID)
	}
	if candidate.RestrictionsKnown && !candidate.Allowed {
		return errors.New("源站标记禁转或限转，已停止发布")
	}
	// 本任务的完整下载已核实，源站因此显示已下载是预期状态。
	candidate.PreviouslyDownloaded = false
	if state := automationTargetCheckState(candidate, task, snapshot.Settings); state != "" {
		return &automationTargetCheckError{state: state}
	}
	if err != nil {
		return errors.New("发布前只读检查失败")
	}
	if !candidate.RestrictionsKnown {
		return errors.New("无法确认源站转种权限，已停止发布")
	}
	candidate = policy.Evaluate(candidate, snapshot.Settings)
	if !candidate.Eligible {
		return errors.New("发布前候选已不符合规则：" + strings.Join(candidate.Reasons, "；"))
	}
	if candidate.InfoHash != task.InfoHash || candidate.Size != task.Size {
		return errors.New("发布前候选完整载荷与预留时不一致")
	}
	return nil
}

// automationPublishOutcome 持久化规范化的上传回执，供原生发布日志使用。不会自动重试上传。
func (s *MigrateService) automationPublishOutcome(queue repository.PublishQueueTask, result map[string]any, status int) (map[string]any, error) {
	if s.automation == nil {
		return nil, errors.New("自动化服务未初始化")
	}
	result = s.automationSafeResult(queue, result)
	state := "needs_review"
	url := processString(result["url"])
	success, _ := result["success"].(bool)
	existing, _ := result["is_existing_torrent"].(bool)
	switch {
	case existing:
		state = repository.AutomationStateAlreadyExists
		result["message"] = "目标站返回已有对应种子"
	case success && status >= 200 && status < 300 && url != "":
		state = "published"
		result["message"] = "已发布，等待核验目标做种"
	default:
		result["message"] = "发布结果待核对，禁止重复提交"
	}
	result["success"] = state == "published"
	result["logs"] = result["message"]
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return result, s.automation.repo.PublishOutcome(queue.ID, state, url, string(encoded), s.automation.now())
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
