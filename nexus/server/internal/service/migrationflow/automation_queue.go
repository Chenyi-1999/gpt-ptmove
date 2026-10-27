package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// automationQueueEnabled 让自动化队列独立于原生手动队列开关，但必须同时启用模块和自动发布。
func (s *MigrateService) automationQueueEnabled() bool {
	if s == nil || s.automation == nil {
		return false
	}
	v, err := s.automation.settings(s.automation.repo)
	return err == nil && v.Enabled && v.AutoPublish
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
