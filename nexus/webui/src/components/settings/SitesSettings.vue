<template>
  <div class="cookie-view-container">
    <header class="sites-settings-heading">
      <h2 class="settings-section-title">站点管理</h2>
      <el-input
        v-model="searchQuery"
        placeholder="搜索站点昵称、标识或官组"
        aria-label="搜索站点"
        clearable
        :prefix-icon="Search"
        class="search-input"
      />
    </header>

    <section class="cookie-controls star-surface" aria-labelledby="cookie-cloud-title">
      <h3 id="cookie-cloud-title">CookieCloud</h3>
      <el-form :model="cookieCloudForm" label-position="top" class="cookie-cloud-form">
        <el-form-item label="同步地址">
          <el-input
            v-model="cookieCloudForm.url"
            placeholder="http://127.0.0.1:8088"
            clearable
          ></el-input>
        </el-form-item>
        <el-form-item label="KEY">
          <el-input
            v-model="cookieCloudForm.key"
            placeholder="KEY (UUID)"
            clearable
          ></el-input>
        </el-form-item>
        <el-form-item label="端对端密码">
          <el-input
            v-model="cookieCloudForm.e2e_password"
            type="password"
            show-password
            placeholder="端对端加密密码"
            clearable
          ></el-input>
        </el-form-item>
        <el-form-item class="cookie-cloud-actions">
          <el-button
            type="primary"
            @click="handleSaveAndSync"
            :loading="isCookieActionLoading"
          >
            <el-icon>
              <Refresh />
            </el-icon>
            <span>同步 Cookie</span>
          </el-button>
          <el-link
            href="https://github.com/Chenyi-1999/gpt-ptmove/tree/master/nexus/extension"
            target="_blank"
            rel="noopener noreferrer"
            type="primary"
            class="browser-extension-link"
          >
            浏览器扩展源码
          </el-link>
        </el-form-item>
      </el-form>
    </section>

    <div class="settings-filter">
      <el-radio-group
        v-model="siteFilter"
        aria-label="站点显示范围"
        @change="handleFilterChange"
      >
        <el-radio-button label="existing_supported">已有支持站点</el-radio-button>
        <el-radio-button label="supported">所有支持站点</el-radio-button>
        <el-radio-button label="all">所有站点</el-radio-button>
      </el-radio-group>
    </div>

    <div class="settings-view" v-loading="isSitesLoading">
      <el-table
        :data="paginatedSites"
        class="settings-table glass-table"
        :row-class-name="getRowClassName"
        @sort-change="handleSortChange"
        :default-sort="defaultSort"
      >
        <el-table-column
          prop="nickname"
          label="站点昵称"
          width="100"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        />
        <el-table-column
          prop="support_role"
          label="支持"
          width="100"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <span v-if="getSiteRole(scope.row) === 'both'" class="role-tag role-both">
              源站/目标站
            </span>
            <span v-else-if="getSiteRole(scope.row) === 'source'" class="role-tag role-source">
              源站
            </span>
            <span v-else-if="getSiteRole(scope.row) === 'target'" class="role-tag role-target">
              目标站
            </span>
          </template>
        </el-table-column>
        <el-table-column
          prop="site"
          label="站点标识"
          width="100"
          show-overflow-tooltip
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        />
        <el-table-column
          prop="base_url"
          label="基础URL"
          width="150"
          show-overflow-tooltip
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        />
        <el-table-column
          prop="group"
          label="官组"
          show-overflow-tooltip
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        />
        <el-table-column
          prop="speed_limit"
          label="限速"
          width="100"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <div
              style="
                display: flex;
                justify-content: center;
                align-items: center;
                width: 100%;
                height: 100%;
              "
            >
              <el-tag v-if="scope.row.speed_limit == 0" type="error" size="small">
                当前不限速，重启恢复默认限速
              </el-tag>
              <el-tag v-else-if="scope.row.speed_limit > 999" type="success" size="small">
                不限速
              </el-tag>
              <el-tag v-else type="primary" size="small"> {{ scope.row.speed_limit }} MB/s </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="ratio_threshold"
          label="分享率阈值"
          width="120"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <el-tag v-if="scope.row.ratio_threshold" size="small" type="warning">
              ≥ {{ scope.row.ratio_threshold }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="seed_speed_limit"
          label="出种限速"
          width="110"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <el-tag
              v-if="scope.row.ratio_threshold && scope.row.seed_speed_limit !== null"
              size="small"
              type="info"
            >
              {{ scope.row.seed_speed_limit }} MB/s
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="has_cookie"
          label="Cookie"
          width="100"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <el-tag v-if="scope.row.site === 'rousi'" type="info"> 无需配置 </el-tag>
            <el-tag v-else :type="scope.row.has_cookie ? 'success' : 'danger'">
              {{ scope.row.has_cookie ? '已配置' : '未配置' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="has_passkey"
          label="Passkey"
          width="100"
          align="center"
          sortable="custom"
          :sort-orders="['ascending', 'descending']"
        >
          <template #default="scope">
            <el-tag
              v-if="['hddolby', 'm-team', 'hdtime', 'rousi'].includes(scope.row.site)"
              :type="scope.row.has_passkey ? 'success' : 'danger'"
            >
              {{ scope.row.has_passkey ? '已配置' : '未配置' }}
            </el-tag>
            <el-tag v-else type="success"> 自动获取 </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" align="center" fixed="right">
          <template #default="scope">
            <div class="site-row-actions">
              <el-button type="primary" :icon="Edit" link @click="handleOpenDialog(scope.row)">
                编辑
              </el-button>
              <HoldButton
                :key="scope.row.id"
                appearance="text"
                label="删除站点"
                @hold="handleDelete(scope.row)"
              >
                删除
              </HoldButton>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="settings-footer star-surface">
      <div class="pagination-container">
        <div class="page-size-text">{{ pagination.pageSize }} 条/页</div>
        <el-pagination
          v-model:current-page="pagination.currentPage"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          layout="total, prev, pager, next, jumper"
          background
        />
      </div>
    </div>

    <!-- 编辑站点对话框 -->
    <el-dialog
      v-model="dialogVisible"
      title="编辑站点"
      width="700px"
      :close-on-click-modal="false"
      class="settings-dialog site-edit-dialog"
    >
      <el-form :model="siteForm" ref="siteFormRef" label-position="top">
        <el-form-item label="站点标识" prop="site" required>
          <el-input v-model="siteForm.site" placeholder="例如：pt" disabled></el-input>
          <div class="form-tip">站点标识不可修改。</div>
        </el-form-item>
        <el-form-item label="站点昵称" prop="nickname" required>
          <el-input v-model="siteForm.nickname" placeholder="例如：PT站"></el-input>
        </el-form-item>
        <el-form-item label="基础URL" prop="base_url">
          <el-input v-model="siteForm.base_url" placeholder="例如：pt.com"></el-input>
          <div class="form-tip">用于拼接种子详情页链接。</div>
        </el-form-item>
        <el-form-item label="Tracker域名" prop="special_tracker_domain">
          <el-input
            v-model="siteForm.special_tracker_domain"
            placeholder="例如：pt-tracker.com"
          ></el-input>
          <div class="form-tip">
            如果站点的Tracker域名与主域名的二级域名（则域名去掉前缀后缀部分）不同，请在此填写。
          </div>
        </el-form-item>
        <el-form-item label="关联官组" prop="group">
          <el-input v-model="siteForm.group" placeholder="例如：PT, PTWEB"></el-input>
          <div class="form-tip">用于识别种子所属发布组，多个组用英文逗号(,)分隔。</div>
        </el-form-item>
        <el-form-item label="Cookie" prop="cookie">
          <el-input
            v-model="siteForm.cookie"
            type="textarea"
            :rows="3"
            :placeholder="siteForm.site === 'rousi' ? '无需设置' : '从浏览器获取的Cookie字符串'"
            :disabled="siteForm.site === 'rousi'"
          ></el-input>
        </el-form-item>
        <el-form-item label="Passkey" prop="passkey">
          <el-input v-model="siteForm.passkey" placeholder="站点的Passkey"></el-input>
          <div
            v-if="siteForm.site === 'hddolby'"
            class="form-tip form-choice-heading"
          >
            杜比的passkey为种子详情页复制种子链接时downhash=后的部分
          </div>
          <div
            v-else-if="siteForm.site === 'rousi'"
            class="form-tip form-choice-heading"
          >
            获取肉丝passkey-
            <el-button
              type="primary"
              size="small"
              tag="a"
              href="https://rousi.pro/account?tab=passkey"
              target="_blank"
              rel="noopener noreferrer"
            >
              跳转
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="上传限速 (MB/s)" prop="speed_limit">
          <el-input-number
            v-model="siteForm.speed_limit"
            :min="0"
            :max="1000"
            style="width: 100%"
          />
          <div class="form-tip" style="color: red; font-size: 12px">
            填写 0 重启恢复默认；超过 999 显示不限速
          </div>
        </el-form-item>
        <el-form-item label="分享率阈值" prop="ratio_threshold">
          <el-input-number
            v-model="siteForm.ratio_threshold"
            :min="1.2"
            :max="100"
            :step="0.1"
            :precision="1"
            style="width: 100%"
          />
          <div class="form-tip" style="font-size: 12px">默认 3.0，达到后触发出种限速</div>
        </el-form-item>
        <el-form-item label="出种限速 (MB/s)" prop="seed_speed_limit">
          <el-input-number
            v-model="siteForm.seed_speed_limit"
            :min="0"
            :max="1000"
            style="width: 100%"
          />
          <div class="form-tip" style="color: var(--star-accent)">默认 5 MB/s</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" @click="handleSave" :loading="isSaving"> 保存 </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import axios from 'axios'
import { ElMessageBox } from 'element-plus'
import { Edit, Refresh, Search } from '@element-plus/icons-vue'
import HoldButton from '@/components/HoldButton.vue'
import { ElMessage } from '@/utils/uiNotify'

type SiteConfig = {
  id: number | string | null
  site: string
  nickname: string
  base_url?: string
  special_tracker_domain?: string
  group?: string
  cookie?: string
  passkey?: string
  speed_limit?: number | null
  ratio_threshold?: number | null
  seed_speed_limit?: number | null
  has_cookie?: boolean
  has_passkey?: boolean
  [key: string]: unknown
}

type SiteStatus = {
  site: string
  is_source?: boolean
  is_target?: boolean
  [key: string]: unknown
}

type CookieCloudForm = {
  url: string
  key: string
  e2e_password: string
}

type SortOrder = 'ascending' | 'descending' | null

type SortState = {
  prop: string
  order: SortOrder
}

type PaginationState = {
  currentPage: number
  pageSize: number
  total: number
}

type SiteForm = {
  id: number | string | null
  site: string
  nickname: string
  base_url: string
  special_tracker_domain: string
  group: string
  cookie: string
  passkey: string
  speed_limit: number
  ratio_threshold: number
  seed_speed_limit: number
}

// --- 状态管理 ---
const isSaving = ref(false) // 用于站点编辑对话框的保存按钮

// --- 站点管理状态 ---
const sitesList = ref<SiteConfig[]>([]) // 存储从后端获取的原始列表
const existingSitesSet = ref<Set<string>>(new Set()) // 存储“有此站点”的标识集合（复用后端 active 规则）
const sitesStatusList = ref<SiteStatus[]>([]) // 存储源/目标站点状态信息
const isSitesLoading = ref(false)
const isCookieActionLoading = ref(false) // [新增] 用于新的"同步Cookie"按钮的加载状态
const cookieCloudForm = ref<CookieCloudForm>({ url: '', key: '', e2e_password: '' })
const searchQuery = ref('')
const siteFilter = ref('existing_supported')

// --- 排序状态 ---
const sortState = ref<SortState>({
  prop: 'support_role',
  order: 'ascending',
})

const defaultSort = computed(() => ({
  prop: sortState.value.prop,
  order: sortState.value.order,
}))

// --- 分页状态 ---
const pagination = ref<PaginationState>({
  currentPage: 1,
  pageSize: 30,
  total: 0,
})

// --- 对话框状态 ---
const dialogVisible = ref(false)
const siteFormRef = ref<unknown>(null)
const siteForm = ref<SiteForm>({
  id: null,
  site: '',
  nickname: '',
  base_url: '',
  special_tracker_domain: '',
  group: '',
  cookie: '',
  passkey: '',
  speed_limit: 0, // 前端显示和输入使用 MB/s 单位
  ratio_threshold: 3.0,
  seed_speed_limit: 5,
})

const API_BASE_URL = '/api'

// --- 计算属性 ---

const sitesStatusMap = computed(() => {
  const map = new Map<string, SiteStatus>()
  for (const status of sitesStatusList.value || []) {
    if (status && status.site) {
      map.set(String(status.site), status)
    }
  }
  return map
})

const getSiteStatus = (site: SiteConfig) => {
  if (!site?.site) return null
  return sitesStatusMap.value.get(String(site.site)) || null
}

const getSiteRole = (site: SiteConfig): 'none' | 'both' | 'source' | 'target' => {
  const status = getSiteStatus(site)
  if (!status) return 'none'
  if (status.is_source && status.is_target) return 'both'
  if (status.is_source) return 'source'
  if (status.is_target) return 'target'
  return 'none'
}

const isSiteConfigComplete = (site: SiteConfig) => {
  const hasCookie = Boolean(site?.has_cookie)
  const hasPasskey = Boolean(site?.has_passkey)
  // 与 Passkey 列展示规则保持一致：大多数站点自动获取，仅少数站点需要手动配置
  const needsPasskey = ['hddolby', 'm-team', 'hdtime', 'rousi'].includes(site?.site)
  // 肉丝站点不需要cookie，其他站点需要cookie
  const needsCookie = site?.site !== 'rousi'
  return (!needsCookie || hasCookie) && (!needsPasskey || hasPasskey)
}

const shouldHighlightIncompleteConfig = computed(() =>
  ['existing_supported', 'supported'].includes(siteFilter.value),
)

const normalizeString = (value: unknown) =>
  String(value ?? '')
    .trim()
    .toLowerCase()

const compareStrings = (a: unknown, b: unknown) => {
  const aText = normalizeString(a)
  const bText = normalizeString(b)
  if (!aText && !bText) return 0
  if (!aText) return 1
  if (!bText) return -1
  return aText.localeCompare(bText, 'zh-CN', { numeric: true, sensitivity: 'base' })
}

const compareNullableNumbers = (a: unknown, b: unknown) => {
  const aNull = a === null || a === undefined
  const bNull = b === null || b === undefined
  const aNumber = aNull ? NaN : Number(a)
  const bNumber = bNull ? NaN : Number(b)
  const aInvalid = aNull || Number.isNaN(aNumber)
  const bInvalid = bNull || Number.isNaN(bNumber)
  if (aInvalid && bInvalid) return 0
  if (aInvalid) return 1
  if (bInvalid) return -1
  return aNumber - bNumber
}

const getSupportRank = (site: SiteConfig) => {
  const role = getSiteRole(site)
  if (role === 'both') return 0
  if (role === 'source') return 1
  if (role === 'target') return 2
  return 3
}

const getCookieSortKey = (site: SiteConfig) => {
  if (site?.site === 'rousi') return 2 // 无需配置，放在已配置之后
  return site?.has_cookie ? 1 : 0
}

const getPasskeySortKey = (site: SiteConfig) => {
  const needsPasskey = ['hddolby', 'm-team', 'hdtime', 'rousi'].includes(site?.site)
  if (!needsPasskey) return 2 // 自动获取，放在已配置之后
  return site?.has_passkey ? 1 : 0
}

const compareByProp = (a: SiteConfig, b: SiteConfig, prop: string) => {
  switch (prop) {
    case 'nickname':
      return compareStrings(a?.nickname, b?.nickname)
    case 'support_role':
      return getSupportRank(a) - getSupportRank(b)
    case 'site':
      return compareStrings(a?.site, b?.site)
    case 'base_url':
      return compareStrings(a?.base_url, b?.base_url)
    case 'group':
      return compareStrings(a?.group, b?.group)
    case 'speed_limit':
      return compareNullableNumbers(a?.speed_limit, b?.speed_limit)
    case 'ratio_threshold':
      return compareNullableNumbers(a?.ratio_threshold, b?.ratio_threshold)
    case 'seed_speed_limit':
      return compareNullableNumbers(a?.seed_speed_limit, b?.seed_speed_limit)
    case 'has_cookie':
      return getCookieSortKey(a) - getCookieSortKey(b)
    case 'has_passkey':
      return getPasskeySortKey(a) - getPasskeySortKey(b)
    default:
      return 0
  }
}

const applySortOrder = (compareResult: number, order: SortOrder) => {
  if (!order) return 0
  return order === 'descending' ? -compareResult : compareResult
}

// 1. 先根据前端搜索框进行过滤
const filteredSites = computed(() => {
  let sites = sitesList.value || []

  if (siteFilter.value === 'existing_supported') {
    sites = sites.filter((site) => {
      const status = getSiteStatus(site)
      const isSupported = Boolean(status?.is_source || status?.is_target)
      return isSupported && existingSitesSet.value.has(site.site)
    })
  } else if (siteFilter.value === 'supported') {
    sites = sites.filter((site) => {
      const status = getSiteStatus(site)
      return Boolean(status?.is_source || status?.is_target)
    })
  }

  const term = searchQuery.value.trim().toLowerCase()
  if (term) {
    sites = sites.filter((site) => {
      const nickname = (site.nickname || '').toLowerCase()
      const siteIdentifier = (site.site || '').toLowerCase()
      const group = (site.group || '').toLowerCase()
      return nickname.includes(term) || siteIdentifier.includes(term) || group.includes(term)
    })
  }

  return sites
})

// 2. 再根据排序规则对过滤后的结果进行排序
const sortedSites = computed(() => {
  const sites = (filteredSites.value || []).slice()

  return sites.sort((a, b) => {
    if (shouldHighlightIncompleteConfig.value) {
      const aIncomplete = isSiteConfigComplete(a) ? 0 : 1
      const bIncomplete = isSiteConfigComplete(b) ? 0 : 1
      if (aIncomplete !== bIncomplete) return bIncomplete - aIncomplete
    }

    const { prop, order } = sortState.value || {}
    const hasUserSort = Boolean(prop && order)

    if (hasUserSort) {
      const result = compareByProp(a, b, prop)
      const ordered = applySortOrder(result, order)
      if (ordered !== 0) return ordered
    } else {
      const result = compareByProp(a, b, 'support_role')
      if (result !== 0) return result
    }

    const nicknameTie = compareByProp(a, b, 'nickname')
    if (nicknameTie !== 0) return nicknameTie

    return compareByProp(a, b, 'site')
  })
})

// 3. 再根据分页信息对排序后的结果进行切片
const paginatedSites = computed(() => {
  const start = (pagination.value.currentPage - 1) * pagination.value.pageSize
  const end = start + pagination.value.pageSize
  return sortedSites.value.slice(start, end)
})

watch(
  () => sortedSites.value.length,
  (total) => {
    pagination.value.total = total
  },
  { immediate: true },
)

// 监听搜索词变化，如果变化则返回第一页
watch(searchQuery, () => {
  pagination.value.currentPage = 1
})

const handleSortChange = ({ prop, order }: { prop?: string; order?: SortOrder }) => {
  sortState.value = {
    prop: prop || 'support_role',
    order: order || 'ascending',
  }
  pagination.value.currentPage = 1
}

onMounted(() => {
  fetchCookieCloudSettings()
  fetchSites()
})

// --- 方法 ---

const fetchCookieCloudSettings = async () => {
  try {
    const response = await axios.get(`${API_BASE_URL}/settings`)
    if (response.data && response.data.cookiecloud) {
      cookieCloudForm.value.url = response.data.cookiecloud.url || ''
      cookieCloudForm.value.key = response.data.cookiecloud.key || ''
      cookieCloudForm.value.e2e_password = ''
    }
  } catch {
    ElMessage.error('加载CookieCloud配置失败！')
  }
}

// fetchSites 方法以接受筛选参数
const fetchSites = async () => {
  isSitesLoading.value = true
  try {
    const [allSitesResponse, existingSitesResponse, sitesStatusResponse] = await Promise.all([
      axios.get(`${API_BASE_URL}/sites`, { params: { filter_by_torrents: 'all' } }),
      axios.get(`${API_BASE_URL}/sites`, { params: { filter_by_torrents: 'active' } }),
      axios.get(`${API_BASE_URL}/sites/status`),
    ])

    sitesList.value = Array.isArray(allSitesResponse.data) ? (allSitesResponse.data as SiteConfig[]) : []
    existingSitesSet.value = new Set(
      (Array.isArray(existingSitesResponse.data) ? existingSitesResponse.data : [])
        .map((site) => String((site as Record<string, unknown>)?.site || '').trim())
        .filter(Boolean),
    )
    sitesStatusList.value = Array.isArray(sitesStatusResponse.data)
      ? (sitesStatusResponse.data as SiteStatus[])
      : []
  } catch {
    ElMessage.error('获取站点列表失败！')
  } finally {
    isSitesLoading.value = false
  }
}

// 当后端筛选器改变时，重置分页并重新获取数据
const handleFilterChange = () => {
  pagination.value.currentPage = 1
}

const getRowClassName = ({ row }: { row: SiteConfig }) => {
  if (!shouldHighlightIncompleteConfig.value) return ''
  return isSiteConfigComplete(row) ? '' : 'row-config-incomplete'
}

const normalizeSiteForm = (site: SiteConfig): SiteForm => ({
  id: site.id ?? null,
  site: String(site.site || ''),
  nickname: String(site.nickname || ''),
  base_url: String(site.base_url || ''),
  special_tracker_domain: String(site.special_tracker_domain || ''),
  group: String(site.group || ''),
  cookie: String(site.cookie || ''),
  passkey: String(site.passkey || ''),
  speed_limit: typeof site.speed_limit === 'number' ? site.speed_limit : Number(site.speed_limit) || 0,
  ratio_threshold:
    typeof site.ratio_threshold === 'number' ? site.ratio_threshold : Number(site.ratio_threshold) || 3.0,
  seed_speed_limit:
    typeof site.seed_speed_limit === 'number' ? site.seed_speed_limit : Number(site.seed_speed_limit) || 5,
})

// [新增] 合并后的保存与同步功能
const handleSaveAndSync = async () => {
  const rawURL = String(cookieCloudForm.value.url || '').trim()
  const normalizedURL = (() => {
    if (!rawURL) return ''
    const withScheme = /^https?:\/\//i.test(rawURL) ? rawURL : `http://${rawURL}`
    try {
      const parsed = new URL(withScheme)
      parsed.hash = ''
      return parsed.toString().replace(/\/$/, '')
    } catch {
      return ''
    }
  })()

  // 1. 前端校验
  if (!normalizedURL) {
    ElMessage.warning('CookieCloud URL 格式不正确，请填写 http(s)://host[:port]。')
    return
  }
  if (!cookieCloudForm.value.key) {
    ElMessage.warning('CookieCloud KEY 不能为空！')
    return
  }
  cookieCloudForm.value.url = normalizedURL

  isCookieActionLoading.value = true
  try {
    // 2. 第一步：先保存配置
    await axios.post(`${API_BASE_URL}/settings`, {
      cookiecloud: cookieCloudForm.value,
    })

    // 3. 第二步：配置保存成功后，立即执行同步
    const syncResponse = await axios.post(`${API_BASE_URL}/cookiecloud/sync`, cookieCloudForm.value)

    // 4. 处理同步结果
    if (syncResponse.data.success) {
      // 移除消息中"在 CookieCloud 中另有 X 个未匹配的 Cookie。"部分
      let message = syncResponse.data.message
      if (message) {
        message = message.replace(/在 CookieCloud 中另有 \d+ 个未匹配的 Cookie。?/, '')
      }
      ElMessage.success(`配置已保存. ${message || '同步完成！'}`)
      await fetchSites() // 同步成功后刷新站点列表
    } else {
      ElMessage.error(syncResponse.data.message || '同步失败，但配置已保存。')
    }
  } catch (error: unknown) {
    const errorMessage = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string } | undefined)?.message || error.message)
      : error instanceof Error
        ? error.message
        : '操作失败，请检查网络或后端服务。'
    ElMessage.error(errorMessage)
  } finally {
    isCookieActionLoading.value = false
  }
}

const handleOpenDialog = (site: SiteConfig) => {
  siteForm.value = normalizeSiteForm(site)
  dialogVisible.value = true
}

const handleSave = async () => {
  isSaving.value = true
  try {
    const siteData: SiteForm = {
      ...siteForm.value,
      cookie: siteForm.value.cookie ? siteForm.value.cookie.trim() : '',
      ratio_threshold: siteForm.value.ratio_threshold || 3.0,
      seed_speed_limit: siteForm.value.seed_speed_limit || 5,
    }

    const response = await axios.post(`${API_BASE_URL}/sites/update`, siteData)

    if (response.data.success) {
      ElMessage.success(response.data.message)
      dialogVisible.value = false
      await fetchSites()
    } else {
      ElMessage.error(response.data.message || '操作失败！')
    }
  } catch (error: unknown) {
    const msg = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string } | undefined)?.message || error.message)
      : error instanceof Error
        ? error.message
        : '请求失败，请检查网络或后端服务。'
    ElMessage.error(msg)
  } finally {
    isSaving.value = false
  }
}

const handleDelete = (site: SiteConfig) => {
  ElMessageBox.confirm('', '警告', {
    confirmButtonText: '确定删除',
    cancelButtonText: '取消',
    type: 'warning',
    dangerouslyUseHTMLString: true,
    message: `您确定要删除站点【${site.nickname}】吗？<br/>可以通过修改 config.json 然后重启恢复。`,
  })
    .then(async () => {
      try {
        const response = await axios.post(`${API_BASE_URL}/sites/delete`, { id: site.id })
        if (response.data.success) {
          ElMessage.success('站点已删除。')
          await fetchSites()
        } else {
          ElMessage.error(response.data.message || '删除失败！')
        }
      } catch (error: unknown) {
        const msg = axios.isAxiosError(error)
          ? ((error.response?.data as { message?: string } | undefined)?.message || error.message)
          : error instanceof Error
            ? error.message
            : '删除请求失败。'
        ElMessage.error(msg)
      }
    })
    .catch(() => {
      ElMessage.info('操作已取消。')
    })
}

// [移除] 不再需要独立的 saveCookieCloudSettings 和 syncFromCookieCloud 方法
</script>

<style scoped>
.cookie-view-container {
  display: grid;
  min-width: 0;
  gap: 14px;
}

.sites-settings-heading,
.settings-footer,
.pagination-container {
  display: flex;
  min-width: 0;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.sites-settings-heading .search-input {
  width: min(100%, 290px);
}

.cookie-controls {
  min-width: 0;
  padding: 14px 16px;
  border-radius: 12px;
}

.cookie-controls h3 {
  margin: 0 0 12px;
  color: var(--star-ink);
  font-size: 1rem;
}

.cookie-cloud-form {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 170px), 1fr));
  gap: 10px;
}

.cookie-cloud-form .el-form-item {
  min-width: 0;
  margin-bottom: 0;
}

.cookie-cloud-actions {
  grid-column: 1 / -1;
}

.cookie-cloud-actions :deep(.el-form-item__content) {
  flex-wrap: wrap;
  gap: 8px 12px;
}

.settings-view {
  min-width: 0;
  border: 1px solid var(--star-border);
  border-radius: 12px;
}

.settings-table {
  width: 100%;
  border-radius: inherit;
}

.site-row-actions {
  --settings-action-height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.site-row-actions .el-button {
  min-height: var(--settings-action-height);
  padding: 0 4px;
  border: 0;
}

.settings-footer {
  padding: 10px 12px;
  border-radius: 12px;
}

.settings-filter {
  display: flex;
  width: fit-content;
  max-width: 100%;
  min-width: 0;
}

.settings-filter :deep(.el-radio-group) {
  display: flex;
  max-width: 100%;
  flex-wrap: wrap;
  gap: 6px;
}

.settings-filter :deep(.el-radio-button__inner) {
  box-sizing: border-box;
  min-height: 32px;
  padding: 7px 12px;
  border: 1px solid var(--star-border);
  border-radius: 6px;
  line-height: 16px;
}

.pagination-container :deep(.el-pagination) {
  flex-wrap: wrap;
  gap: 6px;
}

.page-size-text,
.form-tip {
  color: var(--star-muted);
  font-size: 0.8125rem;
}

.form-tip {
  margin-top: 4px;
  line-height: 1.5;
}

.form-choice-heading {
  color: var(--star-accent);
  font-weight: 650;
}

.settings-table :deep(tr.row-config-incomplete > td.el-table__cell) {
  background-color: var(--star-danger-soft);
}

.settings-table :deep(tr.row-config-incomplete:hover > td.el-table__cell) {
  background-color: color-mix(in srgb, var(--star-danger) 17%, var(--star-surface));
}

.role-tag {
  display: inline-block;
  padding: 2px 5px;
  border: 1px solid var(--star-border);
  border-radius: 5px;
  font-size: 0.6875rem;
  font-weight: 600;
}

.role-source {
  background-color: var(--star-accent-soft);
  color: var(--star-accent);
}

.role-target {
  background-color: var(--star-success-soft);
  color: var(--star-success);
}

.role-both {
  background-color: var(--star-warning-soft);
  color: var(--star-warning);
}

@media (max-width: 600px) {
  .sites-settings-heading .search-input {
    width: 100%;
  }

  .cookie-controls {
    padding-inline: 12px;
  }

  .settings-table :deep(.el-table-fixed-column--right) {
    position: static !important;
  }
}
</style>
