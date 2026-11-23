export const autoScreenshotCount = 5

interface ScreenshotCandidate {
  id: string
  timeSeconds: number
  recommended?: boolean
}

// The server rejects time points less than 0.8 seconds apart.
export function chooseAutomaticScreenshots<T extends ScreenshotCandidate>(
  candidates: readonly T[],
): T[] {
  const byTime = candidates
    .filter(({ id, timeSeconds }) => id && Number.isFinite(timeSeconds) && timeSeconds > 0)
    .sort((left, right) => left.timeSeconds - right.timeSeconds)
  const take = (ordered: T[]) => {
    const selected: T[] = []
    for (const candidate of ordered) {
      if (selected.length === autoScreenshotCount) break
      if (
        selected.some(
          ({ id, timeSeconds }) =>
            id === candidate.id || Math.abs(timeSeconds - candidate.timeSeconds) < 0.8,
        )
      )
        continue
      selected.push(candidate)
    }
    return selected
  }
  const recommended = take(
    [...byTime].sort(
      (left, right) => Number(Boolean(right.recommended)) - Number(Boolean(left.recommended)),
    ),
  )
  const chosen = recommended.length === autoScreenshotCount ? recommended : take(byTime)
  return chosen.length === autoScreenshotCount
    ? chosen.sort((left, right) => left.timeSeconds - right.timeSeconds)
    : []
}

export function countDistinctScreenshotURLs(values: string[]): number {
  const found = new Set<string>()
  for (const value of values) {
    try {
      const url = new URL(value)
      if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) {
        url.hash = ''
        found.add(url.href)
      }
    } catch {
      continue
    }
  }
  return found.size
}
