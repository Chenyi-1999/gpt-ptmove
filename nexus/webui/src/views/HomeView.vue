<template>
  <div class="star-home">
    <div class="star-home__content">
      <header class="star-home__header">
        <h1>工作台</h1>
        <div class="star-home__actions">
          <button
            type="button"
            class="home-control"
            :disabled="downloaderBusy || trafficBusy"
            @click="refreshDashboard"
          >
            <Refresh aria-hidden="true" />
            刷新数据
          </button>
          <RouterLink to="/torrents" class="home-control home-control--primary">
            <Switch aria-hidden="true" />
            开始转种
          </RouterLink>
        </div>
      </header>

      <nav class="star-home__shortcuts" aria-label="工作台快捷入口">
        <RouterLink v-for="item in shortcuts" :key="item.to" :to="item.to" class="home-shortcut">
          <component :is="item.icon" aria-hidden="true" />
          {{ item.label }}
          <ArrowRight class="home-shortcut__arrow" aria-hidden="true" />
        </RouterLink>
      </nav>

      <dl class="star-home__summary">
        <div class="home-card home-metric">
          <dt><Top aria-hidden="true" /> 上传总量</dt>
          <dd>{{ traffic.length ? formatBytes(uploadTotal) : '—' }}</dd>
          <dd class="home-metric__caption">{{ currentRangeLabel }}</dd>
        </div>
        <div class="home-card home-metric home-metric--blue">
          <dt><Bottom aria-hidden="true" /> 下载总量</dt>
          <dd>{{ traffic.length ? formatBytes(downloadTotal) : '—' }}</dd>
          <dd class="home-metric__caption">{{ currentRangeLabel }}</dd>
        </div>
        <div class="home-card home-metric home-metric--green">
          <dt><Monitor aria-hidden="true" /> 启用下载器</dt>
          <dd>{{ downloaderInfo === null ? '—' : enabledCount }}</dd>
          <dd class="home-metric__caption">
            {{ downloaderInfo === null
              ? downloaderError ? '读取失败' : '等待读取'
              : '共 ' + downloaderInfo.length + ' 个下载器' }}
          </dd>
        </div>
      </dl>

      <HomeSpeedPanel class="star-home__speed" />

      <div class="star-home__panels">
        <section class="home-card home-panel" aria-labelledby="home-traffic-title">
          <div class="home-panel__heading">
            <div>
              <h2 id="home-traffic-title">流量趋势</h2>
              <p>按服务器时间统计已启用下载器的每小时流量</p>
            </div>
            <div class="home-range" role="group" aria-label="流量时段">
              <button
                v-for="option in ranges"
                :key="option.value"
                type="button"
                :aria-pressed="range === option.value"
                @click="range = option.value"
              >
                {{ option.label }}
              </button>
            </div>
          </div>

          <p v-if="trafficError && traffic.length" class="home-status--warning" role="status">
            刷新失败，当前为上次读取的数据。
          </p>
          <template v-if="traffic.length">
            <div class="home-legend" aria-hidden="true">
              <span><i class="home-key" /> 上传</span>
              <span><i class="home-key home-key--blue" /> 下载</span>
            </div>
            <div
              ref="chartHost"
              class="home-chart"
              role="img"
              :aria-label="currentRangeLabel + '流量柱状图，上传 ' + formatBytes(uploadTotal) + '，下载 ' + formatBytes(downloadTotal)"
            />
          </template>
          <div v-else class="home-chart home-empty" role="status">
            <TrendCharts aria-hidden="true" />
            <p v-if="trafficBusy">正在读取流量记录…</p>
            <p v-else-if="trafficError">无法读取流量记录</p>
            <p v-else>当前时段暂无流量记录</p>
          </div>

          <div class="home-panel__footer">
            <details v-if="traffic.length" class="home-detail">
              <summary>每小时数据</summary>
              <table>
                <thead>
                  <tr>
                    <th scope="col">时间</th>
                    <th scope="col">上传</th>
                    <th scope="col">下载</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="point in traffic" :key="point.label">
                    <th scope="row">{{ point.label }}</th>
                    <td>{{ formatBytes(point.uploaded) }}</td>
                    <td>{{ formatBytes(point.downloaded) }}</td>
                  </tr>
                </tbody>
              </table>
            </details>

          </div>
        </section>

        <section class="home-card home-panel" aria-labelledby="home-downloaders-title">
          <div class="home-panel__heading">
            <div>
              <h2 id="home-downloaders-title">下载器</h2>
              <p>每 30 秒刷新</p>
            </div>
            <RouterLink
              to="/settings/downloader"
              class="home-control home-control--icon"
              aria-label="下载器设置"
              title="下载器设置"
            >
              <Setting aria-hidden="true" />
            </RouterLink>
          </div>

          <p
            v-if="downloaderError && downloaderInfo !== null"
            class="home-status--warning"
            role="status"
          >
            刷新失败，当前为上次读取的数据。
          </p>
          <ul v-if="downloaderInfo?.length" class="home-downloaders">
            <li v-for="downloader in downloaderInfo" :key="downloader.id || downloader.name">
              <div class="home-downloader__head">
                <div class="home-downloader__name">
                  <h3>{{ downloader.name }}</h3>
                  <span>
                    {{ downloaderTypeLabel(downloader.type)
                    }}{{ downloader.details?.版本 ? ' · ' + downloader.details.版本 : '' }}
                  </span>
                </div>
                <span
                  class="home-downloader__status"
                  :class="{ 'home-downloader__status--enabled': downloader.enabled && activeStatuses.includes(downloader.status) }"
                >
                  {{ downloader.enabled ? downloader.status || '状态未知' : '已禁用' }}
                </span>
              </div>
              <dl v-if="downloader.enabled" class="home-downloader__metrics">
                <div v-for="metric in metrics" :key="metric.key">
                  <dt>{{ metric.label }}</dt>
                  <dd>{{ downloader.details?.[metric.key] || '—' }}</dd>
                </div>
              </dl>
            </li>
          </ul>
          <div v-else class="home-empty home-empty--downloaders" role="status">
            <Monitor aria-hidden="true" />
            <p v-if="downloaderBusy">正在读取下载器…</p>
            <p v-else-if="downloaderError && downloaderInfo === null">无法读取下载器</p>
            <template v-else>
              <p>暂无下载器配置</p>
              <RouterLink to="/settings/downloader" class="home-link">
                添加下载器 <ArrowRight aria-hidden="true" />
              </RouterLink>
            </template>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import * as echarts from 'echarts'
import HomeSpeedPanel from '@/components/HomeSpeedPanel.vue'
import {
  ArrowRight,
  Bottom,
  Document,
  Grid,
  Monitor,
  Operation,
  Refresh,
  Setting,
  Switch,
  Top,
  TrendCharts,
} from '@element-plus/icons-vue'

interface DownloaderInfo {
  id?: string
  name: string
  type: string
  enabled: boolean
  status: string
  details: Record<string, string> | null
}

interface TrafficResponse {
  labels: string[]
  downloaders: { id: string }[]
  datasets: Record<string, { uploaded: number[]; downloaded: number[] }>
}

interface TrafficPoint {
  label: string
  uploaded: number
  downloaded: number
}

const shortcuts = [
  { to: '/seed-maintenance', label: '种子维护', icon: Operation },
  { to: '/publish-logs', label: '任务记录', icon: Document },
  { to: '/plugins', label: '插件', icon: Grid },
]

const metrics = [
  { key: '今日上传量', label: '今日上传' },
  { key: '今日下载量', label: '今日下载' },
  { key: '累计上传量', label: '累计上传' },
  { key: '累计下载量', label: '累计下载' },
]

const ranges = [
  { value: 'last_24_hours', label: '近 24 小时' },
  { value: 'yesterday', label: '昨日' },
] as const

const activeStatuses = ['已连接', '已配置']
const range = ref<(typeof ranges)[number]['value']>(ranges[0].value)
const currentRangeLabel = computed(() => range.value === 'yesterday' ? ranges[1].label : ranges[0].label)
const downloaderInfo = ref<DownloaderInfo[] | null>(null)
const traffic = ref<TrafficPoint[]>([])
const downloaderBusy = ref(true)
const trafficBusy = ref(true)
const downloaderError = ref(false)
const trafficError = ref(false)
const chartHost = ref<HTMLElement | null>(null)

const enabledCount = computed(() => downloaderInfo.value?.filter((item) => item.enabled).length ?? 0)
const uploadTotal = computed(() => traffic.value.reduce((sum, point) => sum + point.uploaded, 0))
const downloadTotal = computed(() => traffic.value.reduce((sum, point) => sum + point.downloaded, 0))

let downloaderRequest: AbortController | undefined
let trafficRequest: AbortController | undefined
let refreshTimer: number | undefined
let chart: echarts.EChartsType | null = null
let chartObserver: ResizeObserver | undefined

function formatBytes(bytes: unknown): string {
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
  const unit = Math.max(0, Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024))))
  return Number((bytes / 1024 ** unit).toFixed(2)) + ' ' + units[unit]
}

function downloaderTypeLabel(type: string): string {
  if (type === 'qbittorrent') return 'qBittorrent'
  if (type === 'transmission') return 'Transmission'
  return type || '下载器'
}

function toTrafficPoints(data: TrafficResponse): TrafficPoint[] {
  if (
    !Array.isArray(data?.labels) ||
    !data.labels.every((label) => typeof label === 'string') ||
    !Array.isArray(data.downloaders) ||
    typeof data.datasets !== 'object' ||
    data.datasets === null
  ) {
    throw new Error('Invalid traffic response')
  }

  const ids = new Set(data.downloaders.map(({ id }) => id))
  if (!ids.size || !data.labels.length) return []

  const points = data.labels.map((label) => ({ label, uploaded: 0, downloaded: 0 }))
  for (const id of ids) {
    const dataset = data.datasets[id]
    if (dataset?.uploaded?.length !== points.length || dataset?.downloaded?.length !== points.length) {
      throw new Error('Invalid traffic dataset')
    }
    points.forEach((point, index) => {
      const upload = dataset.uploaded[index]
      const download = dataset.downloaded[index]
      if (!Number.isFinite(upload) || upload < 0 || !Number.isFinite(download) || download < 0) {
        throw new Error('Invalid traffic value')
      }
      point.uploaded += upload
      point.downloaded += download
    })
  }
  return points
}

async function loadDownloaders() {
  if (downloaderRequest) return
  const request = new AbortController()
  downloaderRequest = request
  downloaderBusy.value = true

  try {
    const { data } = await axios.get<DownloaderInfo[]>('/api/downloader_info', {
      signal: request.signal,
    })
    if (!Array.isArray(data)) throw new Error('Invalid downloader response')
    if (request.signal.aborted) return
    downloaderInfo.value = data
    downloaderError.value = false
  } catch {
    if (!request.signal.aborted) downloaderError.value = true
  } finally {
    if (downloaderRequest === request) {
      downloaderRequest = undefined
      if (!request.signal.aborted) downloaderBusy.value = false
    }
  }
}

async function loadTraffic() {
  if (trafficRequest) return
  const request = new AbortController()
  trafficRequest = request
  trafficBusy.value = true

  try {
    const { data } = await axios.get<TrafficResponse>('/api/chart_data', {
      params: { range: range.value },
      signal: request.signal,
    })
    if (request.signal.aborted) return
    traffic.value = toTrafficPoints(data)
    trafficError.value = false
  } catch {
    if (!request.signal.aborted) trafficError.value = true
  } finally {
    if (trafficRequest === request) {
      trafficRequest = undefined
      if (!request.signal.aborted) trafficBusy.value = false
    }
  }
}

function refreshDashboard() {
  void loadDownloaders()
  void loadTraffic()
}

function disposeChart() {
  chartObserver?.disconnect()
  chartObserver = undefined
  chart?.dispose()
  chart = null
}

watch(
  [chartHost, traffic],
  ([host, points]) => {
    if (!host || host !== chart?.getDom()) disposeChart()
    if (!host || !points.length) return

    if (!chart) {
      chart = echarts.init(host)
      chartObserver = new ResizeObserver(() => chart?.resize())
      chartObserver.observe(host)
    }
    const style = getComputedStyle(host)
    const color = (token: string) => style.getPropertyValue('--star-data-' + token).trim()
    chart.setOption({
      animation: false,
      grid: { left: 0, right: 4, top: 12, bottom: 4, outerBoundsMode: 'auto' },
      tooltip: {
        trigger: 'axis',
        renderMode: 'richText',
        backgroundColor: color('panel-raised'),
        borderColor: color('border'),
        textStyle: { color: color('ink') },
        valueFormatter: formatBytes,
      },
      xAxis: {
        type: 'category',
        data: points.map((point) => point.label),
        axisTick: { show: false },
        axisLine: { lineStyle: { color: color('border') } },
        axisLabel: { color: color('muted'), hideOverlap: true, formatter: (label: string) => label.slice(11, 16) || label },
      },
      yAxis: {
        type: 'value',
        splitNumber: 4,
        minInterval: 1,
        axisLabel: { color: color('muted'), formatter: (amount: number) => formatBytes(amount) },
        splitLine: { lineStyle: { color: color('border'), type: 'dashed' } },
      },
      series: [
        {
          type: 'bar',
          name: '上传',
          data: points.map((point) => point.uploaded),
          barMaxWidth: 17,
          itemStyle: { color: color('accent'), borderRadius: [3, 3, 0, 0] },
        },
        {
          type: 'bar',
          name: '下载',
          data: points.map((point) => point.downloaded),
          barMaxWidth: 17,
          itemStyle: { color: color('blue'), borderRadius: [3, 3, 0, 0] },
        },
      ],
    } satisfies echarts.EChartsOption)
  },
  { flush: 'post' },
)

watch(range, () => {
  trafficRequest?.abort()
  trafficRequest = undefined
  traffic.value = []
  trafficError.value = false
  void loadTraffic()
})

onMounted(() => {
  refreshDashboard()
  refreshTimer = window.setInterval(refreshDashboard, 30000)
})

onBeforeUnmount(() => {
  window.clearInterval(refreshTimer)
  downloaderRequest?.abort()
  trafficRequest?.abort()
  disposeChart()
})
</script>

<style scoped>
.star-home {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: clamp(20px, 3.2vw, 42px) max(16px, env(safe-area-inset-right, 0px))
    max(34px, env(safe-area-inset-bottom, 0px)) max(16px, env(safe-area-inset-left, 0px));
  color: var(--star-data-ink);
  background: radial-gradient(ellipse at 82% -18%, #30234a 0%, transparent 52%),
    var(--star-data-canvas);
}

.star-home__content {
  width: min(100%, 1320px);
  margin: 0 auto;
}

.star-home__header,
.star-home__actions,
.star-home__shortcuts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 9px;
}

.star-home__header {
  justify-content: space-between;
  column-gap: 18px;
}

.star-home h1,
.star-home h2,
.star-home h3,
.star-home p,
.star-home dl,
.star-home dd {
  margin: 0;
}

.star-home h1 {
  font-size: clamp(27px, 3vw, 36px);
  letter-spacing: -0.035em;
  line-height: 1.25;
}

.star-home__shortcuts {
  margin-block: 15px 23px;
}

.home-control,
.home-shortcut,
.home-link {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--star-data-ink);
  font: inherit;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.35;
  text-decoration: none;
}

.home-control,
.home-shortcut {
  min-height: 38px;
  padding: 6px 10px;
  border: 1px solid var(--star-data-border);
  border-radius: 8px;
  background: var(--star-data-panel);
  cursor: pointer;
}

.home-control:hover,
.home-shortcut:hover {
  border-color: var(--star-data-accent);
  background: var(--star-data-panel-raised);
}

.home-control:disabled {
  opacity: 0.64;
  cursor: default;
}

.home-control--primary {
  color: #181225;
  border-color: transparent;
  background: var(--star-data-accent);
}

.home-control--primary:hover {
  color: #181225;
  background: #cfbfff;
}

.home-control--icon {
  justify-content: center;
  min-width: 38px;
  padding: 6px;
}

.home-shortcut {
  color: var(--star-data-muted);
  background: transparent;
}

.home-control svg,
.home-shortcut svg,
.home-link svg,
.home-metric svg {
  width: 15px;
  height: 15px;
  flex: none;
}

.home-shortcut svg:first-child,
.home-link,
.home-metric svg {
  color: var(--star-data-accent);
}

.home-shortcut__arrow {
  margin-inline-start: 4px;
}

.star-home :is(button, a, summary):focus-visible {
  outline: 2px solid var(--star-data-accent);
  outline-offset: 3px;
}

.star-home .star-home__summary,
.star-home__panels {
  display: grid;
  gap: 14px;
}

.star-home .star-home__summary {
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin-bottom: 14px;
}

.star-home__speed {
  margin-bottom: 14px;
}

.star-home__panels {
  grid-template-columns: minmax(0, 1.75fr) minmax(290px, 1fr);
  align-items: start;
}

.home-card {
  min-width: 0;
  border: 1px solid var(--star-data-border);
  border-radius: var(--star-data-radius);
  background-color: var(--star-data-panel);
}

.home-metric {
  padding: 14px 17px;
  background-image: linear-gradient(120deg, rgb(139 104 229 / 9%), transparent 70%);
}

.home-metric dt {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  color: var(--star-data-muted);
}

.home-metric dd {
  margin-top: 5px;
  font-size: clamp(22px, 2.9vw, 35px);
  font-weight: 720;
  line-height: 1.17;
  letter-spacing: -0.033em;
  overflow-wrap: anywhere;
}

.home-metric .home-metric__caption {
  margin-top: 5px;
  font-size: 12px;
  font-weight: 400;
  letter-spacing: normal;
  color: var(--star-data-muted);
}

.home-metric--blue svg {
  color: var(--star-data-blue);
}

.home-metric--green svg {
  color: var(--star-data-success);
}

.home-panel {
  padding: clamp(13px, 2vw, 20px);
}

.home-panel__heading {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.home-panel h2 {
  font-size: 17px;
  line-height: 1.35;
}

.home-panel__heading p {
  margin-top: 4px;
  color: var(--star-data-muted);
  font-size: 13px;
}

.home-range {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--star-data-border);
  border-radius: 8px;
  background: var(--star-data-canvas);
}

.home-range button {
  min-height: 30px;
  padding-inline: 9px;
  border: 0;
  border-radius: 5px;
  color: var(--star-data-muted);
  background: transparent;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}

.home-range button:hover,
.home-range button[aria-pressed='true'] {
  color: var(--star-data-ink);
  background: var(--star-data-panel-raised);
}

.home-legend {
  display: flex;
  gap: 16px;
  margin-top: 14px;
  color: var(--star-data-muted);
  font-size: 12px;
}

.home-legend span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.home-key {
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background-color: var(--star-data-accent);
}

.home-key--blue {
  background-color: var(--star-data-blue);
}

.home-chart {
  width: 100%;
  height: clamp(185px, 23vw, 285px);
  margin-top: 7px;
}

.home-empty {
  display: grid;
  align-content: center;
  justify-items: center;
  gap: 8px;
  color: var(--star-data-muted);
  font-size: 13px;
  text-align: center;
}

.home-empty > svg {
  width: 23px;
  height: 23px;
  color: var(--star-data-accent);
}

.home-empty--downloaders {
  min-height: 164px;
}

.home-panel__footer {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding-top: 11px;
  margin-top: 8px;
  border-top: 1px solid var(--star-data-border);
}

.home-panel__footer > .home-link {
  margin-inline-start: auto;
}

.home-link:hover {
  text-decoration: underline;
}

.home-detail {
  min-width: 0;
  font-size: 13px;
  color: var(--star-data-muted);
}

.home-detail[open] {
  flex-basis: 100%;
}

.home-detail summary {
  cursor: pointer;
}

.home-detail table {
  width: 100%;
  margin-top: 8px;
  border-collapse: collapse;
  font-variant-numeric: tabular-nums;
  text-align: start;
}

.home-detail :is(th, td) {
  padding: 5px;
  border-bottom: 1px solid var(--star-data-border);
  text-align: start;
  overflow-wrap: anywhere;
}

.home-detail tbody th {
  font-weight: 400;
}

.home-downloaders {
  display: grid;
  gap: 9px;
  margin: 13px 0 0;
  padding: 0;
  list-style: none;
}

.home-downloaders li {
  min-width: 0;
  padding: 11px;
  border: 1px solid var(--star-data-border);
  border-radius: 10px;
  background: var(--star-data-panel-raised);
}

.home-downloader__head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 6px 10px;
}

.home-downloader__name {
  flex: 1 1 110px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.home-downloader__name h3 {
  font-size: 14px;
  font-weight: 650;
  line-height: 1.35;
}

.home-downloader__name span,
.home-downloader__metrics dt {
  font-size: 12px;
  color: var(--star-data-muted);
}

.home-downloader__status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--star-data-muted);
}

.home-downloader__status::before {
  content: '';
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.home-downloader__status--enabled {
  color: var(--star-data-success);
}

.star-home .home-downloader__metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 9px;
  padding-top: 8px;
  margin-top: 8px;
  border-top: 1px solid var(--star-data-border);
}

.home-downloader__metrics dd {
  margin-top: 2px;
  font-size: 13px;
  font-weight: 650;
  overflow-wrap: anywhere;
}

.home-metric dd,
.home-downloader__metrics dd,
.home-downloader__status {
  font-variant-numeric: tabular-nums;
}

.star-home .home-status--warning {
  margin-top: 10px;
  color: #ffd994;
  font-size: 12px;
}

@media (max-width: 900px) {
  .star-home__panels {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 520px) {
  .star-home__shortcuts {
    margin-bottom: 16px;
  }

  .star-home .star-home__summary,
  .star-home__panels {
    gap: 9px;
  }

  .home-metric {
    padding: 10px;
  }

  .home-metric dd {
    font-size: clamp(16px, 4.5vw, 22px);
  }

  .home-metric dt,
  .home-metric .home-metric__caption {
    font-size: 12px;
  }

  .home-metric svg {
    display: none;
  }

  .home-metric dt {
    gap: 4px;
  }

  .home-shortcut__arrow {
    display: none;
  }

  .home-control,
  .home-shortcut,
  .home-range button {
    min-height: 44px;
  }
}
</style>
