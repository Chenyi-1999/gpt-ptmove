package migrationflow

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

type maintenanceMediaSource struct {
	downloader downloaderclient.Downloader
	remote     bool
	videoPath  string
	localFile  string
	config     map[string]any
}

// fillMaintenanceMediaSeed 补全一条确切的维护记录，两类资料独立保存。
// 已有资料跳过。补全失败或人工同时编辑时保留原值。
func (s *MigrateService) fillMaintenanceMediaSeed(seed MaintenanceMediaSeed, progress func(string, string)) (int, []string) {
	row, err := s.repo.GetMaintenanceSeed(seed.TorrentID, seed.SiteName, seed.Hash)
	if err != nil {
		return 0, []string{"维护记录无法读取或已变化"}
	}
	count := 0
	var issues []string
	apply := func(field string, expected, updates map[string]any) {
		s.maintenanceMediaWrites.Lock()
		updated, err := s.repo.UpdateMaintenanceMedia(
			mediaFieldForCompare(row["hash"]), seed.TorrentID, seed.SiteName, expected, updates,
		)
		s.maintenanceMediaWrites.Unlock()
		switch {
		case err != nil:
			issues = append(issues, field+"保存失败")
		case !updated:
			issues = append(issues, field+"已在补全过程中变更，保留当前资料")
		default:
			count++
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	var source *maintenanceMediaSource
	var sourceErr error
	loadSource := func() (*maintenanceMediaSource, error) {
		if source == nil && sourceErr == nil {
			progress("正在核对 qB 中的完整视频", "")
			source, sourceErr = s.locateMaintenanceMedia(ctx, seed)
		}
		return source, sourceErr
	}
	if !processingmedia.HasUsableMediaReport(mediaFieldForCompare(row["mediainfo"])) {
		progress("正在补全 MediaInfo", "")
		report, expected, err := maintenanceMediaReport(ctx, row, loadSource, progress)
		if err != nil {
			issues = append(issues, "MediaInfo："+err.Error())
		} else {
			apply("MediaInfo", expected, map[string]any{"mediainfo": report, "mediainfo_status": "completed"})
		}
	}
	// 已有可用截图（含人工选图及旧版本保存的较少张数）保持原样。
	if automationScreenshotPayload(mediaFieldForCompare(row["screenshots"]), processingshared.ScreenshotReviewStatusConfirmed) != nil {
		progress("正在补全截图", "")
		located, locateErr := loadSource()
		if locateErr != nil {
			issues = append(issues, "截图："+locateErr.Error())
		} else {
			shots, shotErr := located.screenshots(ctx, progress)
			if shotErr != nil {
				issues = append(issues, "截图："+shotErr.Error())
			} else {
				apply("截图",
					map[string]any{"screenshots": row["screenshots"], "screenshot_review_status": row["screenshot_review_status"]},
					map[string]any{"screenshots": shots, "screenshot_review_status": processingshared.ScreenshotReviewStatusNone})
			}
		}
	}
	return count, issues
}

func maintenanceMediaReport(ctx context.Context, row map[string]any, loadSource func() (*maintenanceMediaSource, error), progress func(string, string)) (string, map[string]any, error) {
	status := strings.ToLower(strings.TrimSpace(mediaFieldForCompare(row["mediainfo_status"])))
	if status == "processing" || status == "processing_bdinfo" {
		return "", nil, errors.New("已有媒体分析任务在运行")
	}
	expected := map[string]any{
		"mediainfo": row["mediainfo"], "mediainfo_status": row["mediainfo_status"], "bdinfo_task_id": row["bdinfo_task_id"],
	}
	description := mediaFieldForCompare(row["statement"]) + "\n" + mediaFieldForCompare(row["body"])
	if report := processingmedia.QuotedMediaInfo(description); report != "" {
		expected["statement"], expected["body"] = row["statement"], row["body"]
		return sanitizeAutomationMediaReport(report), expected, nil
	}
	source, err := loadSource()
	if err != nil {
		return "", nil, err
	}
	report, err := source.report(ctx, progress)
	return report, expected, err
}

// locateMaintenanceMedia 以现有缓存定位所属下载器，并以 qB 同哈希的内容和文件清单核实视频。
// 不使用请求路径，不遍历共用保存目录。找不到对应完整文件则停止采集。
func (s *MigrateService) locateMaintenanceMedia(ctx context.Context, seed MaintenanceMediaSeed) (*maintenanceMediaSource, error) {
	if !maintenanceTorrentHashPattern.MatchString(seed.Hash) {
		return nil, errors.New("维护记录没有可核验的 qB 种子哈希")
	}
	if s.cfg == nil {
		return nil, errors.New("请先配置种子使用的下载器")
	}
	id, err := s.repo.FindMaintenanceMediaDownloader(seed.Hash, seed.DownloaderID)
	if err != nil {
		return nil, errors.New("当前下载器缓存中没有这条种子")
	}
	root := s.cfg.Get()
	downloader, decision, err := downloaderclient.DecideProxy(root, id)
	if err != nil || downloader.Type != "qbittorrent" {
		return nil, errors.New("该种子没有可用的 qB 下载器配置")
	}
	if decision.Enabled && !isPublishedEditPullHub(downloader.Host) {
		return nil, errors.New("该下载器未使用已配置的 HTTPS 领单通道")
	}
	status, err := downloader.TorrentStatus(seed.Hash)
	if err != nil || !status.Found || !strings.EqualFold(status.Hash, seed.Hash) || status.Progress != 1 || status.AmountLeft != 0 {
		return nil, errors.New("qB 尚未确认该种子已完整下载")
	}
	contentPath, err := preparedMediaPath(status)
	if err != nil {
		return nil, err
	}
	file, err := downloader.CompletedMediaPath(seed.Hash, status.SavePath, contentPath)
	if err != nil {
		return nil, errors.New("该种子中未确认有完整可读取的视频")
	}
	source := &maintenanceMediaSource{downloader: downloader, remote: decision.Enabled, videoPath: file, config: root}
	if source.remote {
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		available, err := downloader.CheckMediaPathByProxy(check, file)
		if err != nil || !available {
			return nil, errors.New("领单客户端暂时无法读取这条种子的视频")
		}
	} else {
		source.localFile = downloaderclient.TranslateDownloaderPath(root, id, file)
		info, err := os.Stat(source.localFile)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("服务端找不到下载器路径对应的视频文件")
		}
	}
	return source, nil
}

func (s *maintenanceMediaSource) report(ctx context.Context, progress func(string, string)) (string, error) {
	var report string
	var err error
	fileName := s.videoPath
	if s.remote {
		progress("领单客户端正在提取 MediaInfo", "")
		var isBDMV bool
		report, isBDMV, err = s.downloader.FetchMediaInfoByProxyContext(ctx, s.videoPath, path.Base(s.videoPath))
		if isBDMV {
			return "", errors.New("原盘请使用维护详情的 MediaInfo 重新获取入口")
		}
	} else {
		progress("服务端正在提取 MediaInfo", "")
		report, err = processingmedia.ExtractMediaInfo(s.localFile)
		fileName = filepath.ToSlash(s.localFile)
	}
	if err != nil || !validAutomationMediaReport(report, fileName) {
		return "", errors.New("未取得属于该视频的完整媒体信息")
	}
	return sanitizeAutomationMediaReport(report), nil
}

func (s *maintenanceMediaSource) screenshots(ctx context.Context, progress func(string, string)) (string, error) {
	var raw string
	var err error
	if s.remote {
		progress("领单客户端正在生成并上传截图", "计划 5 张")
		raw, err = s.downloader.FetchScreenshotsByProxyContext(ctx, s.videoPath, path.Base(s.videoPath), nil)
	} else {
		var urls []string
		urls, err = processingrepair.GenerateAndUploadScreenshots(processingrepair.ScreenshotGenerateInput{
			Payload:    map[string]any{"save_path": s.videoPath, "downloader_id": s.downloader.ID},
			RootConfig: s.config,
			Progress: func(stage string, completed, total int) {
				if label, details := screenshotTaskProgress(stage, completed, total); label != "" {
					progress(label, details)
				}
			},
		})
		raw = processingrepair.ToBBCodeImages(urls)
	}
	if err != nil {
		return "", errors.New("截图或 Pixhost 上传未完成")
	}
	return maintenanceFiveScreenshots(raw)
}

func maintenanceFiveScreenshots(raw string) (string, error) {
	bbcode, err := automationPixhostScreenshots(raw)
	if err != nil || len(processingrepair.ExtractImageURLsFromText(bbcode)) != 5 {
		return "", errors.New("没有收到 5 张不同的有效 Pixhost 截图，原资料已保留")
	}
	return bbcode, nil
}
