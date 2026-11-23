package migrationflow

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const maintenanceMediaMaxSeeds = 100
const maintenanceMediaTaskKind = "maintenance_media_fill"
const maintenanceMediaLogModule = "维护-批量媒体补全"

var maintenanceMediaHashKey = regexp.MustCompile(`^(?:[0-9a-f]{40}|hash-[a-z0-9-]{1,59})$`)

// MaintenanceMediaSeed 标识一条现有维护记录。下载器 ID 仅用于核对列表选中时的归属。
type MaintenanceMediaSeed struct {
	Hash         string `json:"hash"`
	TorrentID    string `json:"torrent_id"`
	SiteName     string `json:"site_name"`
	DownloaderID string `json:"downloader_id"`
}

// StartMaintenanceMediaFill 登记一批选中的维护记录，返回全局任务标识。
// 一次只执行一个媒体批次，后台逐条检查缺项并写回已保存的维护资料。
func (s *MigrateService) StartMaintenanceMediaFill(requested []MaintenanceMediaSeed) (map[string]any, int) {
	seeds, err := normalizeMaintenanceMediaSeeds(requested)
	if err != nil {
		return maintenanceError(http.StatusBadRequest, err.Error())
	}
	if s.repo == nil || s.observations == nil {
		return maintenanceError(http.StatusServiceUnavailable, "维护任务暂不可用")
	}
	if !s.maintenanceMediaRunning.CompareAndSwap(false, true) {
		return maintenanceError(http.StatusConflict, "已有批量媒体补全在运行，可在首页「任务」查看进度")
	}
	task := s.observeTask(maintenanceMediaTaskKind, "批量补全截图和媒体信息", "/seed-maintenance")
	task.update("正在准备所选种子", fmt.Sprintf("共 %d 条", len(seeds)))
	go s.runMaintenanceMediaFill(task, seeds)
	return map[string]any{"success": true, "task_id": task.id, "task_key": "operation:" + task.id, "total": len(seeds)}, http.StatusAccepted
}

func normalizeMaintenanceMediaSeeds(requested []MaintenanceMediaSeed) ([]MaintenanceMediaSeed, error) {
	if len(requested) == 0 || len(requested) > maintenanceMediaMaxSeeds {
		return nil, fmt.Errorf("每次请选择 1 至 %d 条种子", maintenanceMediaMaxSeeds)
	}
	seeds := make([]MaintenanceMediaSeed, 0, len(requested))
	seen := make(map[[3]string]bool, len(requested))
	for _, seed := range requested {
		seed.Hash = strings.ToLower(strings.TrimSpace(seed.Hash))
		seed.TorrentID = strings.TrimSpace(seed.TorrentID)
		seed.SiteName = strings.TrimSpace(seed.SiteName)
		seed.DownloaderID = strings.TrimSpace(seed.DownloaderID)
		if !maintenanceMediaHashKey.MatchString(seed.Hash) || !maintenanceTorrentIDPattern.MatchString(seed.TorrentID) ||
			seed.SiteName == "" || len(seed.SiteName) > 128 || strings.ContainsAny(seed.SiteName, "\x00\r\n") ||
			len(seed.DownloaderID) > 128 || strings.ContainsAny(seed.DownloaderID, "\x00\r\n") {
			return nil, fmt.Errorf("选中的种子标识无效，请刷新维护列表后重试")
		}
		key := [3]string{seed.Hash, seed.TorrentID, seed.SiteName}
		if seen[key] {
			continue
		}
		seen[key] = true
		seeds = append(seeds, seed)
	}
	return seeds, nil
}

// runMaintenanceMediaFill 逐条处理选中的维护种子并汇总结果。单条失败后继续后续记录。
func (s *MigrateService) runMaintenanceMediaFill(task taskObservation, seeds []MaintenanceMediaSeed) {
	defer s.maintenanceMediaRunning.Store(false)
	defer task.finish(false, "批量补全已中断", "")
	filled, skipped, partial, failed := 0, 0, 0, 0
	lastIssue := ""
	for i, seed := range seeds {
		position := fmt.Sprintf("第 %d/%d 条", i+1, len(seeds))
		identity := fmt.Sprintf("%s #%s", seed.SiteName, seed.TorrentID)
		report := func(phase string, detail string) {
			message := position + " · " + identity + " · " + phase
			done := fmt.Sprintf("已处理 %d/%d 条", i, len(seeds))
			if detail != "" {
				done += " · " + detail
			}
			task.update(message, done)
		}
		report("核对缺失资料", "")
		count, issues := s.fillMaintenanceMediaSeed(seed, report)
		switch {
		case len(issues) == 0 && count > 0:
			filled++
		case len(issues) == 0:
			skipped++
		case count > 0:
			partial++
		default:
			failed++
		}
		if len(issues) != 0 {
			lastIssue = identity + "：" + strings.Join(issues, "；")
			logx.Warnf(maintenanceMediaLogModule, "批次=%s 站点=%s 种子ID=%s 未完成项=%s", task.id, seed.SiteName, seed.TorrentID, strings.Join(issues, "；"))
		}
	}
	summary := fmt.Sprintf("共 %d 条 · 已补全 %d · 已有资料 %d · 部分补全 %d · 失败 %d", len(seeds), filled, skipped, partial, failed)
	message := "批量补全已完成"
	outcome := ""
	if lastIssue != "" {
		message = "批量处理结束 · 最近未完成 " + lastIssue
		if filled+partial > 0 {
			outcome = "partial"
		}
	}
	task.finishWithOutcome(lastIssue == "", message, summary, outcome)
}
