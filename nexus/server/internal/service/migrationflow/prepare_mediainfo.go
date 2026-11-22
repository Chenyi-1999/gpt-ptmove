package migrationflow

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
	"gorm.io/gorm"
)

const prepareMediaInfoLogModule = "转种-MediaInfo补全"

var errPrepareMediaUnavailable = errors.New("下载器未配置可用的远程媒体代理")

// prepareSeedMediaInfo 在转种准备时补全当前草稿，完整报告和受限草稿直接跳过。
// 优先从简介提取，再查询下载器。回填时检查草稿版本。
func (s *MigrateService) prepareSeedMediaInfo(taskID, hash, torrentID, siteName, downloaderID string) error {
	if s == nil || s.repo == nil {
		return errors.New("无法读取要补全 MediaInfo 的草稿")
	}
	row, err := s.repo.GetSeedParameterByKey(hash, torrentID, siteName)
	if err != nil {
		return fmt.Errorf("读取要补全 MediaInfo 的草稿失败: %w", err)
	}
	previousReport := mediaFieldForCompare(row["mediainfo"])
	previousStatus := mediaFieldForCompare(row["mediainfo_status"])
	if processingmedia.HasUsableMediaReport(previousReport) ||
		len(processingtagging.DetectRestrictedTags(processingpersist.ParseStringArray(row["tags"]))) > 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(previousStatus)) {
	case "processing", "processing_bdinfo":
		return errors.New("已有媒体分析任务在运行，请待处理完成后再准备")
	}

	description := repository.PreparedMediaDescription{
		Statement: mediaFieldForCompare(row["statement"]),
		Body:      mediaFieldForCompare(row["body"]),
	}
	report := processingmedia.QuotedMediaInfo(description.Statement + "\n" + description.Body)
	var expectedDescription *repository.PreparedMediaDescription
	if report != "" {
		expectedDescription = &description
	} else {
		report, err = s.fetchPreparedMediaFromDownloader(taskID, hash, torrentID, siteName, downloaderID, processingshared.ToString(row["title"], ""))
		if err != nil {
			return err
		}
	}
	updated, err := s.repo.UpdatePreparedMediaInfo(hash, torrentID, siteName, previousReport, previousStatus, report, time.Now().Format("2006-01-02 15:04:05"), expectedDescription)
	if err != nil {
		return fmt.Errorf("保存 MediaInfo 失败: %w", err)
	}
	if !updated {
		latest, readErr := s.repo.GetSeedParameterByKey(hash, torrentID, siteName)
		if readErr == nil && processingmedia.HasUsableMediaReport(mediaFieldForCompare(latest["mediainfo"])) {
			return nil
		}
		return errors.New("补全媒体报告期间资料已变化，原有资料已保留")
	}
	if expectedDescription != nil {
		s.emitLog(taskID, "补全 MediaInfo", "MediaInfo 从简介提取", "success")
		logx.Infof(prepareMediaInfoLogModule, "从简介提取报告 torrent_id=%s site_name=%s bytes=%d", torrentID, siteName, len(report))
		return nil
	}
	s.emitLog(taskID, "补全 MediaInfo", "媒体报告已回填至转种资料", "success")
	logx.Infof(prepareMediaInfoLogModule, "报告已保存 torrent_id=%s site_name=%s bytes=%d", torrentID, siteName, len(report))
	return nil
}

func (s *MigrateService) fetchPreparedMediaFromDownloader(taskID, hash, torrentID, siteName, downloaderID, title string) (string, error) {
	downloaderID = strings.TrimSpace(downloaderID)
	if downloaderID == "" {
		current, lookupErr := s.repo.GetCurrentTorrentByHash(hash)
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%w：本地缓存找不到该种子的下载器", errPrepareMediaUnavailable)
		}
		if lookupErr != nil {
			return "", fmt.Errorf("读取种子下载器失败: %w", lookupErr)
		}
		downloaderID = strings.TrimSpace(processingshared.ToString(current["downloader_id"], ""))
	}
	if s.cfg == nil {
		return "", errPrepareMediaUnavailable
	}
	downloader, decision, err := downloaderclient.DecideProxy(s.cfg.Get(), downloaderID)
	if err != nil || !decision.Enabled || downloader.Type != "qbittorrent" {
		return "", errPrepareMediaUnavailable
	}

	status, err := downloader.TorrentStatus(hash)
	if err != nil {
		return "", fmt.Errorf("查询下载器内的种子状态失败: %w", err)
	}
	if !status.Found || !strings.EqualFold(status.Hash, hash) || status.Progress != 1 || status.AmountLeft != 0 {
		return "", errors.New("下载器中未找到已完整下载的这条种子，MediaInfo 待补全")
	}
	remotePath, err := preparedMediaPath(status)
	if err != nil {
		return "", err
	}

	s.emitLog(taskID, "补全 MediaInfo", "正在向下载器侧媒体代理请求报告", "processing")
	logx.Infof(prepareMediaInfoLogModule, "请求下载器侧报告 torrent_id=%s site_name=%s downloader_id=%s", torrentID, siteName, downloaderID)
	report, isBDMV, err := downloader.FetchMediaInfoByProxy(remotePath, title)
	if err != nil {
		return "", fmt.Errorf("下载器侧 MediaInfo 提取失败: %w", err)
	}
	if isBDMV {
		return "", errors.New("下载器检测到蓝光原盘，待补全 BDInfo 报告")
	}
	if !processingmedia.HasUsableMediaReport(report) {
		return "", errors.New("下载器未返回完整的媒体报告，MediaInfo 待补全")
	}
	return report, nil
}

// preparedMediaPath 只使用 hash 对应条目的内容路径或具名子路径，避免扫描共用保存目录。
func preparedMediaPath(status downloaderclient.AutomationTorrentStatus) (string, error) {
	savePath := path.Clean(status.SavePath)
	if !path.IsAbs(savePath) {
		return "", errors.New("下载器未提供有效的媒体保存路径")
	}
	contentPath := strings.TrimSpace(status.ContentPath)
	if contentPath == "" {
		name := strings.TrimSpace(status.Name)
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return "", errors.New("下载器未提供可确认的种子内容路径")
		}
		contentPath = path.Join(savePath, name)
	}
	contentPath = path.Clean(contentPath)
	prefix := strings.TrimRight(savePath, "/") + "/"
	if !path.IsAbs(contentPath) || contentPath == savePath || !strings.HasPrefix(contentPath, prefix) {
		return "", errors.New("下载器内容路径无法定位到种子的独立文件或目录")
	}
	return contentPath, nil
}

// mediaFieldForCompare 保留草稿原始字段（包括空白），与数据库的更新条件一致。
func mediaFieldForCompare(value any) string {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return ""
	}
}
