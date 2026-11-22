package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// usesIYUUBatches 仅对采用当前规则的织梦运行开启批量查重，保留旧任务的发布保护。
func usesIYUUBatches(snapshot automationSnapshot) bool {
	return snapshot.Settings.BatchSize > 0 && acquireautomation.IsZmPTSource(acquireautomation.Profile{BaseURL: snapshot.Source.BaseURL})
}

// iyuuConfigured 检查现有令牌设置，不读取或写入站点凭据。
func (s *AutomationService) iyuuConfigured() bool {
	return s.iyuuLookup != nil && s.cfg != nil && processString(s.cfg.Get()["iyuu_token"]) != ""
}

// processIYUUBatches 核对文件已完整的批次。现有任务完成后再登记下一批扫描。
func (s *AutomationService) processIYUUBatches(ctx context.Context, current policy.Settings) {
	runs, err := s.repo.BatchRuns()
	if err != nil {
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		var snapshot automationRunSnapshot
		if json.Unmarshal([]byte(run.Snapshot), &snapshot) != nil || !usesIYUUBatches(snapshot.Route) {
			continue
		}
		tasks, err := s.repo.WaitingBatchTasks(run.ID, s.now())
		if err != nil {
			continue
		}
		if len(tasks) > 0 {
			s.checkIYUUBatch(ctx, snapshot.Route, tasks)
		}
		if current.Enabled {
			_, _ = s.repo.ResumeBatchRun(run, s.now())
		}
	}
}

// checkIYUUBatch 只向 IYUU 发送本批的源哈希；没有明确结果的任务继续等待。
func (s *AutomationService) checkIYUUBatch(ctx context.Context, snapshot automationSnapshot, tasks []repository.AutomationTask) {
	token := automationID("batch")
	claimed, err := s.repo.ClaimBatchTasks(tasks, token, s.now())
	if err != nil || len(claimed) == 0 {
		return
	}
	hashes := make([]string, 0, len(claimed))
	for _, task := range claimed {
		defer s.repo.Release(task.ID, token)
		if s.authorizedTask(task, snapshot) == nil {
			hashes = append(hashes, task.InfoHash)
		}
	}
	var results map[string]bool
	if len(hashes) > 0 && s.iyuuConfigured() {
		lookupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		results, err = s.iyuuLookup(lookupCtx, snapshot.Target.BaseURL, hashes)
		cancel()
		if err != nil {
			results = nil
		}
	}
	for _, task := range claimed {
		if s.authorizedTask(task, snapshot) != nil {
			continue
		}
		var evidence *bool
		if duplicate, ok := results[strings.ToLower(task.InfoHash)]; ok {
			evidence = &duplicate
		}
		_ = s.repo.ResolveBatchTask(task, evidence, s.now())
	}
}

type automationDuplicateDownloader interface {
	HandleAutomationDuplicate(hash, taskID, expectedSavePath, action, location string) error
	AutomationDuplicateCleanupComplete(hash, taskID, action, location string) (bool, error)
}

// finishBatchDuplicate 处理已被 IYUU 确认重复的自有下载；外部操作只提交一次。
func (s *AutomationService) finishBatchDuplicate(task repository.AutomationTask, snapshot automationSnapshot) {
	frozen := snapshot.Settings
	action := frozen.DuplicateAction
	finish := func(message string) {
		_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{
			"state": repository.AutomationStateAlreadyExists, "message": message,
		}, s.now())
	}
	if action == "keep" {
		finish("IYUU 已确认目标站存在，保留源下载")
		return
	}
	current, err := s.settings(s.repo)
	if err != nil {
		return
	}
	if !task.DuplicateCleanupAttempted && (current.DuplicateAction != action || current.DuplicateDirectory != frozen.DuplicateDirectory) {
		finish("IYUU 已确认目标站存在，处理设置已改变，保留源下载")
		return
	}
	d, err := s.downloader(task.DownloaderID)
	if err != nil {
		finish("IYUU 已确认目标站存在，下载器无法连接，重复处理待核对")
		return
	}
	if native, ok := d.(downloaderclient.Downloader); ok {
		d = native.WithEffectGuard(func() error {
			if err := s.authorizedTask(task, snapshot); err != nil {
				return err
			}
			settings, err := s.settings(s.repo)
			if err != nil || settings.DuplicateAction != action || settings.DuplicateDirectory != frozen.DuplicateDirectory {
				return errors.New("重复种子处理设置已改变")
			}
			return nil
		})
	}
	cleaner, ok := d.(automationDuplicateDownloader)
	if !ok {
		finish("IYUU 已确认目标站存在，下载器不支持设置的处理方式")
		return
	}
	if task.DuplicateCleanupAttempted {
		done, err := cleaner.AutomationDuplicateCleanupComplete(task.InfoHash, task.ID, action, frozen.DuplicateDirectory)
		if err == nil && done {
			finish(duplicateCleanupMessage(action))
		}
		return
	}
	if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{
		"duplicate_cleanup_attempted": true, "message": "目标站已有种子，正在核对 qB 重复处理结果",
	}, s.now()); err != nil {
		return
	}
	err = cleaner.HandleAutomationDuplicate(task.InfoHash, task.ID, task.SavePath, action, frozen.DuplicateDirectory)
	switch {
	case err == nil:
		finish(duplicateCleanupMessage(action))
	case errors.Is(err, downloaderclient.ErrAutomationDuplicateCleanupPending):
		// 下个轮询只读取 qB 状态，避免重复移动。
	default:
		finish("IYUU 已确认目标站存在，qB 重复处理未获确认，可在下载器检查")
	}
}

// duplicateCleanupMessage 区分移动完成与只删除 qB 任务的已确认结果。
func duplicateCleanupMessage(action string) string {
	if action == "move" {
		return "IYUU 已确认目标站存在，下载已移到重复目录"
	}
	return "IYUU 已确认目标站存在，已删除 qB 任务并保留文件"
}
