<template>
  <section class="home-speed-panel" aria-labelledby="home-speed-title">
    <header class="home-speed-panel__header">
      <div class="home-speed-panel__title">
        <h2 id="home-speed-title">实时速度</h2>
        <p>近 60 秒 · 每 2 秒刷新</p>
      </div>
      <div class="home-speed-panel__select-wrap">
        <span>下载器</span>
        <el-select
          id="home-speed-downloader"
          v-model="selectedDownloaderId"
          class="home-speed-panel__select"
          aria-label="选择要查看速度的下载器"
          :placeholder="hasLoaded ? '暂无下载器' : '选择下载器'"
          :disabled="!hasDownloaders"
          fit-input-width
        >
          <el-option
            v-for="downloader in downloaders"
            :key="downloader.id"
            :label="downloader.name"
            :value="downloader.id"
          />
        </el-select>
      </div>
    </header>

    <p v-if="error && hasSamples" class="home-speed-panel__notice" role="status">
      刷新失败，显示上次读取的数据。
    </p>
    <dl v-if="selectedDownloader" class="home-speed-panel__rates" aria-label="当前传输速度">
      <div class="home-speed-panel__rate home-speed-panel__rate--upload">
        <dt><i aria-hidden="true" />上传</dt>
        <dd>{{ formatDisplayRate(currentUpload) }}</dd>
      </div>
      <div class="home-speed-panel__rate home-speed-panel__rate--download">
        <dt><i aria-hidden="true" />下载</dt>
        <dd>{{ formatDisplayRate(currentDownload) }}</dd>
      </div>
    </dl>

    <div
      v-if="hasDownloaders && hasSamples"
      ref="chartHost"
      class="home-speed-panel__chart"
      role="img"
      :aria-label="chartLabel"
    />
    <div v-else class="home-speed-panel__empty" role="status">
      <TrendCharts aria-hidden="true" />
      <p v-if="busy && !hasLoaded">正在读取速度记录…</p>
      <p v-else-if="error">无法读取速度记录</p>
      <p v-else-if="hasLoaded && !hasDownloaders">暂无已启用的下载器</p>
      <p v-else-if="hasLoaded && !hasSamples">暂无速度样本</p>
      <p v-else>等待速度记录…</p>
    </div>
    <footer class="home-speed-panel__footer">
      <span v-if="selectedDownloader" class="home-speed-panel__name">{{ selectedDownloader.name }}</span>
      <span>{{ latestSample ? `采样于 ${latestSample.time}` : '等待采样' }}</span>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import * as echarts from 'echarts'
import { TrendCharts } from '@element-plus/icons-vue'

interface SpeedDownloader {
  id: string
  name: string
}

interface SpeedValue {
  ul_speed?: number
  dl_speed?: number
  sampled?: boolean
}

interface SpeedDataset {
  time: string
  speeds: Record<string, SpeedValue>
}

interface RecentSpeedResponse {
  labels: string[]
  datasets: SpeedDataset[]
  downloaders: SpeedDownloader[]
}

interface SpeedPoint {
  time: string
  upload: number | null
  download: number | null
}

const response = ref<RecentSpeedResponse | null>(null)
const selectedDownloaderId = ref('')
const busy = ref(false)
const error = ref(false)
const hasLoaded = ref(false)
const chartHost = ref<HTMLElement | null>(null)

const downloaders = computed(() => response.value?.downloaders ?? [])
const selectedDownloader = computed(() =>
  downloaders.value.find((downloader) => downloader.id === selectedDownloaderId.value) ?? null,
)
const hasDownloaders = computed(() => downloaders.value.length > 0)
const points = computed<SpeedPoint[]>(() => {
  const id = selectedDownloaderId.value
  if (!id) return []
  const rows = (response.value?.datasets ?? []).map((dataset) => {
    const speed = dataset.speeds?.[id]
    const sampled = speed !== undefined && speed.sampled !== false
    return {
      time: dataset.time,
      upload: sampled ? finiteRate(speed?.ul_speed) : null,
      download: sampled ? finiteRate(speed?.dl_speed) : null,
    }
  })
  return orderRecentPoints(rows)
})
const hasSamples = computed(() => points.value.some((point) => point.upload !== null || point.download !== null))
const latestSample = computed(() => [...points.value].reverse().find((point) => point.upload !== null || point.download !== null))
const currentUpload = computed(() => latestSample.value?.upload ?? null)
const currentDownload = computed(() => latestSample.value?.download ?? null)
const chartLabel = computed(() => {
  const name = selectedDownloader.value?.name ?? '下载器'
  return `${name}近 60 秒实时速度趋势，上传 ${formatDisplayRate(currentUpload.value)}，下载 ${formatDisplayRate(currentDownload.value)}`
})

function finiteRate(value: unknown): number {
  const rate = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(rate) && rate > 0 ? rate : 0
}

function formatRate(value: number): string {
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit += 1
  }
  const digits = unit === 0 || amount >= 100 ? 0 : amount >= 10 ? 1 : 2
  return `${amount.toFixed(digits)} ${units[unit]}`
}

function formatDisplayRate(value: number | null): string {
  return value === null ? '—' : formatRate(value)
}

function orderRecentPoints(rows: SpeedPoint[]): SpeedPoint[] {
  if (rows.length < 2) return rows
  const toSecondOfDay = (time: string) => {
    const match = /(?:^|\s)(\d{2}):(\d{2}):(\d{2})$/.exec(time)
    return match ? Number(match[1]) * 3600 + Number(match[2]) * 60 + Number(match[3]) : null
  }
  const keyed = rows.map((point) => ({ point, second: toSecondOfDay(point.time) }))
  if (keyed.some((item) => item.second === null)) return rows
  keyed.sort((left, right) => (left.second as number) - (right.second as number))

  // The API groups by local clock time. Rotate after its largest gap so midnight remains chronological.
  let splitAt = 0
  let largestGap = -1
  for (let index = 0; index < keyed.length; index += 1) {
    const current = keyed[index].second as number
    const nextIndex = (index + 1) % keyed.length
    const next = keyed[nextIndex].second as number
    const gap = (next - current + 86400) % 86400
    if (gap > largestGap) {
      largestGap = gap
      splitAt = nextIndex
    }
  }
  return [...keyed.slice(splitAt), ...keyed.slice(0, splitAt)].map((item) => item.point)
}

let refreshTimer: number | undefined
let request: AbortController | undefined
let chart: echarts.EChartsType | null = null
let chartObserver: ResizeObserver | undefined

async function refreshSpeeds() {
  if (busy.value || document.visibilityState !== 'visible') return
  const controller = new AbortController()
  request = controller
  busy.value = true
  try {
    const result = await axios.get<RecentSpeedResponse>('/api/recent_speed_data', {
      params: { seconds: 60 },
      signal: controller.signal,
      timeout: 8000,
    })
    if (controller.signal.aborted || request !== controller) return
    const next = result.data
    response.value = {
      labels: Array.isArray(next.labels) ? next.labels : [],
      datasets: Array.isArray(next.datasets) ? next.datasets : [],
      downloaders: Array.isArray(next.downloaders)
        ? next.downloaders.filter((downloader) => downloader?.id && downloader?.name)
        : [],
    }
    if (!response.value.downloaders.some((downloader) => downloader.id === selectedDownloaderId.value)) {
      selectedDownloaderId.value = response.value.downloaders[0]?.id ?? ''
    }
    error.value = false
    hasLoaded.value = true
  } catch {
    if (!controller.signal.aborted && request === controller) error.value = true
  } finally {
    if (request === controller) {
      request = undefined
      busy.value = false
    }
  }
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') void refreshSpeeds()
}

function disposeChart() {
  chartObserver?.disconnect()
  chartObserver = undefined
  chart?.dispose()
  chart = null
}

watch([chartHost, points], ([host, rows]) => {
  if (!host || host !== chart?.getDom()) disposeChart()
  if (!host || !rows.length) return
  if (!chart) {
    chart = echarts.init(host)
    chartObserver = new ResizeObserver(() => chart?.resize())
    chartObserver.observe(host)
  }
  const style = getComputedStyle(host)
  const token = (name: string) => style.getPropertyValue(`--star-data-${name}`).trim()
  chart.setOption({
    animation: false,
    grid: { left: 0, right: 6, top: 13, bottom: 3, outerBoundsMode: 'auto' },
    tooltip: {
      trigger: 'axis',
      renderMode: 'richText',
      backgroundColor: token('panel-raised'),
      borderColor: token('border'),
      textStyle: { color: token('ink') },
      formatter: (params) => {
        const entries = Array.isArray(params) ? params : [params]
        const time = String(entries[0]?.name ?? '')
        return [time, ...entries.map((entry) =>
          `${entry.seriesName}  ${formatDisplayRate(typeof entry.value === 'number' ? entry.value : null)}`,
        )].join('\n')
      },
    },
    xAxis: {
      type: 'category',
      data: rows.map((point) => point.time),
      boundaryGap: false,
      axisTick: { show: false },
      axisLine: { lineStyle: { color: token('border') } },
      axisLabel: { color: token('muted'), hideOverlap: true, formatter: (value: string) => value.slice(-5) },
    },
    yAxis: {
      type: 'value',
      min: 0,
      splitNumber: 3,
      axisLabel: { color: token('muted'), formatter: (value: number) => formatRate(value) },
      splitLine: { lineStyle: { color: token('border'), type: 'dashed' } },
    },
    series: [
      {
        type: 'line',
        name: '上传',
        data: rows.map((point) => point.upload),
        showSymbol: false,
        smooth: 0.22,
        lineStyle: { width: 2, color: token('accent') },
        areaStyle: { color: token('accent'), opacity: 0.08 },
      },
      {
        type: 'line',
        name: '下载',
        data: rows.map((point) => point.download),
        showSymbol: false,
        smooth: 0.22,
        lineStyle: { width: 2, color: token('blue') },
        areaStyle: { color: token('blue'), opacity: 0.08 },
      },
    ],
  } satisfies echarts.EChartsOption)
}, { flush: 'post' })

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  void refreshSpeeds()
  refreshTimer = window.setInterval(() => void refreshSpeeds(), 2000)
})

onBeforeUnmount(() => {
  window.clearInterval(refreshTimer)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  request?.abort()
  disposeChart()
})
</script>

<style scoped>
.home-speed-panel {
  min-width: 0;
  padding: clamp(13px, 2vw, 20px);
  border: 1px solid var(--star-data-border);
  border-radius: var(--star-data-radius);
  color: var(--star-data-ink);
  background: var(--star-data-panel);
}

.home-speed-panel__header,
.home-speed-panel__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.home-speed-panel__header {
  flex-wrap: wrap;
}

.home-speed-panel h2,
.home-speed-panel p,
.home-speed-panel dd {
  margin: 0;
}

.home-speed-panel h2 {
  font-size: 17px;
  line-height: 1.35;
}

.home-speed-panel__title p {
  margin-top: 4px;
  color: var(--star-data-muted);
  font-size: 13px;
}

.home-speed-panel__select-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--star-data-muted);
  font-size: 12px;
}

.home-speed-panel__select {
  width: clamp(132px, 24vw, 220px);
}

.home-speed-panel__rates {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin: 15px 0 0;
}

.home-speed-panel__rate {
  min-width: 0;
  padding: 11px 13px;
  border: 1px solid var(--star-data-border);
  border-radius: 10px;
  background: var(--star-data-panel-raised);
}

.home-speed-panel__rate dt {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--star-data-muted);
  font-size: 12px;
}

.home-speed-panel__rate i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--star-data-accent);
}

.home-speed-panel__rate--download i {
  background: var(--star-data-blue);
}

.home-speed-panel__rate dd {
  margin-top: 5px;
  font-size: clamp(19px, 2.4vw, 27px);
  font-weight: 700;
  letter-spacing: -0.025em;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

.home-speed-panel__chart,
.home-speed-panel__empty {
  width: 100%;
  height: clamp(180px, 21vw, 255px);
  margin-top: 10px;
}

.home-speed-panel__empty {
  display: grid;
  align-content: center;
  justify-items: center;
  gap: 8px;
  color: var(--star-data-muted);
  font-size: 13px;
  text-align: center;
}

.home-speed-panel__empty svg {
  width: 24px;
  height: 24px;
  color: var(--star-data-accent);
}

.home-speed-panel__notice {
  margin-top: 10px !important;
  color: #ffd994;
  font-size: 12px;
}

.home-speed-panel__footer {
  flex-wrap: wrap;
  padding-top: 10px;
  margin-top: 3px;
  border-top: 1px solid var(--star-data-border);
  color: var(--star-data-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.home-speed-panel__name {
  min-width: 0;
  color: var(--star-data-ink);
  overflow-wrap: anywhere;
}

@media (max-width: 520px) {
  .home-speed-panel__header {
    align-items: flex-start;
  }

  .home-speed-panel__select-wrap {
    width: 100%;
    justify-content: space-between;
  }

  .home-speed-panel__select {
    flex: 1;
    min-width: 0;
  }

  .home-speed-panel__select :deep(.el-select__wrapper) {
    min-height: 44px;
  }

  .home-speed-panel__rate {
    padding: 9px 10px;
  }
}
</style>
