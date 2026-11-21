package migrationflow

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
	"gorm.io/gorm"
)

const maintenanceRefetchLogModule = "维护-重新抓取"

var maintenanceTorrentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var maintenanceTorrentHashPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// RefetchMaintenanceSeed 重新提取源站种子资料，仅原子更新指定维护记录并返回该记录。
// 不匹配、来源限制未知、抓取失败或记录并发修改时保留原资料。不会入队发布或添加下载器任务。
func (s *MigrateService) RefetchMaintenanceSeed(payload map[string]any) (map[string]any, int) {
	value := func(key string) string { return strings.TrimSpace(processingshared.ToString(payload[key], "")) }
	sourceSite, sourceID := value("sourceSite"), value("searchTerm")
	targetSite, targetID, targetHash := value("target_site_name"), value("target_torrent_id"), strings.ToLower(value("target_hash"))
	if sourceSite == "" || targetSite == "" || !maintenanceTorrentIDPattern.MatchString(sourceID) ||
		!maintenanceTorrentIDPattern.MatchString(targetID) || (targetHash != "" && !maintenanceTorrentHashPattern.MatchString(targetHash)) {
		return maintenanceError(http.StatusBadRequest, "请提供站点、有效的种子 ID 和维护记录标识")
	}
	previous, err := s.repo.GetMaintenanceSeed(targetID, targetSite, targetHash)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return maintenanceError(http.StatusNotFound, "维护记录不存在，请重新打开页面")
	case errors.Is(err, repository.ErrAmbiguousMaintenanceSeed):
		return maintenanceError(http.StatusConflict, "存在多条同站点同 ID 的记录，请刷新页面后重试")
	case err != nil:
		return maintenanceError(http.StatusInternalServerError, "读取维护记录失败")
	}
	storedHash := strings.TrimSpace(processingshared.ToString(previous["hash"], ""))
	targetHash = strings.ToLower(storedHash)
	if !maintenanceTorrentHashPattern.MatchString(targetHash) {
		return maintenanceError(http.StatusConflict, "维护记录缺少可核验的种子 hash")
	}
	targetConfig, err := s.repo.GetSiteByName(targetSite)
	targetCode := strings.TrimSpace(processingshared.ToString(targetConfig["site"], ""))
	if err != nil || targetCode == "" {
		return maintenanceError(http.StatusConflict, "无法确认原站点配置，请检查站点设置")
	}

	taskID := value("task_id")
	if taskID == "" {
		taskID = s.newID("log")
	}
	s.ensureLogStream(taskID)
	defer s.closeLogStream(taskID)
	emit := func(step, message, status string) { s.emitLog(taskID, step, message, status) }
	name := strings.TrimSpace(processingshared.ToString(previous["name"], ""))
	current, _ := s.repo.GetCurrentTorrentByHash(storedHash)
	savePath := strings.TrimSpace(processingshared.ToString(current["save_path"], ""))
	downloaderID := strings.TrimSpace(processingshared.ToString(current["downloader_id"], ""))
	if localName := strings.TrimSpace(processingshared.ToString(current["name"], "")); localName != "" {
		name = localName
	}
	acquired, status, err := acquirefetch.AcquireSeedForFetch(acquirefetch.FetchAcquireInput{
		SourceSite: sourceSite, SearchTerm: sourceID, TorrentName: name, SavePath: savePath, DownloaderID: downloaderID, TaskID: taskID,
		RestrictSourceOrigin: true,
	}, acquirefetch.FetchAcquireDeps{Repo: s.repo, EmitLog: emit})
	if err != nil {
		if errors.Is(err, acquirefetch.ErrSourceCookieExpired) {
			return maintenanceError(http.StatusUnprocessableEntity, "源站登录状态失效，请更新 Cookie 后重试")
		}
		return maintenanceError(status, "抓取源站资料失败，请检查种子 ID 和站点设置后重试")
	}
	if strings.TrimSpace(acquired.DetailHTML) == "" {
		return maintenanceError(http.StatusBadGateway, "未读取到源站详情，请检查站点登录状态后重试")
	}
	if err := checkMaintenanceSource(acquired, targetCode, targetID, targetHash, s.inspectSourceRestriction, func() ([]byte, error) {
		_, _, raw, downloadErr := acquirefetch.DownloadTorrentFromConfiguredOrigin(targetConfig, targetID)
		return raw, downloadErr
	}); err != nil {
		return maintenanceError(http.StatusConflict, err.Error())
	}
	acquired.SavePath, acquired.DownloaderID, acquired.TorrentNameForPath = savePath, downloaderID, name
	rootConfig := map[string]any{}
	if s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	updates, err := processingpersist.PrepareMaintenanceSeedRecord(processingpersist.FetchStoreProcessInput{
		TaskID: taskID, SearchTerm: sourceID, AcquireResult: acquired, ExtractorEngine: s.extractorEngine,
	}, processingpersist.FetchStoreProcessDeps{
		Repo: s.repo, EmitLog: emit, Now: time.Now(), BuildSimpleTitleComponents: processingtitle.BuildSimpleTitleComponentsWithMediaInfo,
		FetchRepairDeps: processingrepair.FetchRepairDeps{EmitLog: s.emitLog, RootConfig: rootConfig, CSPTToken: s.csptToken()},
	}, previous)
	if err != nil {
		return maintenanceError(http.StatusUnprocessableEntity, err.Error())
	}
	updated, err := s.repo.UpdateMaintenanceSeed(previous, updates)
	if err != nil {
		logx.Warnf(maintenanceRefetchLogModule, "更新维护记录失败 状态码=%d", http.StatusInternalServerError)
		return maintenanceError(http.StatusInternalServerError, "保存维护资料失败")
	}
	if !updated {
		return maintenanceError(http.StatusConflict, "抓取期间维护记录已变更，请重新打开后重试")
	}
	emit("重新拉取", "已更新当前维护记录", "success")
	return s.getDBSeedInfo(maintenanceSeedQueryRepo{SeedQueryRepo: s.repo, row: previous, changes: updates}, targetID, targetSite, "")
}

func maintenanceError(status int, message string) (map[string]any, int) {
	return map[string]any{"success": false, "message": message}, status
}

func checkMaintenanceSource(source acquirefetch.FetchAcquireResult, targetCode, targetID, targetHash string,
	inspect func(publishworkflow.Context) publishworkflow.SourceRestriction, loadTarget func() ([]byte, error)) error {
	if strings.EqualFold(source.SiteIdentifier, targetCode) && source.TorrentID == targetID {
		if strings.EqualFold(source.Meta.InfoHash, targetHash) {
			return nil
		}
		return errors.New("源站种子内容已变更，原维护资料已保留")
	}
	decision := inspect(publishworkflow.Context{
		TorrentID: source.TorrentID, SiteName: source.SiteIdentifier, Hash: source.Meta.InfoHash, SourceDetailURL: source.DetailURL,
	})
	if decision.Status != publishworkflow.SourceRestrictionAllowed {
		if decision.Status == publishworkflow.SourceRestrictionDenied {
			return errors.New("所选源站禁止转载，无法用其资料更新其他站点的维护记录")
		}
		return errors.New("无法确认所选源站的转种权限，请使用原站点资料或检查站点设置")
	}
	if strings.EqualFold(source.Meta.InfoHash, targetHash) {
		return nil
	}
	raw, err := loadTarget()
	if err != nil {
		return errors.New("无法读取原站点的种子以比对内容，请检查原站点设置")
	}
	meta, err := acquirefetch.ParseTorrentMeta(raw)
	if err != nil || !strings.EqualFold(meta.InfoHash, targetHash) || acquirefetch.CompareTorrentPayload(source.TorrentData, raw) != nil {
		return errors.New("所选源站种子与维护记录的内容不一致，原资料已保留")
	}
	return nil
}

// maintenanceSeedQueryRepo 使用刚验证及更新的记录构建标准查询响应，避免昵称匹配串行。
type maintenanceSeedQueryRepo struct {
	processingpersist.SeedQueryRepo
	row, changes map[string]any
}

func (r maintenanceSeedQueryRepo) GetSeedParameter(string, string) (map[string]any, error) {
	merged := make(map[string]any, len(r.row))
	for key, value := range r.row {
		merged[key] = value
	}
	for key, value := range r.changes {
		merged[key] = value
	}
	return merged, nil
}
