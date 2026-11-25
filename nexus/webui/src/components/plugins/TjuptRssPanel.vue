<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import TjuptHrDurationHint from './TjuptHrDurationHint.vue'

type Downloader = { id: string; name: string }
type Candidate = {
  id: string
  title: string
  size_bytes: number | null
  detail_url: string
  assessment_hours: number | null
  eligible: boolean
  reason: string
}
type Assessment = {
  code: number | null
  label: string
  seeded_seconds?: number | null
  required_seconds?: number | null
}
type Torrent = {
  hash: string
  name: string
  state: string
  progress: number | null
  assessment: Assessment
}
type FeedSettings = {
  downloader_id: string
  interval_minutes: number
  min_gib: number
  max_gib: number
  total_limit_gib: number
  active_limit: number
  tags: string
  category: string
  save_path: string
  cleanup_enabled: boolean
  upload_threshold_kib: number
  low_speed_minutes: number
  use_site_upload_limit?: boolean
}
type RunResult = {
  added: number
  skipped: number
  failed: number
  message: string
}
type FeedState = {
  settings: FeedSettings
  enabled: boolean
  running: boolean
  rss_configured: boolean
  hnr_configured: boolean
  site_upload_limit_mbps: number | null
  last_run_at?: string
  last_message?: string
  last_result?: RunResult | null
}
type Action = 'save' | 'refresh' | 'trial' | 'start' | 'stop' | 'preview' | 'status' | 'upload-limit'

const downloaders = ref<Downloader[]>([])
const feed = ref<FeedState | null>(null)
const draft = ref<FeedSettings | null>(null)
const rssURL = ref('')
const hnrToken = ref('')
const candidates = ref<Candidate[] | null>(null)
const torrents = ref<Torrent[] | null>(null)
const trial = ref<RunResult | null>(null)
const loading = ref(true)
const busy = ref<Action | null>(null)
const loadError = ref('')
const configError = ref('')
const uploadLimitError = ref('')
const runError = ref('')
const previewError = ref('')
const statusError = ref('')
const savedNotice = ref('')
let pollTimer: number | undefined
let polling = false

function editableSettings(settings: FeedSettings): FeedSettings {
  return {
    downloader_id: settings.downloader_id,
    interval_minutes: settings.interval_minutes ?? 1,
    min_gib: settings.min_gib,
    max_gib: settings.max_gib,
    total_limit_gib: settings.total_limit_gib,
    active_limit: settings.active_limit,
    tags: settings.tags ?? '',
    category: settings.category ?? '',
    save_path: settings.save_path ?? '',
    cleanup_enabled: settings.cleanup_enabled ?? false,
    upload_threshold_kib: settings.upload_threshold_kib ?? 100,
    low_speed_minutes: settings.low_speed_minutes ?? 60,
  }
}

const hasUnsaved = computed(() => Boolean(
  draft.value && feed.value && (
    JSON.stringify(draft.value) !== JSON.stringify(editableSettings(feed.value.settings)) ||
    rssURL.value.trim() || hnrToken.value.trim()
  ),
))
const readyToRun = computed(() => Boolean(
  feed.value?.rss_configured && downloaders.value.some(({ id }) => id === feed.value?.settings.downloader_id),
))
const readyForStatus = computed(() => Boolean(
  feed.value?.hnr_configured && downloaders.value.some(({ id }) => id === feed.value?.settings.downloader_id),
))
const canStart = computed(() => readyToRun.value && !hasUnsaved.value && !feed.value?.running)
const runLabel = computed(() => {
  if (feed.value?.running) return feed.value.enabled ? '正在处理 RSS' : '正在结束本轮'
  if (feed.value?.enabled) return '循环运行中'
  return '已停止'
})

async function request<T>(path: string, method = 'GET', payload?: object): Promise<T> {
  const response = await fetch('/api/plugins/tjupt' + path, {
    method,
    headers: payload ? { 'Content-Type': 'application/json' } : undefined,
    body: payload ? JSON.stringify(payload) : undefined,
  })
  const data = (await response.json().catch(() => null)) as (T & { message?: string }) | null
  if (!response.ok || !data) throw new Error(data?.message || '请求未完成（HTTP ' + response.status + '）')
  return data
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [downloaderResult, state] = await Promise.all([
      request<{ items: Downloader[] }>('/downloaders'),
      request<FeedState>('/feed'),
    ])
    downloaders.value = downloaderResult.items ?? []
    feed.value = state
    draft.value = editableSettings(state.settings)
    if (!draft.value.downloader_id && downloaders.value.length === 1) {
      draft.value.downloader_id = downloaders.value[0].id
    }
  } catch (error) {
    loadError.value = errorMessage(error, '读取北洋园插件配置失败。')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (busy.value || !draft.value || !hasUnsaved.value) return
  configError.value = ''
  savedNotice.value = ''
  if (draft.value.min_gib > draft.value.max_gib) {
    configError.value = '最小种子体积不能高于最大种子体积。'
    return
  }
  if (!Number.isSafeInteger(draft.value.active_limit) || draft.value.active_limit < 0) {
    configError.value = '在管任务数上限必须是非负整数，填 0 表示不限数量。'
    return
  }
  if (
    draft.value.cleanup_enabled &&
    !feed.value?.hnr_configured &&
    !hnrToken.value.trim()
  ) {
    configError.value = '启用达标后自动清理前，请先填写北洋园 API Token。'
    return
  }
  if (!Number.isInteger(draft.value.upload_threshold_kib) || draft.value.upload_threshold_kib < 1) {
    configError.value = '低速上传阈值必须是大于 0 的整数。'
    return
  }
  if (!Number.isInteger(draft.value.low_speed_minutes) || draft.value.low_speed_minutes < 1) {
    configError.value = '连续低速时长必须是大于 0 的整数。'
    return
  }

  busy.value = 'save'
  const payload = {
    ...draft.value,
    tags: draft.value.tags.trim(),
    category: draft.value.category.trim(),
    save_path: draft.value.save_path.trim(),
    ...(rssURL.value.trim() ? { rss_url: rssURL.value.trim() } : {}),
    ...(hnrToken.value.trim() ? { hnr_token: hnrToken.value.trim() } : {}),
  }
  try {
    const state = await request<FeedState>('/feed', 'PUT', payload)
    feed.value = state
    draft.value = editableSettings(state.settings)
    rssURL.value = ''
    hnrToken.value = ''
    candidates.value = null
    torrents.value = null
    trial.value = null
    runError.value = ''
    previewError.value = ''
    statusError.value = ''
    savedNotice.value = '配置已保存。'
  } catch (error) {
    configError.value = errorMessage(error, '保存配置失败。')
  } finally {
    busy.value = null
  }
}

async function refreshRun(quiet = false) {
  if (loading.value || busy.value || (quiet && polling)) return
  const priorState = feed.value
  if (quiet) {
    polling = true
  } else {
    busy.value = 'refresh'
    runError.value = ''
  }
  try {
    const state = await request<FeedState>('/feed')
    if (quiet && (busy.value || feed.value !== priorState)) return
    if (!hasUnsaved.value) draft.value = editableSettings(state.settings)
    feed.value = state
  } catch (error) {
    if (!quiet) runError.value = errorMessage(error, '读取运行状态失败。')
  } finally {
    if (quiet) polling = false
    else busy.value = null
  }
}

async function runTrial() {
  if (busy.value || !canStart.value || feed.value?.enabled) return
  busy.value = 'trial'
  runError.value = ''
  trial.value = null
  try {
    trial.value = await request<RunResult>('/feed/trial', 'POST')
  } catch (error) {
    runError.value = errorMessage(error, '试运行失败。')
  } finally {
    busy.value = null
  }
}

async function toggleRun() {
  if (busy.value || !feed.value) return
  const action = feed.value.enabled ? 'stop' : 'start'
  if (action === 'start' && !canStart.value) return
  busy.value = action
  runError.value = ''
  try {
    feed.value = await request<FeedState>('/feed/' + action, 'POST')
  } catch (error) {
    runError.value = errorMessage(error, action === 'start' ? '启动循环失败。' : '停止循环失败。')
  } finally {
    busy.value = null
  }
}

async function toggleSiteUploadLimit() {
  if (busy.value || !feed.value) return
  const enabled = !feed.value.settings.use_site_upload_limit
  busy.value = 'upload-limit'
  uploadLimitError.value = ''
  try {
    // This setting is saved independently so unrelated unsaved form changes stay intact.
    feed.value = await request<FeedState>('/feed/upload-limit', 'POST', { enabled })
  } catch (error) {
    uploadLimitError.value = errorMessage(error, '更新站点上传限速失败。')
  } finally {
    busy.value = null
  }
}

const siteUploadLimitLabel = computed(() => {
  const value = feed.value?.site_upload_limit_mbps
  if (value == null) return '暂不可读取'
  return value === 0 ? '不限速' : `${value} MB/s`
})

async function preview() {
  if (busy.value || !feed.value?.rss_configured) return
  busy.value = 'preview'
  previewError.value = ''
  candidates.value = null
  try {
    const result = await request<{ items: Candidate[] }>('/feed/preview', 'POST')
    candidates.value = result.items ?? []
  } catch (error) {
    previewError.value = errorMessage(error, '读取 RSS 失败。')
  } finally {
    busy.value = null
  }
}

async function refreshStatus() {
  if (busy.value || !readyForStatus.value) return
  busy.value = 'status'
  statusError.value = ''
  torrents.value = null
  try {
    const result = await request<{ items: Torrent[] }>('/feed/status', 'POST')
    torrents.value = result.items ?? []
  } catch (error) {
    statusError.value = errorMessage(error, '查询 H&R 状态失败。')
  } finally {
    busy.value = null
  }
}

function formatSize(bytes: number | null) {
  if (bytes === null || !Number.isFinite(bytes)) return '未知'
  return (bytes / 1024 ** 3).toFixed(2) + ' GiB'
}

function candidateResult(item: Candidate) {
  if (item.eligible) return item.assessment_hours == null ? 'H&R 未知' : 'HR ' + item.assessment_hours + 'H'
  if (item.size_bytes == null || item.size_bytes <= 0) return '体积未知'
  const settings = feed.value?.settings
  if (settings && item.size_bytes > settings.max_gib * 1024 ** 3) return '体积超限'
  if (settings && item.size_bytes < settings.min_gib * 1024 ** 3) return '低于最小体积'
  return item.reason || '未通过筛选'
}

function progressLabel(value: number | null) {
  return value == null ? '未知' : Math.round(value * 100) + '%'
}

function remainingHnr(assessment: Assessment) {
  const { code, seeded_seconds: seeded, required_seconds: required } = assessment
  if (code === 20 || code === 21) return '0 分钟'
  if (code != null && code >= 40 && code <= 44) return '考核已结束'
  if (required == null || seeded == null || required <= 0 || seeded < 0) return '暂不可计算'
  if (seeded >= required) return '0 分钟 · 待站点确认'
  const minutes = Math.ceil((required - seeded) / 60)
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return [hours ? hours + ' 小时' : '', rest ? rest + ' 分钟' : ''].filter(Boolean).join(' ')
}

function formatTime(timestamp: string) {
  const date = new Date(timestamp)
  return Number.isNaN(date.getTime()) ? timestamp : date.toLocaleString()
}

onMounted(() => {
  void load()
  pollTimer = window.setInterval(() => {
    if (feed.value?.enabled || feed.value?.running) void refreshRun(true)
  }, 5000)
})
onUnmounted(() => window.clearInterval(pollTimer))
</script>

<template>
  <section class="tjupt-panel" aria-label="北洋园 RSS 刷流">
    <p v-if="loading" class="empty-state" role="status">正在读取配置…</p>
    <div v-else-if="loadError" class="panel-card">
      <p class="panel-message panel-error" role="alert">{{ loadError }}</p>
      <button class="action-button action-secondary" type="button" @click="load">重新读取配置</button>
    </div>

    <template v-else-if="feed && draft">
      <form class="panel-card" aria-labelledby="tjupt-feed-heading" @submit.prevent="save">
        <div class="section-heading">
          <h3 id="tjupt-feed-heading">下载配置</h3>
          <span v-if="hasUnsaved" class="status-pill status-muted">有未保存的修改</span>
        </div>
        <fieldset class="panel-form" :disabled="busy === 'save'">
          <label class="field">
            <span>北洋园 RSS 地址</span>
            <input v-model="rssURL" type="password" autocomplete="new-password" :placeholder="feed.rss_configured ? '已保存，留空保留' : '粘贴北洋园 RSS 地址'" />
          </label>
          <label class="field">
            <span>北洋园 API Token</span>
            <input v-model="hnrToken" type="password" autocomplete="new-password" :placeholder="feed.hnr_configured ? '已保存，留空保留' : '用于查询剩余做种时间'" />
          </label>
          <label class="field">
            <span>下载器</span>
            <select v-model="draft.downloader_id" :disabled="!downloaders.length">
              <option value="">选择 qB 下载器</option>
              <option v-if="draft.downloader_id && !downloaders.some(({ id }) => id === draft?.downloader_id)" :value="draft.downloader_id" disabled>已保存的下载器不可用</option>
              <option v-for="item in downloaders" :key="item.id" :value="item.id">{{ item.name }}</option>
            </select>
          </label>
          <label class="field">
            <span>RSS 获取间隔（分钟）</span>
            <input v-model.number="draft.interval_minutes" type="number" min="1" max="1440" step="1" required />
          </label>
          <label class="field">
            <span>标签</span>
            <input v-model="draft.tags" type="text" placeholder="多个标签用逗号分隔" />
          </label>
          <label class="field">
            <span>分类</span>
            <input v-model="draft.category" type="text" placeholder="可选，留空不分类" />
          </label>
          <div class="field">
            <div class="field-heading"><span id="tjupt-rss-size-label">种子体积范围（GiB）</span><TjuptHrDurationHint /></div>
            <div class="range-inputs" role="group" aria-labelledby="tjupt-rss-size-label">
              <input v-model.number="draft.min_gib" type="number" min="0" max="2000" step="any" aria-label="最小种子体积" required />
              <span>至</span>
              <input v-model.number="draft.max_gib" type="number" min="0.1" max="2000" step="any" aria-label="最大种子体积" required />
            </div>
          </div>
          <label class="field">
            <span>在管任务数上限（0 表示不限数量）</span>
            <input v-model.number="draft.active_limit" type="number" min="0" step="1" required />
            <small class="help-text">设置为 0 时不限制同时管理的任务数；正整数表示最多管理的任务数。</small>
          </label>
          <label class="field">
            <span>管理体积上限（GiB）</span>
            <input v-model.number="draft.total_limit_gib" type="number" min="0.1" max="20000" step="any" required />
          </label>
          <label class="field">
            <span>保存目录</span>
            <input v-model="draft.save_path" type="text" placeholder="留空使用 qB 默认目录" />
          </label>
          <div class="cleanup-settings">
            <label class="cleanup-toggle">
              <input v-model="draft.cleanup_enabled" type="checkbox" />
              <span>HR 达标后按低速条件自动清理任务</span>
            </label>
            <div class="cleanup-fields" :aria-disabled="!draft.cleanup_enabled">
              <label class="field">
                <span>上传低速阈值（KiB/s）</span>
                <input
                  v-model.number="draft.upload_threshold_kib"
                  type="number"
                  min="1"
                  max="1048576"
                  step="1"
                  required
                  :disabled="!draft.cleanup_enabled"
                />
              </label>
              <label class="field">
                <span>连续低速时间（分钟）</span>
                <input
                  v-model.number="draft.low_speed_minutes"
                  type="number"
                  min="1"
                  max="1440"
                  step="1"
                  required
                  :disabled="!draft.cleanup_enabled"
                />
              </label>
            </div>
            <p class="help-text">
              开启后仅在北洋园 HR 两次确认达标、间隔至少 15 分钟，且上传速度连续低于阈值 {{ draft.low_speed_minutes }} 分钟后移除 qB 任务并保留文件。上传速度回到阈值时，低速计时重新开始。
            </p>
          </div>
        </fieldset>
        <p v-if="!downloaders.length" class="help-text">暂无可用 qB 下载器，请先在下载器管理中添加。</p>
        <div class="action-row">
          <button class="action-button" type="submit" :aria-disabled="!!busy || !hasUnsaved">{{ busy === 'save' ? '正在保存…' : '保存' }}</button>
          <span v-if="savedNotice && !hasUnsaved" class="success-text" role="status">{{ savedNotice }}</span>
        </div>
        <p v-if="configError" class="panel-message panel-error" role="alert">{{ configError }}</p>
      </form>

      <section class="panel-card upload-limit-card" aria-labelledby="tjupt-upload-limit-heading">
        <div class="section-heading">
          <div>
            <h3 id="tjupt-upload-limit-heading">站点上传限速</h3>
            <p class="help-text">使用北洋园站点设置中的上传限速（当前：{{ siteUploadLimitLabel }}）。<RouterLink to="/settings/cookie">前往站点管理</RouterLink></p>
          </div>
          <span class="status-pill" :class="feed.settings.use_site_upload_limit ? 'status-safe' : 'status-muted'" role="status">
            {{ feed.settings.use_site_upload_limit ? '已开启' : '已关闭' }}
          </span>
        </div>
        <p class="help-text">适用于正式运行和试运行新加入 qB 的任务，仅限制上传，不改变已有任务。</p>
        <div class="action-row">
          <button
            class="action-button"
            :class="feed.settings.use_site_upload_limit ? 'action-secondary' : ''"
            type="button"
            :aria-pressed="feed.settings.use_site_upload_limit"
            :aria-disabled="!!busy"
            @click="toggleSiteUploadLimit"
          >
            {{ busy === 'upload-limit' ? '保存中…' : feed.settings.use_site_upload_limit ? '关闭限速' : '开启限速' }}
          </button>
        </div>
        <p v-if="uploadLimitError" class="panel-message panel-error" role="alert">{{ uploadLimitError }}</p>
      </section>

      <section class="panel-card" aria-labelledby="tjupt-run-heading">
        <div class="section-heading">
          <h3 id="tjupt-run-heading">运行 RSS 下载</h3>
          <span class="status-pill" :class="feed.enabled ? 'status-safe' : 'status-muted'" role="status">{{ runLabel }}</span>
        </div>
        <p class="help-text">试运行最多添加 1 个种子，添加后保持暂停。正式启动会接续同一配置下已确认的试运行任务，并每 {{ feed.settings.interval_minutes }} 分钟获取 RSS、下载符合条件的种子。</p>
        <div class="action-row">
          <button class="action-button action-secondary" type="button" :aria-disabled="!!busy || !canStart || feed.enabled" @click="runTrial">
            {{ busy === 'trial' ? '正在试运行…' : '试运行一次' }}
          </button>
          <button class="action-button" type="button" :aria-disabled="!!busy || (!feed.enabled && !canStart)" @click="toggleRun">
            {{ busy === 'start' ? '正在启动…' : busy === 'stop' ? '正在停止…' : feed.enabled ? '停止循环' : '正式启动' }}
          </button>
          <button class="action-button action-secondary" type="button" :aria-disabled="!!busy" @click="refreshRun()">
            {{ busy === 'refresh' ? '正在刷新…' : '刷新状态' }}
          </button>
        </div>
        <p v-if="hasUnsaved" class="help-text">保存修改后再试运行或启动循环。</p>
        <p v-else-if="!readyToRun" class="help-text">保存 RSS 地址和下载器后再运行。</p>
        <p v-if="runError" class="panel-message panel-error" role="alert">{{ runError }}</p>
        <p v-if="trial" class="panel-message" :class="trial.failed ? 'panel-error' : 'panel-notice'" role="status">
          试运行添加 {{ trial.added }} 个 · 跳过 {{ trial.skipped }} 个 · 失败 {{ trial.failed }} 个
          <span v-if="trial.message" class="subline">{{ trial.message }}</span>
        </p>
        <div v-if="feed.last_run_at || feed.last_message || feed.last_result" class="run-result">
          <p v-if="feed.last_run_at" class="help-text">上次运行于 {{ formatTime(feed.last_run_at) }}</p>
          <p v-if="feed.last_result" class="help-text">
            添加 {{ feed.last_result.added }} 个 · 跳过 {{ feed.last_result.skipped }} 个 · 失败 {{ feed.last_result.failed }} 个
          </p>
          <p v-if="feed.last_message" class="help-text">{{ feed.last_message }}</p>
        </div>
      </section>

      <section class="panel-card" aria-labelledby="tjupt-preview-heading">
        <div class="section-heading">
          <h3 id="tjupt-preview-heading">RSS 选种预览</h3>
          <button class="action-button action-secondary" type="button" :aria-disabled="!!busy || !feed.rss_configured" @click="preview">
            {{ busy === 'preview' ? '正在读取…' : '读取 RSS 预览' }}
          </button>
        </div>
        <p class="help-text">{{ feed.rss_configured ? '按已保存的体积范围筛选，只读取订阅。' : '保存 RSS 地址后即可查看预览。' }}</p>
        <p v-if="previewError" class="panel-message panel-error" role="alert">{{ previewError }}</p>
        <p v-if="candidates" class="help-text" role="status">已读取 {{ candidates.length }} 条 RSS 候选。</p>
        <div v-if="candidates?.length" class="table-wrap" tabindex="0" role="region" aria-label="RSS 候选">
          <table>
            <thead><tr><th scope="col">种子</th><th scope="col">大小</th><th scope="col">结果</th></tr></thead>
            <tbody>
              <tr v-for="item in candidates" :key="item.id">
                <td><a v-if="item.detail_url" :href="item.detail_url" target="_blank" rel="noopener noreferrer">{{ item.title }}</a><span v-else>{{ item.title }}</span></td>
                <td data-label="大小">{{ formatSize(item.size_bytes) }}</td>
                <td data-label="结果"><span class="status-pill" :class="item.eligible ? 'status-safe' : 'status-muted'">{{ candidateResult(item) }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else-if="candidates" class="empty-state">订阅未返回可识别的候选。</p>
      </section>

      <details class="panel-card hr-status">
        <summary>
          做种任务的 H&amp;R
          <span v-if="torrents" class="hr-status-count">{{ torrents.length }} 项</span>
        </summary>
        <div class="hr-status-body">
          <p class="help-text">{{ readyForStatus ? '读取所选下载器中带「starpt-tjupt」标签的种子，按北洋园记录计算实际剩余做种时间。' : '保存下载器及北洋园 API Token 后即可查询。' }}</p>
          <div class="action-row">
            <button class="action-button action-secondary" type="button" :aria-disabled="!!busy || !readyForStatus" @click="refreshStatus">
              {{ busy === 'status' ? '正在查询…' : '查询 H&R 状态' }}
            </button>
          </div>
          <p v-if="statusError" class="panel-message panel-error" role="alert">{{ statusError }}</p>
          <p v-if="torrents" class="help-text" role="status">已查询 {{ torrents.length }} 个北洋园任务。</p>
          <div v-if="torrents?.length" class="table-wrap" tabindex="0" role="region" aria-label="H&R 考核结果">
            <table>
              <thead><tr><th scope="col">种子</th><th scope="col">下载进度</th><th scope="col">剩余做种</th></tr></thead>
              <tbody>
                <tr v-for="torrent in torrents" :key="torrent.hash">
                  <td>{{ torrent.name }}</td>
                  <td data-label="下载进度">{{ progressLabel(torrent.progress) }}<small class="subline">{{ torrent.state }}</small></td>
                  <td data-label="剩余做种"><strong>{{ remainingHnr(torrent.assessment) }}</strong><small class="subline">{{ torrent.assessment.label }}</small></td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else-if="torrents" class="empty-state">没有找到带「starpt-tjupt」标签的北洋园任务。</p>
        </div>
      </details>
    </template>
  </section>
</template>

<style scoped>
.tjupt-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
  margin-top: 20px;
}
.panel-card {
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 12px;
}
.section-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.section-heading h3 {
  margin: 0;
  font-size: 1rem;
}
.panel-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  min-width: 0;
  margin: 14px 0 0;
  padding: 0;
  border: 0;
}
.field {
  display: grid;
  align-content: end;
  gap: 6px;
  min-width: 0;
  color: var(--star-muted, #a8b3cf);
  font-size: .85rem;
  font-weight: 600;
}
.field-heading {
  display: flex;
  align-items: center;
  gap: 3px;
  min-height: 19px;
}
.field-heading :deep(.hr-hint-trigger) {
  height: 19px;
}
.cleanup-settings {
  grid-column: 1 / -1;
  display: grid;
  gap: 9px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 10px;
  background: var(--star-surface-muted, #17203a);
}
.cleanup-toggle {
  display: flex;
  align-items: center;
  gap: 9px;
  min-height: 24px;
  color: var(--star-ink, #f4f3fb);
  font-size: .9rem;
  font-weight: 600;
}
.cleanup-toggle input {
  width: 18px;
  height: 18px;
  margin: 0;
  accent-color: var(--star-accent, #b8a4ff);
}
.cleanup-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.cleanup-fields[aria-disabled='true'] {
  opacity: .56;
}
.cleanup-settings > .help-text {
  margin: 0;
}
.field input, .field select {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  min-height: 42px;
  padding: 9px 11px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 9px;
  background: var(--star-surface-muted, #17203a);
  color: var(--star-ink, #f4f3fb);
  font: inherit;
}
.field input::placeholder {
  color: var(--star-muted, #a8b3cf);
  font-weight: 400;
}
.range-inputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 7px;
}
.action-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 9px;
  margin-top: 14px;
}
.action-button {
  min-height: 42px;
  padding: 8px 13px;
  border: 1px solid var(--star-accent, #b8a4ff);
  border-radius: 9px;
  background: var(--star-accent-soft, #251e42);
  color: var(--star-ink, #f4f3fb);
  font: inherit;
  font-weight: 650;
  cursor: pointer;
}
.action-secondary {
  border-color: var(--star-border, #293553);
  background: var(--star-surface-muted, #17203a);
}
.action-button:not([aria-disabled='true']):hover {
  border-color: var(--star-accent-hover, #d4c8ff);
}
.action-button[aria-disabled='true'] {
  cursor: default;
  opacity: .56;
}
.help-text, .empty-state {
  margin: 8px 0 0;
  color: var(--star-muted, #a8b3cf);
  font-size: .88rem;
  line-height: 1.55;
}
.run-result {
  margin-top: 14px;
  padding-top: 5px;
  border-top: 1px solid var(--star-border, #293553);
}
.table-wrap {
  overflow-x: auto;
  margin-top: 12px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 10px;
}
table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: .88rem;
}
th, td {
  padding: 11px 12px;
  border-bottom: 1px solid var(--star-border, #293553);
  vertical-align: middle;
}
th {
  color: var(--star-muted, #a8b3cf);
  font-size: .8rem;
  font-weight: 600;
  white-space: nowrap;
}
tbody tr:last-child td {
  border-bottom: 0;
}
td:first-child {
  max-width: 320px;
  overflow-wrap: anywhere;
}
td a {
  color: var(--star-accent-hover, #d4c8ff);
}
.subline {
  display: block;
  margin-top: 4px;
  color: var(--star-muted, #a8b3cf);
  font-size: .8rem;
  overflow-wrap: anywhere;
}
.status-pill {
  display: inline-block;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: .78rem;
  line-height: 1.4;
}
.status-safe {
  background: rgb(54 148 103 / 20%);
  color: var(--star-success, #8be0ae);
}
.status-muted {
  background: rgb(155 166 191 / 13%);
  color: var(--star-muted, #a8b3cf);
}
.hr-status {
  padding: 0;
}
.hr-status summary {
  padding: 14px 16px;
  font-weight: 650;
  line-height: 1.4;
  cursor: pointer;
}
.hr-status summary::marker {
  color: var(--star-accent, #b8a4ff);
}
.hr-status[open] > summary {
  border-bottom: 1px solid var(--star-border, #293553);
}
.hr-status-count {
  margin-inline-start: 8px;
  color: var(--star-muted, #a8b3cf);
  font-size: .8rem;
}
.hr-status-body {
  min-width: 0;
  padding: 12px 16px 16px;
}
.hr-status-body > .help-text:first-child {
  margin-top: 0;
}
.empty-state {
  padding: 12px;
  border: 1px dashed var(--star-border, #293553);
  border-radius: 10px;
}
.panel-message {
  margin: 10px 0 0;
  padding: 10px 12px;
  border-radius: 8px;
  line-height: 1.5;
}
.panel-error {
  background: rgb(129 42 54 / 24%);
  color: var(--star-danger, #ffb8bc);
}
.panel-error + .action-button {
  margin-top: 10px;
}
.panel-notice {
  background: rgb(54 148 103 / 16%);
}
.panel-notice, .success-text {
  color: var(--star-success, #8be0ae);
}
:where(button, input, select, a, summary, .table-wrap):focus-visible {
  outline: 2px solid var(--star-accent, #b8a4ff);
  outline-offset: 2px;
}
@media (max-width: 700px) {
  .panel-card, .hr-status summary, .hr-status-body {
    padding: 12px;
  }
  .hr-status {
    padding: 0;
  }
  .panel-form {
    grid-template-columns: minmax(0, 1fr);
  }
  .cleanup-settings {
    grid-column: auto;
  }
  .cleanup-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  table {
    display: block;
    position: relative;
  }
  thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  tbody {
    display: block;
  }
  tbody tr {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px 12px;
    padding: 12px;
  }
  tbody tr + tr {
    border-top: 1px solid var(--star-border, #293553);
  }
  td {
    display: block;
    min-width: 0;
    padding: 0;
    border: 0;
    overflow-wrap: anywhere;
  }
  td:first-child {
    grid-column: 1 / -1;
    max-width: none;
    font-weight: 650;
  }
  td[data-label]::before {
    display: block;
    margin-bottom: 3px;
    color: var(--star-muted, #a8b3cf);
    font-size: .76rem;
    content: attr(data-label);
  }
}
</style>
