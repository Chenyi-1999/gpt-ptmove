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
	if run.Execute && run.Month != policy.Month(s.now()) {
		_ = s.repo.FinishRunLeased(run, "failed", "配额月份已改变，请重新登记扫描", "[]", 0, s.now())
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
	rows, _, err := s.scan(ctx, snapshot.Route.Settings)
	if err != nil {
		_ = s.repo.FinishRunLeased(run, "failed", err.Error(), "[]", 0, s.now())
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
	count := 0
	message := "只读扫描完成，没有预留或下载"
	state := "completed"
	if run.Execute {
		raw, _ := json.Marshal(snapshot.Route)
		for _, c := range rows {
			if ctx.Err() != nil {
				state = "failed"
				message = "扫描时间耗尽，已有预留保持不变"
				break
			}
			if !c.Eligible || (len(selected) > 0 && !selected[c.ID]) {
				continue
			}
			task := repository.AutomationTask{ID: automationID("auto"), RunID: run.ID, Month: run.Month, Source: snapshot.Route.Source.ID, Target: snapshot.Route.Target.ID, SourceID: c.ID, Title: c.Title, InfoHash: c.InfoHash, Size: c.Size, Seeders: c.Seeders, Official: c.Official, Snapshot: string(raw), DownloaderID: snapshot.Route.Settings.DownloaderID, SavePath: snapshot.Route.Settings.SavePath, State: "reserved", Explicit: run.Explicit, CreatedAt: s.now().UnixMilli(), UpdatedAt: s.now().UnixMilli()}
			err = s.repo.ReserveForRun(&task, snapshot.Route.Settings.MonthlyLimit, int64(snapshot.Route.Settings.MonthlyBudgetGiB*float64(policy.GiB)), run.ID, run.LeaseToken, s.now())
			if err != nil {
				if errors.Is(err, repository.ErrAutomationQuota) {
					message = "扫描完成，本月预留次数或预算已用完"
					break
				}
				if errors.Is(err, repository.ErrAutomationDuplicate) {
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
		}
		if message == "只读扫描完成，没有预留或下载" {
			message = "扫描完成，合格候选已预留"
		}
	}
	raw, _ := json.Marshal(rows)
	_ = s.repo.FinishRunLeased(run, state, message, string(raw), count, s.now())
}
