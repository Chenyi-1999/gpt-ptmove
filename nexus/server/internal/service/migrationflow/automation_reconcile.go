package migrationflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	publishautomation "github.com/pt-nexus/server/internal/service/publish/automation"
)

// reconcileTask 在已提交事务外验证目标载荷；唯一允许的外部写入是下载器补种。
func (s *AutomationService) reconcileTask(ctx context.Context, task repository.AutomationTask) {
	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return
	}
	if s.authorizedTask(task, snapshot) != nil {
		return
	}
	if len(snapshot.Target.AnnounceHosts) == 0 {
		s.failTask(task, "冻结目标站缺少可信 tracker 域名，无法确认目标做种，请人工核对", true)
		return
	}
	d, err := s.downloader(task.DownloaderID)
	if err != nil {
		return
	}
	if native, ok := d.(downloaderclient.Downloader); ok {
		d = native.WithEffectGuard(func() error { return s.authorizedTask(task, snapshot) })
	}
	if task.State != "seeding" || task.TargetHash == "" {
		p, _ := acquireautomation.ProfileFrom(snapshot.Target)
		cookie, err := s.profiles.Cookie(task.Target)
		if err != nil {
			return
		}
		data, normalized, err := s.scanner.TorrentAt(ctx, p, cookie, task.ResultURL)
		if err != nil {
			return
		}
		source, err := os.ReadFile(task.TorrentPath)
		if err != nil {
			s.failTask(task, "源种子元数据不可用，无法证明同载荷", true)
			return
		}
		if err = acquirefetch.CompareTorrentPayload(source, data); err != nil {
			s.failTask(task, "目标种子载荷不匹配，禁止补种", true)
			return
		}
		if err = acquirefetch.ValidateTorrentNetwork(data, snapshot.Target.AnnounceHosts...); err != nil {
			s.failTask(task, "目标种子 tracker 不符合冻结站点声明", true)
			return
		}
		meta, err := acquirefetch.ParseTorrentMeta(data)
		if err != nil {
			return
		}
		if s.authorizedTask(task, snapshot) != nil {
			return
		}
		targetPath := filepath.Join(filepath.Dir(task.TorrentPath), task.ID+".target.torrent")
		if err = os.WriteFile(targetPath, data, 0o600); err != nil {
			return
		}
		if err = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"target_torrent_path": targetPath, "state": "seeding", "target_hash": meta.InfoHash, "result_url": normalized, "message": "已验证目标载荷，等待完整做种确认"}, s.now()); err != nil {
			return
		}
		task.State = "seeding"
		task.TargetHash = meta.InfoHash
		task.TargetTorrentPath = targetPath
		task.ResultURL = normalized

	}
	if s.authorizedTask(task, snapshot) != nil {
		return
	}
	data, err := os.ReadFile(task.TargetTorrentPath)
	if err != nil {
		s.failTask(task, "目标种子元数据不可用，请重新登记核对", true)
		return
	}
	if err = d.EnsureTargetSeeding(data, task.ID+"-target.torrent", task.SavePath, task.TargetHash); err != nil {
		s.failTask(task, "目标补种结果未确认，请核对下载器后再登记核对", true)
		return
	}
	if s.authorizedTask(task, snapshot) != nil {
		return
	}
	verified, err := publishautomation.VerifySeeding(d, task.TargetHash, snapshot.Target.AnnounceHosts)
	if err != nil || !verified {
		return
	}
	_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": "succeeded", "message": "目标完整载荷与 tracker 做种已确认", "result_json": `{"same_payload":true,"target_tracker_verified":true}`}, s.now())
}
