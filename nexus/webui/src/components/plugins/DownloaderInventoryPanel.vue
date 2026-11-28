<script setup lang="ts">
import { ArrowDown, Refresh } from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { requestPlugin, type WasmPlugin } from './pluginApi'

const props = defineProps<{ plugin: WasmPlugin; refreshing: boolean }>()
const emit = defineEmits<{
  updated: [plugin: WasmPlugin]
  busy: [busy: boolean]
}>()

const inventoryPath = '/downloader-summary/inventory'
const dimensions = {
  category: '分类',
  tag: '标签',
  site: '站点',
  path: '路径',
} as const
const stateLabels = {
  seeding: '做种中',
  downloading: '下载中',
  paused: '已暂停',
  checking: '校验中',
  queued: '排队中',
  error: '异常',
  other: '其他',
} as const
type Dimension = keyof typeof dimensions
type State = keyof typeof stateLabels
type Downloader = { id: string; name: string; enabled: boolean }
type Group = { key: string; label: string; total: number; counts: Record<string, number> }
type Inventory = { refreshed_at: number; total: number; counts: Record<string, number>; groups: Group[] }
type Torrent = {
  name: string
  state: string
  category: string
  tags: string[]
  site: string
  path: string
  progress: number
  size: number
}
type DetailPage = {
  refreshed_at: number
  total: number
  page: number
  page_size: number
  rows: Torrent[]
}

const numberFormat = new Intl.NumberFormat('zh-CN')
const dateFormat = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' })
const downloaders = ref<Downloader[]>([])
const downloaderId = ref('')
const dimension = ref<Dimension>('category')
const status = ref<State | ''>('')
const inventory = ref<Inventory>()
const expandedGroup = ref<string | null>(null)
const detail = ref<DetailPage>()
const requestedPage = ref(1)
const listLoading = ref(false)
const inventoryLoading = ref(false)
const detailLoading = ref(false)
const toggling = ref(false)
const listError = ref('')
const inventoryError = ref('')
const detailError = ref('')
const toggleError = ref('')
let listRequest = 0
let inventoryRequest = 0
let detailRequest = 0

const hasEnabledDownloader = computed(() => downloaders.value.some((row) => row.enabled))
const blocked = computed(() => props.refreshing || toggling.value)
const groups = computed(() => inventory.value?.groups.filter((group) => groupCount(group) > 0) ?? [])
const detailPages = computed(() =>
  detail.value ? Math.max(1, Math.ceil(detail.value.total / detail.value.page_size)) : 1,
)

function message(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback
}

function groupCount(group: Group) {
  return status.value ? (group.counts[status.value] ?? 0) : group.total
}

function stateLabel(state: string) {
  return stateLabels[state as State] ?? stateLabels.other
}

function formatTime(seconds: number) {
  return Number.isFinite(seconds) && seconds > 0 ? dateFormat.format(new Date(seconds * 1000)) : '—'
}

function formatProgress(progress: number) {
  return Number.isFinite(progress) ? numberFormat.format(Math.round(progress * 100)) + '%' : '—'
}

function formatSize(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.max(0, Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1))
  const value = bytes / 1024 ** index
  return numberFormat.format(Math.round(value * 10) / 10) + ' ' + units[index]
}

function clearDetail() {
  detailRequest++
  expandedGroup.value = null
  detail.value = undefined
  requestedPage.value = 1
  detailLoading.value = false
  detailError.value = ''
}

async function loadDownloaders() {
  const request = ++listRequest
  listLoading.value = true
  listError.value = ''
  try {
    const data = await requestPlugin<{ downloaders: Downloader[] }>(inventoryPath, 'GET', '无法读取下载器')
    if (request !== listRequest) return
    if (!Array.isArray(data.downloaders)) throw new Error('下载器列表格式无效')
    downloaders.value = data.downloaders
    if (!data.downloaders.some((row) => row.id === downloaderId.value && row.enabled)) {
      downloaderId.value = data.downloaders.find((row) => row.enabled)?.id ?? ''
    }
  } catch (error) {
    if (request === listRequest) listError.value = message(error, '无法读取下载器')
  } finally {
    if (request === listRequest) listLoading.value = false
  }
}

async function loadInventory(refresh = false) {
  const request = ++inventoryRequest
  clearDetail()
  if (!refresh) inventory.value = undefined
  inventoryError.value = ''
  inventoryLoading.value = Boolean(downloaderId.value && props.plugin.enabled)
  if (!inventoryLoading.value) return

  const query = new URLSearchParams({ downloader_id: downloaderId.value, dimension: dimension.value })
  if (refresh) query.set('refresh', '1')
  try {
    const data = await requestPlugin<Inventory>(inventoryPath + '?' + query, 'GET', '无法读取种子统计')
    if (!Array.isArray(data.groups)) throw new Error('种子统计格式无效')
    if (request === inventoryRequest) inventory.value = data
  } catch (error) {
    if (request === inventoryRequest) inventoryError.value = message(error, '无法读取种子统计')
  } finally {
    if (request === inventoryRequest) inventoryLoading.value = false
  }
}

async function loadDetail(group: string, page: number) {
  const request = ++detailRequest
  requestedPage.value = page
  detailLoading.value = true
  detailError.value = ''
  const query = new URLSearchParams({
    downloader_id: downloaderId.value,
    dimension: dimension.value,
    details: '1',
    group,
    page: String(page),
    page_size: '25',
  })
  if (status.value) query.set('status', status.value)
  try {
    const data = await requestPlugin<DetailPage>(inventoryPath + '?' + query, 'GET', '无法读取种子名单')
    if (!Array.isArray(data.rows)) throw new Error('种子名单格式无效')
    if (request === detailRequest) detail.value = data
  } catch (error) {
    if (request === detailRequest) detailError.value = message(error, '无法读取种子名单')
  } finally {
    if (request === detailRequest) detailLoading.value = false
  }
}

function toggleGroup(group: string) {
  const opening = expandedGroup.value !== group
  clearDetail()
  if (!opening) return
  expandedGroup.value = group
  void loadDetail(group, 1)
}

async function togglePlugin() {
  if (blocked.value) return
  toggling.value = true
  toggleError.value = ''
  emit('busy', true)
  try {
    const data = await requestPlugin<{ plugin: WasmPlugin }>(
      '/downloader-summary/enabled',
      'PUT',
      '更新插件状态失败',
      { enabled: !props.plugin.enabled },
    )
    emit('updated', data.plugin)
  } catch (error) {
    toggleError.value = message(error, '更新插件状态失败')
  } finally {
    toggling.value = false
    emit('busy', false)
  }
}

watch(
  () => props.plugin.enabled,
  (enabled) => {
    if (enabled) {
      void loadDownloaders()
      return
    }
    listRequest++
    listLoading.value = false
    listError.value = ''
    downloaders.value = []
    downloaderId.value = ''
    void loadInventory()
  },
  { immediate: true },
)
watch([downloaderId, dimension], () => void loadInventory())
watch(status, clearDetail)
onBeforeUnmount(() => {
  listRequest++
  inventoryRequest++
  detailRequest++
  if (toggling.value) emit('busy', false)
})
</script>

<template>
  <div class="inventory-panel">
    <div class="inventory-heading">
      <span :class="{ 'inventory-enabled': plugin.enabled }" role="status">
        {{ plugin.enabled ? '已启用' : '已停用' }}
      </span>
      <el-button :disabled="blocked" :loading="toggling" @click="togglePlugin">
        {{ plugin.enabled ? '停用插件' : '启用插件' }}
      </el-button>
    </div>
    <p v-if="toggleError" class="inventory-message inventory-error" role="alert">{{ toggleError }}</p>
    <div v-if="plugin.enabled" class="inventory-browser">
      <p v-if="listLoading" class="inventory-message" role="status">正在读取下载器…</p>
      <div v-else-if="listError" class="inventory-message inventory-error" role="alert">
        <span>{{ listError }}</span>
        <el-button @click="loadDownloaders">重试</el-button>
      </div>
      <p v-else-if="!downloaders.length" class="inventory-message" role="status">暂无已配置的下载器。</p>
      <p v-else-if="!hasEnabledDownloader" class="inventory-message" role="status">当前下载器均已停用。</p>
      <template v-else>
        <div class="inventory-filters">
          <label class="inventory-field">
            <span id="inventory-downloader-label">下载器</span>
            <select v-model="downloaderId" aria-labelledby="inventory-downloader-label" :disabled="blocked">
              <option v-for="downloader in downloaders" :key="downloader.id" :value="downloader.id" :disabled="!downloader.enabled">
                {{ downloader.name }}{{ downloader.enabled ? '' : '（已停用）' }}
              </option>
            </select>
          </label>
          <label class="inventory-field">
            <span id="inventory-dimension-label">统计维度</span>
            <select v-model="dimension" aria-labelledby="inventory-dimension-label" :disabled="blocked">
              <option v-for="(label, key) in dimensions" :key="key" :value="key">{{ label }}</option>
            </select>
          </label>
          <label class="inventory-field">
            <span id="inventory-status-label">种子状态</span>
            <select v-model="status" aria-labelledby="inventory-status-label" :disabled="blocked">
              <option value="">全部状态</option>
              <option v-for="(label, key) in stateLabels" :key="key" :value="key">{{ label }}</option>
            </select>
          </label>
          <el-button :icon="Refresh" :disabled="blocked || !downloaderId" :loading="inventoryLoading" @click="loadInventory(true)">
            刷新统计
          </el-button>
        </div>

        <div class="inventory-results" :aria-busy="inventoryLoading">
          <div v-if="inventoryError" class="inventory-message inventory-error" role="alert">
            <span>{{ inventoryError }}</span>
            <el-button v-if="!inventoryLoading" @click="loadInventory(true)">重试</el-button>
          </div>
          <p v-if="inventoryLoading && !inventory" class="inventory-message" role="status">正在读取种子统计…</p>
          <template v-if="inventory">
            <div class="result-meta">
              <strong v-if="status">匹配 {{ numberFormat.format(inventory.counts[status] ?? 0) }} 个 · 共 {{ numberFormat.format(inventory.total) }} 个种子</strong>
              <strong v-else>共 {{ numberFormat.format(inventory.total) }} 个种子</strong>
              <span>{{ inventoryError ? '上次成功读取' : '统计时间' }} {{ formatTime(inventory.refreshed_at) }}</span>
            </div>
            <p v-if="dimension === 'tag' && groups.length" class="tag-note">带有多个标签的种子会计入各自标签。</p>
            <p v-if="!groups.length" class="inventory-message" role="status">
              {{ inventory.total ? '没有符合所选状态的种子。' : '该下载器没有种子。' }}
            </p>
            <ul v-else class="inventory-groups">
              <li v-for="(group, index) in groups" :key="group.key" class="inventory-group">
                <button
                  type="button"
                  class="group-toggle"
                  :disabled="blocked || inventoryLoading"
                  :aria-expanded="expandedGroup === group.key"
                  :aria-controls="'inventory-detail-' + index"
                  @click="toggleGroup(group.key)"
                >
                  <span class="group-label">{{ group.label }}</span>
                  <span class="group-count">{{ numberFormat.format(groupCount(group)) }} 个</span>
                  <ArrowDown :class="{ 'group-open': expandedGroup === group.key }" aria-hidden="true" />
                </button>
                <ul v-if="!status" class="group-counts" aria-label="状态分布">
                  <template v-for="(label, key) in stateLabels" :key="key">
                    <li v-if="group.counts[key]" :data-state="key">{{ label }} {{ numberFormat.format(group.counts[key]) }}</li>
                  </template>
                </ul>

                <div :id="'inventory-detail-' + index" v-show="expandedGroup === group.key" class="group-detail">
                  <template v-if="expandedGroup === group.key">
                    <p v-if="detailLoading" class="inventory-message" role="status">正在读取种子名单…</p>
                    <div v-else-if="detailError" class="inventory-message inventory-error" role="alert">
                      <span>{{ detailError }}</span>
                      <el-button @click="loadDetail(group.key, requestedPage)">重试</el-button>
                    </div>
                    <template v-else-if="detail">
                      <p v-if="!detail.rows.length" class="inventory-message" role="status">当前没有匹配的种子。</p>
                      <ul v-else class="detail-list" aria-label="种子名单">
                        <li v-for="(torrent, row) in detail.rows" :key="row" class="torrent-detail">
                          <div class="torrent-heading">
                            <strong>{{ torrent.name || '未命名种子' }}</strong>
                            <span class="torrent-state" :data-state="torrent.state">{{ stateLabel(torrent.state) }}</span>
                          </div>
                          <dl class="torrent-meta">
                            <div><dt>进度</dt><dd>{{ formatProgress(torrent.progress) }}</dd></div>
                            <div><dt>体积</dt><dd>{{ formatSize(torrent.size) }}</dd></div>
                            <div><dt>分类</dt><dd>{{ torrent.category || '未分类' }}</dd></div>
                            <div><dt>站点</dt><dd>{{ torrent.site || '未识别' }}</dd></div>
                            <div class="torrent-meta-wide"><dt>标签</dt><dd>{{ torrent.tags?.join('、') || '无' }}</dd></div>
                            <div class="torrent-meta-wide"><dt>路径</dt><dd>{{ torrent.path || '未提供' }}</dd></div>
                          </dl>
                        </li>
                      </ul>
                    </template>
                    <nav v-if="detail && detailPages > 1" class="inventory-pagination" aria-label="种子名单分页">
                      <el-button :disabled="detailLoading || detail.page <= 1" @click="loadDetail(group.key, detail.page - 1)">上一页</el-button>
                      <span>第 {{ detail.page }} / {{ detailPages }} 页</span>
                      <el-button :disabled="detailLoading || detail.page >= detailPages" @click="loadDetail(group.key, detail.page + 1)">下一页</el-button>
                    </nav>
                  </template>
                </div>
              </li>
            </ul>
          </template>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.inventory-panel,
.inventory-browser,
.inventory-results {
  display: grid;
  min-width: 0;
  gap: 14px;
}

.inventory-panel {
  margin-top: 16px;
}

.inventory-heading,
.result-meta,
.inventory-pagination {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
}

.inventory-heading,
.result-meta span,
.tag-note,
.inventory-message,
.inventory-group,
.inventory-pagination {
  color: var(--star-muted, #a8b3cf);
}

.inventory-enabled {
  color: var(--star-accent, #b8a4ff);
}

.inventory-filters {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) auto;
  align-items: end;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 12px;
  background: var(--star-surface-muted, #17203a);
}
.inventory-filters :deep(.el-button) {
  min-height: 38px;
}

.inventory-field {
  display: grid;
  gap: 5px;
  min-width: 0;
  color: var(--star-muted, #a8b3cf);
  font-size: 0.86rem;
}

.inventory-field select {
  width: 100%;
  min-height: 38px;
  padding: 5px 8px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 8px;
  color: var(--star-ink, #f4f3fb);
  background: var(--star-surface, #12182b);
  font: inherit;
}

.result-meta span,
.tag-note,
.inventory-pagination,
.torrent-meta {
  font-size: 0.84rem;
}

.tag-note,
.inventory-message {
  margin: 0;
  line-height: 1.55;
}

.inventory-message {
  display: flex;
  align-items: center;
  gap: 8px 12px;
  flex-wrap: wrap;
  padding: 10px 12px;
  overflow-wrap: anywhere;
}

.inventory-error {
  color: var(--star-warning-ink, #ffd27a);
  background: var(--star-warning-soft, #302719);
  border: 1px solid var(--star-border, #293553);
  border-radius: 10px;
}

.inventory-groups,
.group-counts,
.detail-list {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.inventory-group {
  min-width: 0;
  border: 1px solid var(--star-border, #293553);
  border-radius: 10px;
  background: var(--star-surface-muted, #17203a);
}

.group-toggle {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto 16px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 44px;
  padding: 9px 12px;
  border: 0;
  border-radius: 9px;
  background: transparent;
  color: var(--star-ink, #f4f3fb);
  font: inherit;
  text-align: start;
  cursor: pointer;
}

.group-toggle:hover:not(:disabled) {
  background: var(--star-accent-soft, #251e42);
}

.group-toggle:disabled {
  cursor: wait;
}

.group-label,
.torrent-heading strong,
.torrent-meta dd {
  min-width: 0;
  overflow-wrap: anywhere;
}

.group-label {
  font-weight: 600;
}

.group-count {
  color: var(--star-accent, #b8a4ff);
  font-size: 0.88rem;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.group-toggle svg {
  width: 16px;
  height: 16px;
  color: var(--star-muted, #a8b3cf);
}

.group-open {
  transform: rotate(180deg);
}

.group-counts {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 8px;
  margin: 0 12px 10px;
}

.group-counts li,
.torrent-state {
  border: 1px solid var(--star-border, #293553);
  border-radius: 999px;
  padding: 2px 7px;
  font-size: 0.78rem;
  line-height: 1.5;
  background: var(--star-surface, #12182b);
}

[data-state='seeding'],
[data-state='downloading'] {
  color: var(--star-accent, #b8a4ff);
}

[data-state='error'] {
  color: var(--star-warning-ink, #ffd27a);
}

.group-detail {
  display: grid;
  gap: 12px;
  min-width: 0;
  padding: 10px 12px 12px;
  border-top: 1px solid var(--star-border, #293553);
}

.torrent-detail {
  min-width: 0;
  padding: 10px;
  border-radius: 8px;
  background: var(--star-surface, #12182b);
}

.torrent-heading {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  justify-content: space-between;
  color: var(--star-ink, #f4f3fb);
  line-height: 1.45;
}

.torrent-state {
  flex-shrink: 0;
}

.torrent-meta {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 6px 12px;
  margin: 8px 0 0;
  line-height: 1.45;
}

.torrent-meta div {
  min-width: 0;
}

.torrent-meta .torrent-meta-wide {
  grid-column: 1 / -1;
}

.torrent-meta dd {
  margin: 0;
  color: var(--star-ink, #f4f3fb);
}

.inventory-pagination {
  justify-content: center;
}

:where(.group-toggle, .inventory-field select):focus-visible {
  outline: 2px solid var(--star-accent, #b8a4ff);
  outline-offset: 2px;
}

@media (max-width: 900px) {
  .inventory-filters,
  .torrent-meta {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 420px) {
  .inventory-field:first-child {
    grid-column: 1 / -1;
  }
}
</style>
