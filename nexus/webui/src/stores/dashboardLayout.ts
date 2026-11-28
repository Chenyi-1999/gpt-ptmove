import axios from 'axios'
import { defineStore } from 'pinia'
import { computed, shallowRef } from 'vue'

export interface DashboardLayout<PanelID extends string> {
  columns: 1 | 2
  order: PanelID[]
  hidden: PanelID[]
}

export function createDashboardLayoutStore<PanelID extends string>(
  id: string,
  endpoint: string,
  panels: Readonly<Record<PanelID, string>>,
) {
  const panelIds = Object.keys(panels) as PanelID[]
  const known = new Set<string>(panelIds)

  function panelList(value: unknown) {
    return Array.isArray(value)
      ? value.filter((panel): panel is PanelID => typeof panel === 'string' && known.has(panel))
      : []
  }

  function normalize(value: unknown): DashboardLayout<PanelID> {
    const incoming =
      value && typeof value === 'object' ? (value as Partial<DashboardLayout<PanelID>>) : {}
    return {
      columns: incoming.columns === 1 ? 1 : 2,
      order: [...new Set([...panelList(incoming.order), ...panelIds])],
      hidden: [...new Set(panelList(incoming.hidden))],
    }
  }

  return defineStore(id, () => {
    const layout = shallowRef(normalize(undefined))
    const columns = computed(() => layout.value.columns)
    const order = computed(() => layout.value.order)
    const hidden = computed(() => layout.value.hidden)
    const visibleOrder = computed(() =>
      layout.value.order.filter((panel) => !layout.value.hidden.includes(panel)),
    )
    let loaded = false
    let loading: Promise<void> | undefined
    let saveRevision = 0

    function load(force = false): Promise<void> {
      if (loading) return loading
      if (!force && loaded) return Promise.resolve()
      const revision = saveRevision
      loading = axios
        .get<unknown>(endpoint, { timeout: 10_000 })
        .then(({ data }) => {
          if (revision === saveRevision) {
            layout.value = normalize(data)
            loaded = true
          }
        })
        .finally(() => {
          loading = undefined
        })
      return loading
    }

    async function save(draft: DashboardLayout<PanelID>): Promise<void> {
      const next = normalize(draft)
      const { data } = await axios.post<{ success?: boolean; message?: string }>(endpoint, next)
      if (data?.success !== true) throw new Error(data?.message || '布局保存失败')
      saveRevision++
      layout.value = next
      loaded = true
    }

    return { columns, order, hidden, visibleOrder, load, save }
  })
}
