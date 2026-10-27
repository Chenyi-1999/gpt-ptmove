package migrationflow

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const automationUploadGuardKey = "_ptnexus_before_upload"

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

// authorizeAutomationUpload 在实际上传 POST 前核对不可回退尝试标记及当前开关、冻结路由。
func (s *MigrateService) authorizeAutomationUpload(queue repository.PublishQueueTask, paths ...string) error {
	if !s.automationQueueEnabled() {
		return errors.New("自动化模块或自动发布已关闭，尚未发出的上传已停止；请保留核对状态")
	}
	task, err := s.automation.repo.TaskForQueue(queue.ID)
	if err != nil {
		return err
	}
	if !task.PublishAttempted || task.State != "publishing" {
		return errors.New("自动发布尝试围栏已改变，禁止上传")
	}
	if len(paths) > 0 {
		file, err := os.Open(paths[0])
		if err != nil {
			return errors.New("自动发布源种子不可读，禁止上传")
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
		_ = file.Close()
		if readErr != nil || len(raw) > 16*1024*1024 {
			return errors.New("自动发布源种子无效，禁止上传")
		}
		meta, err := acquirefetch.ParseTorrentMeta(raw)
		if err != nil || !strings.EqualFold(meta.InfoHash, task.InfoHash) || meta.Size != task.Size {
			return errors.New("自动发布实际种子与任务冻结载荷不一致，禁止上传")
		}
	}

	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return errors.New("自动发布快照无效")
	}
	return s.automation.validateFrozen(snapshot)
}

// failAutomationBeforePublish 只结束尚未尝试上传的自动任务，取消或未知上传证据不被覆盖。
func (s *MigrateService) failAutomationBeforePublish(queue repository.PublishQueueTask, message string) {
	if queue.Trigger == "automation" && s.automation != nil {
		if err := s.automation.repo.FailBeforePublish(queue.ID, message, s.automation.now()); err != nil {
			logx.Errorf(publishQueueLogModule, "同步自动发布前失败记录失败 id=%d", queue.ID)
		}
	}
}
