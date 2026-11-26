export type PluginSetting = string | number | boolean

export type PluginField = {
  key: string
  label: string
  type: 'string' | 'boolean' | 'integer'
  default?: PluginSetting
  options?: string[]
}

type PluginIdentity = {
  id: string
  name: string
  version: string
  description?: string
  enabled?: boolean
}

type LegacyBlock = {
  kind: 'text' | 'link' | 'metric'
  title: string
  text?: string
  target?: string
  metric?: string
}

type CatalogPlugin = PluginIdentity & {
  kind?: 'builtin' | 'legacy'
  blocks?: LegacyBlock[]
}

export type WasmPlugin = PluginIdentity & {
  kind: 'wasm'
  enabled: boolean
  fields?: PluginField[]
  actions: { id: string; label: string }[]
  settings?: Record<string, PluginSetting>
}

export type PluginInfo = CatalogPlugin | WasmPlugin

export type PluginCatalog = {
  items: PluginInfo[]
  issues: { file: string; message: string }[]
  metrics: Record<string, number | null>
}

export type PluginActionResult = {
  message?: string
  blocks?: {
    kind: 'text' | 'metric' | 'table'
    title: string
    text?: string
    value?: string | number
    columns?: string[]
    rows?: string[][]
  }[]
}

export async function requestPlugin<T>(
  path: string,
  method: 'GET' | 'POST' | 'PUT',
  errorMessage: string,
  payload?: Record<string, unknown>,
): Promise<T> {
  const response = await fetch('/api/plugins' + path, {
    method,
    ...(payload && {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  })
  const data = await response.json().catch(() => null)
  if (!response.ok || !data) {
    throw new Error(typeof data?.message === 'string' ? data.message : errorMessage)
  }
  return data as T
}
