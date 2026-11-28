import { createDashboardLayoutStore } from './dashboardLayout'

export const SITES_PANEL_LABELS = {
  summary: '做种汇总',
  'site-chart': '站点空间分布',
  'group-chart': '官组分布',
  'site-stats': '做种站点统计',
  'group-stats': '做种官组统计',
  'local-scan': '本地文件扫描',
} as const

export type SitesPanelId = keyof typeof SITES_PANEL_LABELS

export const useSitesLayoutStore = createDashboardLayoutStore(
  'sitesLayout',
  '/api/ui_settings/sites_layout',
  SITES_PANEL_LABELS,
)
