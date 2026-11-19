package migrationflow

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

// CheckSourceRestriction 依据已登记的迁移上下文只读核验当前源站权限。
// 已知禁止与未知权限均可供界面展示；真正发布时独立复核，不接收界面判定作为授权。
func (s *MigrateService) CheckSourceRestriction(taskID string) (map[string]any, int) {
	var ctx publishworkflow.Context
	var ok bool
	if s != nil {
		ctx, ok = s.contextState.Get(strings.TrimSpace(taskID))
	}
	if !ok {
		result := publishworkflow.SourceRestrictionFailure(unknownSourceRestriction("任务上下文已过期，请重新打开源站资料"))
		return result, 400
	}
	result := publishworkflow.SourceRestrictionResult(s.inspectSourceRestriction(ctx))
	result["success"] = true
	return result, 200
}

func unknownSourceRestriction(message string) publishworkflow.SourceRestriction {
	return publishworkflow.SourceRestriction{Status: publishworkflow.SourceRestrictionUnknown, Message: message}
}

// inspectSourceRestriction 使用服务端原生站点与迁移资料核验来源，拒绝从发布字段推断来源。
func (s *MigrateService) inspectSourceRestriction(ctx publishworkflow.Context) publishworkflow.SourceRestriction {
	if s == nil || s.repo == nil || strings.TrimSpace(ctx.SiteName) == "" {
		return unknownSourceRestriction("缺少可信源站记录，请重新抓取源站资料")
	}
	expectedHash := strings.TrimSpace(ctx.Hash)
	if value, err := hex.DecodeString(expectedHash); err != nil || len(value) != 20 {
		return unknownSourceRestriction("缺少可信源站种子标识，请重新抓取源站资料")
	}
	site, err := acquirefetch.PrepareSourceSite(s.repo, ctx.SiteName)
	if err != nil {
		return unknownSourceRestriction("源站不可用或未启用源站迁移，无法确认转种权限")
	}
	base := acquirefetch.NormalizeSiteBaseURL(processingshared.ToString(site["base_url"], ""))
	cookie := strings.TrimSpace(processingshared.ToString(site["cookie"], ""))
	if base == "" || cookie == "" {
		return unknownSourceRestriction("缺少可读取源站详情的站点地址或凭据")
	}
	declared := siteprofile.Default()
	declared.BaseURL = base
	scanner := acquireautomation.NewScanner()
	if s.automation != nil && s.automation.profiles != nil {
		code := strings.TrimSpace(processingshared.ToString(site["site"], ""))
		profile, err := s.automation.profiles.Resolve(code)
		if err != nil || profile.BaseURL != normalizedOrigin(base) {
			return unknownSourceRestriction("源站配置已改变或无法读取，无法确认转种权限")
		}
		declared = *profile
		scanner = s.automation.scanner
	}
	profile, err := acquireautomation.ProfileFrom(declared)
	if err != nil || scanner == nil {
		return unknownSourceRestriction("源站检查服务不可用，无法确认转种权限")
	}
	id, err := acquireautomation.SourceRestrictionID(profile, ctx.TorrentID, ctx.SourceDetailURL)
	if err != nil {
		return unknownSourceRestriction("源站详情身份不明确，请重新抓取源站资料")
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	known, allowed, hash, err := scanner.InspectSourceRestriction(requestCtx, profile, cookie, id)
	if err != nil || !known {
		return unknownSourceRestriction("源站详情或种子无法核验，无法确认转种权限")
	}
	if !allowed {
		return publishworkflow.SourceRestriction{Status: publishworkflow.SourceRestrictionDenied}
	}
	if !strings.EqualFold(hash, expectedHash) {
		return unknownSourceRestriction("源站实际种子与迁移资料不符，请重新抓取源站资料")
	}
	return publishworkflow.SourceRestriction{Status: publishworkflow.SourceRestrictionAllowed}
}
