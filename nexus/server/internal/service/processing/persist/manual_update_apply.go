package persist

import (
	"fmt"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingack "github.com/pt-nexus/server/internal/service/processing/acknowledgment"
)

// ManualUpdateRepo 定义手工参数回写所需仓储接口。
type ManualUpdateRepo interface {
	GetSeedParameter(torrentID, siteName string) (map[string]any, error)
	UpsertSeedParameter(record map[string]any) error
	ListSitesGroupAndDescription() ([]map[string]any, error)
}

// ApplyManualUpdateFromPayload 执行转种面板手工参数回写，并返回发布预览所需结构。
// 参数/返回：payload 为前端提交参数；newID 用于生成兜底 hash；reverseMappings 为前端映射配置。
// 失败场景：缺少 torrent_id/site_name 或写库失败时返回对应状态码。
// 副作用：覆盖写入 seed_parameters（Delete + Create）。
func ApplyManualUpdateFromPayload(
	repo ManualUpdateRepo,
	payload map[string]any,
	newID func(prefix string) string,
	reverseMappings map[string]any,
) (map[string]any, int) {
	torrentID := strings.TrimSpace(toStringAny(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(toStringAny(payload["site_name"], ""))
	torrentName := strings.TrimSpace(toStringAny(payload["torrent_name"], ""))
	updated, _ := payload["updated_parameters"].(map[string]any)
	if updated == nil {
		updated = map[string]any{}
	}
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "message": "错误：torrent_id 和 site_name 不能为空"}, 400
	}

	existing, readErr := repo.GetSeedParameter(torrentID, siteName)
	mediaBase, hasMediaBase := payload["maintenance_media_base"].(map[string]any)
	if hasMediaBase {
		if readErr != nil {
			return map[string]any{"success": false, "message": "维护记录已无法读取，请刷新后再保存"}, 409
		}
		preserveUntouchedMaintenanceMedia(updated, mediaBase)
	}
	generatedHash := strings.TrimSpace(toStringAny(existing["hash"], ""))
	if generatedHash == "" {
		if newID != nil {
			generatedHash = strings.TrimSpace(newID("hash"))
		}
		if generatedHash == "" {
			generatedHash = fmt.Sprintf("hash-%d", time.Now().UnixNano())
		}
	}

	buildResult := BuildManualUpdatedSeedRecord(BuildManualUpdateInput{
		GeneratedHash: generatedHash,
		TorrentID:     torrentID,
		SiteName:      siteName,
		TorrentName:   torrentName,
		Existing:      existing,
		Updated:       updated,
	})
	standardized := buildResult.Standardized
	record := buildResult.Record

	applyManualUpdateTeamAcknowledgment(repo, record)
	if err := repo.UpsertSeedParameter(record); err != nil {
		return map[string]any{"success": false, "message": "更新失败: " + err.Error()}, 500
	}

	normalized := NormalizeSeedRow(record)
	normalized["standardized_params"] = standardized
	response := map[string]any{
		"success":                  true,
		"standardized_params":      standardized,
		"final_publish_parameters": BuildFinalPublishParameters(normalized),
		"complete_publish_params":  BuildCompletePublishParams(normalized),
		"raw_params_for_preview":   BuildRawPreviewParams(normalized),
		"reverse_mappings":         reverseMappings,
		"message":                  "参数更新并标准化成功",
	}
	if hasMediaBase {
		response["saved_maintenance_media"] = map[string]any{
			"screenshots": record["screenshots"], "screenshot_review_status": record["screenshot_review_status"], "mediainfo": record["mediainfo"],
		}
	}
	return response, 200
}

// preserveUntouchedMaintenanceMedia 移除旧表单中未编辑的媒体字段，使保存保留后台已经回填的资料。
func preserveUntouchedMaintenanceMedia(updated, base map[string]any) {
	for _, key := range []string{"screenshots", "screenshot_review_status", "mediainfo"} {
		original, hasOriginal := base[key].(string)
		value, hasValue := updated[key].(string)
		if hasOriginal && hasValue && value == original {
			delete(updated, key)
		}
	}
}

func applyManualUpdateTeamAcknowledgment(repo ManualUpdateRepo, record map[string]any) {
	if repo == nil || record == nil {
		return
	}
	rawSites, err := repo.ListSitesGroupAndDescription()
	if err != nil {
		logx.Warnf(teamAckLogModule, "官组致谢读取sites失败(手工回写) torrent_id=%s site=%s err=%v",
			strings.TrimSpace(toStringAny(record["torrent_id"], "")),
			strings.TrimSpace(toStringAny(record["site_name"], "")),
			err,
		)
		return
	}
	sites := make([]processingack.SiteRow, 0, len(rawSites))
	for _, row := range rawSites {
		sites = append(sites, processingack.SiteRow{
			Group:       strings.TrimSpace(toStringAny(row["group"], "")),
			Description: strings.TrimSpace(toStringAny(row["description"], "")),
		})
	}
	teamKey := strings.TrimSpace(toStringAny(record["team"], ""))
	statement := strings.TrimSpace(toStringAny(record["statement"], ""))
	logx.Infof(teamAckLogModule, "官组致谢检查(手工回写) torrent_id=%s site=%s team_key=%s statement_len=%d",
		strings.TrimSpace(toStringAny(record["torrent_id"], "")),
		strings.TrimSpace(toStringAny(record["site_name"], "")),
		teamKey,
		len([]rune(statement)),
	)
	updated, applied, _ := processingack.ApplyTeamAcknowledgmentIfNeeded(statement, teamKey, sites)
	if applied {
		record["statement"] = updated
	}
}
