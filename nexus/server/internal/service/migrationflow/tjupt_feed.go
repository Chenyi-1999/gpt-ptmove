package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"gorm.io/gorm"
)

// TjuptFeedSettings 是北洋园插件自己的订阅和 qB 投种设置。
type TjuptFeedSettings struct {
	DownloaderID       string  `json:"downloader_id"`
	IntervalMinutes    int     `json:"interval_minutes"`
	MinGiB             float64 `json:"min_gib"`
	MaxGiB             float64 `json:"max_gib"`
	TotalLimitGiB      float64 `json:"total_limit_gib"`
	ActiveLimit        int     `json:"active_limit"`
	Tags               string  `json:"tags"`
	Category           string  `json:"category"`
	SavePath           string  `json:"save_path"`
	CleanupEnabled     bool    `json:"cleanup_enabled"`
	UploadThresholdKiB int     `json:"upload_threshold_kib"`
	LowSpeedMinutes    int     `json:"low_speed_minutes"`
}

// TjuptFeedInput 保存同步规则，留空凭据默认保留原值。
type TjuptFeedInput struct {
	TjuptFeedSettings
	RSSURL             string `json:"rss_url"`
	HNRToken           string `json:"hnr_token"`
	ClearRSS           bool   `json:"clear_rss"`
	ClearHNRToken      bool   `json:"clear_hnr_token"`
	ActiveLimit        *int   `json:"active_limit"`
	CleanupEnabled     *bool  `json:"cleanup_enabled"`
	UploadThresholdKiB *int   `json:"upload_threshold_kib"`
	LowSpeedMinutes    *int   `json:"low_speed_minutes"`
}

// TjuptFeedView 返回页面状态，不输出订阅地址或令牌。
type TjuptFeedView struct {
	Settings struct {
		TjuptFeedSettings
		UseSiteUploadLimit bool `json:"use_site_upload_limit"`
	} `json:"settings"`
	SiteUploadLimitMBps *int   `json:"site_upload_limit_mbps"`
	Enabled             bool   `json:"enabled"`
	RSSConfigured       bool   `json:"rss_configured"`
	HNRConfigured       bool   `json:"hnr_configured"`
	Running             bool   `json:"running"`
	LastRunAt           string `json:"last_run_at,omitempty"`
	LastMessage         string `json:"last_message"`
}

// TjuptFeedResult 汇总本轮处理结果，恢复试运行任务不计入新增。
type TjuptFeedResult struct {
	Added   int    `json:"added"`
	Skipped int    `json:"skipped"`
	Failed  int    `json:"failed"`
	Message string `json:"message"`
}

var (
	// ErrTjuptFeedConfig 表示插件设置无效或操作所需设置尚未保存。
	ErrTjuptFeedConfig = errors.New("北洋园 RSS 设置无效")
	// ErrTjuptFeedBusy 表示指定操作与本轮运行或另一种模式冲突。
	ErrTjuptFeedBusy = errors.New("北洋园 RSS 当前无法执行此操作")
)

type tjuptFeedConfig struct {
	TjuptFeedSettings
	rss        string
	hnr        string
	storedTags string
}

// TjuptFeed 编排北洋园直连 qB 的试运行与定时下载。
type TjuptFeed struct {
	plugin       *TjuptRSSService
	repo         *repository.TjuptFeedRepository
	site         tjuptAcquisition
	secrets      *secretBox
	connect      func(downloaderclient.Downloader, func() error) tjuptFeedDownloader
	startOnce    sync.Once
	wake         chan struct{}
	cleanupEpoch string
}

// NewTjuptFeed 初始化独立投种台账和加密设置，首次安装默认关闭循环。
func NewTjuptFeed(db *gorm.DB, plugin *TjuptRSSService, dataDir string) (*TjuptFeed, error) {
	ctx := context.Background()
	base := defaultTjuptAutomation()
	legacy, err := repository.ReadLegacyTjuptControl(ctx, db)
	if err != nil {
		return nil, err
	}
	var old TjuptAutomationSettings
	if legacy.Policy != "" && json.Unmarshal([]byte(legacy.Policy), &old) == nil && validateTjuptAutomation(old) == nil {
		base = old
	}
	settings := TjuptFeedSettings{
		DownloaderID: base.DownloaderID, IntervalMinutes: 1, MinGiB: base.MinGiB, MaxGiB: base.MaxGiB,
		TotalLimitGiB: base.TotalLimitGiB, ActiveLimit: base.ActiveLimit, Tags: downloaderclient.TjuptFeedDisplayTag,
		UploadThresholdKiB: tjuptFeedDefaultUploadKiB, LowSpeedMinutes: tjuptFeedDefaultLowSpeedMinutes,
	}
	policy, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	repo, err := repository.NewTjuptFeedRepository(db, string(policy), settings.IntervalMinutes)
	if err != nil {
		return nil, err
	}
	box, err := openSecretBox(dataDir)
	if err != nil {
		return nil, err
	}
	epoch, err := tjuptFeedRandomID()
	if err != nil {
		return nil, err
	}
	return &TjuptFeed{
		plugin: plugin, repo: repo, site: tjupt.New(), secrets: box, wake: make(chan struct{}, 1),
		cleanupEpoch: epoch,
		connect: func(d downloaderclient.Downloader, guard func() error) tjuptFeedDownloader {
			return d.WithEffectGuard(guard)
		},
	}, nil
}

// View 返回插件设置和最近运行结果，不会访问北洋园或 qB。
func (f *TjuptFeed) View(ctx context.Context) (TjuptFeedView, error) {
	row, err := f.repo.Control(ctx)
	if err != nil {
		return TjuptFeedView{}, err
	}
	view := TjuptFeedView{
		Enabled: row.Enabled, Running: row.LeaseUntil > time.Now().Unix(),
		RSSConfigured: len(row.RSS) > 0, HNRConfigured: len(row.HNR) > 0,
		LastRunAt: tjuptTime(row.LastRun), LastMessage: row.LastMessage,
	}
	view.Settings.TjuptFeedSettings, err = decodeTjuptFeedSettings(row.Policy)
	if err != nil {
		return view, err
	}
	view.Settings.Tags = strings.Join(tjuptFeedTags(view.Settings.Tags), ", ")
	view.Settings.UseSiteUploadLimit = row.UseSiteUploadLimit
	if limit, err := tjuptSiteUploadLimit(ctx, f.repo); err == nil {
		view.SiteUploadLimitMBps = &limit
	}
	return view, nil
}

// SetSiteUploadLimit 在循环启用或暂停时独立保存上传限速开关。
func (f *TjuptFeed) SetSiteUploadLimit(ctx context.Context, enabled bool) error {
	row, err := f.repo.Control(ctx)
	if err != nil || row.UseSiteUploadLimit == enabled {
		return err
	}
	applied, err := f.repo.SetSiteUploadLimit(ctx, row.SiteUploadLimitRevision, enabled)
	if err == nil && !applied {
		return ErrTjuptFeedBusy
	}
	return err
}

// tjuptSiteUploadLimit 按站点设置的显示规则处理 0 和超过 999 MB/s 的不限速值。
func tjuptSiteUploadLimit(ctx context.Context, repo *repository.TjuptFeedRepository) (int, error) {
	limit, err := repo.SiteUploadLimit(ctx)
	if limit > 999 {
		return 0, err
	}
	return limit, err
}

// Save 校验并保存新设置，保存不会领取 RSS、连接 qB 或改变启停状态。
func (f *TjuptFeed) Save(ctx context.Context, input TjuptFeedInput) error {
	row, err := f.repo.Control(ctx)
	if err != nil {
		return err
	}
	if row.Enabled || row.LeaseUntil > time.Now().Unix() {
		return fmt.Errorf("%w，请停止循环或等待本轮结束后再保存", ErrTjuptFeedBusy)
	}
	current, err := decodeTjuptFeedSettings(row.Policy)
	if err != nil {
		return err
	}
	s := input.TjuptFeedSettings
	s.ActiveLimit = current.ActiveLimit
	if input.ActiveLimit != nil {
		s.ActiveLimit = *input.ActiveLimit
	}
	s.CleanupEnabled, s.UploadThresholdKiB, s.LowSpeedMinutes = current.CleanupEnabled, current.UploadThresholdKiB, current.LowSpeedMinutes
	if input.CleanupEnabled != nil {
		s.CleanupEnabled = *input.CleanupEnabled
	}
	if input.UploadThresholdKiB != nil {
		s.UploadThresholdKiB = *input.UploadThresholdKiB
	}
	if input.LowSpeedMinutes != nil {
		s.LowSpeedMinutes = *input.LowSpeedMinutes
	}
	s.DownloaderID = strings.TrimSpace(s.DownloaderID)
	s.Tags = strings.Join(tjuptFeedTags(s.Tags), ", ")
	s.Category = strings.TrimSpace(s.Category)
	s.SavePath = strings.TrimSpace(s.SavePath)
	if err := validateTjuptFeedSettings(s); err != nil {
		return err
	}
	if s.DownloaderID != "" {
		if _, err := f.plugin.downloader(s.DownloaderID); err != nil {
			return tjuptFeedConfigError("请选择已启用的 qBittorrent 下载器")
		}
	}
	rss, hnr := strings.TrimSpace(input.RSSURL), strings.TrimSpace(input.HNRToken)
	if (input.ClearRSS && rss != "") || (input.ClearHNRToken && hnr != "") {
		return tjuptFeedConfigError("不能同时更新和清空同一个凭据")
	}
	if rss != "" && !validTjuptFeedRSS(rss) {
		return tjuptFeedConfigError("RSS 地址必须是北洋园 HTTPS 订阅链接")
	}
	if len(hnr) > 1024 || strings.IndexFunc(hnr, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return tjuptFeedConfigError("H&R API Token 格式无效")
	}
	row.RSS, err = f.updateSecret(row.RSS, rss, input.ClearRSS)
	if err != nil {
		return err
	}
	row.HNR, err = f.updateSecret(row.HNR, hnr, input.ClearHNRToken)
	if err != nil {
		return err
	}
	if s.CleanupEnabled && len(row.HNR) == 0 {
		return tjuptFeedConfigError("启用自动退种前请保存北洋园 H&R API Token")
	}
	policy, err := json.Marshal(s)
	if err != nil {
		return err
	}
	row.Policy, row.IntervalMinutes = string(policy), s.IntervalMinutes
	applied, err := f.repo.Save(ctx, row, time.Now().Unix())
	if err == nil && !applied {
		return ErrTjuptFeedBusy
	}
	return err
}

// SetEnabled 仅保存启停状态；正式循环在后台领取任务。
func (f *TjuptFeed) SetEnabled(ctx context.Context, enabled bool) error {
	row, err := f.repo.Control(ctx)
	if err != nil || row.Enabled == enabled {
		return err
	}
	if enabled {
		if _, _, err = f.runConfig(row); err != nil {
			return err
		}
		if err = f.checkLegacy(ctx); err != nil {
			return err
		}
	}
	applied, err := f.repo.SetEnabled(ctx, row.Revision, enabled, time.Now().Unix())
	if err == nil && !applied {
		return ErrTjuptFeedBusy
	}
	if err == nil && enabled {
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
	return err
}

// Preview 只读使用保存的 RSS 和体积条件。
func (f *TjuptFeed) Preview(ctx context.Context) ([]tjupt.Candidate, error) {
	row, err := f.repo.Control(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := f.loadConfig(row, true, false)
	if err != nil {
		return nil, err
	}
	if cfg.rss == "" {
		return nil, tjuptFeedConfigError("请先保存北洋园 RSS 地址")
	}
	return f.plugin.PreviewRange(ctx, cfg.rss, cfg.MinGiB, cfg.MaxGiB)
}

// Status 只读查询所选 qB 的北洋园任务与已保存的 H&R 账号。
func (f *TjuptFeed) Status(ctx context.Context) ([]TjuptTorrentStatus, error) {
	row, err := f.repo.Control(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := f.loadConfig(row, false, true)
	if err != nil {
		return nil, err
	}
	if cfg.hnr == "" || cfg.DownloaderID == "" {
		return nil, tjuptFeedConfigError("请先保存 H&R API Token 和下载器")
	}
	if _, err = f.plugin.downloader(cfg.DownloaderID); err != nil {
		return nil, tjuptFeedConfigError("所选 qBittorrent 下载器不可用")
	}
	return f.plugin.Status(ctx, cfg.DownloaderID, cfg.hnr)
}

// Start 启动调度器。只有用户已启用的设置才会触发 RSS 请求。
func (f *TjuptFeed) Start(ctx context.Context) {
	f.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if _, err := f.run(ctx, false); err != nil {
					logx.Warnf("北洋园 RSS", "本轮未完成，请在插件查看运行结果")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-f.wake:
				}
			}
		}()
	})
}

// Trial 只按已保存的规则读取一次 RSS 并暂停添加至多一个新任务。
func (f *TjuptFeed) Trial(ctx context.Context) (TjuptFeedResult, error) {
	return f.run(ctx, true)
}

func (f *TjuptFeed) checkLegacy(ctx context.Context) error {
	legacy, err := f.repo.LegacyControl(ctx)
	if err == nil && (legacy.Enabled || legacy.LeaseUntil > time.Now().Unix()) {
		err = fmt.Errorf("%w，请停止旧版北洋园自动化并等待本轮结束", ErrTjuptFeedBusy)
	}
	return err
}

func (f *TjuptFeed) runConfig(row repository.TjuptFeedControl) (tjuptFeedConfig, downloaderclient.Downloader, error) {
	cfg, err := f.loadConfig(row, true, false)
	if err != nil {
		return cfg, downloaderclient.Downloader{}, err
	}
	if cfg.rss == "" {
		return cfg, downloaderclient.Downloader{}, tjuptFeedConfigError("请先保存北洋园 RSS 地址")
	}
	d, err := f.plugin.downloader(cfg.DownloaderID)
	if err != nil {
		return cfg, d, tjuptFeedConfigError("请选择已启用的 qBittorrent 下载器")
	}
	return cfg, d, nil
}

func (f *TjuptFeed) loadConfig(row repository.TjuptFeedControl, rss, hnr bool) (tjuptFeedConfig, error) {
	var cfg tjuptFeedConfig
	settings, err := decodeTjuptFeedSettings(row.Policy)
	if err != nil {
		return cfg, err
	}
	cfg.TjuptFeedSettings = settings
	cfg.storedTags = settings.Tags
	cfg.Tags = strings.Join(tjuptFeedTags(settings.Tags), ", ")
	if err := validateTjuptFeedSettings(cfg.TjuptFeedSettings); err != nil {
		return cfg, err
	}
	for _, part := range []struct {
		use    bool
		cipher []byte
		value  *string
	}{{rss, row.RSS, &cfg.rss}, {hnr, row.HNR, &cfg.hnr}} {
		if part.use && len(part.cipher) > 0 {
			raw, err := f.secrets.open(part.cipher)
			if err != nil {
				return cfg, tjuptFeedConfigError("无法读取已保存的凭据，请重新保存")
			}
			*part.value = string(raw)
		}
	}
	return cfg, nil
}

func (f *TjuptFeed) updateSecret(current []byte, value string, clear bool) ([]byte, error) {
	if clear {
		return nil, nil
	}
	if value == "" {
		return current, nil
	}
	return f.secrets.seal([]byte(value))
}

func validateTjuptFeedSettings(s TjuptFeedSettings) error {
	if s.IntervalMinutes < 1 || s.IntervalMinutes > 1440 || !validTjuptSizeRange(s.MinGiB, s.MaxGiB) ||
		!tjuptNumberIn(s.TotalLimitGiB, 0.1, 20000) || s.ActiveLimit < 0 {
		return tjuptFeedConfigError("请核对种子体积、容量、任务数和 RSS 获取间隔")
	}
	if s.UploadThresholdKiB < 1 || s.UploadThresholdKiB > 1048576 || s.LowSpeedMinutes < 1 || s.LowSpeedMinutes > 1440 {
		return tjuptFeedConfigError("请核对自动退种的上传阈值和持续低速时长")
	}
	if len(s.DownloaderID) > 128 || len(s.Category) > 128 || len(s.SavePath) > 512 ||
		strings.IndexFunc(s.Category+s.SavePath, unicode.IsControl) >= 0 ||
		(s.SavePath != "" && (!path.IsAbs(s.SavePath) || path.Clean(s.SavePath) != s.SavePath || strings.Contains(s.SavePath, "\\"))) {
		return tjuptFeedConfigError("分类或保存目录格式无效")
	}
	tags := tjuptFeedTags(s.Tags)
	if len(tags) > 15 {
		return tjuptFeedConfigError("最多设置 15 个标签")
	}
	for _, tag := range tags {
		if len(tag) > 64 || strings.IndexFunc(tag, unicode.IsControl) >= 0 || strings.HasPrefix(tag, tjuptFeedMarkerPrefix) {
			return tjuptFeedConfigError("标签格式无效")
		}
	}
	return nil
}

func decodeTjuptFeedSettings(policy string) (TjuptFeedSettings, error) {
	settings := TjuptFeedSettings{UploadThresholdKiB: tjuptFeedDefaultUploadKiB, LowSpeedMinutes: tjuptFeedDefaultLowSpeedMinutes}
	err := json.Unmarshal([]byte(policy), &settings)
	return settings, err
}

func tjuptFeedTags(raw string) []string {
	common := downloaderclient.TjuptFeedDisplayTag
	tags := []string{common}
	seen := map[string]bool{common: true, tjuptTag: true}
	for _, tag := range strings.Split(raw, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	return tags
}

func validTjuptFeedRSS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 4096 && u.Scheme == "https" &&
		(u.Hostname() == "tjupt.org" || u.Hostname() == "www.tjupt.org") &&
		(u.Port() == "" || u.Port() == "443") && u.User == nil && u.Fragment == "" &&
		(u.Path == "/rss.php" || u.Path == "/torrentrss.php")
}

func tjuptFeedConfigError(message string) error {
	return fmt.Errorf("%w：%s", ErrTjuptFeedConfig, message)
}
