package migrationflow

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
	"gorm.io/gorm"
)

const automationMediaRetryDelay = 2 * time.Minute

var errAutomationMediaRestricted = errors.New("原生资料标记禁转或限制转载")

type automationMediaClient interface {
	CompletedMediaPath(hash, savePath, contentPath string) (string, error)
	CheckMediaPathByProxy(context.Context, string) (bool, error)
	FetchMediaInfoByProxyContext(context.Context, string, string) (string, bool, error)
	FetchScreenshotsByProxyContext(context.Context, string, string, *int) (string, error)
}

type automationMediaDraftRepo struct {
	processingpersist.FetchAndStoreEntryRepo
	save func(map[string]any) error
}

// UpsertSeedParameter 仅保存此任务的原始站点草稿，避免覆盖已存在的人工资料。
func (r *automationMediaDraftRepo) UpsertSeedParameter(record map[string]any) error {
	return r.save(record)
}

// automationMediaDraft 复用同一载荷的已保存资料，缺少草稿时只抓取源站资料。
// 缺图和缺媒体报告不会在通用抓取路径直接操作下载器。
func (s *AutomationService) automationMediaDraft(task repository.AutomationTask, snapshot automationSnapshot, guard func() error) (map[string]any, error) {
	nativeName := snapshot.Source.NativeName()
	row, err := s.migrate.repo.GetSeedParameter(task.SourceID, nativeName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err = guard(); err != nil {
			return nil, err
		}
		draftRepo := &automationMediaDraftRepo{
			FetchAndStoreEntryRepo: s.migrate.repo,
			save: func(record map[string]any) error {
				if err := guard(); err != nil {
					return err
				}
				return s.migrate.repo.InsertAutomationSeed(task, snapshot.Source.NativeCode(), record, s.now())
			},
		}
		result, status := s.migrate.fetchAndStore(map[string]any{
			"task_id": task.ID, "sourceSite": nativeName, "searchTerm": task.SourceID,
			"torrentName": task.Title, "savePath": task.SavePath, "downloaderId": task.DownloaderID,
		}, draftRepo)
		if status >= 400 || !processingshared.ToBool(result["success"]) {
			return nil, errors.New("源站资料暂未准备完成")
		}
		row, err = s.migrate.repo.GetSeedParameter(task.SourceID, nativeName)
	}
	if err != nil {
		return nil, errors.New("暂时无法读取此种子的原生资料")
	}
	if !strings.EqualFold(mediaFieldForCompare(row["hash"]), task.InfoHash) || mediaFieldForCompare(row["site_name"]) == "" {
		return nil, errors.New("原生资料与冻结的种子哈希不一致")
	}
	if len(processingtagging.DetectRestrictedTags(processingpersist.ParseStringArray(row["tags"]))) > 0 {
		return nil, errAutomationMediaRestricted
	}
	return row, nil
}

// prepareAutomationMedia 先复用草稿和简介，再请飞牛补全缺项。
// 只有来自同一完整下载、通过围栏并与草稿字段比较成功的回填可让自动任务继续。
func (s *AutomationService) prepareAutomationMedia(ctx context.Context, task repository.AutomationTask, snapshot automationSnapshot, downloader automationDownloader) error {
	guard := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.authorizedTask(task, snapshot)
	}
	if err := guard(); err != nil {
		return err
	}
	row, err := s.automationMediaDraft(task, snapshot, guard)
	if err != nil {
		return err
	}
	screenshots := mediaFieldForCompare(row["screenshots"])
	rawScreenStatus := mediaFieldForCompare(row["screenshot_review_status"])
	screenStatus := strings.ToLower(strings.TrimSpace(rawScreenStatus))
	needScreenshots := automationScreenshotPayload(screenshots, screenStatus) != nil
	if screenStatus != processingshared.ScreenshotReviewStatusNone &&
		(screenStatus != processingshared.ScreenshotReviewStatusConfirmed || needScreenshots) {
		return errors.New("已有人工截图待确认或需修复，保留原有选择")
	}

	report := mediaFieldForCompare(row["mediainfo"])
	mediaStatus := mediaFieldForCompare(row["mediainfo_status"])
	tags := mediaFieldForCompare(row["tags"])
	site := mediaFieldForCompare(row["site_name"])
	save := func(update repository.AutomationMediaUpdate) error {
		if err := guard(); err != nil {
			return err
		}
		update.Tags = tags
		return s.migrate.repo.UpdateAutomationSeedMedia(task, site, update, s.now())
	}
	saveReport := func(result string, description *repository.PreparedMediaDescription) error {
		if err := save(repository.AutomationMediaUpdate{
			PreviousValue: report, PreviousStatus: mediaStatus,
			MediaInfo: &result, Description: description,
		}); err != nil {
			return errors.New("回填 MediaInfo 前资料或任务已变化，等待复核")
		}
		return nil
	}

	needReport := !processingmedia.HasUsableMediaReport(report)
	if needReport {
		switch strings.ToLower(strings.TrimSpace(mediaStatus)) {
		case "processing", "processing_bdinfo":
			return errors.New("等待已有媒体分析任务完成")
		}
		description := repository.PreparedMediaDescription{
			Statement: mediaFieldForCompare(row["statement"]),
			Body:      mediaFieldForCompare(row["body"]),
		}
		if quoted := processingmedia.QuotedMediaInfo(description.Statement + "\n" + description.Body); quoted != "" {
			if err := saveReport(quoted, &description); err != nil {
				return err
			}
			needReport = false
		}
	}
	if !needReport && !needScreenshots {
		return nil
	}

	client, ok := downloader.(automationMediaClient)
	if native, isNative := downloader.(downloaderclient.Downloader); isNative &&
		(!native.UseProxy || !isPublishedEditPullHub(native.Host)) {
		ok = false
	}
	if !ok {
		return errors.New("等待配置飞牛 HTTPS 领单客户端")
	}
	mediaPath, err := checkAutomationMediaPath(downloader, client, task, guard)
	if err != nil {
		return err
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if err := guard(); err != nil {
		return err
	}
	checkCtx, stopCheck := context.WithTimeout(remoteCtx, 30*time.Second)
	available, err := client.CheckMediaPathByProxy(checkCtx, mediaPath)
	stopCheck()
	if err != nil || !available {
		return errors.New("飞牛客户端尚无法读取该种子的完整视频")
	}
	recheck := func() error {
		current, err := checkAutomationMediaPath(downloader, client, task, guard)
		if err != nil {
			return err
		}
		if current != mediaPath {
			return errors.New("媒体回填前视频路径已变化，等待重新核对")
		}
		return nil
	}
	if needReport {
		if err := guard(); err != nil {
			return err
		}
		reportText, bdmv, err := client.FetchMediaInfoByProxyContext(remoteCtx, mediaPath, path.Base(mediaPath))
		if err != nil || bdmv || !validAutomationMediaReport(reportText, mediaPath) {
			return errors.New("飞牛尚未返回属于此视频的有效 MediaInfo")
		}
		if err := recheck(); err != nil {
			return err
		}
		if err := saveReport(sanitizeAutomationMediaReport(reportText), nil); err != nil {
			return err
		}
	}
	if needScreenshots {
		if err := guard(); err != nil {
			return err
		}
		raw, err := client.FetchScreenshotsByProxyContext(remoteCtx, mediaPath, path.Base(mediaPath), nil)
		if err != nil {
			return errors.New("飞牛客户端尚未完成截图或 Pixhost 上传")
		}
		generated, err := automationPixhostScreenshots(raw)
		if err != nil {
			return err
		}
		if err := recheck(); err != nil {
			return err
		}
		if err := save(repository.AutomationMediaUpdate{
			PreviousValue: screenshots, PreviousStatus: rawScreenStatus, Screenshots: &generated,
		}); err != nil {
			return errors.New("回填截图前人工资料或任务已变化，保留原有选择")
		}
	}
	logx.Infof("自动转种", "飞牛媒体资料已核实并补齐 task_id=%s", task.ID)
	return nil
}

// checkAutomationMediaPath 要求冻结的 qB 哈希、目录及文件清单一致，不允许读取共用目录。
func checkAutomationMediaPath(downloader automationDownloader, client automationMediaClient, task repository.AutomationTask, guard func() error) (string, error) {
	if err := guard(); err != nil {
		return "", err
	}
	status, err := downloader.TorrentStatus(task.InfoHash)
	if err != nil || !status.Found || !strings.EqualFold(status.Hash, task.InfoHash) ||
		status.Progress != 1 || status.AmountLeft != 0 || !sameAutomationSavePath(status.SavePath, task.SavePath) {
		return "", errors.New("下载器未确认原始种子已在冻结目录完整下载")
	}
	contentPath, err := preparedMediaPath(status)
	if err != nil {
		return "", err
	}
	if err := guard(); err != nil {
		return "", err
	}
	return client.CompletedMediaPath(task.InfoHash, status.SavePath, contentPath)
}

func validAutomationMediaReport(report, mediaPath string) bool {
	if len(report) > 256<<10 || !processingmedia.HasUsableMediaReport(report) {
		return false
	}
	for _, pattern := range []*regexp.Regexp{fullMediaName, automationFFprobeName} {
		for _, name := range pattern.FindAllStringSubmatch(report, -1) {
			if path.Base(strings.ReplaceAll(strings.TrimSpace(name[2]), "\\", "/")) != path.Base(mediaPath) {
				return false
			}
		}
	}
	return true
}

var automationFFprobeName = regexp.MustCompile(`(?m)^(Input #[0-9]+,[^\r\n]* from ')([^\r\n]+)(':)`)

func sanitizeAutomationMediaReport(report string) string {
	return automationFFprobeName.ReplaceAllStringFunc(sanitizePublishedMediaInfo(report), func(line string) string {
		parts := automationFFprobeName.FindStringSubmatch(line)
		return parts[1] + path.Base(strings.ReplaceAll(parts[2], "\\", "/")) + parts[3]
	})
}

var automationShotLine = regexp.MustCompile(`(?i)^(?:\[url=([^\s\[\]]+)\])?\[img\]([^\s\[\]]+)\[/img\](\[/url\])?$`)
var automationPixhostImageHost = regexp.MustCompile(`^img[0-9]+\.pixhost\.to$`)
var automationPixhostImagePath = regexp.MustCompile(`(?i)^/images/[0-9]+/[a-z0-9_.-]+\.(?:png|jpe?g|webp)$`)
var automationPixhostShowPath = regexp.MustCompile(`(?i)^/show/[0-9]+/[a-z0-9_.-]+\.(?:png|jpe?g|webp)$`)

// automationPixhostScreenshots 保留客户端产生的真实 Pixhost 图片路径；展示页、缩略图和无效内容不能当正片截图。
func automationPixhostScreenshots(raw string) (string, error) {
	invalid := errors.New("飞牛未返回至少三张有效且不同的 Pixhost 截图")
	if len(raw) > 16<<10 {
		return "", invalid
	}
	urls := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := automationShotLine.FindStringSubmatch(line)
		if len(parts) != 4 || (parts[1] == "") != (parts[3] == "") {
			return "", invalid
		}
		if parts[1] != "" {
			show := automationPixhostURL(parts[1])
			if show == nil || show.Host != "pixhost.to" || !automationPixhostShowPath.MatchString(show.Path) {
				return "", invalid
			}
		}
		image := automationPixhostURL(parts[2])
		if image == nil || !automationPixhostImageHost.MatchString(image.Host) || !automationPixhostImagePath.MatchString(image.Path) {
			return "", invalid
		}
		if key := image.String(); !seen[key] {
			seen[key] = true
			urls = append(urls, key)
		}
	}
	if len(urls) < 3 {
		return "", invalid
	}
	return processingrepair.ToBBCodeImages(urls), nil
}

func automationPixhostURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Port() != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "#%") {
		return nil
	}
	return u
}
