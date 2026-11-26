package migrationflow

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pt-nexus/server/internal/repository"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// processRuns 将持久扫描意图在事务外执行；每个候选预留都匹配扫描租约与执行授权。
func (s *AutomationService) processRuns(ctx context.Context, current policy.Settings) {
	runs, err := s.repo.PendingRuns()
	if err != nil {
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		if run.Scheduled && !current.Enabled {
			continue
		}
		token := automationID("scan")
		claimed, err := s.repo.ClaimRun(run.ID, token, s.now())
		if err != nil {
			continue
		}
		s.scanRun(ctx, claimed)
	}
}

// scanRun 冻结路由、只读候选扫描和原子预留分离，API 幂等事务不会持有网络请求。
func (s *AutomationService) scanRun(ctx context.Context, run repository.AutomationRun) {
	if run.Execute && s.requirePluginEnabled(ctx) != nil {
		_ = s.repo.PauseRunLeased(run, run.Selected)
		return
	}
	var snapshot automationRunSnapshot
	if json.Unmarshal([]byte(run.Snapshot), &snapshot) != nil {
		_ = s.repo.FinishRunLeased(run, "failed", "扫描快照损坏", "[]", 0, s.now())
		return
	}
	if err := s.validateFrozen(snapshot.Route); err != nil {
		_ = s.repo.FinishRunLeased(run, "failed", err.Error(), "[]", 0, s.now())
		return
	}
	batchRoute := usesIYUUBatches(snapshot.Route)
	batch := run.Execute && batchRoute
	expired := run.Execute && run.Month != policy.Month(s.now())
	prior := 0
	var excludedIDs map[string]bool
	if batchRoute {
		var err error
		prior, err = s.repo.ReservedForCurrentBatch(run)
		if err != nil {
			return
		}
		if prior >= snapshot.Route.Settings.BatchSize || (prior > 0 && (!batch || expired || !s.iyuuConfigured())) {
			_ = s.repo.FinishRunLeased(run, "batch_active", "本批已预留，恢复下载和 IYUU 查重", run.CandidatesJSON, run.Selected+prior, s.now())
			return
		}
	}
	if batch {
		if !s.iyuuConfigured() {
			_ = s.repo.FinishRunLeased(run, "failed", "IYUU 令牌未配置，未开始下载", "[]", run.Selected, s.now())
			return
		}
		var err error
		excludedIDs, err = s.repo.BatchExcludedSourceIDs(snapshot.Route.Source.ID, snapshot.Route.Target.ID, run.Month)
		if err != nil {
			return
		}
	}
	if expired {
		_ = s.repo.FinishRunLeased(run, "completed", "本月计划已结束", run.CandidatesJSON, run.Selected, s.now())
		return
	}
	observed := s.migrate.observeTask("candidate_scan", "执行自动化候选扫描", automationPluginRoute)
	defer observed.finish(false, "候选扫描未完成", "")
	rows, _, err := s.scan(ctx, snapshot.Route.Settings, observed, excludedIDs)
	if run.Execute && errors.Is(s.requirePluginEnabled(ctx), repository.ErrAutomationPluginDisabled) {
		_ = s.repo.PauseRunLeased(run, run.Selected)
		observed.finish(false, "自动转种插件已停用，扫描等待恢复", "")
		return
	}
	if err != nil {
		state := "failed"
		message := err.Error()
		if batch && prior > 0 {
			state = "batch_active"
			message = "候选补扫失败，继续核对已预留的本批任务"
		}
		_ = s.repo.FinishRunLeased(run, state, message, "[]", run.Selected+prior, s.now())
		return
	}
	if err = s.validateFrozen(snapshot.Route); err != nil {
		_ = s.repo.FinishRunLeased(run, "failed", err.Error(), "[]", 0, s.now())
		return
	}
	selected := map[string]bool{}
	for _, id := range snapshot.SelectedIDs {
		selected[id] = true
	}
	count := prior
	message := "只读扫描完成，没有预留或下载"
	state := "completed"
	if run.Execute {
		observed.update("正在登记合格候选", candidateResultProgress(rows))
		raw, _ := json.Marshal(snapshot.Route)
		for index := range rows {
			c := &rows[index]
			if ctx.Err() != nil {
				state = "failed"
				message = "扫描时间耗尽，已有预留保持不变"
				break
			}
			if !c.Eligible || (len(selected) > 0 && !selected[c.ID]) {
				continue
			}
			task := repository.AutomationTask{ID: automationID("auto"), RunID: run.ID, Month: run.Month, Source: snapshot.Route.Source.ID, Target: snapshot.Route.Target.ID, SourceID: c.ID, Title: c.Title, InfoHash: c.InfoHash, Size: c.Size, Seeders: c.Seeders, Official: c.Official, Snapshot: string(raw), DownloaderID: snapshot.Route.Settings.DownloaderID, SavePath: snapshot.Route.Settings.SavePath, State: "reserved", Explicit: run.Explicit, CreatedAt: s.now().UnixMilli(), UpdatedAt: s.now().UnixMilli(), BatchCheck: batch}
			err = s.repo.ReserveForRun(&task, snapshot.Route.Settings.MonthlyLimit, 0, run.ID, run.LeaseToken, s.now())
			if err != nil {
				if errors.Is(err, repository.ErrAutomationPluginDisabled) {
					selected := run.Selected
					if !batch {
						selected += count
					}
					_ = s.repo.PauseRunLeased(run, selected)
					observed.finish(false, "自动转种插件已停用，保留已有预留", "")
					return
				}
				if errors.Is(err, repository.ErrAutomationQuota) {
					message = "扫描结束，本月发布名额已满或已预留"
					break
				}
				if errors.Is(err, repository.ErrAutomationDuplicate) {
					c.Eligible = false
					c.Reasons = append(c.Reasons, "源种子已有下载记录或已被其他自动任务预留")
					continue
				}
				if errors.Is(err, repository.ErrAutomationFence) {
					message = "扫描授权或租约已改变，停止新增任务"
					break
				}
				message = "任务预留失败，已有任务保持不变"
				state = "failed"
				break
			}
			count++
			if batch && count >= snapshot.Route.Settings.BatchSize {
				break
			}
		}
		if message == "只读扫描完成，没有预留或下载" {
			message = "扫描范围内暂无符合规则且可新增的候选"
			if count > 0 {
				message = "扫描完成，合格候选已预留"
			}
		}
		if batch && count > 0 {
			state = "batch_active"
			message = "本批候选已预留，等待下载与 IYUU 批量查重"
		}
	}
	raw, _ := json.Marshal(rows)
	if err := s.repo.FinishRunLeased(run, state, message, string(raw), run.Selected+count, s.now()); err == nil {
		observed.finish(state != "failed", message, candidateResultProgress(rows))
	}
}
