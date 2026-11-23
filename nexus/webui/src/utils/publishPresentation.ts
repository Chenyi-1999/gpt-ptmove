const publishStatuses: Record<
  string,
  { label: string; tone: 'success' | 'failed' | 'running' | 'neutral' }
> = {
  success: { label: '发布成功', tone: 'success' },
  exists: { label: '已存在', tone: 'neutral' },
  edited: { label: '已编辑', tone: 'neutral' },
  failed: { label: '发布失败', tone: 'failed' },
  queued: { label: '待发布', tone: 'running' },
  running: { label: '发布中', tone: 'running' },
  filtered: { label: '已过滤', tone: 'neutral' },
  pre_check_limit: { label: '预检查受限', tone: 'failed' },
  cancelled: { label: '已取消', tone: 'neutral' },
  incomplete: { label: '未完成', tone: 'neutral' },
}

export const getPublishDisplayStatus = (status: string) => {
  const key = status?.trim()
  return Object.prototype.hasOwnProperty.call(publishStatuses, key)
    ? publishStatuses[key]!
    : publishStatuses.incomplete!
}

export const getPublishResponseStatus = (result: {
  success?: boolean
  is_existing_torrent?: boolean
  auto_edit_result?: { success?: boolean } | null
  source_restriction_status?: string
}) => {
  if (
    result.source_restriction_status === 'denied' ||
    result.source_restriction_status === 'unknown'
  )
    return 'failed'
  if (result.success === false) return 'failed'
  if (result.success !== true) return 'incomplete'
  if (result.auto_edit_result?.success === true) return 'edited'
  if (result.is_existing_torrent === true) return 'exists'
  return 'success'
}

export const getTaskDisplayStatus = (task: {
  kind: string
  status: string
  progressText: string
  outcome?: string
}) => {
  if (task.kind === 'media_fill' && task.outcome === 'partial')
    return { label: '部分完成', tone: 'neutral' } as const
  if (task.outcome)
    return getPublishDisplayStatus(task.outcome === 'published' ? 'success' : task.outcome)
  if (task.kind === 'publish_log') return getPublishDisplayStatus(task.progressText)
  if (task.kind === 'publish_queue' && task.status === 'success') {
    return { label: '队列处理完成', tone: 'neutral' } as const
  }
  if (task.status === 'running') return { label: '进行中', tone: 'running' } as const
  if (task.status === 'failed') return { label: '失败', tone: 'failed' } as const
  if (task.status === 'success') return { label: '已完成', tone: 'success' } as const
  return { label: '未完成', tone: 'neutral' } as const
}

const sourceMaterialLabels = {
  poster: '海报',
  screenshots: '截图',
  body: '简介正文',
  mediainfo: 'MediaInfo / BDInfo',
}
const requiredStandardLabels = {
  type: '类型',
  medium: '媒介',
  video_codec: '视频编码',
  audio_codec: '音频编码',
  resolution: '分辨率',
  team: '制作组',
}

export function getMissingSeedFields(
  row: Partial<Record<keyof typeof requiredStandardLabels, string>> & {
    missing_required_fields?: string[]
  },
): string[] {
  const missingMaterial = Object.entries(sourceMaterialLabels)
    .filter(([key]) => row.missing_required_fields?.includes(key))
    .map(([, label]) => label)
  const missingStandardParams = Object.entries(requiredStandardLabels)
    .filter(([key]) => {
      const value = row[key as keyof typeof requiredStandardLabels]
      return typeof value === 'string' && !value.trim()
    })
    .map(([, label]) => label)
  return [...missingMaterial, ...missingStandardParams]
}

// A result action may open only a credential-free web URL. Tracker details support
// different path shapes, so retain known navigation parameters and drop everything else.
export const safeTrackerResultUrl = (value: unknown): string => {
  if (typeof value !== 'string' || !/^https?:\/\//i.test(value.trim())) return ''
  try {
    const url = new URL(value.split('|DIRECT_DOWNLOAD:')[0]?.trim() ?? '')
    if (url.username || url.password) return ''
    const id = url.searchParams.get('id')
    if (url.hostname === 'rousi.pro') {
      url.pathname = url.pathname.replace(/^\/api\/v1\/torrents\//, '/torrent/')
    }
    const isNexusPage = /\/(?:details|offers)\.php$/i.test(url.pathname)
    const isTorrentPage =
      /\/torrent\/(?:info\/\d+|\d+|[a-f\d]{8}(?:-[a-f\d]{4}){3}-[a-f\d]{12})\/?$/i.test(
        url.pathname,
      )
    if (!isTorrentPage && !(isNexusPage && id && /^\d+$/.test(id))) return ''
    url.search = isNexusPage && id ? new URLSearchParams({ id }).toString() : ''
    url.hash = ''
    return url.href
  } catch {
    return ''
  }
}

export const redactPrivateText = (value: string) =>
  value
    .replace(/<(?:script|style)\b[^>]*>[\s\S]*?<\/(?:script|style)\s*>/gi, '[页面内容已隐藏]')
    .replace(/<\/?[a-z][^<>]*>/gi, '')
    .replace(
      /(\b(?:authorization|proxy-authorization|set-cookie|cookie)\s*:\s*)[^\r\n]+/gi,
      '$1[已隐藏]',
    )
    .replace(/https?:\/\/[^\s<>"'\]）]+/gi, (url) => {
      try {
        const parsed = new URL(url)
        if (/download|announce|passkey|authkey|\.torrent$/i.test(parsed.pathname))
          return '[私有链接已隐藏]'
        parsed.username = ''
        parsed.password = ''
        parsed.search = ''
        parsed.hash = ''
        return parsed.href
      } catch {
        return '[链接已隐藏]'
      }
    })
    .replace(
      /(["']?\b(?:access[_-]?token|auth[_-]?token|api[_-]?token|api[_-]?key|authkey|passkey|torrent[_-]?pass(?:key)?|password|passwd|cookie|secret|token)\b["']?\s*[=:]\s*)(?:\[已隐藏\]|"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;&<}\]]+)/gi,
      '$1[已隐藏]',
    )

type AutoAddTone = 'success' | 'failed' | 'neutral'

export function getAutoAddPresentation(
  status: string,
  raw: unknown,
): {
  label: string
  detail: string
  tone: AutoAddTone
} {
  if (status === 'queued' || status === 'running') {
    return { label: '待执行', detail: '', tone: 'neutral' }
  }
  if (status === 'cancelled') return { label: '未执行', detail: '', tone: 'neutral' }

  let record: unknown = raw
  if (typeof raw === 'string') {
    try {
      record = JSON.parse(raw)
    } catch {
      record = null
    }
  }
  if (record && typeof record === 'object') {
    const result = record as Record<string, unknown>
    const detail =
      typeof result.message === 'string' ? redactPrivateText(result.message.trim()) : ''
    if (
      result.skipped === true ||
      /^(?:未执行|未启用)|跳过(?:自动添加|：)|自动添加已跳过/.test(detail)
    ) {
      return { label: '未执行', detail, tone: 'neutral' }
    }
    if (result.success === true) {
      const name =
        typeof result.downloader_name === 'string'
          ? redactPrivateText(result.downloader_name.trim())
          : ''
      return {
        label: '已添加',
        detail: [name, detail].filter(Boolean).join(' · '),
        tone: 'success',
      }
    }
    if (result.success === false) return { label: '添加失败', detail, tone: 'failed' }
  }

  const label = ['failed', 'filtered', 'pre_check_limit'].includes(status) ? '未执行' : '无添加记录'
  return { label, detail: '', tone: 'neutral' }
}
