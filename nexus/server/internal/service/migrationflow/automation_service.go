package migrationflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	acquireautomation "github.com/pt-nexus/server/internal/service/acquire/automation"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
)

// AutomationService 编排可选自动下载和原生转种；设置、任务和发布证据均持久化。
// 不变量：预览只读；配额原子预留；上传仅一次；关闭执行不改变用户已有种子。
type AutomationService struct {
	repo           *repository.AutomationRepository
	cfg            *config.Manager
	migrate        *MigrateService
	profiles       *SiteProfileService
	scanner        *acquireautomation.Scanner
	now            func() time.Time
	downloaderHook func(string) (automationDownloader, error)
	mu             sync.Mutex
	start          sync.Once
}
type automationSnapshot struct {
	Settings policy.Settings     `json:"settings"`
	Source   siteprofile.Profile `json:"source"`
	Target   siteprofile.Profile `json:"target"`
}

// NewAutomationService 创建模块并恢复不确定上传，失败时不启用调度。
func NewAutomationService(store *repository.Store, cfg *config.Manager, migrate *MigrateService, profiles *SiteProfileService) (*AutomationService, error) {
	repo, err := repository.NewAutomationRepository(store)
	if err != nil {
		return nil, err
	}
	s := &AutomationService{repo: repo, cfg: cfg, migrate: migrate, profiles: profiles, scanner: acquireautomation.NewScanner(), now: time.Now}
	if err = repo.RecoverPublishing(s.now()); err != nil {
		return nil, err
	}
	if migrate != nil {
		migrate.automation = s
	}
	return s, nil
}
func automationID(prefix string) string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(data)
}
func automationError(err error, status int) (map[string]any, int) {
	return map[string]any{"success": false, "message": err.Error()}, status
}
func automationMap(value any) map[string]any {
	raw, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}
func (s *AutomationService) settings(repo *repository.AutomationRepository) (policy.Settings, error) {
	raw, err := repo.Settings()
	if err != nil {
		return policy.Defaults(), err
	}
	v, err := policy.DecodeSettings(raw)
	if raw == "" && s.cfg != nil {
		root := s.cfg.Get()
		if cross, ok := root["cross_seed"].(map[string]any); ok {
			v.DownloaderID = processString(cross["default_downloader"])
			v.SavePath = processString(cross["save_path"])
		}
	}
	return v, err
}

// GetSettings 返回公开设置与本月保守预留用量，不返回站点凭据。
func (s *AutomationService) GetSettings() map[string]any {
	return s.GetSettingsContext(context.Background())
}

// GetSettingsContext 在机器事务中读取同一版本设置和配额。
func (s *AutomationService) GetSettingsContext(ctx context.Context) map[string]any {
	repo := s.repo.WithContext(ctx)
	v, err := s.settings(repo)
	if err != nil {
		return map[string]any{"success": false, "message": "自动化配置读取失败"}
	}
	quota, err := repo.Quota(policy.Month(s.now()))
	if err != nil {
		return map[string]any{"success": false, "message": "自动化配额读取失败"}
	}
	return map[string]any{"success": true, "settings": v, "quota": quota}
}

// UpdateSettings 为浏览器入口保存白名单设置。
func (s *AutomationService) UpdateSettings(body map[string]any) (map[string]any, int) {
	return s.UpdateSettingsContext(context.Background(), body)
}

// UpdateSettingsContext 将机器 API 的幂等记录与设置保存纳入同一事务。
func (s *AutomationService) UpdateSettingsContext(ctx context.Context, body map[string]any) (map[string]any, int) {
	repo := s.repo.WithContext(ctx)
	current, err := s.settings(repo)
	if err != nil {
		return automationError(errors.New("自动化配置读取失败"), 500)
	}
	next, err := policy.MergeSettings(current, body)
	if err != nil {
		return automationError(err, 400)
	}
	if next.Enabled {
		_, target, resolveErr := s.resolveContext(ctx, next)
		if resolveErr != nil {
			return automationError(resolveErr, 400)
		}
		if len(target.AnnounceHosts) == 0 {
			return automationError(errors.New("目标站未配置可信 tracker 域名，无法证明目标做种；请先更新站点声明 announce_hosts"), 400)
		}
		if _, err = s.downloader(next.DownloaderID); err != nil {
			return automationError(err, 400)
		}
	}
	raw, _ := json.Marshal(next)
	if err = repo.SaveSettings(string(raw), !next.Enabled, s.now()); err != nil {
		return automationError(errors.New("自动化配置保存失败"), 500)
	}
	return map[string]any{"success": true, "settings": next}, 200
}
func (s *AutomationService) resolve(v policy.Settings) (*siteprofile.Profile, *siteprofile.Profile, error) {
	return s.resolveContext(context.Background(), v)
}
func (s *AutomationService) resolveContext(ctx context.Context, v policy.Settings) (*siteprofile.Profile, *siteprofile.Profile, error) {
	if s.profiles == nil {
		return nil, nil, errors.New("站点模块未初始化")
	}
	resolve := s.profiles.Resolve
	if tx := repository.MachineTransaction(ctx); tx != nil {
		resolve = func(id string) (*siteprofile.Profile, error) { return s.profiles.resolve(tx, id) }
	}
	source, err := resolve(v.SourceSite)
	if err != nil {
		return nil, nil, errors.New("源站未配置，请先导入或设置原生站点")
	}
	target, err := resolve(v.TargetSite)
	if err != nil {
		return nil, nil, errors.New("目标站未配置，请先导入或设置原生站点")
	}
	hasRole := func(p *siteprofile.Profile, role string) bool {
		for _, v := range p.Roles {
			if v == role {
				return true
			}
		}
		return false
	}
	if !hasRole(source, "source") || !hasRole(target, "target") {
		return nil, nil, errors.New("站点未启用对应源站或目标站角色")
	}
	mapping, err := publishmapping.LoadSitePublishConfig(target.NativeCode())
	if err != nil || mapping == nil || len(mapping.FormFields) == 0 || len(mapping.Mappings) == 0 {
		return nil, nil, errors.New("目标站缺少可执行的原生发布映射")
	}
	return source, target, nil
}
func (s *AutomationService) scan(ctx context.Context, v policy.Settings) ([]policy.Candidate, automationSnapshot, error) {
	source, target, err := s.resolveContext(ctx, v)
	if err != nil {
		return nil, automationSnapshot{}, err
	}
	snapshot := automationSnapshot{Settings: v, Source: *source, Target: *target}
	sp, err := acquireautomation.ProfileFrom(source)
	if err != nil {
		return nil, snapshot, err
	}
	tp, err := acquireautomation.ProfileFrom(target)
	if err != nil {
		return nil, snapshot, err
	}
	sourceCookie, err := s.profiles.CookieContext(ctx, source.ID)
	if err != nil {
		return nil, snapshot, errors.New("源站凭据不可用")
	}
	targetCookie, err := s.profiles.CookieContext(ctx, target.ID)
	if err != nil {
		return nil, snapshot, errors.New("目标站凭据不可用")
	}
	rss, err := s.profiles.RSSContext(ctx, source.ID)
	if err != nil {
		return nil, snapshot, errors.New("源站订阅配置不可用")
	}
	rows, err := s.scanner.Scan(ctx, sp, tp, sourceCookie, targetCookie, rss, v)
	if err == nil {
		s.excludePreviouslyDownloaded(ctx, source.ID, rows, v)
	}
	return rows, snapshot, err
}

// excludePreviouslyDownloaded 只读核对合格候选的自动化账本与已配置下载器。
// 现有或正在下载的载荷被排除，未知历史不得入队。相同哈希只查询一次下载器。
func (s *AutomationService) excludePreviouslyDownloaded(ctx context.Context, source string, rows []policy.Candidate, settings policy.Settings) {
	repo := s.repo.WithContext(ctx)
	var reader automationDownloader
	var readerErr error
	readerResolved := false
	type downloadLookup struct {
		found bool
		err   error
	}
	queried := make(map[string]downloadLookup)
	for index := range rows {
		candidate := &rows[index]
		if !candidate.Eligible {
			continue
		}
		seen, err := repo.HasDownloadHistory(source, candidate.ID, candidate.InfoHash, "")
		if err != nil {
			candidate.Eligible = false
			candidate.Reasons = append(candidate.Reasons, "自动下载历史读取失败，无法排除已下载")
			continue
		}
		if !seen && settings.DownloaderID != "" {
			if !readerResolved {
				reader, readerErr = s.downloader(settings.DownloaderID)
				if reader == nil && readerErr == nil {
					readerErr = errors.New("下载器不可用")
				}
				readerResolved = true
			}
			hash := strings.ToLower(candidate.InfoHash)
			lookup, ok := queried[hash]
			if !ok {
				lookup.err = readerErr
				if lookup.err == nil {
					lookup.err = ctx.Err()
				}
				if lookup.err == nil {
					status, statusErr := reader.TorrentStatus(hash)
					lookup.found, lookup.err = status.Found, statusErr
				}
				queried[hash] = lookup
			}
			if lookup.err != nil {
				candidate.Eligible = false
				candidate.Reasons = append(candidate.Reasons, "下载器状态未知，无法排除已下载")
				continue
			}
			seen = lookup.found
		}
		if seen {
			candidate.PreviouslyDownloaded = true
			*candidate = policy.Evaluate(*candidate, settings)
		}
	}
	policy.Rank(rows, settings)
}

// checkTaskDownloadHistory 为待执行任务复核其他任务的下载记录。
// 已拥有的本任务记录不视为重复。此检查只读，不接触下载器或目标站。
func (s *AutomationService) checkTaskDownloadHistory(ctx context.Context, task repository.AutomationTask) error {
	seen, err := s.repo.WithContext(ctx).HasDownloadHistory(task.Source, task.SourceID, task.InfoHash, task.ID)
	if err != nil {
		return errors.New("自动下载历史读取失败")
	}
	if seen {
		return errors.New("已有其他自动下载记录")
	}
	return nil
}

// Preview 只读取站点详情、查重和 torrent 元数据，不预留、不落盘、不加种、不上传。
func (s *AutomationService) Preview(ctx context.Context, body map[string]any) (map[string]any, int) {
	v, err := s.settings(s.repo.WithContext(ctx))
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
	rows, snapshot, err := s.scan(ctx, v)
	if err != nil {
		return automationError(err, 400)
	}
	return map[string]any{"success": true, "candidates": rows, "settings": v, "source_digest": snapshot.Source.Digest, "target_digest": snapshot.Target.Digest}, 200
}

// Run 仅登记冻结的扫描意图；网络扫描与原子配额预留均由后台执行。
func (s *AutomationService) Run(ctx context.Context, body map[string]any) (map[string]any, int) {
	return s.run(ctx, body, false)
}

type automationRunSnapshot struct {
	Route       automationSnapshot `json:"route"`
	SelectedIDs []string           `json:"selected_ids"`
}

func (s *AutomationService) run(ctx context.Context, body map[string]any, scheduled bool) (map[string]any, int) {
	repo := s.repo.WithContext(ctx)
	v, err := s.settings(repo)
	if err != nil {
		return automationError(errors.New("配置读取失败"), 500)
	}
	selected := []string{}
	if ids, ok := body["selected_ids"].([]any); ok {
		for _, raw := range ids {
			if id, ok := raw.(string); ok {
				selected = append(selected, id)
			}
		}
	}
	if ids, ok := body["selected_ids"].([]string); ok {
		selected = append(selected, ids...)
	}
	explicit := len(selected) > 0 || body["enqueue"] == true
	execute := v.Enabled || explicit
	if execute {
		if v.DownloaderID == "" || v.SavePath == "" {
			return automationError(errors.New("请先选择原生下载器和完整文件保存目录"), 400)
		}
		if _, err = s.downloader(v.DownloaderID); err != nil {
			return automationError(err, 400)
		}
	}
	source, target, err := s.resolveContext(ctx, v)
	if err != nil {
		return automationError(err, 400)
	}
	if execute && len(target.AnnounceHosts) == 0 {
		return automationError(errors.New("目标站缺少可信 announce_hosts，无法安全核对目标做种；只读扫描仍可用"), 400)
	}
	snapshot := automationRunSnapshot{Route: automationSnapshot{Settings: v, Source: *source, Target: *target}, SelectedIDs: selected}
	raw, _ := json.Marshal(snapshot)
	run := repository.AutomationRun{ID: automationID("run"), Month: policy.Month(s.now()), Status: "queued", Snapshot: string(raw), CreatedAt: s.now().UnixMilli(), Execute: execute, Explicit: explicit, Scheduled: scheduled, Message: "扫描已登记，等待后台读取候选"}
	if scheduled {
		key := "monthly:" + run.Month
		run.ScheduleKey = &key
	}
	if err = repo.CreateRun(&run); err != nil {
		return automationError(errors.New("当月计划已执行或运行记录写入失败"), 409)
	}
	return map[string]any{"success": true, "run": run, "tasks": []repository.AutomationTask{}, "candidates": []policy.Candidate{}, "message": run.Message}, 202
}

// ListTasks 返回真实任务和当前月份预留，不创建演示记录。
func (s *AutomationService) ListTasks() (map[string]any, int) {
	return s.ListTasksContext(context.Background())
}

// ListTasksContext 在机器事务中读取真实任务和用量，不产生外部操作。
func (s *AutomationService) ListTasksContext(ctx context.Context) (map[string]any, int) {
	repo := s.repo.WithContext(ctx)
	rows, err := repo.Tasks()
	if err != nil {
		return automationError(errors.New("读取任务失败"), 500)
	}
	q, err := repo.Quota(policy.Month(s.now()))
	if err != nil {
		return automationError(errors.New("读取配额失败"), 500)
	}
	return map[string]any{"success": true, "tasks": rows, "quota": q}, 200
}

// ListRuns 返回持久运行记录。
func (s *AutomationService) ListRuns() (map[string]any, int) {
	rows, err := s.repo.Runs()
	if err != nil {
		return automationError(errors.New("读取运行记录失败"), 500)
	}
	result := []map[string]any{}
	for _, row := range rows {
		value := automationMap(row)
		candidates := []policy.Candidate{}
		if row.CandidatesJSON != "" {
			_ = json.Unmarshal([]byte(row.CandidatesJSON), &candidates)
		}
		value["candidates"] = candidates
		result = append(result, value)
	}
	return map[string]any{"success": true, "runs": result}, 200
}

// Cancel 取消任务但保留下载器已有文件和种子。
func (s *AutomationService) Cancel(id string) (map[string]any, int) {
	return s.CancelContext(context.Background(), id)
}

// CancelContext 在机器幂等事务中取消任务。
func (s *AutomationService) CancelContext(ctx context.Context, id string) (map[string]any, int) {
	if err := s.repo.WithContext(ctx).Cancel(id, s.now()); err != nil {
		return automationError(err, 409)
	}
	return map[string]any{"success": true, "message": "任务已取消，已有下载保持原状"}, 200
}

// Retry 只允许恢复尚未上传的失败任务。
func (s *AutomationService) Retry(id string) (map[string]any, int) {
	return s.RetryContext(context.Background(), id)
}

// RetryContext 沿用原任务和配额，不为不确定上传提供重试。
func (s *AutomationService) RetryContext(ctx context.Context, id string) (map[string]any, int) {
	if err := s.repo.WithContext(ctx).Retry(id, s.now()); err != nil {
		return automationError(err, 409)
	}
	return map[string]any{"success": true, "message": "已恢复原任务，发布尝试不可重置"}, 200
}

type automationDownloader interface {
	AddSourceTorrent([]byte, string, string, string) error
	EnsureTargetSeeding([]byte, string, string, string) error
	TorrentStatus(string) (downloaderclient.AutomationTorrentStatus, error)
	TorrentFilesComplete(string) (bool, error)
	Trackers(string) ([]downloaderclient.AutomationTracker, error)
	FreeSpace(string) (int64, error)
}

func (s *AutomationService) downloader(id string) (automationDownloader, error) {
	if s.downloaderHook != nil {
		return s.downloaderHook(id)
	}
	if s.cfg == nil {
		return nil, errors.New("下载器配置未初始化")
	}
	d, err := downloaderclient.FromConfig(s.cfg.Get(), id)
	if err != nil {
		return nil, errors.New("原生下载器不可用")
	}
	if d.Type != "qbittorrent" {
		return nil, errors.New("自动下载模块当前仅支持 qBittorrent")
	}
	return d, nil
}
func processString(value any) string { v, _ := value.(string); return strings.TrimSpace(v) }

// Reconcile 只登记同源目标详情及明确核对授权，后台核对载荷与做种；本方法不发请求。
func (s *AutomationService) Reconcile(ctx context.Context, id, targetURL string) (map[string]any, int) {
	repo := s.repo.WithContext(ctx)
	task, err := repo.Task(id)
	if err != nil {
		return automationError(errors.New("任务不存在"), 404)
	}
	var snapshot automationSnapshot
	if json.Unmarshal([]byte(task.Snapshot), &snapshot) != nil {
		return automationError(errors.New("任务快照损坏"), 409)
	}
	if err = s.validateFrozenContext(ctx, snapshot); err != nil {
		return automationError(err, 409)
	}
	if len(snapshot.Target.AnnounceHosts) == 0 {
		return automationError(errors.New("冻结目标站缺少可信 announce_hosts，无法核对做种；请保留人工核对状态"), 409)
	}
	p, _ := acquireautomation.ProfileFrom(snapshot.Target)
	normalized, err := acquireautomation.ValidateDetailURL(p, targetURL)
	if err != nil {
		return automationError(err, 400)
	}
	if err = repo.RequestReconcile(id, normalized, s.now()); err != nil {
		return automationError(err, 409)
	}
	return map[string]any{"success": true, "message": "已登记目标核对；后台将仅核对完整载荷并修复做种"}, 200
}
func (s *AutomationService) validateFrozen(snapshot automationSnapshot) error {
	return s.validateFrozenContext(context.Background(), snapshot)
}
func (s *AutomationService) validateFrozenContext(ctx context.Context, snapshot automationSnapshot) error {
	source, target, err := s.resolveContext(ctx, snapshot.Settings)
	if err != nil {
		return err
	}
	sourceJSON, _ := json.Marshal(source)
	frozenSourceJSON, _ := json.Marshal(snapshot.Source)
	targetJSON, _ := json.Marshal(target)
	frozenTargetJSON, _ := json.Marshal(snapshot.Target)
	if string(sourceJSON) != string(frozenSourceJSON) || string(targetJSON) != string(frozenTargetJSON) {
		return errors.New("站点声明已改变，请重新预览；旧任务不会自动切换站点")
	}
	return nil
}

// Start 启动可关闭的低频轮询，首装关闭时不会访问站点或下载器。
func (s *AutomationService) Start() {
	s.start.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
				s.Tick(ctx)
				cancel()
			}
		}()
	})
}

// Tick 领取有租约的真实任务；多次调用由互斥及数据库比较交换防止重叠。
func (s *AutomationService) Tick(ctx context.Context) {
	if !s.mu.TryLock() {
		return
	}
	defer s.mu.Unlock()
	v, err := s.settings(s.repo)
	if err != nil {
		return
	}
	if policy.Due(s.now(), v) {
		_, _ = s.run(ctx, map[string]any{}, true)
	}
	s.processRuns(ctx, v)
	rows, err := s.repo.WorkTasks()
	if err != nil {
		return
	}
	for _, task := range rows {
		if ctx.Err() != nil {
			return
		}
		_ = s.repo.TouchPoll(task.ID, s.now())
		if task.State == "published" || task.State == "seeding" || task.State == "reconciling" {
			if (!v.Enabled && !task.Explicit) || (task.State == "published" && !v.AutoPublish && !task.Explicit) {
				continue
			}
			token := automationID("lease")
			claimed, err := s.repo.Claim(task.ID, token, s.now())
			if err == nil {
				s.reconcileTask(ctx, claimed)
				_ = s.repo.Release(task.ID, token)
			}
			continue
		}
		if !v.Enabled && !task.Explicit {
			continue
		}
		if task.State != "reserved" && task.State != "downloading" && task.State != "preparing" && task.State != "ready" {
			continue
		}
		token := automationID("lease")
		claimed, err := s.repo.Claim(task.ID, token, s.now())
		if err != nil {
			continue
		}
		s.advanceTask(ctx, claimed)
		_ = s.repo.Release(task.ID, token)
	}
}
func (s *AutomationService) failTask(task repository.AutomationTask, message string, review bool) {
	state := "failed"
	if review {
		state = "needs_review"
	}
	_ = s.repo.Advance(task.ID, task.LeaseToken, task.State, map[string]any{"state": state, "message": message}, s.now())
	logx.Warnf("自动转种", "任务停止 task_id=%s state=%s reason=%s", task.ID, state, message)
}
