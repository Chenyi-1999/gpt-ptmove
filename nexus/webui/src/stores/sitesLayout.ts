import axios from 'axios'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

export const SITES_PANEL_LABELS = {
  summary: '做种汇总',
  'site-chart': '站点空间分布',
  'group-chart': '官组分布',
  'site-stats': '做种站点统计',
  'group-stats': '做种官组统计',
  'local-scan': '本地文件扫描',
} as const

export type SitesPanelId = keyof typeof SITES_PANEL_LABELS

interface SitesLayout {
  columns: 1 | 2
  order: SitesPanelId[]
}

const endpoint = '/api/ui_settings/sites_layout'
const panelIds = Object.keys(SITES_PANEL_LABELS) as SitesPanelId[]

function isPanelId(value: unknown): value is SitesPanelId {
  return typeof value === 'string' && Object.prototype.hasOwnProperty.call(SITES_PANEL_LABELS, value)
}

function normalizeLayout(value: unknown): SitesLayout {
  const incoming = value && typeof value === 'object' ? (value as Partial<SitesLayout>) : {}
  const savedOrder = Array.isArray(incoming.order) ? incoming.order.filter(isPanelId) : []
  return {
    columns: incoming.columns === 1 ? 1 : 2,
    order: [...new Set([...savedOrder, ...panelIds])],
  }
}

export const useSitesLayoutStore = defineStore('sitesLayout', () => {
  const layout = ref<SitesLayout>(normalizeLayout(undefined))
  const columns = computed(() => layout.value.columns)
  const order = computed(() => layout.value.order)
  let loaded = false
  let loading: Promise<void> | undefined
  let saveRevision = 0

  function load(force = false): Promise<void> {
    if (loading) return loading
    if (!force && loaded) return Promise.resolve()
    const revision = saveRevision
    loading = axios.get<unknown>(endpoint)
      .then(({ data }) => {
        if (revision === saveRevision) {
          layout.value = normalizeLayout(data)
          loaded = true
        }
      })
      .finally(() => { loading = undefined })
    return loading
  }

  async function save(draft: SitesLayout): Promise<void> {
    const next = normalizeLayout(draft)
    const { data } = await axios.post<{ success?: boolean; message?: string }>(endpoint, next)
    if (data?.success !== true) throw new Error(data?.message || '布局保存失败')
    saveRevision++
    layout.value = next
    loaded = true
  }

  return { columns, order, load, save }
})
