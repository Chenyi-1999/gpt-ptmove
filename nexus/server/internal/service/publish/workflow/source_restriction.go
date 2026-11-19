package workflow

import (
	"errors"
	"io"
	"os"
	"strings"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

// SourceRestriction 表示本次发布的服务端源站核验结果。
type SourceRestriction struct {
	Status  string
	Message string
}

const (
	SourceRestrictionAllowed = "allowed"
	SourceRestrictionDenied  = "denied"
	SourceRestrictionUnknown = "unknown"
)

// SourceRestrictionResult 生成给迁移预检及发布结果共用的状态字段。
func SourceRestrictionResult(check SourceRestriction) map[string]any {
	status, message := check.Status, check.Message
	switch status {
	case SourceRestrictionAllowed:
		if message == "" {
			message = "源站转种权限已核验"
		}
	case SourceRestrictionDenied:
		if message == "" {
			message = "源站标记禁转或限转，已停止发布"
		}
	default:
		status = SourceRestrictionUnknown
		if message == "" {
			message = "无法确认源站转种权限，已停止发布"
		}
	}
	return map[string]any{"source_restriction_status": status, "source_restriction_message": message}
}

// SourceRestrictionFailure 生成可区分源站拒绝与权限未知的发布前拦截结果。
func SourceRestrictionFailure(check SourceRestriction) map[string]any {
	result := SourceRestrictionResult(check)
	result["success"] = false
	result["pre_check"] = true
	result["limit_reached"] = true
	result["message"] = result["source_restriction_message"]
	result["logs"] = result["source_restriction_message"]
	result["url"] = nil
	return result
}

// readVerifiedPublishTorrent 读取将交给发布器的同一份字节，拒绝替换、损坏或未知种子。
func readVerifiedPublishTorrent(path, expectedHash string) ([]byte, error) {
	const maxTorrentBytes = 16 << 20
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxTorrentBytes+1))
	if err != nil || len(raw) > maxTorrentBytes {
		return nil, errors.New("发布种子读取失败或超出大小限制")
	}
	meta, err := acquirefetch.ParseTorrentMeta(raw)
	if err != nil || !strings.EqualFold(meta.InfoHash, expectedHash) || acquirefetch.ValidateTorrentNetwork(raw) != nil {
		return nil, errors.New("发布种子与源站已核验种子不一致")
	}
	return raw, nil
}
