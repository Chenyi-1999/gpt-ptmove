package settings

import (
	"errors"
	"slices"
)

const (
	homeLayoutKey  = "home_layout"
	sitesLayoutKey = "sites_layout"
)

var homeLayoutPanelOrder = []string{"tasks", "speed", "traffic", "downloaders"}

var sitesLayoutPanelOrder = []string{
	"summary",
	"site-chart",
	"group-chart",
	"site-stats",
	"group-stats",
	"local-scan",
}

// DashboardLayoutSettings 保存页面列数、模块顺序和隐藏的模块。
type DashboardLayoutSettings struct {
	Columns int      `json:"columns"`
	Order   []string `json:"order"`
	Hidden  []string `json:"hidden"`
}

// GetHomeLayoutSettings 读取首页布局，缺失时使用默认的两列布局和模块顺序。
func (s *SettingsService) GetHomeLayoutSettings() DashboardLayoutSettings {
	return s.getDashboardLayout(homeLayoutKey, homeLayoutPanelOrder)
}

// SaveHomeLayoutSettings 保存首页布局，返回校验或写入错误。
func (s *SettingsService) SaveHomeLayoutSettings(settings DashboardLayoutSettings) error {
	return s.saveDashboardLayout(homeLayoutKey, homeLayoutPanelOrder, settings)
}

// GetSitesLayoutSettings 读取做种检索布局，缺失时使用默认的两列布局和模块顺序。
func (s *SettingsService) GetSitesLayoutSettings() DashboardLayoutSettings {
	return s.getDashboardLayout(sitesLayoutKey, sitesLayoutPanelOrder)
}

// SaveSitesLayoutSettings 保存做种检索布局，旧版请求未传隐藏模块时保留现有值。
func (s *SettingsService) SaveSitesLayoutSettings(settings DashboardLayoutSettings) error {
	return s.saveDashboardLayout(sitesLayoutKey, sitesLayoutPanelOrder, settings)
}

func (s *SettingsService) getDashboardLayout(key string, panels []string) DashboardLayoutSettings {
	layout := nestedMap(s.cfg.Get(), "ui_settings", key)
	settings := DashboardLayoutSettings{
		Columns: 2,
		Order:   dashboardPanelIDs(layout["order"]),
		Hidden:  dashboardPanelIDs(layout["hidden"]),
	}
	if layout["columns"] == float64(1) {
		settings.Columns = 1
	}
	return normalizeDashboardLayout(settings, panels)
}

func (s *SettingsService) saveDashboardLayout(key string, panels []string, settings DashboardLayoutSettings) error {
	if settings.Columns != 1 && settings.Columns != 2 {
		return errors.New("列数必须为 1 或 2")
	}

	return s.cfg.UpdateUISettings(func(uiSettings map[string]any) {
		layout := ensureMap(uiSettings, key)
		if settings.Hidden == nil {
			settings.Hidden = dashboardPanelIDs(layout["hidden"])
		}
		settings = normalizeDashboardLayout(settings, panels)
		layout["columns"] = settings.Columns
		layout["order"] = settings.Order
		layout["hidden"] = settings.Hidden
	})
}

func normalizeDashboardLayout(settings DashboardLayoutSettings, panels []string) DashboardLayoutSettings {
	settings.Order = normalizeDashboardPanelIDs(settings.Order, panels)
	for _, panelID := range panels {
		if !slices.Contains(settings.Order, panelID) {
			settings.Order = append(settings.Order, panelID)
		}
	}
	settings.Hidden = normalizeDashboardPanelIDs(settings.Hidden, panels)
	return settings
}

func normalizeDashboardPanelIDs(ids, panels []string) []string {
	result := make([]string, 0, len(panels))
	for _, id := range ids {
		if slices.Contains(panels, id) && !slices.Contains(result, id) {
			result = append(result, id)
		}
	}
	return result
}

func dashboardPanelIDs(raw any) []string {
	items := toSlice(raw)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, ok := item.(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
