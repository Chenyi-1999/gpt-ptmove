import { createDashboardLayoutStore } from './dashboardLayout'

export const HOME_PANEL_LABELS = {
  tasks: '任务卡',
  speed: '实时速度',
  traffic: '流量趋势',
  downloaders: '下载器',
} as const

export type HomePanelId = keyof typeof HOME_PANEL_LABELS

export const useHomeLayoutStore = createDashboardLayoutStore(
  'homeLayout',
  '/api/ui_settings/home_layout',
  HOME_PANEL_LABELS,
)
