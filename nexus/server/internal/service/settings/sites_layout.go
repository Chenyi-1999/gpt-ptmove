package settings

import (
	"fmt"
)

var sitesLayoutPanelOrder = []string{
	"summary",
	"site-chart",
	"group-chart",
	"site-stats",
	"group-stats",
	"local-scan",
}

// SitesLayoutSettings 保存做种检索页面的列数和模块顺序。
type SitesLayoutSettings struct {
	Columns int      `json:"columns"`
	Order   []string `json:"order"`
}

// GetSitesLayoutSettings 读取做种检索布局，配置缺失或格式异常时返回默认布局。
// 参数/返回：无参数；返回经过模块 ID 归一化的列数和顺序。
// 失败场景：无，配置异常会使用默认值。
// 副作用：无副作用，仅读取内存配置。
func (s *SettingsService) GetSitesLayoutSettings() SitesLayoutSettings {
	defaults := SitesLayoutSettings{Columns: 2, Order: append([]string(nil), sitesLayoutPanelOrder...)}
	cfg := s.cfg.Get()
	uiSettings, ok := cfg["ui_settings"].(map[string]any)
	if !ok {
		return defaults
	}
	layout, ok := uiSettings["sites_layout"].(map[string]any)
	if !ok {
		return defaults
	}

	switch columns := layout["columns"].(type) {
	case float64:
		if columns == 1 || columns == 2 {
			defaults.Columns = int(columns)
		}
	case int:
		if columns == 1 || columns == 2 {
			defaults.Columns = columns
		}
	}

	if rawOrder, exists := layout["order"]; exists {
		order := make([]string, 0, len(sitesLayoutPanelOrder))
		if values, ok := rawOrder.([]any); ok {
			for _, value := range values {
				if panelID, ok := value.(string); ok {
					order = append(order, panelID)
				}
			}
		} else if values, ok := rawOrder.([]string); ok {
			order = append(order, values...)
		}
		defaults.Order = normalizeSitesLayoutOrder(order)
	}
	return defaults
}

// SaveSitesLayoutSettings 保存做种检索页面布局，并保留其他 UI 配置。
// 参数/返回：settings 为列数和模块顺序；返回配置写入错误。
// 失败场景：列数不是 1 或 2 时返回校验错误，配置文件写入失败时返回底层错误。
// 副作用：将布局写入持久化配置文件。
func (s *SettingsService) SaveSitesLayoutSettings(settings SitesLayoutSettings) error {
	if settings.Columns != 1 && settings.Columns != 2 {
		return fmt.Errorf("列数必须为 1 或 2")
	}

	cfg := s.cfg.Get()
	uiSettings := ensureMap(cfg, "ui_settings")
	uiSettings["sites_layout"] = map[string]any{
		"columns": settings.Columns,
		"order":   normalizeSitesLayoutOrder(settings.Order),
	}
	cfg["ui_settings"] = uiSettings
	return s.cfg.Save(cfg)
}

func normalizeSitesLayoutOrder(order []string) []string {
	known := make(map[string]bool, len(sitesLayoutPanelOrder))
	for _, panelID := range sitesLayoutPanelOrder {
		known[panelID] = true
	}

	result := make([]string, 0, len(sitesLayoutPanelOrder))
	seen := make(map[string]bool, len(sitesLayoutPanelOrder))
	for _, panelID := range order {
		if known[panelID] && !seen[panelID] {
			result = append(result, panelID)
			seen[panelID] = true
		}
	}
	for _, panelID := range sitesLayoutPanelOrder {
		if !seen[panelID] {
			result = append(result, panelID)
		}
	}
	return result
}
