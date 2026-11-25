package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"gorm.io/gorm"
)

const tjuptGiB = 1 << 30

// TjuptAutomationSettings 是管理员可配置的下载和做种清理策略。
type TjuptAutomationSettings struct {
	Enabled            bool    `json:"enabled"`
	CleanupMode        string  `json:"cleanup_mode"`
	DownloaderID       string  `json:"downloader_id"`
	IntervalMinutes    int     `json:"interval_minutes"`
	MinGiB             float64 `json:"min_gib"`
	MaxGiB             float64 `json:"max_gib"`
	TotalLimitGiB      float64 `json:"total_limit_gib"`
	BufferHours        float64 `json:"buffer_hours"`
	UploadThresholdKiB int64   `json:"upload_threshold_kib"`
	LowSpeedMinutes    int     `json:"low_speed_minutes"`
	ActiveLimit        int     `json:"active_limit"`
}

// TjuptAutomationView 是插件可显示的设置、历史运行和容量使用量。
type TjuptAutomationView struct {
	Settings         TjuptAutomationSettings `json:"settings"`
	RSSConfigured    bool                    `json:"rss_configured"`
	WriteConfigured  bool                    `json:"write_configured"`
	DeleteConfigured bool                    `json:"delete_configured"`
	SaveRoot         string                  `json:"save_root"`
	LastRunAt        string                  `json:"last_run_at,omitempty"`
	LastMessage      string                  `json:"last_message"`
	UsedGiB          float64                 `json:"used_gib"`
	Items            []TjuptManagedItem      `json:"items"`
}

// TjuptManagedItem 是容量台账中仍需管理的种子。
type TjuptManagedItem struct {
	Hash      string `json:"hash"`
	Title     string `json:"title"`
	SizeBytes int64  `json:"size_bytes"`
	State     string `json:"state"`
	LowSince  string `json:"low_since,omitempty"`
}

type tjuptAcquisition interface {
	AutoCandidates(context.Context, string) ([]tjupt.AutoCandidate, error)
	DownloadCandidate(context.Context, tjupt.AutoCandidate) (tjupt.Torrent, error)
}

// TjuptAutomation 定时检查自己的容量台账、RSS 候选和 H&R 清理条件。
type TjuptAutomation struct {
	plugin      *TjuptRSSService
	repo        *repository.TjuptAutomationRepository
	site        tjuptAcquisition
	rss         string
	saveRoot    string
	exclusiveQB bool
}

// ErrTjuptPolicy 表示规则或运行配置不能安全使用。
var ErrTjuptPolicy = errors.New("北洋园自动化设置无效")

func defaultTjuptAutomation() TjuptAutomationSettings {
	return TjuptAutomationSettings{
		CleanupMode: "mark", IntervalMinutes: 5, MaxGiB: 10, TotalLimitGiB: 50,
		BufferHours: 6, UploadThresholdKiB: 100, LowSpeedMinutes: 60, ActiveLimit: 3,
	}
}

// NewTjuptAutomation 初始化持久规则。默认停止调度，RSS 及账号密钥由部署环境提供。
func NewTjuptAutomation(db *gorm.DB, plugin *TjuptRSSService) (*TjuptAutomation, error) {
	defaults := defaultTjuptAutomation()
	policy, err := json.Marshal(defaults)
	if err != nil {
		return nil, err
	}
	repo, err := repository.NewTjuptAutomationRepository(db, string(policy), defaults.IntervalMinutes)
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(os.Getenv("PTMOVE_TJUPT_AUTO_ROOT"))
	if root == "/" || !path.IsAbs(root) || path.Clean(root) != root || strings.ContainsAny(root, "\\\r\n\x00") {
		root = ""
	}
	return &TjuptAutomation{
		plugin: plugin, repo: repo, site: tjupt.New(), saveRoot: root,
		rss:         string(loadTjuptFileSecret("PTMOVE_TJUPT_RSS_URL", 20, 4096, false)),
		exclusiveQB: os.Getenv("PTMOVE_TJUPT_AUTO_EXCLUSIVE_QB") == "1",
	}, nil
}

func (a *TjuptAutomation) writeConfigured() bool {
	return a.saveRoot != "" && a.plugin.RetireConfigured()
}

// View 返回当前只读配置和容量使用量，不访问站点或下载器。
func (a *TjuptAutomation) View(ctx context.Context) (TjuptAutomationView, error) {
	control, err := a.repo.Control(ctx)
	if err != nil {
		return TjuptAutomationView{}, err
	}
	var settings TjuptAutomationSettings
	if err = json.Unmarshal([]byte(control.Policy), &settings); err != nil {
		return TjuptAutomationView{}, err
	}
	rows, err := a.repo.LiveTasks(ctx)
	if err != nil {
		return TjuptAutomationView{}, err
	}
	view := TjuptAutomationView{
		Settings: settings, WriteConfigured: a.writeConfigured(), SaveRoot: a.saveRoot,
		DeleteConfigured: a.writeConfigured() && a.exclusiveQB,
		RSSConfigured:    a.rss != "", LastRunAt: tjuptTime(control.LastRun), LastMessage: control.LastMessage,
		Items: make([]TjuptManagedItem, 0, len(rows)),
	}
	for _, row := range rows {
		view.UsedGiB += float64(row.SizeBytes) / tjuptGiB
		view.Items = append(view.Items, TjuptManagedItem{
			Hash: row.Hash, Title: row.Title, SizeBytes: row.SizeBytes, State: row.State, LowSince: tjuptTime(row.LowSince),
		})
	}
	return view, nil
}

// Save 保存规则。当队列中有已登记种子时继续使用原来的下载器和工作目录。
func (a *TjuptAutomation) Save(ctx context.Context, settings TjuptAutomationSettings) error {
	if err := validateTjuptAutomation(settings); err != nil {
		return err
	}
	if settings.Enabled {
		if a.rss == "" || !a.writeConfigured() {
			return errors.Join(ErrTjuptPolicy, errors.New("启用前请配置北洋园 RSS、账号签名和飞牛专用目录"))
		}
		if settings.CleanupMode == "delete" && !a.exclusiveQB {
			return errors.Join(ErrTjuptPolicy, errors.New("实际删除需要插件独占的飞牛 qB 实例"))
		}
		d, err := a.plugin.downloader(settings.DownloaderID)
		if err != nil || d.APIKey == "" {
			return errors.Join(ErrTjuptPolicy, ErrTjuptDownloader)
		}
	}
	policy, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	err = a.repo.SaveControl(ctx, string(policy), settings.Enabled, settings.IntervalMinutes, time.Now().Unix(), settings.DownloaderID, a.saveRoot)
	if errors.Is(err, repository.ErrTjuptAutomationScope) {
		return errors.Join(ErrTjuptPolicy, err)
	}
	return err
}

func validateTjuptAutomation(s TjuptAutomationSettings) error {
	if (s.CleanupMode != "mark" && s.CleanupMode != "delete") ||
		s.IntervalMinutes < 1 || s.IntervalMinutes > 1440 ||
		!validTjuptSizeRange(s.MinGiB, s.MaxGiB) || !tjuptNumberIn(s.TotalLimitGiB, 0.1, 20000) ||
		!tjuptNumberIn(s.BufferHours, 0, 720) || s.UploadThresholdKiB < 1 || s.UploadThresholdKiB > 1048576 ||
		s.LowSpeedMinutes < 1 || s.LowSpeedMinutes > 10080 || s.ActiveLimit < 1 || s.ActiveLimit > 50 ||
		(s.Enabled && strings.TrimSpace(s.DownloaderID) == "") || len(s.DownloaderID) > 128 {
		return ErrTjuptPolicy
	}
	return nil
}

func tjuptNumberIn(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && value >= minimum && value <= maximum
}

func validTjuptSizeRange(minimum, maximum float64) bool {
	return tjuptNumberIn(minimum, 0, 2000) && tjuptNumberIn(maximum, 0.1, 2000) && minimum <= maximum
}

func tjuptTime(stamp int64) string {
	if stamp <= 0 {
		return ""
	}
	return time.Unix(stamp, 0).UTC().Format(time.RFC3339)
}

// Start 启动后台调度。部署条件未满足时只开放插件界面与只读功能。
func (a *TjuptAutomation) Start(ctx context.Context) {
	if !a.writeConfigured() || a.rss == "" {
		return
	}
	go func() {
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := a.runIfDue(ctx); err != nil {
					logx.Warnf("北洋园刷流", "自动化运行状态未能写入")
				}
			}
		}
	}()
}
