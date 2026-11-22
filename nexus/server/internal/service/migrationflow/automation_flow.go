package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

// authorizedTask 在每个新的外部动作前检查租约、模块开关和冻结站点声明。
func (s *AutomationService) authorizedTask(task repository.AutomationTask, snapshot automationSnapshot) error {
	current, err := s.repo.Task(task.ID)
	if err != nil {
		return err
	}
	if current.LeaseToken != task.LeaseToken || current.LeaseUntil <= s.now().UnixMilli() || current.State != task.State {
		return repository.ErrAutomationFence
	}
	v, err := s.settings(s.repo)
	if err != nil {
		return err
	}
	if !v.Enabled && !current.Explicit {
		return errors.New("模块已关闭，任务保持等待")
	}
	return s.validateFrozen(snapshot)
}

// advanceTask 只编排获取、完整下载、原生处理和原子队列交接，底层解析在各自分层实现。
func (s *AutomationService) advanceTask(ctx context.Context, task repository.AutomationTask) {
	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		s.failTask(task, "任务快照无效", false)
		return
	}
	if s.authorizedTask(task, snapshot) != nil {
		return
	}
	if task.State == repository.AutomationStateDuplicateCleanup {
		s.finishBatchDuplicate(task, snapshot)
		return
	}
	if task.State == repository.AutomationStateWaitingMedia && s.now().UnixMilli() < task.MediaRetryAt {
		return
	}
	d, err := s.downloader(task.DownloaderID)
	if err != nil {
		if task.State == "preparing" || task.State == repository.AutomationStateWaitingMedia {
			s.waitForAutomationMedia(task, "等待下载器和飞牛客户端恢复连接")
			return
		}
		s.failTask(task, "原生下载器不可用", false)
		return
	}
	if native, ok := d.(downloaderclient.Downloader); ok {
		d = native.WithEffectGuard(func() error { return s.authorizedTask(task, snapshot) })
	}
	if task.State == "reserved" {
		if task.BatchCheck && task.Month != policy.Month(s.now()) {
			s.failTask(task, "本批已跨月，未提交新下载", false)
			return
		}
		if task.BatchCheck && !s.iyuuConfigured() {
			s.failTask(task, "IYUU 令牌未配置，未提交新下载", false)
			return
		}
		if err = s.checkTaskDownloadHistory(ctx, task); err != nil {
			s.failTask(task, err.Error()+"，未提交新下载", false)
			return
		}
		sp, _ := acquireautomation.ProfileFrom(snapshot.Source)
		tp, _ := acquireautomation.ProfileFrom(snapshot.Target)
		sourceCookie, err := s.profiles.Cookie(task.Source)
		if err != nil {
			s.failTask(task, "源站凭据不可用", false)
			return
		}
		targetCookie, err := s.profiles.Cookie(task.Target)
		if err != nil {
			s.failTask(task, "目标站凭据不可用", false)
			return
		}
		candidate, err := s.scanner.Inspect(ctx, sp, tp, sourceCookie, targetCookie, task.SourceID)
		if err != nil {
			s.failTask(task, "下载前重新检查失败", false)
			return
		}
		switch automationTargetCheckState(candidate, task, snapshot.Settings) {
		case repository.AutomationStateAlreadyExists:
			_ = s.repo.SkipExistingBeforeDownload(task, s.now())
			return
		case repository.AutomationStateWaitingTarget:
			_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": repository.AutomationStateWaitingTarget, "message": "下载前目标查重待核对，未添加种子"}, s.now())
			return
		}
		candidate = policy.Evaluate(candidate, snapshot.Settings)
		if !candidate.Eligible || candidate.InfoHash != task.InfoHash || candidate.Size != task.Size {
			message := "候选完整载荷与预留时不一致"
			if !candidate.Eligible {
				message = "下载前候选已不符合规则：" + strings.Join(candidate.Reasons, "；")
			}
			s.failTask(task, message, false)
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if err = acquirefetch.ValidateTorrentNetwork(candidate.Torrent, snapshot.Source.AnnounceHosts...); err != nil {
			s.failTask(task, "源种子 tracker 不符合冻结站点声明", false)
			return
		}
		status, err := d.TorrentStatus(task.InfoHash)
		if err != nil {
			s.failTask(task, "下载器状态未知，未提交新下载", false)
			return
		}
		if status.Found {
			s.failTask(task, "相同种子已存在于下载器，未提交新下载", false)
			return
		}
		if snapshot.Settings.ReserveSpaceGiB > 0 {
			free, spaceErr := d.FreeSpace(task.SavePath)
			if spaceErr != nil || free < task.Size+int64(snapshot.Settings.ReserveSpaceGiB*float64(policy.GiB)) {
				s.failTask(task, "目录可用空间未知或不足，未提交下载", false)
				return
			}
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if err = s.checkTaskDownloadHistory(ctx, task); err != nil {
			s.failTask(task, err.Error()+"，未提交新下载", false)
			return
		}
		dir := filepath.Join(config.ResolveRuntimePaths().DataDir, "automation", "torrents")
		if err = os.MkdirAll(dir, 0o700); err != nil {
			s.failTask(task, "无法保存任务种子元数据", false)
			return
		}
		torrentPath := filepath.Join(dir, task.ID+".torrent")
		if err = os.WriteFile(torrentPath, candidate.Torrent, 0o600); err != nil {
			s.failTask(task, "无法保存任务种子元数据", false)
			return
		}
		if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": "downloading", "torrent_path": torrentPath, "download_attempted": true, "message": "等待完整文件下载与实际校验"}, s.now()); err != nil {
			return
		}
		task.State = "downloading"
		task.TorrentPath = torrentPath
		task.DownloadAttempted = true
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if err = d.AddSourceTorrent(candidate.Torrent, task.ID+".torrent", task.SavePath, task.InfoHash); err != nil {
			if errors.Is(err, downloaderclient.ErrAutomationSourceExists) {
				s.failTask(task, "相同种子在提交前已存在于下载器，未提交新下载", false)
				return
			}
			if s.authorizedTask(task, snapshot) != nil {
				return
			}
			confirmed, queryErr := d.TorrentStatus(task.InfoHash)
			if queryErr == nil && confirmed.Found && sameAutomationSavePath(confirmed.SavePath, task.SavePath) {
				return
			}
			s.failTask(task, "源种子添加结果未知，按哈希核对未确认，禁止盲目重加", true)
			return
		}
	}
	if task.State == "downloading" {
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		status, err := d.TorrentStatus(task.InfoHash)
		if err != nil {
			return
		}
		if !status.Found {
			s.failTask(task, "源下载未找到，保留预留，请核对下载器", false)
			return
		}
		if !sameAutomationSavePath(status.SavePath, task.SavePath) {
			s.failTask(task, "源下载保存目录改变", false)
			return
		}
		if status.ProgressKnown && (task.DownloadProgress == nil || *task.DownloadProgress != status.Progress) {
			if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"download_progress": status.Progress}, s.now()); err != nil {
				return
			}
		}
		if status.Progress < 1 || status.AmountLeft != 0 {
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		complete, err := d.TorrentFilesComplete(task.InfoHash)
		if err != nil || !complete {
			return
		}
		if task.BatchCheck {
			_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{
				"state": repository.AutomationStateWaitingBatch, "message": "文件已完整，等待本批下载结束后统一用 IYUU 查重",
			}, s.now())
			return
		}
		if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": "preparing", "message": "完整文件完成，交给原生资料处理"}, s.now()); err != nil {
			return
		}
		task.State = "preparing"
	}
	if task.State == "preparing" || task.State == repository.AutomationStateWaitingMedia {
		if s.migrate == nil {
			s.failTask(task, "原生迁移服务不可用", false)
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if err := s.prepareAutomationMedia(ctx, task, snapshot, d); err != nil {
			if s.authorizedTask(task, snapshot) == nil {
				if errors.Is(err, errAutomationMediaRestricted) {
					s.failTask(task, err.Error(), false)
				} else {
					s.waitForAutomationMedia(task, err.Error())
				}
			}
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if _, err = s.migrate.automationSeedForPublish(task, snapshot); err != nil {
			s.waitForAutomationMedia(task, err.Error())
			return
		}
		if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": "ready", "message": "资料已就绪，等待独立自动发布开关", "media_retry_at": 0}, s.now()); err != nil {
			return
		}
		task.State = "ready"
	}
	if task.State == "ready" {
		if task.BatchCheck {
			pending, err := s.repo.BatchCleanupPending(task.RunID, s.now())
			if err != nil || pending || task.BatchCheckedAt <= 0 || task.BatchDuplicate {
				return
			}
		}
		v, err := s.settings(s.repo)
		if err != nil || !v.Enabled || !v.AutoPublish {
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		if err = s.enqueueNative(task, snapshot); err != nil {
			s.failTask(task, "原生队列交接失败，未尝试上传："+err.Error(), false)
		}
	}
}

// waitForAutomationMedia 保留下载及配额，下一轮在退避期结束后仅检查媒体资料。
func (s *AutomationService) waitForAutomationMedia(task repository.AutomationTask, message string) {
	now := s.now()
	_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{
		"state": repository.AutomationStateWaitingMedia, "message": message, "media_retry_at": now.Add(automationMediaRetryDelay).UnixMilli(),
	}, now)
}

// enqueueNative 从持久原生资料重建上下文，并在一个事务内关联唯一队列行。
func (s *AutomationService) enqueueNative(task repository.AutomationTask, snapshot automationSnapshot) error {
	if _, err := s.migrate.automationSeedForPublish(task, snapshot); err != nil {
		return err
	}
	lookup, err := processingpersist.LookupSeedForMigration(processingpersist.DBSeedLookupInput{TorrentID: task.SourceID, SiteName: snapshot.Source.NativeName()}, s.migrate.repo)
	if err != nil {
		return err
	}
	if !strings.EqualFold(lookup.Hash, task.InfoHash) {
		return errors.New("原生资料载荷已改变")
	}
	upload := lookup.Normalized
	upload["downloader_id"] = task.DownloaderID
	upload["save_path"] = task.SavePath
	id := automationID("ctx")
	nativeContext := publishworkflow.BuildContextFromDBRow(id, task.SourceID, lookup.SiteName, task.InfoHash, lookup.Name, task.SavePath, task.DownloaderID, snapshot.Source.NativeName(), task.SourceID)
	nativeContext.OriginalTorrentPath = task.TorrentPath
	payload := map[string]any{"task_id": id, "sourceSite": snapshot.Source.NativeName(), "targetSite": snapshot.Target.NativeName(), "downloaderId": task.DownloaderID, "savePath": task.SavePath, "auto_add_to_downloader": false, "auto_add_existing_to_downloader": false, "auto_update_existing_torrent": false, "publish_trigger": "automation", "publish_scene": "automation", "torrent_id": task.SourceID}
	payloadJSON, _ := json.Marshal(payload)
	uploadJSON, _ := json.Marshal(upload)
	contextJSON, _ := json.Marshal(nativeContext)
	now := s.now().Format(repository.PublishQueueTimeLayout)
	queue := repository.PublishQueueTask{GroupID: task.RunID, Status: repository.PublishQueueStatusQueued, TaskID: id, Trigger: "automation", Scene: "automation", TorrentID: task.SourceID, SourceSite: snapshot.Source.NativeName(), TargetSite: snapshot.Target.NativeName(), DownloaderID: task.DownloaderID, Title: task.Title, PayloadJSON: string(payloadJSON), UploadDataJSON: string(uploadJSON), ContextJSON: string(contextJSON), NextRunAt: &now, CreatedAt: now, UpdatedAt: now}
	return s.repo.Enqueue(task, &queue, s.now())
}
