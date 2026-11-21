package migrationflow

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	"github.com/pt-nexus/server/internal/service/publish/publishededit"
)

const publishedEditLogModule = "已发布详情编辑"

var publishedEditIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var errPublishedEditReserved = errors.New("published edit revision already reserved")

// PublishedEditDraft 绑定预览所用版本、实际修改内容及确认凭据。
type PublishedEditDraft struct {
	Revision         string  `json:"revision"`
	ScreenshotCode   *string `json:"screenshot_bbcode"`
	MediaInfo        *string `json:"mediainfo"`
	CoverURL         *string `json:"cover_url,omitempty"`
	ResolvedCoverURL string  `json:"resolved_cover_url,omitempty"`
	Confirmation     string  `json:"confirmation"`
}

// InitPublishedEdit 初始化编辑记录和仅用于本次进程预览的签名密钥。
func (s *MigrateService) InitPublishedEdit(store *repository.Store) error {
	if _, err := rand.Read(s.publishedEditKey[:]); err != nil {
		return err
	}
	repo, err := repository.NewPublishedEditRepository(store)
	if err != nil {
		return err
	}
	s.publishedEditRepo = repo
	return nil
}

// publishedEditCandidate 仅以已成功的任务记录及固定的 NovaHD 目标地址声明入口。
func publishedEditCandidate(row *repository.PublishLogEntry) string {
	if row.Status != "success" && row.Status != "edited" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(row.ResultURL))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "pt.novahd.top") ||
		u.User != nil || u.EscapedPath() != "/details.php" || u.Fragment != "" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["id"]) != 1 || !publishedEditIDPattern.MatchString(q.Get("id")) {
		return ""
	}
	return q.Get("id")
}

type publishedEditTarget struct {
	row     *repository.PublishLogEntry
	id      string
	url     string
	adapter *publishededit.NovaHD
}

func (s *MigrateService) resolvePublishedEdit(logID uint64) (publishedEditTarget, map[string]any, int) {
	if s.publishLogRepo == nil || s.publishedEditRepo == nil {
		return publishedEditTarget{}, publishedEditError(http.StatusServiceUnavailable, "已发布编辑暂未就绪"), http.StatusServiceUnavailable
	}
	row, found, err := s.publishLogRepo.FindByID(logID)
	if err != nil {
		return publishedEditTarget{}, publishedEditError(http.StatusInternalServerError, "读取任务记录失败"), http.StatusInternalServerError
	}
	if !found {
		return publishedEditTarget{}, publishedEditError(http.StatusNotFound, "任务记录不存在"), http.StatusNotFound
	}
	id := publishedEditCandidate(row)
	if id == "" {
		return publishedEditTarget{}, publishedEditError(http.StatusConflict, "这条记录缺少已成功发布的 NovaHD 目标链接"), http.StatusConflict
	}
	site, err := s.repo.GetSiteByName(strings.TrimSpace(row.TargetSite))
	if err != nil || !strings.EqualFold(processingshared.ToString(site["site"], ""), "novahd") {
		return publishedEditTarget{}, publishedEditError(http.StatusConflict, "无法核对 NovaHD 站点配置"), http.StatusConflict
	}
	base := strings.TrimSpace(processingshared.ToString(site["base_url"], ""))
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	adapter, err := publishededit.NewNovaHD(base, processingshared.ToString(site["cookie"], ""))
	if err != nil {
		return publishedEditTarget{}, publishedEditError(http.StatusUnprocessableEntity, "NovaHD 登录或站点配置不可用，请检查设置"), http.StatusUnprocessableEntity
	}
	return publishedEditTarget{row: row, id: id, url: "https://pt.novahd.top/details.php?id=" + id, adapter: adapter}, nil, 0
}

func publishedEditError(status int, message string) map[string]any {
	result := map[string]any{"success": false, "message": message}
	if status == http.StatusConflict {
		result["status"] = "conflict"
	}
	return result
}

func publishedEditAdapterError(err error) (map[string]any, int) {
	switch {
	case errors.Is(err, publishededit.ErrPermission):
		return publishedEditError(http.StatusForbidden, "NovaHD 未确认此种子由当前绑定账号发布，或账号没有编辑权限"), http.StatusForbidden
	case errors.Is(err, publishededit.ErrUnsupported):
		return publishedEditError(http.StatusUnprocessableEntity, "NovaHD 编辑页不可用或表单结构无法确认，未提交修改"), http.StatusUnprocessableEntity
	case errors.Is(err, publishededit.ErrConflict):
		return publishedEditError(http.StatusConflict, "NovaHD 当前资料已改变，请重新打开编辑并预览"), http.StatusConflict
	case errors.Is(err, publishededit.ErrCoverMissing):
		return publishedEditError(http.StatusUnprocessableEntity, "无法核对 NovaHD 当前封面"), http.StatusUnprocessableEntity
	case errors.Is(err, publishededit.ErrCoverUnavailable):
		return publishedEditError(http.StatusUnprocessableEntity, "封面原图暂时无法加载，请检查链接后重新预览"), http.StatusUnprocessableEntity
	case errors.Is(err, publishededit.ErrCoverUnchanged):
		return publishedEditError(http.StatusBadRequest, "当前封面已经使用这条原图地址"), http.StatusBadRequest
	case errors.Is(err, publishededit.ErrInvalidChange):
		return publishedEditError(http.StatusBadRequest, "没有可保存的变化，或截图／MediaInfo 格式无效"), http.StatusBadRequest
	case errors.Is(err, publishededit.ErrUnverified):
		return map[string]any{"success": false, "status": "unknown", "message": "提交结果待核对。请重新打开编辑核对站上资料，同一版本不会再次提交。"}, http.StatusBadGateway
	case errors.Is(err, errPublishedEditReserved):
		return publishedEditError(http.StatusConflict, "这个版本已有保存记录，请重新打开编辑核对站上当前资料"), http.StatusConflict
	default:
		return publishedEditError(http.StatusBadGateway, "无法读取或保存 NovaHD 资料，请检查站点连接后核对"), http.StatusBadGateway
	}
}

// OpenPublishedEdit 读取 NovaHD 站上现有资料和版本，仅在验证归属及编辑权限后返回。
func (s *MigrateService) OpenPublishedEdit(ctx context.Context, logID uint64) (map[string]any, int) {
	target, failure, status := s.resolvePublishedEdit(logID)
	if failure != nil {
		return failure, status
	}
	snapshot, err := target.adapter.Read(ctx, target.url)
	if err != nil {
		return publishedEditAdapterError(err)
	}
	return map[string]any{
		"success": true, "revision": snapshot.Revision, "screenshot_bbcode": snapshot.ScreenshotBBCode,
		"mediainfo": snapshot.MediaInfo, "details_url": target.url, "cover_url": snapshot.CoverURL,
	}, http.StatusOK
}

// PreviewPublishedEdit 比对站上实时版本，返回差异与限时确认凭据。它不写入数据库或站点。
func (s *MigrateService) PreviewPublishedEdit(ctx context.Context, logID uint64, draft PublishedEditDraft) (map[string]any, int) {
	if message := draft.coverValidationMessage(false); message != "" {
		return publishedEditError(http.StatusBadRequest, message), http.StatusBadRequest
	}
	target, failure, status := s.resolvePublishedEdit(logID)
	if failure != nil {
		return failure, status
	}
	snapshot, err := target.adapter.Read(ctx, target.url)
	if err != nil {
		return publishedEditAdapterError(err)
	}
	if draft.Revision != snapshot.Revision {
		return publishedEditAdapterError(publishededit.ErrConflict)
	}
	expiry := time.Now().Add(15 * time.Minute).Unix()
	if draft.CoverURL != nil {
		if snapshot.CoverURL == "" {
			return publishedEditError(http.StatusUnprocessableEntity, "无法核对 NovaHD 当前封面，编辑未提交"), http.StatusUnprocessableEntity
		}
		resolved, err := publishededit.ResolvePixhostCover(ctx, *draft.CoverURL)
		if err != nil {
			if errors.Is(err, publishededit.ErrInvalidChange) {
				return publishedEditError(http.StatusBadRequest, "请输入有效的 Pixhost HTTPS 展示页或图片直链"), http.StatusBadRequest
			}
			return publishedEditError(http.StatusUnprocessableEntity, "未能找到可加载的 Pixhost 原图，请检查链接"), http.StatusUnprocessableEntity
		}
		if resolved == snapshot.CoverURL {
			return publishedEditError(http.StatusBadRequest, "当前封面已经使用这条原图地址"), http.StatusBadRequest
		}
		change, err := snapshot.PreviewCover(resolved)
		if err != nil {
			return publishedEditAdapterError(err)
		}
		draft.ResolvedCoverURL = change.CoverURLAfter
		return map[string]any{
			"success": true, "cover_url_before": change.CoverURLBefore, "cover_url_after": change.CoverURLAfter,
			"confirmation": s.signPublishedEdit(logID, target.id, draft, expiry),
		}, http.StatusOK
	}
	change, err := snapshot.Preview(draft.ScreenshotCode, draft.MediaInfo)
	if err != nil {
		return publishedEditAdapterError(err)
	}
	return map[string]any{
		"success": true, "before": change.Before, "after": change.After,
		"mediainfo_before": change.MediaInfoBefore, "mediainfo_after": change.MediaInfoAfter,
		"screenshot_bbcode_before": change.ScreenshotBBCodeBefore, "screenshot_bbcode_after": change.ScreenshotBBCodeAfter,
		"confirmation": s.signPublishedEdit(logID, target.id, draft, expiry),
	}, http.StatusOK
}

// SavePublishedEdit 验证预览、记录差异、发送一次编辑请求并回读验证。
func (s *MigrateService) SavePublishedEdit(ctx context.Context, logID uint64, draft PublishedEditDraft) (map[string]any, int) {
	if message := draft.coverValidationMessage(true); message != "" {
		return publishedEditError(http.StatusBadRequest, message), http.StatusBadRequest
	}
	target, failure, status := s.resolvePublishedEdit(logID)
	if failure != nil {
		return failure, status
	}
	if !s.validPublishedEditConfirmation(logID, target.id, draft, time.Now()) {
		return publishedEditError(http.StatusBadRequest, "请先预览当前修改并确认保存，预览确认有效期为 15 分钟"), http.StatusBadRequest
	}
	attempt := repository.PublishedEditAttempt{
		PublishLogID: logID, TargetID: target.id, Revision: draft.Revision, Status: "pending",
	}
	reserved := false
	reserve := func(change publishededit.Change) error {
		attempt.BeforeJSON = publishedEditSnapshotJSON(change.Before, change.MediaInfoBefore)
		attempt.AfterJSON = publishedEditSnapshotJSON(change.After, change.MediaInfoAfter)
		ok, reserveErr := s.publishedEditRepo.Reserve(ctx, &attempt)
		if reserveErr != nil {
			return reserveErr
		}
		if !ok {
			return errPublishedEditReserved
		}
		reserved = true
		return nil
	}
	var result publishededit.SaveResult
	var err error
	if draft.CoverURL != nil {
		result, err = target.adapter.SaveCover(ctx, target.url, draft.Revision, draft.ResolvedCoverURL, reserve)
	} else {
		result, err = target.adapter.Save(ctx, target.url, draft.Revision, draft.ScreenshotCode, draft.MediaInfo, reserve)
	}
	if !reserved {
		if err == nil {
			err = publishededit.ErrUnverified
		}
		return publishedEditAdapterError(err)
	}

	final := "unknown"
	if err == nil && result.Verified {
		final = "verified"
	}
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if journalErr := s.publishedEditRepo.Complete(finalCtx, attempt.ID, final); journalErr != nil {
		logx.Warnf(publishedEditLogModule, "编辑记录回写失败 日志ID=%d 目标ID=%s", logID, target.id)
	}
	logx.Infof(publishedEditLogModule, "编辑提交完成 日志ID=%d 目标ID=%s 核对状态=%s", logID, target.id, final)
	if final != "verified" {
		return publishedEditAdapterError(publishededit.ErrUnverified)
	}
	message := "已保存至 NovaHD，并核对站上资料"
	if draft.CoverURL != nil {
		message = "封面已保存至 NovaHD，并核对详情页"
	}
	return map[string]any{"success": true, "status": final, "message": message}, http.StatusOK
}

func (draft PublishedEditDraft) coverValidationMessage(saving bool) string {
	if draft.CoverURL == nil {
		if draft.ResolvedCoverURL != "" {
			return "缺少要更换的封面链接"
		}
		return ""
	}
	if draft.ScreenshotCode != nil || draft.MediaInfo != nil {
		return "更换封面需单独预览和保存"
	}
	if strings.TrimSpace(*draft.CoverURL) == "" || len(*draft.CoverURL) > 2048 {
		return "请输入有效的 Pixhost 封面链接"
	}
	if saving != (draft.ResolvedCoverURL != "") || len(draft.ResolvedCoverURL) > 2048 {
		return "请使用预览确认的封面原图地址"
	}
	return ""
}

func publishedEditSnapshotJSON(description, mediaInfo string) string {
	body, _ := json.Marshal(map[string]string{"description": description, "mediainfo": mediaInfo})
	return string(body)
}

func (s *MigrateService) signPublishedEdit(logID uint64, targetID string, draft PublishedEditDraft, expiry int64) string {
	draft.Confirmation = ""
	body, _ := json.Marshal([]any{logID, targetID, draft, expiry})
	mac := hmac.New(sha256.New, s.publishedEditKey[:])
	_, _ = mac.Write(body)
	return strconv.FormatInt(expiry, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *MigrateService) validPublishedEditConfirmation(logID uint64, targetID string, draft PublishedEditDraft, now time.Time) bool {
	rawExpiry, _, ok := strings.Cut(draft.Confirmation, ".")
	expiry, err := strconv.ParseInt(rawExpiry, 10, 64)
	if !ok || err != nil || expiry <= now.Unix() {
		return false
	}
	return hmac.Equal([]byte(draft.Confirmation), []byte(s.signPublishedEdit(logID, targetID, draft, expiry)))
}
