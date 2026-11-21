package migrationflow

import (
	"errors"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const dbSeedQueryLogModule = "迁移-种子查询"

func (s *MigrateService) GetDBSeedInfo(torrentID, siteName, taskID string) (map[string]any, int) {
	return s.getDBSeedInfo(s.repo, torrentID, siteName, taskID)
}

// PrepareSeedInfo 在单种转种准备时刷新缺失的 MediaInfo，再返回当前数据库资料。
// 参数/返回：输入种子编号、站点及日志任务编号；返回标准化资料和 HTTP 状态码。
// 失败场景：查询参数无效或数据库不可读时返回错误；媒体补全失败时保留资料并返回 mediainfo_prepare_error。
// 副作用：仅当缓存资料 MediaInfo 不完整时请求配置的飞牛媒体代理并回写报告。
func (s *MigrateService) PrepareSeedInfo(torrentID, siteName, taskID string) (map[string]any, int) {
	return s.getDBSeedInfoWithPreparation(s.repo, torrentID, siteName, taskID, true)
}

func (s *MigrateService) getDBSeedInfo(repo processingpersist.SeedQueryRepo, torrentID, siteName, taskID string) (map[string]any, int) {
	return s.getDBSeedInfoWithPreparation(repo, torrentID, siteName, taskID, false)
}

func (s *MigrateService) getDBSeedInfoWithPreparation(repo processingpersist.SeedQueryRepo, torrentID, siteName, taskID string, prepare bool) (map[string]any, int) {
	taskID = strings.TrimSpace(taskID)
	logx.Infof(dbSeedQueryLogModule, "开始查询 torrent_id=%s site_name=%s task_id=%s", torrentID, siteName, taskID)
	if taskID != "" {
		s.ensureLogStream(taskID)
	}

	response, statusCode, lookupResult := processingpersist.ExecuteDBSeedQueryEntry(
		processingpersist.DBSeedQueryEntryInput{
			TorrentID: torrentID,
			SiteName:  siteName,
			TaskID:    taskID,
		},
		processingpersist.DBSeedQueryEntryDeps{
			Repo: repo,
			EmitLog: func(step, message, status string) {
				s.emitLog(taskID, step, message, status)
			},
			CloseLog: func() {
				s.closeLogStream(taskID)
			},
			NewContextID: func() string {
				return s.newID("ctx")
			},
			RegisterDBContext: func(contextID string, lookup processingpersist.DBSeedLookupResult, tid string) {
				publishworkflow.RegisterDBContext(
					s.contextState,
					contextID,
					tid,
					lookup.SiteName,
					lookup.Hash,
					lookup.Name,
					lookup.SavePath,
					lookup.DownloaderID,
					lookup.Nickname,
					// 对齐 Python：数据库命中时也允许发布阶段重新下载种子文件。
					tid,
				)
			},
			ReverseMappings: s.reverseMappings(),
			PrepareSeed: func(lookup processingpersist.DBSeedLookupResult) (processingpersist.DBSeedLookupResult, string, bool) {
				if !prepare || processingmedia.HasUsableMediaReport(processingshared.ToString(lookup.Normalized["mediainfo"], "")) {
					return lookup, "", false
				}
				if lookup.Hash == "" || lookup.SiteName == "" {
					return lookup, "种子资料缺少身份信息，无法安全补全", false
				}
				prepareErr := s.prepareSeedMediaInfo(taskID, lookup.Hash, torrentID, lookup.SiteName, "")
				if errors.Is(prepareErr, errPrepareMediaUnavailable) {
					return lookup, "", false
				}
				warning := ""
				if prepareErr != nil {
					warning = strings.TrimSpace(prepareErr.Error())
					if warning == "" {
						warning = "自动获取 MediaInfo 失败"
					}
					if len(warning) > 240 {
						warning = warning[:240]
					}
				}
				fresh, err := processingpersist.LookupSeedForMigration(processingpersist.DBSeedLookupInput{TorrentID: torrentID, SiteName: lookup.SiteName}, repo)
				if err != nil || !strings.EqualFold(fresh.Hash, lookup.Hash) || !strings.EqualFold(fresh.SiteName, lookup.SiteName) {
					logx.Warnf(dbSeedQueryLogModule, "转种准备资料身份校验失败 torrent_id=%s site_name=%s expected_hash=%s actual_hash=%s actual_site=%s err=%v", torrentID, lookup.SiteName, lookup.Hash, fresh.Hash, fresh.SiteName, err)
					return lookup, "", true
				}
				return fresh, warning, false
			},
		},
	)

	switch statusCode {
	case 400:
		logx.Warnf(dbSeedQueryLogModule, "参数校验失败 torrent_id=%s site_name=%s", torrentID, siteName)
	case 202:
		logx.Infof(dbSeedQueryLogModule, "数据库未命中 torrent_id=%s site_name=%s task_id=%s", torrentID, siteName, taskID)
	case 500:
		logx.Errorf(dbSeedQueryLogModule, "数据库读取失败 torrent_id=%s site_name=%s message=%s", torrentID, siteName, strings.TrimSpace(processingshared.ToString(response["message"], "")))
	case 200:
		if lookupResult != nil {
			logx.Infof(
				dbSeedQueryLogModule,
				"数据库查询完成 torrent_id=%s site_name=%s seed_id=%s hash=%s source=database",
				torrentID,
				siteName,
				lookupResult.SeedID,
				lookupResult.Hash,
			)
		}
	case 409:
		logx.Warnf(dbSeedQueryLogModule, "转种准备期间种子身份发生变化 torrent_id=%s site_name=%s", torrentID, siteName)
	}

	return response, statusCode
}
