package migrationflow

import (
	"context"
	"fmt"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// observeAutomationScan 将扫描器的详情和分页计数转成同一任务的实时进度。
func observeAutomationScan(ctx context.Context, task taskObservation) context.Context {
	return acquireautomation.WithScanProgress(ctx, func(update acquireautomation.ScanProgress) {
		progress := fmt.Sprintf("已处理 %d 条详情", update.Completed)
		switch {
		case update.Stage == "listing" && update.Page > 0:
			task.update(fmt.Sprintf("正在读取源站第 %d 页", update.Page), progress)
		case update.Stage == "listing":
			task.update("正在读取源站列表", "")
		case update.Current > 0:
			task.update(fmt.Sprintf("正在核验第 %d 条种子详情", update.Current), progress)
		default:
			task.update("正在核验种子详情", progress)
		}
	})
}

func eligibleCandidates(rows []policy.Candidate) int {
	count := 0
	for _, row := range rows {
		if row.Eligible {
			count++
		}
	}
	return count
}

func candidateResultProgress(rows []policy.Candidate) string {
	eligible := eligibleCandidates(rows)
	return fmt.Sprintf("共 %d 条 · 可选 %d 条 · 已排除 %d 条", len(rows), eligible, len(rows)-eligible)
}

// automationMonitorEntry 保留现有 qB 实测百分比，并把自动化执行纳入全局任务视图。
func automationMonitorEntry(task repository.AutomationTaskProgress) map[string]any {
	states := map[string]string{
		"reserved": "待下载", "downloading": "下载中", "waiting_batch": "等待 IYUU 批量查重",
		"preparing": "准备发布资料", "waiting_media": "等待补齐媒体资料", "ready": "资料已就绪",
		"queued_publish": "等待发布", "publishing": "发布中", "published": "等待做种确认",
		"seeding": "确认做种中", "reconciling": "核对中", "duplicate_cleanup": "处理重复下载",
	}
	phase := states[task.State]
	progress := phase
	status, outcome := "running", ""
	switch task.State {
	case "failed":
		status = "failed"
	case "needs_review":
		status, outcome = "failed", "incomplete"
	case "cancelled":
		status, outcome = "success", "cancelled"
	case repository.AutomationStateAlreadyExists:
		status, outcome = "success", "exists"
	}
	if task.State == "downloading" && task.DownloadProgress != nil && *task.DownloadProgress >= 0 && *task.DownloadProgress <= 1 {
		progress = fmt.Sprintf("下载进度 %.0f%%", *task.DownloadProgress*100)
	}
	return map[string]any{"key": "automation:" + task.ID, "kind": "automation", "title": task.Title,
		"status": status, "outcome": outcome, "message": task.Message, "progress_text": progress, "updated_at": task.UpdatedAt,
		"started_at": task.CreatedAt, "route_target": map[string]string{"path": "/settings/automation"}}
}
