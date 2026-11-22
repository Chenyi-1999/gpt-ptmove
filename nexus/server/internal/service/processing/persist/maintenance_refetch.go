package persist

import (
	"errors"
	"strings"

	extract "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// PrepareMaintenanceSeedRecord 提取并修复当前源站资料，生成可更新维护记录的字段。
// 详情无法提取时返回错误。使用交互式截图检查，执行过程中不写种子参数表或启动入库后任务。
func PrepareMaintenanceSeedRecord(input FetchStoreProcessInput, deps FetchStoreProcessDeps, previous map[string]any) (map[string]any, error) {
	acquired := input.AcquireResult
	if strings.TrimSpace(acquired.DetailHTML) == "" {
		return nil, errors.New("未读取到源站详情，请检查站点登录状态后重试")
	}
	extracted := extract.ExtractReviewData(input.ExtractorEngine, extract.Input{
		SiteCode: acquired.SiteIdentifier, SiteNickname: acquired.Nickname,
		BaseURL:   toStringAny(acquired.SourceInfo["base_url"], ""),
		Cookie:    toStringAny(acquired.SourceInfo["cookie"], ""),
		TorrentID: input.SearchTerm, PageHTML: acquired.DetailHTML,
	})
	review := extracted.ReviewData
	quotedReport := ""
	if !processingmedia.HasUsableMediaReport(review.Mediainfo) {
		quotedReport = processingmedia.QuotedMediaInfo(extract.TorrentDescriptionBBCode(acquired.DetailHTML))
	}
	if firstNonEmpty(review.Body, review.Statement, review.Poster, review.Screens, review.Mediainfo, review.Subtitle,
		review.IMDbLink, review.DoubanLink, review.TMDbLink, quotedReport) == "" {
		return nil, errors.New("源站详情没有提取到可更新的种子资料，请检查种子 ID 或站点配置")
	}
	if !processingmedia.HasUsableMediaReport(review.Mediainfo) {
		if previousReport := toStringAny(previous["mediainfo"], ""); processingmedia.HasUsableMediaReport(previousReport) {
			review.Mediainfo = previousReport
		}
	}
	draft := NewSeedDraft(acquired.Meta.InfoHash, input.SearchTerm, acquired.SiteIdentifier, acquired.Nickname)
	draft.Title = firstNonEmpty(toStringAny(previous["title"], ""), acquired.TorrentName)
	draft.TorrentFileNames = acquired.TorrentFileNames
	if review.Screens == "" {
		review.Screens = toStringAny(previous["screenshots"], "")
	}
	prepared, err := RunFetchRepairAndFinalize(FetchRepairFinalizeInput{
		TaskID: input.TaskID, SavePath: acquired.SavePath, DownloaderID: acquired.DownloaderID,
		TorrentNameForPath:   acquired.TorrentNameForPath,
		ScreenshotReviewMode: processingshared.ScreenshotReviewModeInteractive,
		RootConfig:           deps.FetchRepairDeps.RootConfig, ReviewData: review, DetailHTML: acquired.DetailHTML, QuotedMediaInfo: quotedReport,
		Draft: draft, MetaName: acquired.Meta.Name, SiteIdentifier: acquired.SiteIdentifier,
	}, FetchRepairFinalizeDeps{
		FetchRepairDeps: deps.FetchRepairDeps, Now: deps.Now,
		BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
	})
	if err != nil {
		return nil, err
	}
	record := prepared.FinalizeResult.Record
	normalizeAndApplyTeamAcknowledgment(draft, acquired.SiteIdentifier, record, FetchPersistPipelineDeps{
		Repo: deps.Repo, EmitLog: deps.EmitLog,
	})
	return maintenanceUpdates(previous, record, prepared.FinalizeResult.MediainfoValid), nil
}

func maintenanceUpdates(previous, record map[string]any, mediaValid bool) map[string]any {
	updates := map[string]any{}
	for _, field := range []string{"title", "subtitle", "imdb_link", "douban_link", "tmdb_link", "type", "medium",
		"video_codec", "audio_codec", "resolution", "team", "source", "tags", "poster", "statement", "body",
		"screenshots", "title_components", "removed_ardtudeclarations"} {
		value := strings.TrimSpace(toStringAny(record[field], ""))
		if value != "" && value != "[]" && value != toStringAny(previous[field], "") {
			updates[field] = value
		}
	}
	if _, changed := updates["screenshots"]; changed {
		updates["screenshot_review_status"] = record["screenshot_review_status"]
	} else if record["screenshot_review_status"] == processingshared.ScreenshotReviewStatusPending &&
		previous["screenshot_review_status"] != processingshared.ScreenshotReviewStatusPending {
		updates["screenshot_review_status"] = processingshared.ScreenshotReviewStatusPending
	}
	if mediaValid && toStringAny(previous["bdinfo_task_id"], "") == "" &&
		toStringAny(record["mediainfo"], "") != toStringAny(previous["mediainfo"], "") {
		for _, field := range []string{"mediainfo", "mediainfo_status", "bdinfo_error", "bdinfo_started_at", "bdinfo_completed_at"} {
			updates[field] = record[field]
		}
	}
	if len(updates) > 0 {
		updates["updated_at"] = record["updated_at"]
		updates["is_reviewed"] = false
	}
	return updates
}
