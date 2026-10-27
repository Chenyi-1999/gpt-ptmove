package automation

import (
	"errors"
	"net/url"
	"strings"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

// ValidatePayload 要求目标 torrent 与完整源 torrent 的信息字典及总字节一致。
func ValidatePayload(data []byte, sourceHash string, sourceBytes int64) error {
	meta, err := acquirefetch.ParseTorrentMeta(data)
	if err != nil {
		return errors.New("目标种子元数据无效")
	}
	if !strings.EqualFold(meta.InfoHash, sourceHash) || meta.Size != sourceBytes {
		return errors.New("目标种子与源文件载荷不一致，禁止自动补种")
	}
	return nil
}

// SeedingClient 定义目标完整做种验证所需的只读下载器能力，支持离线替身。
type SeedingClient interface {
	TorrentStatus(string) (downloaderclient.AutomationTorrentStatus, error)
	TorrentFilesComplete(string) (bool, error)
	Trackers(string) ([]downloaderclient.AutomationTracker, error)
}

// VerifySeeding 只有完整文件、正常做种状态及目标 tracker 明确在线时才完成任务。
func VerifySeeding(d SeedingClient, hash string, announceHosts []string) (bool, error) {
	status, err := d.TorrentStatus(hash)
	if err != nil {
		return false, err
	}
	if !status.Found || status.Progress < 1 || status.AmountLeft != 0 {
		return false, nil
	}
	state := strings.ToLower(status.State)
	if state != "uploading" && state != "stalledup" && state != "forcedup" {
		return false, nil
	}
	complete, err := d.TorrentFilesComplete(hash)
	if err != nil || !complete {
		return false, err
	}
	trackers, err := d.Trackers(hash)
	if err != nil {
		return false, err
	}
	for _, tracker := range trackers {
		u, parseErr := url.Parse(tracker.URL)
		if parseErr != nil || tracker.Status != 2 {
			continue
		}
		for _, host := range announceHosts {
			if strings.EqualFold(u.Hostname(), host) {
				return true, nil
			}
		}
	}
	return false, nil
}
