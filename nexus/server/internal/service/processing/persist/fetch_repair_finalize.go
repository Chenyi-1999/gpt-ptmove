package persist

import (
	"errors"
	"strings"
	"time"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

// FetchRepairFinalizeInput 定义“抓取修复 + 草稿收敛”组合流程输入。
type FetchRepairFinalizeInput struct {
	TaskID               string
	SavePath             string
	DownloaderID         string
	TorrentNameForPath   string
	ScreenshotReviewMode string
	RootConfig           map[string]any
	ReviewData           parser.ReviewExtractedData
	DetailHTML           string
	QuotedMediaInfo      string

	Draft          *SeedDraft
	MetaName       string
	SiteIdentifier string
}

// FetchRepairFinalizeDeps 定义“抓取修复 + 草稿收敛”组合流程依赖。
type FetchRepairFinalizeDeps struct {
	FetchRepairDeps processingrepair.FetchRepairDeps
	Now             time.Time

	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any
}

// FetchRepairFinalizeResult 定义“抓取修复 + 草稿收敛”组合流程输出。
type FetchRepairFinalizeResult struct {
	RepairResult          processingrepair.ParallelFetchRepairResult
	FinalizeResult        FinalizeFetchedSeedResult
	RestrictionPrecheck   FetchRestrictionPrecheckResult
	SkippedRepairByTagHit bool
}

// RunFetchRepairAndFinalize 执行抓取修复并将草稿收敛为可入库记录。
// 参数/返回：输入为草稿与修复所需上下文，依赖包含修复回调与标题组件构建函数；返回修复结果与最终收敛结果。
// 失败场景：Draft 为空或最终收敛失败时返回 error。
// 副作用：会发起网络请求/媒体分析（由 repair 依赖触发），并原地修改 Draft。
func RunFetchRepairAndFinalize(input FetchRepairFinalizeInput, deps FetchRepairFinalizeDeps) (FetchRepairFinalizeResult, error) {
	if input.Draft == nil {
		return FetchRepairFinalizeResult{}, errors.New("seed draft is nil")
	}

	quotedReport := ""
	if !processingmedia.HasUsableMediaReport(input.ReviewData.Mediainfo) {
		quotedReport = input.QuotedMediaInfo
		if quotedReport == "" {
			quotedReport = processingmedia.QuotedMediaInfo(parser.TorrentDescriptionBBCode(input.DetailHTML))
		}
		if quotedReport != "" {
			input.ReviewData.Mediainfo = quotedReport
			inferred := parser.InferStandardizedValues(input.ReviewData.Title, quotedReport, input.ReviewData.Body)
			for key, field := range map[string]*string{
				"video_codec": &input.ReviewData.VideoCodec,
				"audio_codec": &input.ReviewData.AudioCodec,
				"resolution":  &input.ReviewData.Resolution,
			} {
				if (*field == "" || strings.HasSuffix(*field, ".other")) && inferred[key] != "" {
					*field = inferred[key]
				}
			}
		}
	}
	input.Draft.ApplyReviewExtract(input.ReviewData, input.DetailHTML)
	restrictionPrecheck := DetectFetchRestrictionPrecheck(
		input.Draft,
		input.SiteIdentifier,
		input.SavePath,
		input.TorrentNameForPath,
		input.DownloaderID,
		input.RootConfig,
	)
	torrentName := strings.TrimSpace(input.Draft.Title)
	subtitle := strings.TrimSpace(input.Draft.Subtitle)
	imdbLink := strings.TrimSpace(input.Draft.IMDbLink)
	doubanLink := strings.TrimSpace(input.Draft.DoubanLink)
	tmdbLink := strings.TrimSpace(input.Draft.TMDbLink)

	repairResult := processingrepair.ParallelFetchRepairResult{
		ReviewData:             input.ReviewData,
		IMDbLink:               imdbLink,
		DoubanLink:             doubanLink,
		TMDbLink:               tmdbLink,
		ScreenshotReviewStatus: "none",
	}
	if restrictionPrecheck.Matched {
		if deps.FetchRepairDeps.EmitLog != nil {
			deps.FetchRepairDeps.EmitLog(
				input.TaskID,
				"标签预检",
				"检测到受限标签，已跳过海报/简介/截图修复: "+strings.Join(restrictionPrecheck.RestrictedTags, ", "),
				"warning",
			)
		}
	} else {
		repairResult = processingrepair.RunParallelFetchRepairs(
			processingrepair.ParallelFetchRepairInput{
				TaskID:               input.TaskID,
				SavePath:             input.SavePath,
				DownloaderID:         input.DownloaderID,
				TorrentNameForPath:   input.TorrentNameForPath,
				TorrentName:          torrentName,
				ScreenshotReviewMode: input.ScreenshotReviewMode,
				Subtitle:             subtitle,
				ReviewData:           input.ReviewData,
				IMDbLink:             imdbLink,
				DoubanLink:           doubanLink,
				TMDbLink:             tmdbLink,
			},
			deps.FetchRepairDeps,
		)
		input.Draft.ApplyRepairResult(repairResult)
	}

	finalizeResult, err := FinalizeFetchedSeed(FinalizeFetchedSeedInput{
		Draft:                      input.Draft,
		MetaName:                   input.MetaName,
		SiteIdentifier:             input.SiteIdentifier,
		SavePath:                   input.SavePath,
		DownloaderID:               input.DownloaderID,
		TorrentNameForPath:         input.TorrentNameForPath,
		RootConfig:                 input.RootConfig,
		Now:                        deps.Now,
		BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
	})
	if err != nil {
		return FetchRepairFinalizeResult{}, err
	}

	if quotedReport != "" && finalizeResult.MediainfoValid && input.Draft.Mediainfo == quotedReport && deps.FetchRepairDeps.EmitLog != nil {
		deps.FetchRepairDeps.EmitLog(input.TaskID, "补全 MediaInfo", "MediaInfo 从简介提取", "success")
	}
	return FetchRepairFinalizeResult{
		RepairResult:          repairResult,
		FinalizeResult:        finalizeResult,
		RestrictionPrecheck:   restrictionPrecheck,
		SkippedRepairByTagHit: restrictionPrecheck.Matched,
	}, nil
}
