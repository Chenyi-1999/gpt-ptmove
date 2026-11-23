package migrationflow

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

var fullMediaName = regexp.MustCompile(`(?mi)^([ \t]*Complete name[ \t]*:[ \t]*)([^\r\n]*)`)

// PublishedEditMediaRequest 选择一次远程媒体生成需要的资料。
type PublishedEditMediaRequest struct {
	Screenshots bool `json:"screenshots"`
	MediaInfo   bool `json:"mediainfo"`
}

// GeneratePublishedEditMedia 验证目标归属并由现有飞牛领单通道生成草稿资料。
func (s *MigrateService) GeneratePublishedEditMedia(ctx context.Context, logID uint64, request PublishedEditMediaRequest) (map[string]any, int) {
	fail := func(status int, message string) (map[string]any, int) {
		return publishedEditError(status, message), status
	}
	if !request.Screenshots && !request.MediaInfo {
		return fail(http.StatusBadRequest, "请选择要补充的媒体资料")
	}
	observed := s.observeTask("published_media", "补充已发布种子的媒体资料", "")
	defer observed.finish(false, "媒体资料未能生成", "")
	observed.update("正在核对种子归属和视频文件", "")
	target, failure, status := s.resolvePublishedEdit(logID)
	if failure != nil {
		return failure, status
	}
	if _, err := target.adapter.Read(ctx, target.url); err != nil {
		return publishedEditAdapterError(err)
	}

	row := target.row
	hashes, err := s.repo.PublishedMediaHashes(row.TorrentID, strings.ToLower(strings.TrimSpace(row.SourceSite)))
	if err != nil {
		return fail(http.StatusInternalServerError, "读取发布对应的原始媒体记录失败")
	}
	if len(hashes) != 1 {
		return fail(http.StatusConflict, "找不到唯一的原始媒体记录，可手动填写截图和 MediaInfo")
	}
	hash := strings.ToLower(strings.TrimSpace(hashes[0]))
	if !maintenanceTorrentHashPattern.MatchString(hash) {
		return fail(http.StatusConflict, "原始媒体记录缺少有效种子哈希")
	}

	downloaderID := strings.TrimSpace(row.DownloaderID)
	if downloaderID == "" {
		if cached, lookupErr := s.repo.GetCurrentTorrentByHash(hash); lookupErr == nil {
			downloaderID = strings.TrimSpace(processingshared.ToString(cached["downloader_id"], ""))
		}
	}
	if s.cfg == nil {
		return fail(http.StatusServiceUnavailable, "飞牛领单通道未配置")
	}
	downloader, decision, err := downloaderclient.DecideProxy(s.cfg.Get(), downloaderID)
	if err != nil || !decision.Enabled || downloader.Type != "qbittorrent" || !isPublishedEditPullHub(downloader.Host) {
		return fail(http.StatusConflict, "原始媒体没有配置可用的飞牛领单下载器，可手动填写资料")
	}

	torrent, err := downloader.TorrentStatus(hash)
	if err != nil {
		return fail(http.StatusBadGateway, "飞牛暂未能按种子哈希读取 qB 状态")
	}
	if !torrent.Found || !strings.EqualFold(torrent.Hash, hash) || torrent.Progress != 1 || torrent.AmountLeft != 0 {
		return fail(http.StatusConflict, "飞牛 qB 中未找到这条种子的已完整下载文件")
	}
	remotePath, err := preparedMediaPath(torrent)
	if err != nil {
		return fail(http.StatusConflict, err.Error())
	}

	available, err := downloader.CheckMediaPathByProxy(ctx, remotePath)
	if err != nil {
		return fail(http.StatusBadGateway, "飞牛暂时无法检查媒体文件，请检查领单客户端状态")
	}
	if !available {
		return fail(http.StatusConflict, "飞牛媒体容器找不到 qB 记录的文件。请检查文件是否仍在，且已按 qB 的路径挂载到媒体容器。")
	}

	out := map[string]any{"success": true}
	fieldErrors := map[string]string{}
	if request.MediaInfo {
		observed.update("飞牛正在提取 MediaInfo", "")
		report, isBDMV, mediaErr := downloader.FetchMediaInfoByProxy(remotePath, torrent.Name)
		isMediaInfo, _, _ := processingmedia.ValidateMediaInfoFormat(report)
		if mediaErr != nil || isBDMV || !isMediaInfo {
			fieldErrors["mediainfo"] = "飞牛未返回有效的 MediaInfo 文本"
		} else {
			out["mediainfo"] = sanitizePublishedMediaInfo(report)
		}
	}
	if request.Screenshots {
		observed.update("飞牛正在生成并上传截图", "等待客户端下载结果")
		bbcode, shotErr := downloader.FetchScreenshotsByProxy(remotePath, torrent.Name, nil)
		if shotErr != nil {
			fieldErrors["screenshot_bbcode"] = "飞牛截图或上传图床失败"
		} else {
			out["screenshot_bbcode"] = bbcode
		}
	}
	_, hasMedia := out["mediainfo"]
	_, hasScreenshots := out["screenshot_bbcode"]
	if len(fieldErrors) > 0 {
		out["field_errors"] = fieldErrors
		out["message"] = "部分媒体资料未能生成，可重试对应项目或手动填写"
	}
	if !hasMedia && !hasScreenshots {
		// 没有可用结果时返回失败，仍保留每项失败原因。
		out["success"] = false
		out["message"] = "飞牛未生成可用资料，可检查客户端状态后重试"
		return out, http.StatusBadGateway
	}
	logx.Infof(publishedEditLogModule, "媒体草稿请求完成 日志ID=%d 失败项=%d", logID, len(fieldErrors))
	if len(fieldErrors) > 0 {
		observed.finish(false, "部分资料已生成", fmt.Sprintf("%d 项待重试", len(fieldErrors)))
	} else {
		progress := ""
		if shots, ok := out["screenshot_bbcode"].(string); ok {
			progress = fmt.Sprintf("已返回 %d 张截图", strings.Count(strings.ToLower(shots), "[/img]"))
		}
		observed.finish(true, "媒体草稿已准备", progress)
	}
	return out, http.StatusOK
}

func isPublishedEditPullHub(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "ptmove-pull-hub", "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func sanitizePublishedMediaInfo(report string) string {
	return fullMediaName.ReplaceAllStringFunc(report, func(line string) string {
		parts := fullMediaName.FindStringSubmatch(line)
		name := strings.ReplaceAll(strings.TrimSpace(parts[2]), "\\", "/")
		return parts[1] + path.Base(name)
	})
}
