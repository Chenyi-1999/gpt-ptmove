package migrationflow

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server/internal/config"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

func (s *MigrateService) ParseTitle(title string, mediainfo string, requestID string) (map[string]any, int) {
	return processingtitle.ParseTitleEntry(processingtitle.ParseTitleEntryInput{
		Title:     strings.TrimSpace(title),
		Mediainfo: strings.TrimSpace(mediainfo),
		RequestID: requestID,
		LogModule: "迁移-标题解析",
	})
}

func (s *MigrateService) MediaValidate(payload map[string]any) (map[string]any, int) {
	mediaType := strings.TrimSpace(processingshared.ToString(payload["type"], ""))
	var task taskObservation
	switch mediaType {
	case "screenshot_preview":
		task = s.observeTask("screenshot", "生成候选截图", "")
	case "screenshot_finalize":
		task = s.observeTask("screenshot", "生成所选截图", "")
	case "screenshot":
		task = s.observeTask("screenshot", "获取截图", "")
	case "poster":
		task = s.observeTask("media", "获取海报", "")
	case "intro":
		task = s.observeTask("media", "获取简介", "")
	}
	defer task.finish(false, "获取未完成", "")
	result, status := processingrepair.MediaValidateEntry(payload, processingrepair.MediaValidateEntryDeps{
		ScreenshotProgress: func(stage string, completed, total int) { recordScreenshotProgress(task, stage, completed, total) },
		GetRootConfig: func() map[string]any {
			if s == nil || s.cfg == nil {
				return map[string]any{}
			}
			return s.cfg.Get()
		},
		GetPTGenSources: s.ptgenSources,
		GetSeedMedia: func(seedID string) string {
			hash, torrentID, siteName, parseErr := processingpersist.ParseSeedID(seedID)
			if parseErr != nil {
				return ""
			}
			row, rowErr := s.repo.GetSeedParameterByKey(hash, torrentID, siteName)
			if rowErr != nil {
				return ""
			}
			return strings.TrimSpace(processingshared.ToString(row["mediainfo"], ""))
		},
	})
	if status >= 400 || result["success"] != true {
		task.finish(false, "获取失败", "")
		return result, status
	}
	progress := ""
	if candidates, ok := result["candidates"].([]processingrepair.ScreenshotPreviewCandidate); ok {
		progress = fmt.Sprintf("已返回 %d 张候选截图", len(candidates))
	} else if images, ok := result["screenshots"].(string); ok {
		progress = fmt.Sprintf("已获取 %d 张截图", strings.Count(strings.ToLower(images), "[/img]"))
	}
	task.finish(true, "获取完成", progress)
	return result, status
}

// recordScreenshotProgress 显示处理器实际回传的计数，远程旧客户端仅显示可确认的阶段。
func recordScreenshotProgress(task taskObservation, stage string, completed, total int) {
	message, progress := screenshotTaskProgress(stage, completed, total)
	if message != "" {
		task.update(message, progress)
	}
}

func screenshotTaskProgress(stage string, completed, total int) (string, string) {
	switch stage {
	case "locating":
		return "正在定位视频文件", ""
	case "capturing", "uploading":
		verb := "截取"
		if stage == "uploading" {
			verb = "上传"
		}
		label := "正在" + verb + "截图"
		progress := ""
		if completed >= 0 && total > 0 {
			progress = fmt.Sprintf("已%s %d/%d 张", verb, completed, total)
		}
		return label, progress
	case "remote", "remote_capture", "remote_upload":
		label := "飞牛正在处理截图"
		if stage == "remote_capture" {
			label = "飞牛正在生成候选截图"
		} else if stage == "remote_upload" {
			label = "飞牛正在生成并上传所选截图"
		}
		progress := ""
		if total > 0 {
			progress = fmt.Sprintf("计划 %d 张，等待客户端回传", total)
		}
		return label, progress
	}
	return "", ""
}

func (s *MigrateService) ptgenSources() []config.PTGenSource {
	if s == nil || s.cfg == nil {
		return nil
	}
	return s.cfg.PTGenSources()
}

func (s *MigrateService) RefreshMediainfoAsync(payload map[string]any) (map[string]any, int) {
	task := s.observeTask("mediainfo", "获取 MediaInfo", "")
	defer task.finish(false, "MediaInfo 获取未完成", "")
	task.update("正在核对媒体报告", "")
	result, status := s.refreshMediainfoAsync(payload)
	if status < 400 && result["success"] == true {
		message := "MediaInfo 获取完成"
		if bdinfo, ok := result["bdinfo_async"].(map[string]any); ok && bdinfo["bdinfo_status"] == "processing" {
			message = "已交由 BDInfo 任务提取"
		}
		task.finish(true, message, "")
	}
	return result, status
}
