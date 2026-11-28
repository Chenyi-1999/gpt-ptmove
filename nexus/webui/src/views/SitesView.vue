<template>
  <div class="sites-view-container">
    <header class="sites-overview sites-overview__heading">
      <h1>站点与存储</h1>
      <el-button
        class="sites-overview__refresh"
        :loading="siteStatsLoading || groupStatsLoading"
        :icon="Refresh"
        @click="refreshStats"
      >
        刷新统计
      </el-button>
    </header>

    <div class="layout-wrapper" :class="{ 'layout-wrapper--two-columns': sitesLayout.columns === 2 }">
      <template v-for="panel in sitesLayout.order" :key="panel">
        <section v-if="panel === 'summary'" class="sites-overview__chart-card"
          data-panel="summary" aria-labelledby="seeding-summary-title">
          <h2 id="seeding-summary-title" class="quadrant-title">做种汇总</h2>
          <div class="sites-overview__metrics">
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
                <span class="sites-overview__metric-note">按站点分别计入</span>
              </div>
            </article>
            <article class="sites-overview__metric">
              <span class="sites-overview__metric-icon sites-overview__metric-icon--amber"><Medal aria-hidden="true" /></span>
              <div>
                <p>官组记录</p>
                <strong>{{ groupStatsData.length || (!groupStatsLoading && !groupStatsError) ? formatCount(groupStatsData.length) : '—' }}</strong>
                <span class="sites-overview__metric-note">{{ selectedSite || '全部站点' }}</span>
              </div>
            </article>
          </div>
        </section>

        <section v-else-if="panel === 'site-chart'" class="sites-overview__chart-card"
          data-panel="site-chart" aria-labelledby="site-storage-title">
          <div class="sites-overview__chart-heading">
            <h2 id="site-storage-title">站点空间分布</h2>
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
        </section>

        <section v-else-if="panel === 'group-chart'" class="sites-overview__chart-card"
          data-panel="group-chart" aria-labelledby="group-count-title">
          <div class="sites-overview__chart-heading">
            <h2 id="group-count-title">官组分布</h2>
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
        </section>

        <section v-else-if="panel === 'site-stats'" class="quadrant glass-card glass-rounded"
          data-panel="site-stats" aria-labelledby="site-seeding-stats-title">
          <h2 id="site-seeding-stats-title" class="quadrant-title">做种站点统计</h2>
          <div class="table-wrapper">
            <el-table v-loading="siteStatsLoading" :data="siteStatsData" class="stats-table glass-table" border max-height="360"
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
        </section>

        <section v-else-if="panel === 'group-stats'" class="quadrant glass-card glass-rounded"
          data-panel="group-stats" aria-labelledby="group-seeding-stats-title">
          <div class="group-title">
            <h2 id="group-seeding-stats-title" class="quadrant-title">做种官组统计</h2>
            <el-select v-model="selectedSite" placeholder="全部站点" clearable filterable size="small" class="group-site-select"
              aria-label="筛选官组所属站点"
              @change="handleSiteChange">
              <el-option v-for="s in siteStatsData" :key="s.site_name" :label="s.site_name" :value="s.site_name" />
            </el-select>
          </div>
          <div class="table-wrapper">
            <el-table v-loading="groupStatsLoading" :data="groupStatsData" class="stats-table glass-table" border max-height="360"
              :default-sort="{ prop: 'total_size', order: 'descending' }">
              <template #empty>
                <el-empty description="无官组数据" />
              </template>
              <el-table-column prop="site_name" label="所属站点" align="center" sortable width="110" />
              <el-table-column label="官组" prop="group_suffix" sortable align="center" />
              <el-table-column prop="torrent_count" label="数量" sortable align="center" width="80" />
              <el-table-column prop="total_size" label="体积" sortable align="center" width="90">
                <template #default="scope">
                  <span>{{ formatBytes(scope.row.total_size) }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </section>

        <section v-else-if="panel === 'local-scan'" class="quadrant local-scan-quadrant glass-card glass-rounded"
          data-panel="local-scan" aria-labelledby="local-file-scan-title">
          <div class="scan-header">
            <div class="scan-header-left">
              <h2 id="local-file-scan-title" class="quadrant-title">本地文件扫描</h2>
              <p class="scan-description">核验保存目录的顶层项目，检查缺失和孤立文件。远程目录由 HTTPS 领单客户端核验。</p>
            </div>
            <div class="scan-controls">
              <el-select v-model="selectedPath" clearable placeholder="选择路径(可选)" filterable size="default"
                class="scan-path-select" aria-label="选择扫描路径" :disabled="scanning || syncingDownloaders">
                <el-option-group v-for="downloader in downloadersWithPaths" :key="downloader.id" :label="downloader.name">
                  <el-option v-for="pathItem in downloader.paths" :key="pathItem.path" :label="pathItem.path"
                    :value="pathItem.path">
                    <span>{{ pathItem.path }}</span>
                    <span v-if="pathItem.access_status === 'unavailable'" class="path-option-status path-option-unavailable">目录不可访问</span>
                    <span v-else-if="pathItem.access_status === 'remote'" class="path-option-status">远程核验</span>
                    <span v-else class="path-option-count">({{ pathItem.count }})</span>
                  </el-option>
                </el-option-group>
              </el-select>
              <el-button @click="syncDownloaders" :loading="syncingDownloaders" :disabled="scanning" :icon="Refresh">
                同步下载器
              </el-button>
              <el-button type="primary" @click="startScan" :loading="scanning" :disabled="syncingDownloaders" :icon="Search">
                {{ selectedPath ? '扫描选定路径' : '扫描全部路径' }}
              </el-button>
            </div>
          </div>

          <el-alert
            v-if="downloadersLoadError"
            class="scan-status-alert"
            type="error"
            :title="downloadersLoadError"
            :closable="false"
            show-icon
          />
          <el-alert
            v-else-if="downloadersLoading"
            class="scan-status-alert"
            type="info"
            title="正在读取下载器和保存目录…"
            :closable="false"
            :show-icon="false"
          />
          <el-alert
            v-else-if="downloaderPathHint"
            class="scan-status-alert"
            :type="downloaderPathHint.type"
            :title="downloaderPathHint.title"
            :description="downloaderPathHint.description"
            :closable="false"
            show-icon
          />
          <el-alert
            v-if="downloaderSyncError"
            class="scan-status-alert"
            type="error"
            :title="downloaderSyncError"
            :closable="false"
            show-icon
          />
          <el-alert
            v-else-if="downloaderSyncMessage"
            class="scan-status-alert"
            :type="downloaderSyncMessage.type"
            :title="downloaderSyncMessage.title"
            :closable="false"
            show-icon
          />
          <el-alert
            v-if="unavailablePathCount > 0 && !downloadersLoadError"
            class="scan-status-alert"
            type="warning"
            :title="`${formatCount(unavailablePathCount)} 个保存目录在服务端不可访问`"
            description="请检查 STARPT 容器的只读挂载。若容器路径与下载器保存路径不同，请配置路径映射。"
            :closable="false"
            show-icon
          />

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
                    : '请按下方原因处理后重新扫描。' }}
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
            <div class="scan-summary__metrics">
              <div class="stat-item">
                <div class="stat-value">{{ summaryValue('total_torrents') }}</div>
                <div class="stat-label">总种子数</div>
              </div>
              <div class="stat-item">
                <div class="stat-value">{{ summaryValue('total_local_items') }}</div>
                <div class="stat-label">目录项目数</div>
                <div v-if="isPartialCount('total_local_items')" class="stat-note">已核验部分</div>
              </div>
              <div class="stat-item stat-danger">
                <div class="stat-value">{{ summaryValue('missing_count') }}</div>
                <div class="stat-label">缺失文件</div>
                <div v-if="isPartialCount('missing_count')" class="stat-note">已核验部分</div>
              </div>
              <div class="stat-item stat-warning">
                <div class="stat-value">{{ summaryValue('orphaned_count') }}</div>
                <div class="stat-label">孤立文件</div>
                <div v-if="isPartialCount('orphaned_count')" class="stat-note">已核验部分</div>
              </div>
              <div class="stat-item stat-success">
                <div class="stat-value">{{ summaryValue('synced_count') }}</div>
                <div class="stat-label">已找到文件</div>
                <div v-if="isPartialCount('synced_count')" class="stat-note">已核验部分</div>
              </div>
            </div>
          </div>

          <!-- 结果详情 -->
          <div v-if="scanResult && scanStatus !== 'unverified'" class="scan-results glass-card glass-rounded">
            <div class="scan-results__controls">
              <el-tabs v-model="activeTab" class="scan-results__tabs">
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
              <el-select v-model="pathFilter" placeholder="筛选路径" aria-label="筛选扫描结果路径" clearable filterable class="path-filter-select">
                <el-option v-for="path in allPaths" :key="path" :label="path" :value="path" />
              </el-select>
            </div>

            <div class="tab-content-area">
              <div v-if="activeTab === 'missing'" class="tab-content">
                <el-table :data="filteredMissingFiles" class="glass-table glass-transition" max-height="360"
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
                <el-table :data="filteredOrphanedFiles" class="glass-table glass-transition" max-height="360"
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
                <el-table :data="filteredSyncedTorrents" class="glass-table glass-transition" max-height="360">
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
        </section>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { Coin, Connection, Files, Medal, Refresh, Search } from '@element-plus/icons-vue'
import axios from 'axios'
import { useContentFiltersStore } from '@/stores/contentFilters'
import { useSitesLayoutStore } from '@/stores/sitesLayout'
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
  access_status?: 'available' | 'unavailable' | 'remote'
}

interface Downloader {
  id: string | number
  name: string
  paths: PathItem[]
  indexed_count?: number
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
const sitesLayout = useSitesLayoutStore()

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
let downloadersRequest = 0
const downloadersLoading = ref(false)
const downloadersLoadError = ref('')
const syncingDownloaders = ref(false)
const downloaderSyncMessage = ref<{ type: 'warning' | 'success'; title: string } | null>(null)
const downloaderSyncError = ref('')

const unavailablePathCount = computed(() => downloadersWithPaths.value
  .flatMap(downloader => downloader.paths || [])
  .filter(pathItem => pathItem.access_status === 'unavailable').length)
const indexedDownloaderCount = computed(() => downloadersWithPaths.value
  .reduce((sum, downloader) => sum + Math.max(0, Number(downloader.indexed_count) || 0), 0))
const visiblePathCount = computed(() => downloadersWithPaths.value
  .reduce((sum, downloader) => sum + (downloader.paths?.length || 0), 0))
const downloaderPathHint = computed(() => {
  if (downloadersLoadError.value || downloadersLoading.value || unavailablePathCount.value) return null
  if (!downloadersWithPaths.value.length) {
    return {
      type: 'info' as const,
      title: '尚未配置可用下载器',
      description: '请先在设置中配置下载器，再点击“同步下载器”读取任务和保存目录。',
    }
  }
  if (visiblePathCount.value > 0) return null
  if (downloadersWithPaths.value.every(downloader => downloader.indexed_count !== undefined) && indexedDownloaderCount.value === 0) {
    return {
      type: 'warning' as const,
      title: '下载器索引为空',
      description: '点击“同步下载器”读取任务和保存目录，更新后再扫描。',
    }
  }
  if (contentFiltersStore.videoOnly) {
    return {
      type: 'info' as const,
      title: '当前没有符合筛选条件的保存目录',
      description: '视频筛选已开启，列表只显示符合视频筛选的任务路径。可在内容筛选中关闭视频筛选，或先同步下载器。',
    }
  }
  return {
    type: 'info' as const,
    title: '暂无可扫描保存目录',
    description: '点击“同步下载器”更新任务和保存目录。',
  }
})

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
  const request = ++downloadersRequest
  downloadersLoading.value = true
  downloadersLoadError.value = ''
  try {
    await contentFiltersStore.load()
    const params = new URLSearchParams()
    contentFiltersStore.appendQuery(params)
    const res = await axios.get<{ downloaders: Downloader[] }>(
      `/api/local_query/downloaders_with_paths?${params.toString()}`,
    )
    if (request === downloadersRequest) downloadersWithPaths.value = res.data.downloaders || []
    return true
  } catch (error) {
    console.error('获取路径列表失败:', error)
    if (request === downloadersRequest) downloadersLoadError.value = '读取下载器路径失败，请检查服务连接后重试。'
    return false
  } finally {
    if (request === downloadersRequest) downloadersLoading.value = false
  }
}

const syncDownloaders = async () => {
  if (syncingDownloaders.value || scanning.value) return
  syncingDownloaders.value = true
  downloaderSyncMessage.value = null
  downloaderSyncError.value = ''
  try {
    const response = await axios.post<{
      success?: boolean
      message?: string
      stats?: { failed_downloaders?: number }
    }>('/api/refresh_data')
    if (response.data?.success !== true) {
      throw new Error(response.data.message || '下载器同步失败')
    }
    if (scanResult.value) scanFromCache.value = true
    const [, pathsLoaded] = await Promise.all([refreshStats(), fetchDownloadersWithPaths()])
    const failedCount = Math.max(0, Number(response.data?.stats?.failed_downloaders) || 0)
    if (!pathsLoaded) {
      downloaderSyncError.value = '下载器同步请求已完成，但保存目录读取失败。请检查服务连接后重试。'
      ElMessage.warning(downloaderSyncError.value)
    } else if (failedCount > 0) {
      const title = `同步已完成，${formatCount(failedCount)} 个下载器未能更新。请检查下载器连接。`
      downloaderSyncMessage.value = { type: 'warning', title }
      ElMessage.warning(title)
    } else {
      const title = '下载器任务索引和保存目录已更新。'
      downloaderSyncMessage.value = { type: 'success', title }
      ElMessage.success(title)
    }
  } catch (error) {
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string } | undefined)?.message || error.message
      : error instanceof Error ? error.message : '下载器同步失败，请检查连接后重试。'
    downloaderSyncError.value = message
    ElMessage.error(message)
  } finally {
    syncingDownloaders.value = false
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
  if (scanning.value || syncingDownloaders.value) return
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
  void sitesLayout.load().catch(() => {
    ElMessage.warning('未能读取做种检索布局，已显示默认布局')
  })
  refreshAllData()
  emits('ready', refreshAllData)
})
</script>

<style scoped lang="scss">
/* Data dashboard surfaces follow STARPT's navy and violet visual system. */
.sites-view-container {
  display: flex;
  flex-direction: column;
  min-height: 100%;
  padding: clamp(16px, 2.4vw, 30px);
  box-sizing: border-box;
  gap: 24px;
  color: var(--star-data-ink, #f4f3fb);
  font-family:
    -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  overflow: auto;
}

.layout-wrapper {
  display: grid;
  width: min(100%, 1480px);
  margin-inline: auto;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

@media (min-width: 769px) {
  .layout-wrapper--two-columns {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.quadrant {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 15px;
  overflow: hidden;
  border-color: var(--star-data-border, #293553) !important;
  border-radius: var(--star-data-radius, 16px) !important;
  background: var(--star-data-panel, #12182b) !important;
  box-shadow: 0 14px 40px rgb(0 0 0 / 12%) !important;
}

.quadrant-title {
  font-size: 1.1em;
  font-weight: 600;
  color: var(--star-data-ink, #f4f3fb);
  margin: 0 0 10px 0;
  flex-shrink: 0;
}

.group-title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.group-title .quadrant-title {
  margin-bottom: 0;
}

.group-site-select {
  width: min(240px, 100%);
}

.stats-table {
  width: 100%;
}

.table-wrapper {
  min-width: 0;
}

.local-scan-quadrant {
  gap: 16px;
  min-height: 300px;
}

.scan-header {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: flex-start;
  flex-shrink: 0;
  gap: 12px;
}

.scan-header-left {
  flex: 1 1 240px;
  min-width: 0;
}

.scan-header-left .quadrant-title {
  margin-bottom: 6px;
}

.scan-description {
  margin: 0;
  font-size: 13px;
  color: var(--star-data-muted, #a8b3cf);
  line-height: 1.5;
}

.scan-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  min-width: 0;
  gap: 10px;
}

.scan-controls :deep(.el-button + .el-button) {
  margin-left: 0;
}

.scan-path-select {
  width: min(280px, 100%);
}

.path-option-status {
  float: right;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.path-option-unavailable {
  color: var(--el-color-warning);
}

.path-option-count {
  float: right;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.scan-summary {
  padding: 10px;
  flex-shrink: 0;
}

.scan-summary__metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 94px), 1fr));
  gap: 8px;
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
  font-size: clamp(1.1rem, 2vw, 1.5rem);
  font-weight: 700;
  overflow-wrap: anywhere;
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

.scan-results__controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
}

.scan-results__tabs {
  flex: 0 1 auto;
  min-width: 0;
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
  width: min(240px, 100%);
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
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 152px), 1fr));
  gap: 10px;
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
  min-height: 88px;
  align-items: center;
  gap: 10px;
  padding: 12px;
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
  display: block;
  margin-top: 3px;
  color: var(--star-data-muted, #a8b3cf);
  font-size: 0.7rem;
  overflow-wrap: anywhere;
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

@media (max-width: 768px), (max-width: 950px) and (hover: none) and (pointer: coarse) {
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

  .layout-wrapper {
    gap: 10px;
  }

  .layout-wrapper--two-columns {
    grid-template-columns: minmax(0, 1fr);
  }

  .sites-overview__metric-icon {
    width: 34px;
    height: 34px;
    border-radius: 10px;
    font-size: 16px;
  }

  .sites-overview__chart-card {
    padding: 16px;
  }

  .quadrant {
    padding: 12px;
  }

  .group-title,
  .scan-header,
  .scan-controls {
    flex-direction: column;
    align-items: stretch;
  }

  .scan-header-left {
    flex: auto;
  }

  .group-site-select,
  .scan-path-select,
  .path-filter-select {
    width: 100%;
  }

  .scan-summary {
    padding: 8px;
  }

  .empty-state-container,
  .loading-container {
    min-height: 180px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .sites-overview__bar {
    transition: none;
  }
}
</style>
