// Only the markup generated below can reach the preview's v-html sink.
export const renderPreviewBBCode = (text: string): string => {
  const escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')

  return escaped
    .replace(/\[quote\]([\s\S]*?)\[\/quote\]/gi, '<blockquote>$1</blockquote>')
    .replace(/\[b\]([\s\S]*?)\[\/b\]/gi, '<strong>$1</strong>')
    .replace(
      /\[color=(\w+|#[0-9a-f]{3,6})\]([\s\S]*?)\[\/color\]/gi,
      '<span style="color: $1;">$2</span>',
    )
    .replace(/\[size=(\d+)\]([\s\S]*?)\[\/size\]/gi, (_match, size: string, content: string) => {
      const defaultSizes: Record<string, number> = { 1: 12, 2: 14, 3: 16, 4: 18, 5: 24, 6: 32, 7: 48 }
      const pixels = defaultSizes[size] ?? Number.parseInt(size, 10) * 4
      return `<span style="font-size: ${pixels}px;">${content}</span>`
    })
    .replace(/\n/g, '<br>')
}

const httpImageUrl = (value: string): string | undefined => {
  const source = value.trim()
  if (!/^https?:\/\//i.test(source)) return
  try {
    const url = new URL(source)
    if (url.hostname && !url.username && !url.password) return url.href
  } catch {
    // Ignore incomplete URLs while the draft is being edited.
  }
}

export const parsePreviewImageUrls = (value: string | string[] | null | undefined): string[] => {
  if (!value) return []
  const bbcode = Array.isArray(value)
    ? []
    : Array.from(value.matchAll(/\[img\](https?:\/\/[^\s[\]]+)\[\/img\]/gi), (match) => match[1])
  const candidates = Array.isArray(value) ? value : bbcode.length ? bbcode : value.split(/[\n,]+/)
  return candidates.map((item) => httpImageUrl(String(item ?? ''))).filter((item) => item !== undefined)
}
