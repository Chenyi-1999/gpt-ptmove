<template>
  <div class="sites-view-container">
    <section class="sites-overview" aria-labelledby="sites-overview-title">
      <header class="sites-overview__heading">
        <div>

          <h1 id="sites-overview-title">站点与存储</h1>

        </div>
        <el-button
          class="sites-overview__refresh"
          :loading="siteStatsLoading || groupStatsLoading"
          :icon="Refresh"
          @click="refreshStats"
        >
          刷新统计
        </el-button>
      </header>

      <div class="sites-overview__metrics" aria-label="做种汇总">
        <article class="sites-overview__metric">
          <span class="sites-overview__metric-icon sites-overview__metric-icon--violet"><Connection aria-hidden="true" /></span>
          <div>
            <p>已收录站点</p>
            <strong>{{ siteStatsData.length || (!siteStatsLoading && !siteStatsError) ? formatCount(siteStatsData.length) : '—' }}</strong>
          </div>
        </article>
        <article class="sites-overview__metric">
          <span class="sites-overview__metric-icon sites-overview__metric-icon--blue"><Files aria-hidden="true" /></span>
          <div>
            <p>各站种子数</p>
            <strong>{{ siteStatsData.length || (!siteStatsLoading && !siteStatsError) ? formatCount(totalTorrents) : '—' }}</strong>
          </div>
        </article>
        <article class="sites-overview__metric">
          <span class="sites-overview__metric-icon sites-overview__metric-icon--mint"><Coin aria-hidden="true" /></span>
          <div>
            <p>各站做种体积</p>
            <strong>{{ siteStatsData.length || (!siteStatsLoading && !siteStatsError) ? formatBytes(totalSiteBytes) : '—' }}</strong>
          </div>
          <span class="sites-overview__metric-note">按站点分别计入</span>
        </article>
        <article class="sites-overview__metric">
          <span class="sites-overview__metric-icon sites-overview__metric-icon--amber"><Medal aria-hidden="true" /></span>
          <div>
            <p>官组记录</p>
            <strong>{{ groupStatsData.length || (!groupStatsLoading && !groupStatsError) ? formatCount(groupStatsData.length) : '—' }}</strong>
          </div>
          <span class="sites-overview__metric-note">{{ selectedSite || '全部站点' }}</span>
        </article>
      </div>

      <div class="sites-overview__charts">
        <article class="sites-overview__chart-card" aria-labelledby="site-storage-title">
          <div class="sites-overview__chart-heading">
            <div>

              <h2 id="site-storage-title">站点空间分布</h2>
            </div>
            <span class="sites-overview__chart-unit">总大小 · 前 5</span>
          </div>
          <div v-if="siteStatsError" class="sites-overview__chart-state" role="status">
            <span>{{ siteStatsError }}</span>
            <button type="button" class="sites-overview__retry" @click="fetchSiteStats">重试</button>
          </div>
          <div v-if="siteStatsLoading && !siteStatsData.length" class="sites-overview__chart-state" role="status">
            正在读取站点统计…
          </div>
          <div v-else-if="!siteStorageRanking.length && !siteStatsError" class="sites-overview__chart-state" role="status">
            暂无站点统计数据
          </div>
          <div v-else-if="siteStorageRanking.length" class="sites-overview__bars">
            <div v-for="(site, index) in siteStorageRanking" :key="site.site_name" class="sites-overview__bar-row">
              <div class="sites-overview__bar-label">
                <span :title="site.site_name">{{ site.site_name }}</span>
                <strong>{{ formatBytes(site.total_size) }}</strong>
              </div>
              <div class="sites-overview__track" role="img" :aria-label="`${site.site_name}，${formatBytes(site.total_size)}`">
                <span
                  class="sites-overview__bar"
                  :class="`sites-overview__bar--${index % 2 ? 'blue' : 'violet'}`"
                  :style="{ width: `${siteStoragePercent(site.total_size)}%` }"
                />
              </div>
            </div>
          </div>
        </article>

        <article class="sites-overview__chart-card" aria-labelledby="group-count-title">
          <div class="sites-overview__chart-heading">
            <div>

              <h2 id="group-count-title">官组分布</h2>
            </div>
            <span class="sites-overview__chart-unit">种子数量 · 前 5</span>
          </div>
          <div v-if="groupStatsError" class="sites-overview__chart-state" role="status">
            <span>{{ groupStatsError }}</span>
            <button type="button" class="sites-overview__retry" @click="fetchGroupStats">重试</button>
          </div>
          <div v-if="groupStatsLoading && !groupStatsData.length" class="sites-overview__chart-state" role="status">
            正在读取官组统计…
          </div>
          <div v-else-if="!groupCountRanking.length && !groupStatsError" class="sites-overview__chart-state" role="status">
            暂无可展示的官组数据
          </div>
          <div v-else-if="groupCountRanking.length" class="sites-overview__bars">
            <div v-for="(group, index) in groupCountRanking" :key="`${group.site_name}-${group.group_suffix}`" class="sites-overview__bar-row">
              <div class="sites-overview__bar-label">
                <span :title="`${group.group_suffix} · ${group.site_name}`">{{ group.group_suffix }}<small>{{ group.site_name }}</small></span>
                <strong>{{ formatCount(group.torrent_count) }}</strong>
              </div>
              <div class="sites-overview__track" role="img" :aria-label="`${group.group_suffix}，${group.site_name}，${formatCount(group.torrent_count)} 个种子`">
                <span
                  class="sites-overview__bar"
                  :class="`sites-overview__bar--${index % 2 ? 'blue' : 'violet'}`"
                  :style="{ width: `${groupCountPercent(group.torrent_count)}%` }"
                />
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>

    <div class="layout-wrapper">
      <!-- 第一个象限：做种站点统计 -->
      <div class="quadrant glass-card glass-rounded">
        <h2 class="quadrant-title">做种站点统计</h2>
        <!-- [修改] 添加 table-wrapper 用于在极端情况下提供水平滚动 -->
        <div class="table-wrapper">
          <el-table v-loading="siteStatsLoading" :data="siteStatsData" class="stats-table glass-table"  border height="100%"
            :default-sort="{ prop: 'total_size', order: 'descending' }">
            <template #empty>
              <el-empty description="无站点数据" />
            </template>
            <el-table-column prop="site_name" label="站点名称" align="center" sortable min-width="120" />
            <el-table-column prop="torrent_count" label="做种数量" sortable align="center" />
            <el-table-column prop="total_size" label="做种总体积" sortable align="center" min-width="100">
              <template #default="scope">
                <span>{{ formatBytes(scope.row.total_size) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>

      <!-- 第二个象限：做种官组统计 -->
      <div class="quadrant glass-card glass-rounded">
        <h2 class="quadrant-title group-title">
          <span>做种官组统计</span>
          <el-select v-model="selectedSite" placeholder="全部站点" clearable filterable size="small" class="group-site-select"
            @change="handleSiteChange">
            <el-option v-for="s in siteStatsData" :key="s.site_name" :label="s.site_name" :value="s.site_name" />
          </el-select>
        </h2>
        <!-- [修改] 添加 table-wrapper 用于在极端情况下提供水平滚动 -->
        <div class="table-wrapper">
          <el-table v-loading="groupStatsLoading" :data="groupStatsData" class="stats-table glass-table" border height="100%"
            :default-sort="{ prop: 'total_size', order: 'descending' }">
            <template #empty>
              <el-empty description="无官组数据" />
            </template>
            <el-table-column prop="site_name" label="所属站点" align="center" sortable width="110" />
            <el-table-column :label="selectedSite ? '官组' : '官组'" prop="group_suffix" sortable align="center" />
            <el-table-column prop="torrent_count" label="数量" sortable align="center" width="80" />
            <el-table-column prop="total_size" label="体积" sortable align="center" width="90">
              <template #default="scope">
                <span>{{ formatBytes(scope.row.total_size) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>

      <!-- 第三个象限：本地文件扫描 -->
      <div class="quadrant local-scan-quadrant glass-card glass-rounded">
        <div class="scan-header">
          <div class="scan-header-left">
            <h2 class="quadrant-title">本地文件扫描</h2>
            <p class="scan-description">检查种子保存目录的顶层文件和内容目录。远程下载器由 HTTPS 领单客户端核验，只返回缺失和孤立文件的结果。</p>
          </div>
          <div class="scan-controls">
            <el-select v-model="selectedPath" clearable placeholder="选择路径(可选)" filterable size="default"
              class="scan-path-select">
              <el-option-group v-for="downloader in downloadersWithPaths" :key="downloader.id" :label="downloader.name">
                <el-option v-for="pathItem in downloader.paths" :key="pathItem.path" :label="pathItem.path"
                  :value="pathItem.path">
                  <span>{{ pathItem.path }}</span>
                  <span style="float: right; color: #8492a6; font-size: 13px">({{ pathItem.count }})</span>
                </el-option>
              </el-option-group>
            </el-select>
            <el-button type="primary" @click="startScan" :loading="scanning" :icon="Search">
              {{ selectedPath ? '扫描选定路径' : '扫描全部路径' }}
            </el-button>
          </div>
        </div>

        <el-alert
          v-if="scanRequestError"
          class="scan-status-alert"
          type="error"
          :title="scanRequestError"
          :closable="false"
          show-icon
        />

        <el-alert
            v-if="scanResult"
            class="scan-status-alert"
            :type="scanStatus === 'complete' ? 'success' : scanStatus === 'partial' ? 'warning' : 'error'"
            :title="scanStatusTitle"
            :closable="false"
            show-icon
          >
            <template v-if="scanStatus !== 'complete'">
              <p class="scan-status-description">
                {{ scanStatus === 'partial'
                  ? '下方数量只统计已核验的路径；未核验或失败的路径不会按 0 缺失、0 孤立显示。'
                  : '当前结果无法确认文件状态；请确认远程客户端在线并重新扫描。' }}
              </p>
              <p v-if="legacyScanNote" class="scan-status-description">{{ legacyScanNote }}</p>
              <ul v-if="scanIssues.length" class="scan-issue-list">
                <li v-for="(issue, index) in scanIssues" :key="`${issue.downloader_name}-${issue.path}-${index}`">
                  <span class="scan-issue-heading">
                    {{ issue.downloader_name || '未知下载器' }} · {{ issue.path || '路径未提供' }}
                  </span>
                  <el-tag :type="issue.status === 'failed' ? 'danger' : 'warning'" size="small">
                    {{ issue.status === 'failed' ? '读取失败' : '未核验' }}
                  </el-tag>
                  <span v-if="issue.message" class="scan-issue-message">{{ issue.message }}</span>
                </li>
              </ul>
            </template>
          </el-alert>

        <!-- 扫描结果统计 -->
        <div v-if="scanResult" class="scan-summary glass-card glass-rounded">
          <el-row :gutter="16">
            <el-col :xs="12" :sm="8" :md="4">
              <div class="stat-item">
                <div class="stat-value">{{ summaryValue('total_torrents') }}</div>
                <div class="stat-label">总种子数</div>
              </div>
            </el-col>
            <el-col :xs="12" :sm="8" :md="5">
              <div class="stat-item">
                <div class="stat-value">{{ summaryValue('total_local_items') }}</div>
                <div class="stat-label">目录项目数</div>
                <div v-if="isPartialCount('total_local_items')" class="stat-note">已核验部分</div>
              </div>
            </el-col>
            <el-col :xs="12" :sm="8" :md="5">
              <div class="stat-item stat-danger">
                <div class="stat-value">{{ summaryValue('missing_count') }}</div>
                <div class="stat-label">缺失文件</div>
                <div v-if="isPartialCount('missing_count')" class="stat-note">已核验部分</div>
              </div>
            </el-col>
            <el-col :xs="12" :sm="8" :md="5">
              <div class="stat-item stat-warning">
                <div class="stat-value">{{ summaryValue('orphaned_count') }}</div>
                <div class="stat-label">孤立文件</div>
                <div v-if="isPartialCount('orphaned_count')" class="stat-note">已核验部分</div>
              </div>
            </el-col>
            <el-col :xs="12" :sm="8" :md="5">
              <div class="stat-item stat-success">
                <div class="stat-value">{{ summaryValue('synced_count') }}</div>
                <div class="stat-label">已找到文件</div>
                <div v-if="isPartialCount('synced_count')" class="stat-note">已核验部分</div>
              </div>
            </el-col>
          </el-row>
        </div>

        <!-- 结果详情 -->
        <div v-if="scanResult && scanStatus !== 'unverified'" class="scan-results glass-card glass-rounded">
          <div style="display: flex; align-items: center; gap: 12px; ">
            <el-tabs v-model="activeTab" style="flex: 0 0 auto;">
              <el-tab-pane name="missing">
                <template #label>
                  <span class="tab-label">
                    缺失文件
                    <el-badge v-if="scanResult.scan_summary.missing_count > 0"
                      :value="scanResult.scan_summary.missing_count" type="danger" :max="999999" />
                  </span>
                </template>
              </el-tab-pane>
              <el-tab-pane name="orphaned">
                <template #label>
                  <span class="tab-label">
                    孤立文件
                    <el-badge v-if="scanResult.scan_summary.orphaned_count > 0"
                      :value="scanResult.scan_summary.orphaned_count" type="warning" :max="999999" />
                  </span>
                </template>
              </el-tab-pane>
              <el-tab-pane name="synced">
                <template #label>
                  <span class="tab-label">
                    已找到文件
                    <el-badge v-if="scanResult.scan_summary.synced_count > 0"
                      :value="scanResult.scan_summary.synced_count" type="success" :max="999999" />
                  </span>
                </template>
              </el-tab-pane>
            </el-tabs>
            <el-select v-model="pathFilter" placeholder="筛选路径" clearable filterable class="path-filter-select">
              <el-option v-for="path in allPaths" :key="path" :label="path" :value="path" />
            </el-select>
          </div>

          <div class="tab-content-area">
            <div v-if="activeTab === 'missing'" class="tab-content">
              <el-table :data="filteredMissingFiles" class="glass-table glass-transition" height="100%"
                :default-sort="{ prop: 'size', order: 'descending' }">
                <el-table-column prop="name" label="种子名称" show-overflow-tooltip align="left" header-align="center" />
                <el-table-column prop="save_path" label="保存路径"  align="center" width="250" show-overflow-tooltip
                  header-align="center" />
                <el-table-column prop="size" label="大小" width="110" sortable align="center">
                  <template #default="{ row }">
                    {{ formatBytes(row.size) }}
                  </template>
                </el-table-column>
                <el-table-column prop="downloader_name" label="下载器" width="120" align="center" />
              </el-table>
            </div>

            <div v-if="activeTab === 'orphaned'" class="tab-content">
              <el-table :data="filteredOrphanedFiles" class="glass-table glass-transition" height="100%"
                :default-sort="{ prop: 'size', order: 'descending' }">
                <el-table-column prop="name" label="文件/文件夹名称" show-overflow-tooltip align="left"
                  header-align="center" />
                <el-table-column prop="path" label="所在路径" width="250" align="center" 
                  :show-overflow-tooltip="{ placement: 'left' }"
                  header-align="center" />
                <el-table-column prop="size" label="大小" width="120" sortable align="center">
                  <template #default="{ row }">
                    {{ row.size ? formatBytes(row.size) : '未知' }}
                  </template>
                </el-table-column>
                <el-table-column prop="is_file" label="类型" width="120" align="center" header-align="center">
                  <template #default="{ row }">
                    <el-tag :type="row.is_file ? 'success' : 'info'" size="small">
                      {{ row.is_file ? '文件' : '文件夹' }}
                    </el-tag>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <div v-if="activeTab === 'synced'" class="tab-content">
              <el-table :data="filteredSyncedTorrents" class="glass-table glass-transition" height="100%">
                <el-table-column prop="name" label="名称" show-overflow-tooltip align="left" header-align="center" />
                <el-table-column prop="path" label="路径" width="250" show-overflow-tooltip align="center"
                  header-align="center" />
                <el-table-column prop="torrents_count" label="任务数" width="120" align="center" header-align="center">
                  <template #default="{ row }">
                    <el-tag v-if="row.torrents_count > 0" type="warning" size="small">
                      {{ row.torrents_count }}
                    </el-tag>
                    <span v-else>{{ row.torrents_count }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="下载器" width="120" align="center" header-align="center">
                  <template #default="{ row }">
                    <el-tag v-for="(name, index) in row.downloader_names" :key="index" size="small"
                      style="margin-right: 4px">
                      {{ name }}
                    </el-tag>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </div>

        <div v-else-if="scanResult" class="empty-state-container">
          <el-empty description="扫描未完成，因此没有可核验的文件清单" />
        </div>

        <!-- 加载中状态 -->
        <div v-if="scanning" v-loading="scanning" element-loading-text="正在核验保存目录…"
          element-loading-background="transparent" class="loading-container">
        </div>

        <!-- 空状态 -->
        <div v-if="!scanResult && !scanning" class="empty-state-container">
          <el-empty :description="scanRequestError ? '扫描未取得结果，请修复连接后重新扫描' : '点击扫描按钮进行本地文件检查'" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { Coin, Connection, Files, Medal, Refresh, Search } from '@element-plus/icons-vue'
import axios from 'axios'
import { useContentFiltersStore } from '@/stores/contentFilters'
import { ElMessage } from '@/utils/uiNotify'

interface SiteStat {
  site_name: string
  torrent_count: number
  total_size: number
}

interface GroupStat {
  site_name: string
  group_suffix: string
  torrent_count: number
  total_size: number
}

interface PathItem {
  path: string
  count: number
}

interface Downloader {
  id: string | number
  name: string
  paths: PathItem[]
}

interface ScanSummary {
  total_torrents: number
  total_local_items: number
  missing_count: number
  orphaned_count: number
  synced_count: number
  remote_torrents_count?: number
  skipped_remote?: boolean
}

interface ScanIssue {
  path: string
  downloader_name: string
  status: 'unverified' | 'failed'
  message: string
}

interface MissingFile {
  name: string
  save_path: string
  size: number
  downloader_name: string
}

interface OrphanedFile {
  name: string
  path: string
  size?: number
  is_file: boolean
  full_path: string
}

interface SyncedTorrent {
  name: string
  path: string
  torrents_count: number
  downloader_names: string[]
}

interface ScanResult {
  scan_summary: ScanSummary
  missing_files: MissingFile[]
  orphaned_files: OrphanedFile[]
  synced_torrents: SyncedTorrent[]
  scan_status?: 'complete' | 'partial' | 'unverified'
  scan_issues?: ScanIssue[]
}

const emits = defineEmits<{
  ready: [refreshFn: () => Promise<void>]
}>()
const contentFiltersStore = useContentFiltersStore()

const siteStatsLoading = ref(true)
const groupStatsLoading = ref(true)
const siteStatsData = ref<SiteStat[]>([])
const groupStatsData = ref<GroupStat[]>([])
const selectedSite = ref('')
const siteStatsError = ref('')
const groupStatsError = ref('')

const totalTorrents = computed(() =>
  siteStatsData.value.reduce((sum, site) => sum + Math.max(0, Number(site.torrent_count) || 0), 0),
)
const totalSiteBytes = computed(() =>
  siteStatsData.value.reduce((sum, site) => sum + Math.max(0, Number(site.total_size) || 0), 0),
)
const siteStorageRanking = computed(() =>
  [...siteStatsData.value]
    .sort((left, right) => right.total_size - left.total_size)
    .slice(0, 5),
)
const groupCountRanking = computed(() =>
  [...groupStatsData.value]
    .sort((left, right) => right.torrent_count - left.torrent_count)
    .slice(0, 5),
)

// 本地扫描相关
const scanning = ref(false)
const activeTab = ref('missing')
const scanResult = ref<ScanResult | null>(null)
const scanFromCache = ref(false)
let scanRevision = 0
const scanRequestError = ref('')
const selectedPath = ref<string>('')
const downloadersWithPaths = ref<Downloader[]>([])

const scanStatus = computed<NonNullable<ScanResult['scan_status']>>(() => {
  if (!scanResult.value) return 'unverified'
  if (scanResult.value.scan_status) return scanResult.value.scan_status
  // Old cached scans explicitly marked remote downloaders as skipped. Treat these
  // legacy results as partial because their zero counts did not cover remote paths.
  const summary = scanResult.value.scan_summary
  return summary.skipped_remote || (summary.remote_torrents_count ?? 0) > 0 ? 'partial' : 'complete'
})
const scanIssues = computed(() => scanResult.value?.scan_issues ?? [])
const legacyScanNote = computed(() => {
  const summary = scanResult.value?.scan_summary
  if (scanStatus.value !== 'partial' || scanIssues.value.length || !summary?.skipped_remote) return ''
  const count = Number(summary.remote_torrents_count ?? 0)
  return count > 0
    ? `旧版缓存记录了 ${formatCount(count)} 个远程任务未核验，但没有保存对应路径明细。请重新扫描以获取逐路径状态。`
    : '旧版缓存标记了远程路径未核验，但没有保存对应路径明细。请重新扫描以获取逐路径状态。'
})
const scanStatusTitle = computed(() => {
  const prefix = scanFromCache.value ? '上次扫描 · ' : ''
  if (scanStatus.value === 'complete' && scanResult.value?.scan_summary.total_torrents === 0)
    return prefix + '当前范围没有可核验的任务'
  if (scanStatus.value === 'complete') return prefix + '扫描完成，所选路径均已核验'
  if (scanStatus.value === 'partial') return prefix + '扫描部分完成，结果仅覆盖已核验路径'
  return prefix + '扫描未完成，当前结果无法核验'
})

type SummaryCountKey = 'total_torrents' | 'total_local_items' | 'missing_count' | 'orphaned_count' | 'synced_count'
const summaryValue = (key: SummaryCountKey): string => {
  const value = Number(scanResult.value?.scan_summary[key] ?? 0)
  if (key === 'total_torrents') return formatCount(value)
  if (scanStatus.value === 'unverified' || (scanStatus.value === 'partial' && value === 0)) return '未核验'
  return formatCount(value)
}
const isPartialCount = (key: SummaryCountKey): boolean =>
  scanStatus.value === 'partial' && Number(scanResult.value?.scan_summary[key] ?? 0) > 0

// 路径筛选（共用一个）
const pathFilter = ref<string>('')

// 计算属性：获取所有路径的并集
const allPaths = computed(() => {
  if (!scanResult.value) return []

  const paths = new Set<string>()

  // 收集缺失文件的路径
  if (scanResult.value.missing_files) {
    scanResult.value.missing_files.forEach(f => paths.add(f.save_path))
  }

  // 收集孤立文件的路径
  if (scanResult.value.orphaned_files) {
    scanResult.value.orphaned_files.forEach(f => paths.add(f.path))
  }

  // 收集正常做种的路径
  if (scanResult.value.synced_torrents) {
    scanResult.value.synced_torrents.forEach(t => paths.add(t.path))
  }

  return Array.from(paths).sort()
})

// 计算属性：根据筛选条件过滤数据
const filteredMissingFiles = computed(() => {
  if (!scanResult.value?.missing_files) return []
  if (!pathFilter.value) return scanResult.value.missing_files
  return scanResult.value.missing_files.filter(f => f.save_path === pathFilter.value)
})

const filteredOrphanedFiles = computed(() => {
  if (!scanResult.value?.orphaned_files) return []
  if (!pathFilter.value) return scanResult.value.orphaned_files
  return scanResult.value.orphaned_files.filter(f => f.path === pathFilter.value)
})

const filteredSyncedTorrents = computed(() => {
  if (!scanResult.value?.synced_torrents) return []
  if (!pathFilter.value) return scanResult.value.synced_torrents
  return scanResult.value.synced_torrents.filter(t => t.path === pathFilter.value)
})

const formatBytes = (bytes: number, decimals = 2): string => {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
  const i = Math.min(sizes.length - 1, Math.max(0, Math.floor(Math.log(bytes) / Math.log(k))))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

const formatCount = (value: number): string =>
  new Intl.NumberFormat('zh-CN').format(Math.max(0, Number(value) || 0))

const siteStoragePercent = (bytes: number): number => {
  const largest = siteStorageRanking.value[0]?.total_size || 0
  return largest > 0 ? Math.max(2, (Math.max(0, bytes) / largest) * 100) : 0
}

const groupCountPercent = (count: number): number => {
  const largest = groupCountRanking.value[0]?.torrent_count || 0
  return largest > 0 ? Math.max(2, (Math.max(0, count) / largest) * 100) : 0
}

// 获取站点统计数据
const fetchSiteStats = async () => {
  siteStatsLoading.value = true
  siteStatsError.value = ''
  try {
    const response = await axios.get('/api/site_stats')
    const data = response.data
    siteStatsData.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error('获取站点统计数据失败:', error)
    siteStatsError.value = siteStatsData.value.length ? '更新失败，显示上次读取的数据' : '站点统计加载失败'
  } finally {
    siteStatsLoading.value = false
  }
}

// 获取官组统计数据
let groupStatsRequest = 0
const fetchGroupStats = async () => {
  const request = ++groupStatsRequest
  groupStatsLoading.value = true
  groupStatsError.value = ''
  try {
    const url = selectedSite.value ? `/api/group_stats?site=${encodeURIComponent(selectedSite.value)}` : '/api/group_stats'
    const response = await axios.get(url)
    const data = response.data
    if (request === groupStatsRequest) groupStatsData.value = Array.isArray(data) ? data : []
  } catch (error) {
    if (request !== groupStatsRequest) return
    console.error('获取官组统计数据失败:', error)
    groupStatsError.value = groupStatsData.value.length ? '更新失败，显示上次读取的数据' : '官组统计加载失败'
  } finally {
    if (request === groupStatsRequest) groupStatsLoading.value = false
  }
}

const handleSiteChange = () => {
  groupStatsData.value = []
  fetchGroupStats()
}

const refreshStats = async () => {
  await Promise.all([fetchSiteStats(), fetchGroupStats()])
}

// 获取下载器路径列表
const fetchDownloadersWithPaths = async () => {
  try {
    await contentFiltersStore.load()
    const params = new URLSearchParams()
    contentFiltersStore.appendQuery(params)
    const res = await axios.get<{ downloaders: Downloader[] }>(
      `/api/local_query/downloaders_with_paths?${params.toString()}`,
    )
    downloadersWithPaths.value = res.data.downloaders || []
  } catch (error) {
    console.error('获取路径列表失败:', error)
  }
}

// 获取缓存的扫描结果
const fetchCachedScanResult = async () => {
  if (scanning.value) return
  const revision = scanRevision
  try {
    const res = await axios.get<ScanResult>('/api/local_query/scan/cache')
    if (!scanning.value && revision === scanRevision) {
      scanFromCache.value = true
      scanResult.value = res.data
    }
  } catch (error) {
    // 404表示没有缓存，这是正常的，不需要报错
    if (axios.isAxiosError(error) && error.response?.status !== 404) {
      console.error('获取缓存扫描结果失败:', error)
    }
  }
}

// 开始扫描
const startScan = async () => {
  if (scanning.value) return
  scanRevision++
  scanning.value = true
  scanResult.value = null
  scanFromCache.value = false
  scanRequestError.value = ''
  try {
    await contentFiltersStore.load()
    const params = new URLSearchParams()
    if (selectedPath.value) params.set('path', selectedPath.value)
    contentFiltersStore.appendQuery(params)
    const url = `/api/local_query/scan?${params.toString()}`
    const res = await axios.post<ScanResult>(url)
    scanResult.value = res.data
    if (scanStatus.value === 'complete') ElMessage.success('扫描完成！')
    else if (scanStatus.value === 'partial') ElMessage.warning('扫描部分完成，请查看未核验路径')
    else ElMessage.error('扫描未完成，当前结果无法核验')
  } catch (error) {
    console.error('扫描失败:', error)
    scanRequestError.value = '扫描请求失败，未获得可核验结果'
    ElMessage.error('扫描失败，请检查连接后重试')
  } finally {
    scanning.value = false
  }
}

const refreshAllData = async () => {
  await Promise.all([fetchSiteStats(), fetchGroupStats(), fetchDownloadersWithPaths(), fetchCachedScanResult()])
}

onMounted(() => {
  refreshAllData()
  emits('ready', refreshAllData)
})
</script>

<style scoped lang="scss">
.sites-view-container {
  display: flex;
  flex-direction: column;
  /* 在小屏幕上，高度需要自适应，而不是固定为 100vh */
  min-height: 100vh;
  padding: 10px 10px 50px 10px;
  box-sizing: border-box;
  font-family:
    -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  /* 在大屏幕上隐藏滚动条，但在小屏幕上需要时会显示 */
  overflow: auto;
}

.layout-wrapper {
  flex-grow: 1;
  position: relative;
  display: grid;
  /* 默认（大屏幕）状态：左侧宽度等于单行高度，使左侧区域看起来像正方形 */
  grid-template-columns: minmax(300px, calc((100vh - 90px) / 2)) 1fr;
  grid-template-rows: 1fr 1fr;
  gap: 20px;
  min-height: 0;
}

.quadrant {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 15px;
  overflow: hidden;
  min-height: 300px;
}

/* 第一、二个象限（做种站点、做种官组）在左侧上下排列 */
.quadrant:nth-child(1),
.quadrant:nth-child(2) {
  grid-column: 1;
}

/* 第三个象限（待定内容）在右侧，跨越两行 */
.quadrant:nth-child(3) {
  grid-column: 2;
  grid-row: 1 / 3;
}

.quadrant-title {
  font-size: 1.1em;
  font-weight: 600;
  color: #444;
  margin: 0 0 10px 0;
  flex-shrink: 0;
}

.group-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.group-site-select {
  width: 240px;
}

.stats-table {
  width: 100%;
  flex-grow: 1;
  background-color: rgba(255, 255, 255, 0.3) !important;
}

:deep(.el-table) {
  height: 100% !important;
}

/* [新增] 表格包装层，用于提供水平滚动 */
.table-wrapper {
  overflow-x: auto;
  position: relative;
}

/* 本地扫描区块特殊样式 */
.local-scan-quadrant {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.scan-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  flex-shrink: 0;
  gap: 20px;
}

.scan-header-left {
  flex: 1;
  min-width: 0;
}

.scan-header-left .quadrant-title {
  margin-bottom: 6px;
}

.scan-description {
  margin: 0;
  font-size: 13px;
  color: #909399;
  line-height: 1.5;
}

.scan-controls {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.scan-path-select {
  width: 280px;
  margin-right: 10px;
}

.scan-summary {
  padding: 10px;
  flex-shrink: 0;
}

.scan-status-alert {
  margin-bottom: 12px;
  flex-shrink: 0;
}

.scan-status-description {
  margin: 4px 0 0;
  line-height: 1.5;
}

.scan-issue-list {
  display: grid;
  gap: 8px;
  max-height: 180px;
  overflow: auto;
  margin: 10px 0 0;
  padding: 0 4px 0 18px;
}

.scan-issue-list li {
  line-height: 1.5;
}

.scan-issue-heading {
  margin-right: 8px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.scan-issue-message {
  display: block;
  margin-top: 3px;
  color: var(--el-text-color-secondary);
  overflow-wrap: anywhere;
}

.stat-item {
  text-align: center;
  padding: 12px;
  border-radius: 6px;
  transition: all 0.3s;
}

.stat-item:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: #303133;
  margin-bottom: 6px;
}

.stat-label {
  font-size: 13px;
  color: #909399;
  font-weight: 500;
}

.stat-note {
  margin-top: 4px;
  color: var(--el-color-warning);
  font-size: 11px;
}

.stat-danger .stat-value {
  color: #f56c6c;
}

.stat-warning .stat-value {
  color: #e6a23c;
}

.stat-success .stat-value {
  color: #67c23a;
}

.scan-results {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 12px;
}

.tab-content-area {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.tab-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.glass-transition {
  background-color: rgba(255, 255, 255, 0.3) !important;
}

.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.path-filter-select {
  width: 240px;
}

.empty-state-container {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
}

.loading-container {
  flex: 1;
  min-height: 300px;
  display: flex;
  align-items: center;
  justify-content: center;
}

:deep(.el-tabs__header) {
  margin: 0;
}

:deep(.el-tabs__item) {
  padding: 0 15px!important;
}

/* --- 响应式布局 --- */

/* 屏幕宽度小于等于 1200px 时 (例如：小尺寸笔记本电脑、大尺寸平板横屏) */
@media (max-width: 1200px) {
  .layout-wrapper {
    /* 布局变为两列 */
    grid-template-columns: repeat(2, 1fr);
    /* 行高自动，允许网格向下扩展 */
    grid-template-rows: auto;
  }
}

/* 屏幕宽度小于等于 768px 时 (例如：平板竖屏、手机) */
@media (max-width: 768px) {
  .sites-view-container {
    /* 允许整个页面垂直滚动 */
    height: auto;
    overflow-y: auto;
    padding: 15px;
    /* 在小屏幕上可以减小一些边距 */
  }

  .layout-wrapper {
    /* 布局变为单列，所有象限垂直堆叠 */
    grid-template-columns: 1fr;
    gap: 15px;
  }

  .quadrant {
    min-height: 360px;
    height: auto;
    padding: 12px;
  }

  .group-title {
    flex-direction: column;
    align-items: flex-start;
  }

  .group-site-select,
  .scan-path-select,
  .path-filter-select {
    width: 100% !important;
    margin-right: 0 !important;
  }

  .scan-header {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }

  .scan-controls {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }

  .scan-summary {
    padding: 8px;
  }

  .stat-value {
    font-size: 22px;
  }

  :deep(.el-tabs__nav-wrap) {
    overflow-x: auto;
  }

  .loading-container,
  .empty-state-container {
    min-height: 180px;
  }
}

/* Data dashboard surfaces follow STARPT's navy and violet visual system. */
.sites-view-container {
  min-height: 100%;
  padding: clamp(16px, 2.4vw, 30px);
  gap: 24px;
  color: var(--star-data-ink, #f4f3fb);
}

.sites-overview {
  width: min(100%, 1480px);
  margin-inline: auto;
}

.sites-overview__heading,
.sites-overview__chart-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}

.sites-overview__heading {
  margin-bottom: 20px;
}


.sites-overview__heading h1 {
  margin: 0;
  color: var(--star-data-ink, #f4f3fb);
  font-size: clamp(1.35rem, 2.3vw, 1.8rem);
  font-weight: 720;
  letter-spacing: -0.035em;
}


.sites-overview__refresh {
  min-height: 40px;
  border-color: var(--star-data-border, #293553);
  border-radius: 12px;
  color: var(--star-data-ink, #f4f3fb);
  background: var(--star-data-panel, #12182b);
}

.sites-overview__refresh:hover,
.sites-overview__refresh:focus-visible {
  border-color: var(--star-data-accent, #b8a4ff);
  color: var(--star-data-ink, #f4f3fb);
  background: var(--star-data-panel-raised, #17203a);
}

.sites-overview__metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}

.sites-overview__metric,
.sites-overview__chart-card {
  border: 1px solid var(--star-data-border, #293553);
  border-radius: var(--star-data-radius, 16px);
  background: linear-gradient(145deg, rgb(23 32 58 / 93%), rgb(16 21 39 / 97%));
  box-shadow: 0 14px 40px rgb(0 0 0 / 12%);
}

.sites-overview__metric {
  box-sizing: border-box;
  display: flex;
  min-width: 0;
  min-height: 104px;
  align-items: center;
  gap: 13px;
  padding: 17px;
}

.sites-overview__metric-icon {
  display: grid;
  width: 42px;
  height: 42px;
  flex: none;
  place-items: center;
  border: 1px solid rgb(184 164 255 / 15%);
  border-radius: 13px;
  font-size: 19px;
}

.sites-overview__metric-icon--violet {
  color: var(--star-data-accent, #b8a4ff);
  background: rgb(159 112 255 / 13%);
}

.sites-overview__metric-icon--blue {
  color: var(--star-data-blue, #72a7ff);
  background: rgb(90 151 255 / 12%);
}

.sites-overview__metric-icon--mint {
  color: var(--star-data-success, #7ce2b7);
  background: rgb(75 203 157 / 12%);
}

.sites-overview__metric-icon--amber {
  color: #f3c47d;
  background: rgb(226 165 70 / 12%);
}

.sites-overview__metric > div {
  min-width: 0;
}

.sites-overview__metric p {
  margin: 0;
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.8rem;
}

.sites-overview__metric strong {
  display: block;
  margin-top: 6px;
  overflow: hidden;
  color: var(--star-data-ink, #f4f3fb);
  font-size: clamp(1.08rem, 2vw, 1.55rem);
  font-weight: 720;
  letter-spacing: -0.035em;
  line-height: 1.15;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sites-overview__metric-note {
  margin-inline-start: auto;
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.7rem;
  text-align: end;
}

.sites-overview__charts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.sites-overview__chart-card {
  min-width: 0;
  padding: 19px 21px 21px;
}

.sites-overview__chart-heading {
  align-items: flex-start;
  margin-bottom: 16px;
}


.sites-overview__chart-heading h2 {
  margin: 0;
  color: var(--star-data-ink, #f4f3fb);
  font-size: 1rem;
  font-weight: 650;
}

.sites-overview__chart-unit {
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.72rem;
  white-space: nowrap;
}

.sites-overview__bars {
  display: grid;
  gap: 13px;
}

.sites-overview__bar-row {
  min-width: 0;
}

.sites-overview__bar-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  color: #dfe5f5;
  font-size: 0.78rem;
}

.sites-overview__bar-label > span {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sites-overview__bar-label small {
  overflow: hidden;
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.68rem;
  text-overflow: ellipsis;
}

.sites-overview__bar-label strong {
  flex: none;
  color: var(--star-data-ink, #f4f3fb);
  font-size: 0.75rem;
  font-weight: 620;
  font-variant-numeric: tabular-nums;
}

.sites-overview__track {
  height: 7px;
  overflow: hidden;
  border-radius: 99px;
  background: rgb(148 166 204 / 13%);
}

.sites-overview__bar {
  display: block;
  height: 100%;
  min-width: 2px;
  border-radius: inherit;
  transition: width 360ms ease;
}

.sites-overview__bar--violet {
  background: linear-gradient(90deg, #8d64f5, #c4a9ff);
  box-shadow: 0 0 14px rgb(163 115 255 / 29%);
}

.sites-overview__bar--blue {
  background: linear-gradient(90deg, #547fef, #82b4ff);
  box-shadow: 0 0 14px rgb(103 154 255 / 25%);
}

.sites-overview__chart-state {
  display: flex;
  min-height: 143px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.84rem;
  text-align: center;
}

.sites-overview__retry {
  padding: 4px 8px;
  border: 1px solid var(--star-data-border, #293553);
  border-radius: 8px;
  color: var(--star-data-accent, #b8a4ff);
  background: transparent;
  cursor: pointer;
}

.sites-overview__retry:hover,
.sites-overview__retry:focus-visible {
  border-color: var(--star-data-accent, #b8a4ff);
  outline: none;
}

.layout-wrapper {
  width: min(100%, 1480px);
  margin: 0 auto;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.3fr);
  grid-template-rows: minmax(300px, 0.8fr) minmax(380px, 1.2fr);
  gap: 16px;
}

.quadrant {
  border-color: var(--star-data-border, #293553) !important;
  border-radius: var(--star-data-radius, 16px) !important;
  background: var(--star-data-panel, #12182b) !important;
  box-shadow: 0 14px 40px rgb(0 0 0 / 12%) !important;
}

.quadrant-title {
  color: var(--star-data-ink, #f4f3fb);
}

.scan-description {
  color: var(--star-data-muted, #a8b3cf);
}

.stat-item,
.scan-summary,
.scan-results {
  border: 1px solid var(--star-data-border, #293553);
  border-radius: 12px;
  background: var(--star-data-panel-raised, #17203a);
}

.scan-summary,
.scan-results {
  color: var(--star-data-ink, #f4f3fb);
}

.stat-value {
  color: var(--star-data-ink, #f4f3fb);
}

.stat-label {
  color: var(--star-data-muted, #a8b3cf);
}

:deep(.glass-table.el-table),
:deep(.stats-table.el-table),
:deep(.glass-transition.el-table) {
  --el-table-bg-color: var(--star-data-panel, #12182b);
  --el-table-tr-bg-color: var(--star-data-panel, #12182b);
  --el-table-header-bg-color: var(--star-data-panel-raised, #17203a);
  --el-table-row-hover-bg-color: rgb(184 164 255 / 9%);
  --el-table-border-color: var(--star-data-border, #293553);
  --el-table-text-color: #e3e9f7;
  --el-table-header-text-color: #c4cde3;
  background-color: var(--star-data-panel, #12182b) !important;
}

:deep(.el-table th.el-table__cell),
:deep(.el-table tr),
:deep(.el-table td.el-table__cell) {
  background-color: var(--star-data-panel, #12182b);
  border-color: var(--star-data-border, #293553);
}

:deep(.el-table__inner-wrapper::before),
:deep(.el-table--border::after),
:deep(.el-table--border::before) {
  background-color: var(--star-data-border, #293553);
}

:deep(.el-table__empty-text),
:deep(.el-tabs__item) {
  color: var(--star-data-muted, #a8b3cf);
}

:deep(.el-tabs__item.is-active) {
  color: var(--star-data-accent, #b8a4ff);
}

:deep(.el-input__wrapper),
:deep(.el-select__wrapper) {
  border: 1px solid var(--star-data-border, #293553);
  border-radius: 10px;
  background-color: var(--star-data-panel-raised, #17203a);
  box-shadow: none;
}

:deep(.el-input__inner),
:deep(.el-select__selected-item),
:deep(.el-select__placeholder) {
  color: var(--star-data-ink, #f4f3fb);
}

:deep(.el-empty__description p) {
  color: var(--star-data-muted, #a8b3cf);
}

:deep(.el-loading-mask) {
  background-color: rgb(10 13 24 / 66%);
}

@media (max-width: 1100px) {
  .sites-overview__metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .layout-wrapper {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: auto;
  }

  .quadrant:nth-child(3) {
    grid-column: 1 / -1;
    grid-row: auto;
    min-height: 480px;
  }
}

@media (max-width: 768px) {
  .sites-view-container {
    gap: 16px;
    padding: 16px 12px 28px;
  }

  .sites-overview__heading {
    align-items: flex-start;
  }

  .sites-overview__refresh {
    flex: none;
  }

  .sites-overview__metrics {
    gap: 9px;
    margin-bottom: 9px;
  }

  .sites-overview__metric {
    min-height: 88px;
    gap: 9px;
    padding: 12px 10px;
  }

  .sites-overview__metric-icon {
    width: 34px;
    height: 34px;
    border-radius: 10px;
    font-size: 16px;
  }

  .sites-overview__metric-note {
    display: none;
  }

  .sites-overview__charts,
  .layout-wrapper {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .sites-overview__chart-card {
    padding: 16px;
  }

  .quadrant:nth-child(1),
  .quadrant:nth-child(2),
  .quadrant:nth-child(3) {
    grid-column: 1;
    grid-row: auto;
  }

  .quadrant,
  .quadrant:nth-child(3) {
    min-height: 340px;
    padding: 12px;
  }

  .local-scan-quadrant,
  .quadrant:nth-child(3) {
    min-height: 460px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .sites-overview__bar {
    transition: none;
  }
}
</style>
