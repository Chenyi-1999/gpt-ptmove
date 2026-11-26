package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const automationUploadGuardKey = "_ptnexus_before_upload"

// automationSeedForPublish 只读取当前原生记录，不接受队列或请求提供的审核状态。
// 未确认、未知状态、载荷不符或缺媒体资料均拒绝；人工确认允许少于三张，但不能发布空截图。
// 此检查不探测图片网络、不修改资料，也不创建上传尝试标记。
func (s *MigrateService) automationSeedForPublish(task repository.AutomationTask, snapshot automationSnapshot) (map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("原生资料服务不可用，禁止自动发布")
	}
	row, err := s.repo.GetSeedParameter(task.SourceID, snapshot.Source.NativeName())
	if err != nil {
		return nil, errors.New("当前原生资料不可读，禁止自动发布")
	}
	if !strings.EqualFold(strings.TrimSpace(processingshared.ToString(row["hash"], "")), task.InfoHash) {
		return nil, errors.New("当前原生资料载荷已改变，禁止自动发布")
	}
	if len(processingtagging.DetectRestrictedTags(processingpersist.ParseStringArray(row["tags"]))) > 0 {
		return nil, errAutomationMediaRestricted
	}
	// 必须读取原始状态：通用标准化会把未知值降为 none，不能用于自动发布授权。
	status := strings.ToLower(strings.TrimSpace(processingshared.ToString(row["screenshot_review_status"], "")))
	if status != processingshared.ScreenshotReviewStatusNone && status != processingshared.ScreenshotReviewStatusConfirmed {
		return nil, errors.New("截图尚未人工确认或审核状态无效；请在原生资料页确认截图后重试自动任务")
	}
	if err := automationScreenshotPayload(processingshared.ToString(row["screenshots"], ""), status); err != nil {
		return nil, err
	}
	if !processingmedia.HasUsableMediaReport(processingshared.ToString(row["mediainfo"], "")) {
		return nil, errors.New("未确认有效的 MediaInfo 或 BDInfo，等待补全")
	}
	return row, nil
}

// automationScreenshotPayload 复用图片提取规则并仅计入合法的不同网页图片地址，不访问网络。
func automationScreenshotPayload(screenshots, status string) error {
	count := 0
	for _, raw := range processingrepair.ExtractImageURLsFromText(screenshots) {
		u, err := url.Parse(raw)
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil {
			count++
		}
	}
	if count == 0 || (status != processingshared.ScreenshotReviewStatusConfirmed && count < 3) {
		return errors.New("截图为空或数量不足；请在原生资料页修复并确认截图后重试自动任务")
	}
	return nil
}

// guardNativePublishEvidence 使用服务端上下文、种子元数据及原生别名核对账本，不信任请求中的自动化触发标记。
func (s *MigrateService) guardNativePublishEvidence(payload map[string]any, ctx publishworkflow.Context, allowedQueueID int64) error {
	if s == nil || s.automation == nil {
		return nil
	}
	unique := func(values ...string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v != "" && !seen[v] {
				out = append(out, v)
				seen[v] = true
			}
		}
		return out
	}
	nativeAliases := func(names ...string) []string {
		values := unique(names...)
		if s.repo != nil {
			for _, name := range append([]string(nil), values...) {
				if row, err := s.repo.GetSiteByName(name); err == nil {
					values = append(values, processingshared.ToString(row["site"], ""), processingshared.ToString(row["nickname"], ""))
				}
			}
		}
		return unique(values...)
	}
	upload, _ := payload["upload_data"].(map[string]any)
	hashes := []string{strings.ToLower(ctx.Hash), strings.ToLower(processingshared.ToString(upload["hash"], "")), strings.ToLower(processingshared.ToString(payload["hash"], ""))}
	ids := []string{ctx.TorrentID, ctx.SourceDetailURL, processingshared.ToString(upload["torrent_id"], ""), processingshared.ToString(payload["torrent_id"], "")}
	sources := []string{ctx.SiteName, ctx.SourceNickname}
	for _, raw := range []string{processingshared.ToString(payload["seed_id"], ""), processingshared.ToString(payload["seedId"], ""), processingshared.ToString(upload["seed_id"], "")} {
		if hash, id, site, err := processingpersist.ParseSeedID(raw); err == nil {
			hashes = append(hashes, strings.ToLower(hash))
			ids = append(ids, id)
			sources = append(sources, site)
		}
	}
	path := acquirefetch.FindDownloadedTorrentPath(ctx.OriginalTorrentPath, ctx.TorrentDir, ctx.SiteName, ctx.TorrentID)
	if path != "" {
		if file, err := os.Open(path); err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
			_ = file.Close()
			if readErr == nil && len(raw) <= 16*1024*1024 {
				if meta, err := acquirefetch.ParseTorrentMeta(raw); err == nil {
					hashes = append(hashes, meta.InfoHash)
				}
			}
		}
	}
	normalizedIDs := []string{}
	for _, id := range ids {
		if parsed, err := url.Parse(id); err == nil && parsed.IsAbs() {
			for _, key := range []string{"id", "torrent_id"} {
				if v := parsed.Query().Get(key); v != "" {
					normalizedIDs = append(normalizedIDs, v)
				}
			}
		} else {
			normalizedIDs = append(normalizedIDs, id)
		}
	}
	target := processingshared.ToString(payload["targetSite"], processingshared.ToString(payload["target_site"], ""))
	return s.automation.repo.CheckPublishEvidence(nativeAliases(sources...), unique(normalizedIDs...), unique(hashes...), nativeAliases(target), allowedQueueID)
}

// automationUploadFence records whether this invocation has ever allowed a site write.
// Transport retries use the same guard, so a later refusal cannot clear a prior attempt.
type automationUploadFence struct {
	writeAuthorized bool
	pluginRefused   bool
}

func (f *automationUploadFence) record(err error) error {
	f.writeAuthorized = f.writeAuthorized || err == nil
	f.pluginRefused = f.pluginRefused || errors.Is(err, repository.ErrAutomationPluginDisabled)
	return err
}

func (f automationUploadFence) stoppedBeforeFirstWrite() bool {
	return f.pluginRefused && !f.writeAuthorized
}

// authorizeAutomationUpload 在实际上传 POST 前核对不可回退尝试标记及当前开关、冻结路由。
func (s *MigrateService) authorizeAutomationUpload(queue repository.PublishQueueTask, paths ...string) error {
	if err := s.automationQueueAuthorization(); err != nil {
		return err
	}
	task, err := s.automation.repo.TaskForQueue(queue.ID)
	if err != nil {
		return err
	}
	if !task.PublishAttempted || task.State != "publishing" {
		return errors.New("自动发布尝试围栏已改变，禁止上传")
	}
	if len(paths) > 0 {
		if err := verifyAutomationPublishTorrent(paths[0], task); err != nil {
			return err
		}
	}

	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return errors.New("自动发布快照无效")
	}
	row, err := s.automationSeedForPublish(task, snapshot)
	if err != nil {
		return err
	}
	var upload map[string]any
	if json.Unmarshal([]byte(queue.UploadDataJSON), &upload) != nil || upload == nil {
		return errors.New("自动发布字段快照无效，禁止上传")
	}
	screenshots := processingshared.ToString(row["screenshots"], "")
	if processingshared.ToString(upload["screenshots"], "") != screenshots {
		return errors.New("截图在上传授权后改变，禁止使用旧截图上传")
	}
	if processingshared.ToString(upload["mediainfo"], "") != processingshared.ToString(row["mediainfo"], "") {
		return errors.New("MediaInfo 在上传授权后改变，禁止使用旧资料上传")
	}
	complete, _ := upload["complete_publish_params"].(map[string]any)
	for _, section := range []map[string]any{upload, complete} {
		if intro, ok := section["intro"].(map[string]any); ok && processingshared.ToString(intro["screenshots"], "") != screenshots {
			return errors.New("截图发布字段不一致，禁止上传")
		}
	}
	if err := s.automation.validateFrozen(snapshot); err != nil {
		return err
	}
	return s.automation.requirePluginEnabled(context.Background())
}

// verifyAutomationPublishTorrent 在记录尝试及实际发送前核对冻结任务的种子与体积。
func verifyAutomationPublishTorrent(path string, task repository.AutomationTask) error {
	const maxTorrentBytes = 16 << 20
	file, err := os.Open(path)
	if err != nil {
		return errors.New("自动发布源种子不可读，禁止上传")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxTorrentBytes+1))
	if err != nil || len(raw) > maxTorrentBytes {
		return errors.New("自动发布源种子无效，禁止上传")
	}
	meta, err := acquirefetch.ParseTorrentMeta(raw)
	if err != nil || !strings.EqualFold(meta.InfoHash, task.InfoHash) || meta.Size != task.Size {
		return errors.New("自动发布实际种子与任务冻结载荷不一致，禁止上传")
	}
	return nil
}

// failAutomationBeforePublish 只结束尚未尝试上传的自动任务，取消或未知上传证据不被覆盖。
func (s *MigrateService) failAutomationBeforePublish(queue repository.PublishQueueTask, message string) {
	if queue.Trigger == "automation" && s.automation != nil {
		if err := s.automation.repo.FailBeforePublish(queue.ID, message, s.automation.now()); err != nil {
			logx.Errorf(publishQueueLogModule, "同步自动发布前失败记录失败 id=%d", queue.ID)
		}
	}
}
