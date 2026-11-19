<template>
  <section class="publish-logs-view" aria-labelledby="publish-logs-heading">
    <header class="logs-heading">
      <div>
        <h1 id="publish-logs-heading">任务记录</h1>
        <p>发布结果与下载器添加</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="fetchLogs()">刷新</el-button>
    </header>

    <form class="filters" aria-label="筛选任务记录" @submit.prevent="applyFilters">
      <el-input
        v-model="searchQuery"
        aria-label="搜索任务"
        :prefix-icon="Search"
        placeholder="搜索标题、种子 ID 或站点"
        clearable
      />
      <el-select v-model="statusFilter" aria-label="发布状态" placeholder="发布状态" clearable>
        <el-option
          v-for="status in filterStatuses"
          :key="status"
          :label="getPublishDisplayStatus(status).label"
          :value="status"
        />
      </el-select>
      <el-select v-model="sceneFilter" aria-label="场景" placeholder="场景" clearable>
        <el-option label="一种多站" value="multi_site" />
        <el-option label="一站多种" value="multi_torrent" />
      </el-select>
      <el-select
        v-model="triggerFilter"
        aria-label="触发方式"
        placeholder="触发方式"
        clearable
        filterable
        allow-create
        default-first-option
      >
        <el-option label="手动" value="manual" />
        <el-option label="队列" value="queue" />
      </el-select>
      <el-input
        v-model="queueGroupFilter"
        aria-label="队列分组 ID"
        placeholder="队列分组 ID"
        clearable
      />
      <el-input v-model="targetSiteFilter" aria-label="目标站点" placeholder="目标站点" clearable />
      <div class="filter-actions">
        <el-button native-type="submit" type="primary">查询</el-button>
        <el-button @click="clearFilters">清空</el-button>
      </div>
    </form>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

    <div v-loading="loading" class="record-panel" :aria-busy="loading">
      <el-empty
        v-if="!loading && !error && rows.length === 0"
        description="暂无发布记录"
        :image-size="64"
      />
      <div v-if="rows.length" class="record-header" aria-hidden="true">
        <span>种子与站点</span><span>发布时间</span><span>发布结果</span><span>自动添加</span
        ><span>操作</span>
      </div>
      <ul v-if="rows.length" class="record-list" aria-label="发布记录">
        <li v-for="row in displayRows" :key="row.id" class="record">
          <div class="record-title">
            <strong>{{ row.title || row.subtitle || row.torrent_id || '未命名种子' }}</strong>
            <small v-if="row.title && row.subtitle">{{ row.subtitle }}</small>
            <small
              >{{ [row.source_site, row.target_site].filter(Boolean).join(' → ') || '站点未知' }} ·
              {{ formatScene(row.scene) }} · {{ formatTrigger(row.trigger)
              }}<template v-if="row.torrent_id"> · #{{ row.torrent_id }}</template></small
            >
          </div>
          <time class="record-time" :title="`加入时间 ${formatDateTime(row.created_at)}`">{{
            formatDateTime(row.updated_at || row.created_at)
          }}</time>
          <div class="record-outcome">
            <span class="status" :data-tone="row.outcome.tone">{{ row.outcome.label }}</span>
          </div>
          <div class="record-add">
            <span class="status" :data-tone="row.autoAdd.tone">{{ row.autoAdd.label }}</span>
            <small v-if="row.autoAdd.detail">{{ row.autoAdd.detail }}</small>
          </div>
          <div class="record-actions">
            <el-button :icon="Document" text @click="openLogs(row)">日志</el-button>
            <a
              v-if="row.safeUrl"
              :href="row.safeUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="record-link"
              >查看种子 <el-icon><TopRight /></el-icon
            ></a>
            <el-button
              :icon="Delete"
              text
              :disabled="!canDeleteLog(row)"
              aria-label="删除发布记录"
              @click="deleteLog(row)"
            />
          </div>
        </li>
      </ul>

      <footer v-if="total > 0" class="pagination-controls">
        <span>共 {{ total }} 条</span>
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="total"
          :pager-count="5"
          layout="sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </footer>
    </div>

    <LogViewerCard v-model="dialogVisible" :title="dialogTitle" :content="dialogContent" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import axios from 'axios'
import { Delete, Document, Refresh, Search, TopRight } from '@element-plus/icons-vue'
import LogViewerCard from '@/components/LogViewerCard.vue'
import {
  getAutoAddPresentation,
  getPublishDisplayStatus,
  redactPrivateText,
  safeTrackerResultUrl,
} from '@/utils/publishPresentation'
import { ElMessage } from '@/utils/uiNotify'

const emits = defineEmits(['ready'])

type PublishLogRow = {
  id: number | string
  created_at: string
  updated_at: string
  scene: string
  trigger: string
  source_site: string
  target_site: string
  torrent_id: number | string
  title?: string | null
  subtitle?: string | null
  status: string
  result_url?: string | null
  queue_task_id?: number | string | null
  logs?: string | null
  auto_add_result?: string | null
  [key: string]: unknown
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null

const loading = ref(false)
const error = ref('')

const route = useRoute()
const router = useRouter()

const rows = ref<PublishLogRow[]>([])
const filterStatuses = [
  'queued',
  'success',
  'exists',
  'edited',
  'failed',
  'filtered',
  'pre_check_limit',
  'cancelled',
]
const displayRows = computed(() =>
  rows.value.map((row) => ({
    ...row,
    outcome: getPublishDisplayStatus(row.status),
    autoAdd: getAutoAddPresentation(row.status, row.auto_add_result),
    safeUrl: ['success', 'exists', 'edited'].includes(row.status)
      ? safeTrackerResultUrl(row.result_url)
      : '',
  })),
)
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)

const searchQuery = ref('')
const statusFilter = ref('')
const triggerFilter = ref('')
const sceneFilter = ref('')
const queueGroupFilter = ref('')
const targetSiteFilter = ref('')

const dialogVisible = ref(false)
const dialogTitle = ref('日志')
const dialogContent = ref('')

const POLL_INTERVAL_MS = 3000
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollRefreshing = false
let fetchSeq = 0

const readStringSetting = (value: unknown) => {
  if (typeof value === 'string') return value
  if (value === undefined || value === null) return ''
  return String(value)
}

const buildUiSettings = () => ({
  page_size: pageSize.value,
  search_query: searchQuery.value,
  active_filters: {
    status: statusFilter.value,
    trigger: triggerFilter.value,
    scene: sceneFilter.value,
    queue_group_id: queueGroupFilter.value,
    target_site: targetSiteFilter.value,
  },
})

const loadUiSettings = async () => {
  try {
    const response = await axios.get('/api/ui_settings/publish_logs')
    const settings = response.data
    const loadedPageSize = Number(settings?.page_size)
    pageSize.value = Number.isFinite(loadedPageSize) && loadedPageSize > 0 ? loadedPageSize : 20
    searchQuery.value = readStringSetting(settings?.search_query)

    const activeFilters = settings?.active_filters
    if (isRecord(activeFilters)) {
      statusFilter.value = readStringSetting(activeFilters.status)
      triggerFilter.value = readStringSetting(activeFilters.trigger)
      sceneFilter.value = readStringSetting(activeFilters.scene)
      queueGroupFilter.value = readStringSetting(activeFilters.queue_group_id)
      targetSiteFilter.value = readStringSetting(activeFilters.target_site)
    }
  } catch {
    console.error('加载发布日志设置失败')
  }
}

const saveUiSettings = async () => {
  try {
    await axios.post('/api/ui_settings/publish_logs', buildUiSettings())
  } catch {
    console.error('保存发布日志设置失败')
  }
}

const formatTrigger = (trigger: string) => {
  if (trigger === 'queue') return '队列'
  if (trigger === 'manual') return '手动'
  return trigger || '未知'
}

const formatScene = (scene: string) => {
  if (scene === 'multi_site') return '一种多站'
  if (scene === 'multi_torrent') return '一站多种'
  return scene || '未知'
}

const formatDateTime = (raw: string) => {
  const trimmed = (raw || '').trim()
  if (!trimmed) return '—'

  const parts = trimmed.split(' ')
  if (parts.length >= 2) {
    const datePart = parts[0] || ''
    const timePart = parts[1] || ''
    return `${datePart} ${timePart}`
  }

  const dt = new Date(trimmed)
  if (!Number.isNaN(dt.getTime())) {
    const yyyy = dt.getFullYear()
    const mm = String(dt.getMonth() + 1).padStart(2, '0')
    const dd = String(dt.getDate()).padStart(2, '0')
    const hh = String(dt.getHours()).padStart(2, '0')
    const mi = String(dt.getMinutes()).padStart(2, '0')
    const ss = String(dt.getSeconds()).padStart(2, '0')
    return `${yyyy}-${mm}-${dd} ${hh}:${mi}:${ss}`
  }

  return trimmed
}

const fetchLogs = async (options: { silent?: boolean } = {}) => {
  const silent = options.silent === true
  const currentFetchSeq = ++fetchSeq
  if (!silent) {
    loading.value = true
    error.value = ''
  }
  try {
    const response = await axios.get('/api/publish_logs', {
      params: {
        page: currentPage.value,
        page_size: pageSize.value,
        search: searchQuery.value.trim(),
        status: statusFilter.value,
        trigger: triggerFilter.value,
        scene: sceneFilter.value,
        queue_group_id: queueGroupFilter.value.trim(),
        target_site: targetSiteFilter.value.trim(),
      },
    })

    if (!response.data?.success) {
      throw new Error(response.data?.message || '获取发种日志失败')
    }

    if (currentFetchSeq !== fetchSeq) return
    rows.value = Array.isArray(response.data.data) ? (response.data.data as PublishLogRow[]) : []
    total.value = Number(response.data.total || 0)
    if (error.value) {
      error.value = ''
    }
  } catch (e: unknown) {
    if (currentFetchSeq !== fetchSeq) return
    if (!silent) {
      const message = axios.isAxiosError(e)
        ? (e.response?.data as { message?: string; error?: string } | undefined)?.message ||
          (e.response?.data as { error?: string } | undefined)?.error ||
          e.message
        : e instanceof Error
          ? e.message
          : '获取发种日志失败'
      error.value = redactPrivateText(message)
    }
  } finally {
    if (!silent && currentFetchSeq === fetchSeq) {
      loading.value = false
    }
  }
}

const applyFilters = async () => {
  currentPage.value = 1
  await fetchLogs()
  void saveUiSettings()
}

const clearFilters = async () => {
  searchQuery.value = ''
  statusFilter.value = ''
  triggerFilter.value = ''
  sceneFilter.value = ''
  queueGroupFilter.value = ''
  targetSiteFilter.value = ''
  currentPage.value = 1
  if (Object.keys(route.query || {}).length > 0) {
    await router.replace({ path: '/publish-logs', query: {} })
  }
  await fetchLogs()
  void saveUiSettings()
}

const handleSizeChange = async (size: number) => {
  pageSize.value = size
  currentPage.value = 1
  await fetchLogs()
  void saveUiSettings()
}

const handleCurrentChange = async (page: number) => {
  currentPage.value = page
  await fetchLogs()
}

const openLogs = (row: PublishLogRow) => {
  dialogTitle.value = `日志 - ${row.target_site || ''}`
  const autoAdd = getAutoAddPresentation(row.status, row.auto_add_result)
  const lines = [
    redactPrivateText(row.logs || '').trim(),
    `下载器 · ${autoAdd.label}${autoAdd.detail ? ` · ${autoAdd.detail}` : ''}`,
  ]
  dialogContent.value = lines.filter(Boolean).join('\n\n')
  dialogVisible.value = true
}

const canDeleteLog = (row: PublishLogRow) => {
  return Number(row?.id || 0) > 0
}

const deleteLog = async (row: PublishLogRow) => {
  if (!canDeleteLog(row)) return
  const logID = Number(row.id || 0)
  const isQueued =
    String(row?.status || '').trim() === 'queued' && Number(row?.queue_task_id || 0) > 0
  try {
    await ElMessageBox.confirm(
      isQueued ? '确认删除该日志并取消对应待发布任务？' : '确认删除该发种日志条目？',
      isQueued ? '删除待发布日志' : '删除发种日志',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )

    const response = await axios.delete(`/api/publish_logs/${logID}`)
    if (!response.data?.success) {
      throw new Error(response.data?.message || '删除失败')
    }

    ElMessage.success(response.data?.message || '发种日志已删除')
    await fetchLogs()
  } catch (error: unknown) {
    if (error === 'cancel' || error === 'close') return
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message
      : error instanceof Error
        ? error.message
        : '删除失败'
    ElMessage.error(redactPrivateText(message))
  }
}

const readQuery = (key: string) => {
  const raw = route.query[key]
  if (Array.isArray(raw)) return String(raw[0] || '')
  if (raw === undefined || raw === null) return ''
  return String(raw)
}

const applyRouteFilters = (mode: 'merge' | 'replace' = 'merge') => {
  let changed = false

  const queryTrigger = readQuery('trigger').trim()
  const queryScene = readQuery('scene').trim()
  const queryStatus = readQuery('status').trim()
  const querySearch = readQuery('search').trim()
  const queryTargetSite = readQuery('target_site').trim()
  const queryQueueGroupID = readQuery('queue_group_id').trim()

  const nextFilters = {
    trigger: queryTrigger,
    scene: queryScene,
    status: queryStatus,
    search: querySearch,
    targetSite: queryTargetSite,
    queueGroup: queryQueueGroupID,
  }

  if (mode === 'replace') {
    if (triggerFilter.value !== nextFilters.trigger) {
      triggerFilter.value = nextFilters.trigger
      changed = true
    }
    if (sceneFilter.value !== nextFilters.scene) {
      sceneFilter.value = nextFilters.scene
      changed = true
    }
    if (statusFilter.value !== nextFilters.status) {
      statusFilter.value = nextFilters.status
      changed = true
    }
    if (searchQuery.value !== nextFilters.search) {
      searchQuery.value = nextFilters.search
      changed = true
    }
    if (targetSiteFilter.value !== nextFilters.targetSite) {
      targetSiteFilter.value = nextFilters.targetSite
      changed = true
    }
    if (queueGroupFilter.value !== nextFilters.queueGroup) {
      queueGroupFilter.value = nextFilters.queueGroup
      changed = true
    }
    return changed
  }

  if (queryTrigger && triggerFilter.value !== queryTrigger) {
    triggerFilter.value = queryTrigger
    changed = true
  }
  if (queryScene && sceneFilter.value !== queryScene) {
    sceneFilter.value = queryScene
    changed = true
  }
  if (queryStatus && statusFilter.value !== queryStatus) {
    statusFilter.value = queryStatus
    changed = true
  }
  if (querySearch && searchQuery.value !== querySearch) {
    searchQuery.value = querySearch
    changed = true
  }
  if (queryTargetSite && targetSiteFilter.value !== queryTargetSite) {
    targetSiteFilter.value = queryTargetSite
    changed = true
  }
  if (queryQueueGroupID && queueGroupFilter.value !== queryQueueGroupID) {
    queueGroupFilter.value = queryQueueGroupID
    changed = true
  }

  return changed
}

const runPollRefresh = async () => {
  if (pollRefreshing || loading.value) return
  pollRefreshing = true
  try {
    await fetchLogs({ silent: true })
  } finally {
    pollRefreshing = false
  }
}

const startPolling = () => {
  if (pollTimer) {
    clearInterval(pollTimer)
  }
  pollTimer = setInterval(() => {
    void runPollRefresh()
  }, POLL_INTERVAL_MS)
}

const stopPolling = () => {
  if (!pollTimer) return
  clearInterval(pollTimer)
  pollTimer = null
}

onMounted(async () => {
  await loadUiSettings()

  let routeFiltersApplied = false
  if (applyRouteFilters('replace')) {
    currentPage.value = 1
    routeFiltersApplied = true
  }

  await fetchLogs()
  if (routeFiltersApplied) {
    void saveUiSettings()
  }
  emits('ready', fetchLogs)
  startPolling()
})

onBeforeUnmount(() => {
  stopPolling()
})

watch(
  () => route.query,
  async () => {
    if (applyRouteFilters('replace')) {
      currentPage.value = 1
      await fetchLogs()
      void saveUiSettings()
    }
  },
)
</script>

<style scoped>
.publish-logs-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
  height: 100%;
  overflow: auto;
  padding: clamp(14px, 2vw, 28px);
  box-sizing: border-box;
}

.logs-heading,
.pagination-controls,
.record-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logs-heading {
  justify-content: space-between;
  gap: 16px;
}

.logs-heading h1 {
  font-size: clamp(24px, 3vw, 32px);
  font-weight: 650;
  margin: 0;
}

.logs-heading p {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
}

.filters {
  display: grid;
  grid-template-columns: minmax(206px, 2fr) repeat(5, minmax(98px, 1fr)) auto;
  align-items: center;
  gap: 8px;
}

.filter-actions {
  display: flex;
  gap: 8px;
}

.filter-actions .el-button + .el-button,
.record-actions .el-button + .el-button {
  margin-left: 0;
}

.record-panel {
  border: 1px solid var(--el-border-color-light);
  background: var(--el-bg-color-overlay);
  border-radius: 14px;
  min-width: 0;
  min-height: 90px;
}

.record-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.record-header,
.record {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(108px, 0.75fr) minmax(86px, 0.55fr) minmax(
      118px,
      1fr
    ) minmax(168px, auto);
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
}

.record-header {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.record {
  border-top: 1px solid var(--el-border-color-lighter);
}

.record-title,
.record-add {
  display: grid;
  justify-items: start;
  gap: 4px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.record small,
.record time {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-regular);
  font-size: 13px;
}

.status::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  flex: none;
}

.status[data-tone='success'] {
  color: var(--el-color-success-dark-2);
}
.status[data-tone='failed'] {
  color: var(--el-color-danger-dark-2);
}
.status[data-tone='running'] {
  color: var(--el-color-primary-dark-2);
}

.record-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--el-color-primary-dark-2);
  white-space: nowrap;
  font-size: 13px;
  text-decoration: none;
  padding-block: 8px;
}

.record-link:hover,
.record-link:focus-visible {
  text-decoration: underline;
}

.record-actions {
  flex-wrap: wrap;
  column-gap: 6px;
}

.record-actions .el-button {
  padding-inline: 6px;
}

.pagination-controls {
  justify-content: space-between;
  flex-wrap: wrap;
  padding: 10px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

@media (max-width: 1100px) {
  .filters {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (max-width: 780px) {
  .filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .filters > :first-child {
    grid-column: 1 / -1;
  }
  .record-header {
    display: none;
  }
  .record {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 8px;
  }
  .record:first-child {
    border-top: 0;
  }
  .record-title,
  .record-time,
  .record-actions {
    grid-column: 1 / -1;
  }
  .record-actions .el-button,
  .record-link {
    min-height: 44px;
  }
  .pagination-controls :deep(.el-pagination__sizes) {
    display: none;
  }
}
</style>
