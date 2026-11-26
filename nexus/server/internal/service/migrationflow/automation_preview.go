package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// 网页快照明确列出显示字段，不保存站点声明、种子字节、下载路径或执行配置。
type automationPreviewSnapshot struct {
	Candidates   []automationPreviewCandidate `json:"candidates"`
	Settings     automationPreviewSettings    `json:"settings"`
	SourceDigest string                       `json:"source_digest,omitempty"`
	TargetDigest string                       `json:"target_digest,omitempty"`
}

type automationPreviewCandidate struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	DetailURL            string   `json:"detail_url,omitempty"`
	Size                 int64    `json:"size"`
	Seeders              int      `json:"seeders"`
	Leechers             int      `json:"leechers"`
	Official             bool     `json:"official"`
	Episode              bool     `json:"episode"`
	PreviouslyDownloaded bool     `json:"previously_downloaded"`
	DuplicateKnown       bool     `json:"duplicate_known"`
	Duplicate            bool     `json:"duplicate"`
	Eligible             bool     `json:"eligible"`
	Reasons              []string `json:"reasons"`
}

type automationPreviewSettings struct {
	SourceSite   string  `json:"source_site"`
	TargetSite   string  `json:"target_site"`
	MinSizeGiB   float64 `json:"min_size_gib"`
	MaxSizeGiB   float64 `json:"max_size_gib"`
	MinSeeders   int     `json:"min_seeders"`
	MinLeechers  int     `json:"min_leechers"`
	MonthlyLimit int     `json:"monthly_limit"`
	DownloaderID string  `json:"downloader_id"`
}

// Preview 为机器接口读取并筛选候选，不保存网页快照或改变执行状态。
func (s *AutomationService) Preview(ctx context.Context, body map[string]any) (map[string]any, int) {
	return s.preview(ctx, body, 0)
}

// WebPreview 只在扫描成功后保存网页快照。重叠请求按开始顺序保留最新成功结果。
func (s *AutomationService) WebPreview(ctx context.Context, body map[string]any) (map[string]any, int) {
	clock := time.Now().UnixNano()
	for {
		previous := s.previewStarted.Load()
		startedAt := max(clock, previous+1)
		if s.previewStarted.CompareAndSwap(previous, startedAt) {
			return s.preview(ctx, body, startedAt)
		}
	}
}

// LatestWebPreview 恢复网页预览，读取数据库，不重新访问站点或下载器。
func (s *AutomationService) LatestWebPreview(ctx context.Context) (map[string]any, int) {
	row, err := s.repo.WithContext(ctx).LatestPreview()
	if err != nil {
		return automationError(errors.New("候选预览读取失败"), 500)
	}
	if row == nil {
		return map[string]any{"success": true, "preview": nil}, 200
	}
	var snapshot automationPreviewSnapshot
	if err := json.Unmarshal([]byte(row.JSON), &snapshot); err != nil ||
		snapshot.Candidates == nil || row.PreviewedAt <= 0 {
		return automationError(errors.New("已有候选预览格式无效"), 500)
	}
	preview := map[string]any{
		"candidates": snapshot.Candidates, "settings": snapshot.Settings, "previewed_at": row.PreviewedAt,
	}
	if snapshot.SourceDigest != "" {
		preview["source_digest"] = snapshot.SourceDigest
	}
	if snapshot.TargetDigest != "" {
		preview["target_digest"] = snapshot.TargetDigest
	}
	return map[string]any{"success": true, "preview": preview}, 200
}

func (s *AutomationService) preview(ctx context.Context, body map[string]any, startedAt int64) (map[string]any, int) {
	task := s.migrate.observeTask("candidate_preview", "获取预览候选", automationPluginRoute)
	defer task.finish(false, "预览未完成", "")
	repo := s.repo.WithContext(ctx)
	v, err := s.settings(repo)
	if err != nil {
		return automationError(errors.New("配置读取失败"), 500)
	}
	if patch, ok := body["settings"].(map[string]any); ok {
		v, err = policy.MergeSettings(v, patch)
	} else if len(body) > 0 {
		v, err = policy.MergeSettings(v, body)
	}
	if err != nil {
		return automationError(err, 400)
	}
	rows, snapshot, err := s.scan(ctx, v, task)
	if err != nil {
		if ctx.Err() != nil {
			task.finish(false, "预览已中断", "")
		}
		return automationError(err, 400)
	}
	result := map[string]any{"success": true, "candidates": rows, "settings": v, "source_digest": snapshot.Source.Digest, "target_digest": snapshot.Target.Digest}
	if startedAt > 0 {
		task.update("保存预览结果", candidateResultProgress(rows))
		payload, err := json.Marshal(buildWebPreviewSnapshot(rows, snapshot))
		if err != nil {
			return automationError(errors.New("候选预览格式无效"), 500)
		}
		previewedAt := s.now().UnixMilli()
		saved, err := repo.SavePreview(startedAt, previewedAt, string(payload))
		if err != nil {
			task.finish(false, "保存预览失败", "")
			return automationError(errors.New("候选预览保存失败"), 500)
		}
		result["previewed_at"] = previewedAt
		result["preview_saved"] = saved
	}
	task.finish(true, "预览完成", candidateResultProgress(rows))
	return result, 200
}

func buildWebPreviewSnapshot(rows []policy.Candidate, route automationSnapshot) automationPreviewSnapshot {
	settings := route.Settings
	snapshot := automationPreviewSnapshot{
		Candidates:   make([]automationPreviewCandidate, 0, len(rows)),
		SourceDigest: route.Source.Digest,
		TargetDigest: route.Target.Digest,
		Settings: automationPreviewSettings{
			SourceSite: settings.SourceSite, TargetSite: settings.TargetSite,
			MinSizeGiB: settings.MinSizeGiB, MaxSizeGiB: settings.MaxSizeGiB,
			MinSeeders: settings.MinSeeders, MinLeechers: settings.MinLeechers,
			MonthlyLimit: settings.MonthlyLimit, DownloaderID: settings.DownloaderID,
		},
	}
	for _, row := range rows {
		snapshot.Candidates = append(snapshot.Candidates, automationPreviewCandidate{
			ID: row.ID, Title: row.Title, DetailURL: previewDetailURL(row.DetailURL, row.ID),
			Size: row.Size, Seeders: row.Seeders, Leechers: row.Leechers, Official: row.Official,
			Episode: row.Episode, PreviouslyDownloaded: row.PreviouslyDownloaded,
			DuplicateKnown: row.DuplicateKnown, Duplicate: row.Duplicate, Eligible: row.Eligible, Reasons: row.Reasons,
		})
	}
	return snapshot
}

// previewDetailURL 保存由扫描器生成的同源详情链接，只保留与候选一致的标识参数。
func previewDetailURL(raw, id string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	safe := url.Values{}
	for _, key := range []string{"id", "torrent_id"} {
		if values := query[key]; len(values) == 1 && values[0] == id {
			safe.Set(key, id)
		}
	}
	u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = safe.Encode(), "", "", false
	return u.String()
}
