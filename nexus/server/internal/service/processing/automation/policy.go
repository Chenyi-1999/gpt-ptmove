package automation

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

const GiB int64 = 1024 * 1024 * 1024

// Settings 定义安全默认值。全局执行与自动发布开关相互独立。
type Settings struct {
	Enabled              bool    `json:"enabled"`
	AutoPublish          bool    `json:"auto_publish"`
	ScheduleEnabled      bool    `json:"schedule_enabled"`
	ScheduleDay          int     `json:"schedule_day"`
	ScheduleTime         string  `json:"schedule_time"`
	Timezone             string  `json:"timezone"`
	MonthlyLimit         int     `json:"monthly_limit"`
	MinSizeGiB           float64 `json:"min_size_gib"`
	MaxSizeGiB           float64 `json:"max_size_gib"`
	MonthlyBudgetGiB     float64 `json:"monthly_budget_gib"`
	MinSeeders           int     `json:"min_seeders"`
	MinLeechers          int     `json:"min_leechers"`
	ReserveSpaceGiB      float64 `json:"reserve_space_gib"`
	PreferOfficial       bool    `json:"prefer_official"`        // 兼容旧客户端；官种资格始终验证。
	TargetAbsentPriority bool    `json:"target_absent_priority"` // 兼容旧客户端；目标查重始终验证。
	SourceSite           string  `json:"source_site"`
	TargetSite           string  `json:"target_site"`
	DownloaderID         string  `json:"downloader_id"`
	SavePath             string  `json:"save_path"`
}

// Candidate 为只读扫描结果；种子字节只在内存处理，不向 API 导出。
type Candidate struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Size                 int64    `json:"size"`
	InfoHash             string   `json:"info_hash"`
	Seeders              int      `json:"seeders"`
	Leechers             int      `json:"leechers"`
	Official             bool     `json:"official"`
	Episode              bool     `json:"episode"`
	PreviouslyDownloaded bool     `json:"previously_downloaded"`
	RestrictionsKnown    bool     `json:"restrictions_known"`
	Allowed              bool     `json:"allowed"`
	DuplicateKnown       bool     `json:"duplicate_known"`
	Duplicate            bool     `json:"duplicate"`
	Eligible             bool     `json:"eligible"`
	Reasons              []string `json:"reasons"`
	Torrent              []byte   `json:"-"`
	DetailURL            string   `json:"-"`
}

// Defaults 返回关闭执行和调度的首装配置。
func Defaults() Settings {
	return Settings{ScheduleDay: 1, ScheduleTime: "02:00", Timezone: "Asia/Shanghai", MonthlyLimit: 6, MinSizeGiB: 1, MaxSizeGiB: 10, MonthlyBudgetGiB: 60, MinSeeders: 3, PreferOfficial: true, TargetAbsentPriority: true, SourceSite: "zmpt", TargetSite: "novahd"}
}

// DecodeSettings 读取完整配置并校验边界，损坏配置不会启用模块。
func DecodeSettings(raw string) (Settings, error) {
	v := Defaults()
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return v, err
		}
	}
	return v, Validate(v)
}

// MergeSettings 只接受白名单字段，拒绝未知参数与非有限数。
func MergeSettings(current Settings, patch map[string]any) (Settings, error) {
	data, _ := json.Marshal(current)
	all := map[string]any{}
	_ = json.Unmarshal(data, &all)
	for key, val := range patch {
		if _, ok := all[key]; !ok {
			return current, errors.New("未知自动化设置字段: " + key)
		}
		all[key] = val
	}
	data, err := json.Marshal(all)
	if err != nil {
		return current, err
	}
	var next Settings
	if err = json.Unmarshal(data, &next); err != nil {
		return current, errors.New("自动化设置类型错误")
	}
	return next, Validate(next)
}

// Validate 拒绝会破坏配额、调度或路径约束的配置。
func Validate(s Settings) error {
	if s.MonthlyLimit < 1 || s.MonthlyLimit > 100 || s.MinSeeders < 0 || s.MinSeeders > 100000 || s.MinLeechers < 0 || s.MinLeechers > 100000 {
		return errors.New("月度次数需为 1–100，做种数不可为负")
	}
	for _, n := range []float64{s.MinSizeGiB, s.MaxSizeGiB, s.MonthlyBudgetGiB} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 100000 {
			return errors.New("体积和预算必须为有限正数")
		}
	}
	if math.IsNaN(s.ReserveSpaceGiB) || math.IsInf(s.ReserveSpaceGiB, 0) || s.ReserveSpaceGiB < 0 || s.ReserveSpaceGiB > 100000 {
		return errors.New("保留空间需为有限非负数")
	}
	if s.MinSizeGiB > s.MaxSizeGiB || s.MaxSizeGiB > s.MonthlyBudgetGiB {
		return errors.New("体积范围超出月度预算")
	}
	if s.Timezone != "Asia/Shanghai" || s.ScheduleDay < 1 || s.ScheduleDay > 28 {
		return errors.New("调度必须使用上海时区且日期为 1–28")
	}
	if _, err := time.Parse("15:04", s.ScheduleTime); err != nil {
		return errors.New("调度时间应为 HH:mm")
	}
	if strings.TrimSpace(s.SourceSite) == "" || strings.TrimSpace(s.TargetSite) == "" || s.SourceSite == s.TargetSite {
		return errors.New("请选择不同的源站和目标站")
	}
	if s.Enabled && (strings.TrimSpace(s.DownloaderID) == "" || strings.TrimSpace(s.SavePath) == "") {
		return errors.New("启用前需选择原生下载器与保存目录")
	}
	return nil
}

// SourceRejection 返回可在读取种子元数据前确定的源站排除原因。
// 返回空列表时仍需核验完整体积、做种情况和目标站查重，未确认官方身份的候选不通过。
func SourceRejection(c Candidate) []string {
	reasons := []string{}
	if !c.Official {
		reasons = append(reasons, "未确认源站官种")
	}
	if !c.RestrictionsKnown {
		reasons = append(reasons, "禁转检查未知")
	} else if !c.Allowed {
		reasons = append(reasons, "源站禁止或限制转载")
	}
	if c.Episode {
		reasons = append(reasons, "源站标记为分集")
	}
	if c.PreviouslyDownloaded {
		reasons = append(reasons, "源种子已下载或正在下载")
	}
	return reasons
}

// Evaluate 校验源站资格、目标缺失和完整种子信息，任一条件不满足则拒绝。
func Evaluate(c Candidate, s Settings) Candidate {
	c.Reasons = SourceRejection(c)
	if len(c.Reasons) > 0 && len(c.Torrent) == 0 {
		// 源站已确定排除，且读取器未访问种子下载或目标查重地址。
		c.Eligible = false
		return c
	}
	if !c.DuplicateKnown {
		c.Reasons = append(c.Reasons, "目标查重未知")
	} else if c.Duplicate {
		c.Reasons = append(c.Reasons, "目标已有种子")
	}
	if c.Size < int64(s.MinSizeGiB*float64(GiB)) || c.Size > int64(s.MaxSizeGiB*float64(GiB)) {
		c.Reasons = append(c.Reasons, "完整种子体积不符合规则")
	}
	if c.Seeders < s.MinSeeders {
		c.Reasons = append(c.Reasons, "做种数不足")
	}
	if s.MinLeechers > 0 && c.Leechers < s.MinLeechers {
		c.Reasons = append(c.Reasons, "下载数未知或不足")
	}
	if len(c.InfoHash) != 40 || len(c.Torrent) == 0 {
		c.Reasons = append(c.Reasons, "缺少完整种子元数据")
	}
	c.Eligible = len(c.Reasons) == 0
	return c
}

// Rank 将合格条目按完整体积降序排列，同体积优先当前做种数较多的条目。
func Rank(rows []Candidate, _ Settings) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		return a.Seeders > b.Seeders
	})
}

// Month 按固定上海时区生成配额月份，与宿主机时区无关。
func Month(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01")
}

// Due 判断当月计划时刻是否到达；调度默认关闭。
func Due(now time.Time, s Settings) bool {
	if !s.Enabled || !s.ScheduleEnabled {
		return false
	}
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	clock, _ := time.Parse("15:04", s.ScheduleTime)
	due := time.Date(local.Year(), local.Month(), s.ScheduleDay, clock.Hour(), clock.Minute(), 0, 0, local.Location())
	return !local.Before(due)
}
