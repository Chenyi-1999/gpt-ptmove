<template>
  <section class="downloader-page" v-loading="isLoading" aria-labelledby="downloaders-title">
    <header class="downloader-page__header star-surface">
      <h2 id="downloaders-title" class="settings-section-title">下载器</h2>
      <div class="top-actions">
        <el-button @click="addDownloader" :icon="Plus" :disabled="isLoading || isSaving">
          添加下载器
        </el-button>
        <el-button type="primary" @click="saveSettings" :loading="isSaving" :icon="Select">
          保存所有设置
        </el-button>
      </div>
    </header>
    <el-empty v-if="!isLoading && !settings.downloaders.length" description="还没有下载器" />
    <div class="downloader-grid">
      <el-card
        v-for="downloader in settings.downloaders"
        :key="downloader.id"
        :class="{ 'downloader-card--expanded': expandedDownloaderIds.has(downloader.id) }"
        class="downloader-card glass-card glass-rounded glass-transparent-header glass-transparent-body"
      >
        <template #header>
          <div class="card-header">
            <div class="downloader-heading">
              <h3>{{ downloader.name || '新下载器' }}</h3>
              <div class="downloader-meta">
                <span v-if="downloader.type">{{ downloaderTypeLabel(downloader.type) }}</span>
                <span class="downloader-location">{{
                  downloader.use_proxy ? '远程' : '本地'
                }}</span>
                <span>{{ downloader.use_proxy ? '远程媒体' : '直连' }}</span>
                <span
                  class="downloader-config-state"
                  :class="`downloader-config-state--${downloaderSummaries.get(downloader.id)?.state}`"
                  :title="downloaderSummaries.get(downloader.id)?.issue || '配置字段已齐'"
                >
                  {{ downloaderSummaries.get(downloader.id)?.label }}
                </span>
                <span
                  class="downloader-status"
                  :class="{ 'downloader-status--enabled': downloader.enabled }"
                >
                  {{ downloader.enabled ? '已启用' : '已禁用' }}
                </span>
                <span
                  v-if="
                    draftDownloaderIds.has(downloader.id) &&
                    downloaderSummaries.get(downloader.id)?.state === 'missing'
                  "
                >
                  未保存
                </span>
              </div>
            </div>
            <el-button
              class="downloader-disclosure"
              :icon="expandedDownloaderIds.has(downloader.id) ? ArrowUp : EditPen"
              :aria-expanded="expandedDownloaderIds.has(downloader.id)"
              :aria-controls="downloaderEditorId(downloader.id)"
              :aria-label="`${expandedDownloaderIds.has(downloader.id) ? '收起' : '编辑'} ${downloader.name || '新下载器'}`"
              :disabled="isLoading || isSaving"
              @click="toggleDownloader(downloader.id)"
            >
              {{ expandedDownloaderIds.has(downloader.id) ? '收起' : '编辑' }}
            </el-button>
          </div>
        </template>
        <div :id="downloaderEditorId(downloader.id)" class="downloader-editor">
          <template v-if="expandedDownloaderIds.has(downloader.id)">
            <div class="editor-controls">
              <div class="downloader-color-controls">
                <el-popover
                  trigger="click"
                  placement="bottom-start"
                  popper-class="downloader-color-popover"
                >
                  <template #reference>
                    <button
                      type="button"
                      class="downloader-color-trigger"
                      :style="{ backgroundColor: downloader.color }"
                      aria-label="选择下载器颜色"
                      :disabled="isLoading || isSaving"
                    />
                  </template>
                  <el-color-picker-panel
                    v-model="downloader.color"
                    :predefine="predefinedDownloaderColors"
                    :border="false"
                    :validate-event="false"
                  >
                    <template #footer>
                      <el-button
                        size="small"
                        text
                        class="downloader-color-random-btn"
                        :icon="Refresh"
                        @click="downloader.color = randomDownloaderColor()"
                      >
                        随机
                      </el-button>
                    </template>
                  </el-color-picker-panel>
                </el-popover>
              </div>
              <el-button
                :type="
                  connectionTestResults[downloader.id] === 'success'
                    ? 'success'
                    : connectionTestResults[downloader.id] === 'error'
                      ? 'danger'
                      : 'info'
                "
                :plain="!connectionTestResults[downloader.id]"
                @click="testConnection(downloader)"
                :loading="testingConnectionId === downloader.id"
                :disabled="isLoading || isSaving"
                :icon="Link"
              >
                测试
              </el-button>
              <el-button
                :icon="FolderOpened"
                :disabled="isLoading || isSaving"
                @click="openPathMappingDialog(downloader)"
              >
                路径映射
              </el-button>
              <el-switch
                v-model="downloader.enabled"
                :disabled="isLoading || isSaving"
                :aria-label="downloader.name ? '启用 ' + downloader.name : '启用下载器'"
              />

              <el-button
                v-if="draftDownloaderIds.has(downloader.id)"
                class="delete-draft-button"
                plain
                type="danger"
                :icon="Delete"
                :disabled="isLoading || isSaving"
                aria-label="移除未保存的下载器"
                title="移除未保存的下载器"
                @click="deleteDownloader(downloader.id)"
              />
              <HoldButton
                v-else
                compact
                appearance="toolbar"
                :label="downloader.name ? '删除 ' + downloader.name : '删除下载器'"
                :disabled="isLoading || isSaving"
                @hold="confirmDeleteDownloader(downloader.id)"
              />
            </div>
            <p v-if="downloaderSummaries.get(downloader.id)?.issue" class="downloader-config-hint">
              {{ downloaderSummaries.get(downloader.id)?.issue }}
            </p>
            <el-form :model="downloader" label-position="top" :disabled="isLoading || isSaving">
              <el-form-item label="名称">
                <div class="name-and-client-row">
                  <el-input
                    v-model="downloader.name"
                    placeholder="例如：家庭服务器 qB"
                    class="name-input"
                    @input="resetConnectionStatus(downloader.id)"
                  ></el-input>
                  <el-select
                    v-model="downloader.type"
                    placeholder="请选择类型"
                    aria-label="下载器类型"
                    class="client-type-select"
                    @change="resetConnectionStatus(downloader.id)"
                  >
                    <el-option label="qBittorrent" value="qbittorrent"></el-option>
                    <el-option label="Transmission" value="transmission"></el-option>
                  </el-select>
                </div>
              </el-form-item>
              <el-form-item label="连接方式">
                <div class="proxy-settings-row">
                  <el-switch
                    v-model="downloader.use_proxy"
                    inline-prompt
                    active-text="远程"
                    inactive-text="直连"
                    :aria-label="
                      '连接方式：' + (downloader.use_proxy ? '远程截图与 MediaInfo' : '直连下载器')
                    "
                    @change="onConnectionModeChange(downloader)"
                  />
                  <strong class="connection-mode-title">
                    {{ downloader.use_proxy ? '远程截图与 MediaInfo' : '直连下载器' }}
                  </strong>
                  <span class="credential-hint connection-mode-hint">
                    {{
                      downloader.use_proxy
                        ? '种子列表和流量继续走下载器 API，截图和 MediaInfo 由远程媒体程序处理。'
                        : 'STARPT 直接连接下载器 API，有可读媒体目录时在本机处理视频。'
                    }}
                  </span>
                </div>
              </el-form-item>
              <el-form-item v-if="downloader.use_proxy" label="媒体代理端口">
                <div class="proxy-settings-row">
                  <el-input
                    v-model="downloader.proxy_port"
                    type="number"
                    placeholder="19090"
                    class="proxy-port-input"
                    :min="1"
                    :max="65535"
                    aria-label="媒体代理端口"
                    @input="resetConnectionStatus(downloader.id)"
                  />
                  <span class="credential-hint connection-mode-hint">
                    直接连接远程服务器或 NAS 上的媒体代理时，填写代理的服务端口（默认 19090）。
                  </span>
                  <span class="credential-hint connection-mode-hint">
                    远程客户端通过 HTTPS 主动领单时，主机地址填写领单服务（Hub）的内网 API
                    地址（默认端口 28380），媒体代理端口填 29090。HTTPS 领单端口（默认
                    28443）在远程客户端设置中填写。
                  </span>
                </div>
              </el-form-item>
              <el-form-item label="出种限速">
                <el-tooltip
                  content="开启后，此下载器将参与基于站点分享率阈值的出种限速"
                  placement="top"
                  :hide-after="0"
                >
                  <span class="ratio-limiter-label">
                    <el-switch
                      v-model="downloader.enable_ratio_limiter"
                      inline-prompt
                      active-text="开"
                      inactive-text="关"
                      aria-label="出种限速"
                    />
                  </span>
                </el-tooltip>
              </el-form-item>
              <el-form-item v-if="downloader.use_proxy" label="媒体代理 Token">
                <div class="api-token-field">
                  <el-input
                    v-model="downloader.proxy_token"
                    type="password"
                    show-password
                    autocomplete="new-password"
                    placeholder="留空保留已配置 Token，新增至少 32 字符"
                    @input="onProxyTokenInput(downloader)"
                  />
                  <span class="api-token-status">{{
                    downloader.clear_proxy_token
                      ? '待清除'
                      : downloader.proxy_token_configured && !downloader.proxy_token
                        ? '已配置'
                        : downloader.proxy_token
                          ? '待保存'
                          : '未配置'
                  }}</span>
                  <el-checkbox
                    v-if="downloader.proxy_token_configured"
                    v-model="downloader.clear_proxy_token"
                    @change="onClearProxyTokenChange(downloader)"
                    >清除媒体代理 Token</el-checkbox
                  >
                  <span class="credential-hint"
                    >通过 HTTPS 领单时填写 Hub 的 controller.token。直接连接媒体代理时填写该程序的
                    PTMOVE_PROXY_TOKEN。</span
                  >
                </div>
              </el-form-item>
              <el-form-item label="主机地址">
                <el-input
                  v-model="downloader.host"
                  :placeholder="
                    downloader.type === 'transmission'
                      ? '例如：192.168.1.10:9091 或 http://192.168.1.10:9091'
                      : '例如：192.168.1.10:8080'
                  "
                  @input="resetConnectionStatus(downloader.id)"
                ></el-input>
                <span class="credential-hint connection-mode-hint">
                  {{
                    downloader.use_proxy
                      ? '填写 STARPT 能访问的下载器 API 地址（含端口）。媒体代理使用同一个主机名和上面的独立端口。HTTPS 领单的 API 地址示例为 ptmove-pull-hub:28380。'
                      : '填写 STARPT 能访问的下载器 WebUI 或 RPC 地址及端口。'
                  }}
                </span>
              </el-form-item>
              <el-form-item label="用户名">
                <el-input
                  v-model="downloader.username"
                  placeholder="登录用户名"
                  @input="resetConnectionStatus(downloader.id)"
                ></el-input>
              </el-form-item>
              <el-form-item label="密码">
                <el-input
                  v-model="downloader.password"
                  type="password"
                  show-password
                  placeholder="登录密码（未修改则留空）"
                  @input="resetConnectionStatus(downloader.id)"
                ></el-input>
              </el-form-item>
              <el-form-item v-if="downloader.type === 'qbittorrent'" label="API Token">
                <div class="api-token-field">
                  <el-input
                    v-model="downloader.api_key"
                    type="password"
                    show-password
                    autocomplete="new-password"
                    placeholder="留空保留已配置 Token"
                    @input="onApiKeyInput(downloader)"
                  />
                  <span class="api-token-status">{{
                    downloader.clear_api_key
                      ? '待清除'
                      : downloader.api_key_configured && !downloader.api_key
                        ? '已配置'
                        : downloader.api_key
                          ? '待保存'
                          : '未配置'
                  }}</span>
                  <el-checkbox
                    v-if="downloader.api_key_configured"
                    v-model="downloader.clear_api_key"
                    @change="onClearApiKeyChange(downloader)"
                  >
                    明确清除 Token
                  </el-checkbox>
                </div>
              </el-form-item>
              <el-form-item v-if="downloader.type === 'qbittorrent'" label="认证提示">
                <span class="credential-hint"
                  >已配置 API Token 或填写新 Token 时，用户名和密码可留空；留空 Token
                  会保留当前凭据。</span
                >
              </el-form-item>
            </el-form>
          </template>
        </div>
      </el-card>
    </div>
  </section>

  <!-- 路径映射对话框 -->
  <el-dialog
    v-model="pathMappingDialogVisible"
    :title="`路径映射配置 - ${currentDownloader?.name || ''}`"
    width="720px"
    class="settings-dialog"
    :close-on-click-modal="false"
  >
    <div class="path-mapping-container">
      <el-alert title="路径映射说明" type="info" :closable="false">
        <p>配置下载器路径与 STARPT 容器内路径的映射关系。</p>
        <p><strong>下载器路径：</strong>下载器中显示的种子保存路径</p>
        <p><strong>视频文件路径：</strong>挂载到 STARPT 容器内的路径或下载器本地路径</p>
      </el-alert>

      <div class="mapping-list">
        <div v-for="(mapping, index) in currentPathMappings" :key="index" class="mapping-item">
          <el-input v-model="mapping.remote" placeholder="例如：/downloads" class="mapping-input">
            <template #prepend>下载器路径</template>
          </el-input>
          <el-input v-model="mapping.local" placeholder="例如：/app/data/qb1" class="mapping-input">
            <template #prepend>视频文件路径</template>
          </el-input>
          <el-button
            class="mapping-item__remove"
            plain
            type="danger"
            :icon="Delete"
            :disabled="isSaving"
            aria-label="删除此映射规则"
            title="删除此映射规则"
            @click="deletePathMapping(mapping)"
          />
        </div>
      </div>

      <el-button type="primary" :icon="Plus" class="add-mapping" @click="addPathMapping">
        添加映射规则
      </el-button>
    </div>

    <template #footer>
      <el-button @click="pathMappingDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="isSaving" @click="savePathMappings">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessageBox } from 'element-plus'
import {
  ArrowUp,
  Delete,
  EditPen,
  FolderOpened,
  Link,
  Plus,
  Refresh,
  Select,
} from '@element-plus/icons-vue'
import HoldButton from '@/components/HoldButton.vue'
import { useTorrentsViewState } from '@/stores/torrentsViewState'
import { ElMessage } from '@/utils/uiNotify'

type ConnectionTestStatus = 'success' | 'error'

type PathMapping = {
  remote: string
  local: string
}

type DownloaderConfig = {
  id: string
  enabled: boolean
  name: string
  type: string
  host: string
  username: string
  password: string
  password_configured: boolean
  api_key: string
  api_key_configured: boolean
  clear_api_key: boolean
  use_proxy: boolean
  proxy_token: string
  proxy_token_configured: boolean
  clear_proxy_token: boolean
  proxy_media_only: boolean
  proxy_port: number
  color: string
  path_mappings: PathMapping[]
  enable_ratio_limiter: boolean
  [key: string]: unknown
}

type SettingsState = {
  downloaders: DownloaderConfig[]
  realtime_speed_enabled: boolean
  [key: string]: unknown
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null

const settings = ref<SettingsState>({
  downloaders: [],
  realtime_speed_enabled: true,
})
const isLoading = ref(true)
const isSaving = ref(false)
const draftDownloaderIds = ref(new Set<string>())
const expandedDownloaderIds = ref(new Set<string>())
let nextDraftSequence = 0

const downloaderEditorId = (id: string) => `downloader-editor-${encodeURIComponent(id)}`

const toggleDownloader = (id: string) => {
  if (expandedDownloaderIds.value.has(id)) expandedDownloaderIds.value.delete(id)
  else expandedDownloaderIds.value.add(id)
}
const testingConnectionId = ref<string | null>(null)
const connectionTestResults = ref<Record<string, ConnectionTestStatus | undefined>>({})
const API_BASE_URL = '/api'
const torrentsViewState = useTorrentsViewState()

function downloaderTypeLabel(type: string) {
  const normalized = type.trim().toLowerCase()
  if (normalized === 'qbittorrent') return 'qBittorrent'
  if (normalized === 'transmission') return 'Transmission'
  return normalized
}

function downloaderHostname(host: string) {
  const address = host.trim()
  if (!address) return ''
  try {
    const url = new URL(address.includes('://') ? address : `http://${address}`)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.hostname : ''
  } catch {
    return ''
  }
}

function downloaderConfigIssue(downloader: DownloaderConfig, hostname: string) {
  const kind = downloader.type.trim().toLowerCase()
  if (kind !== 'qbittorrent' && kind !== 'transmission') return '请选择支持的下载器类型。'
  if (!hostname) return '请填写有效的下载器主机地址。'
  const hasApiKey =
    Boolean(downloader.api_key.trim()) ||
    (downloader.api_key_configured && !downloader.clear_api_key)
  const hasPassword = Boolean(downloader.password.trim()) || downloader.password_configured
  if (kind === 'qbittorrent' && !hasApiKey && !(downloader.username.trim() && hasPassword))
    return '请填写 qBittorrent API Token，或填写用户名和密码。'
  if (downloader.use_proxy) {
    const port = Number(downloader.proxy_port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) return '请填写有效的媒体代理端口。'
    const token = downloader.proxy_token.trim()
    const hasToken = token
      ? token.length >= 32
      : downloader.proxy_token_configured && !downloader.clear_proxy_token
    if (!hasToken) return '请填写至少 32 字符的媒体代理 Token。'
  }
  return ''
}

const downloaderSummaries = computed(() => {
  return new Map(
    settings.value.downloaders.map((downloader) => {
      const hostname = downloaderHostname(downloader.host)
      const issue = downloaderConfigIssue(downloader, hostname)
      const unsaved = draftDownloaderIds.value.has(downloader.id)
      return [
        downloader.id,
        {
          issue,
          state: issue ? 'missing' : unsaved ? 'draft' : 'ready',
          label: issue ? '待配置' : unsaved ? '待保存' : '已配置',
        },
      ]
    }),
  )
})

const predefinedDownloaderColors = [
  '#409EFF',
  '#67C23A',
  '#E6A23C',
  '#909399',
  '#00C9A7',
  '#6C5CE7',
  '#0984E3',
  '#00B894',
  '#FDCB6E',
  '#A29BFE',
  '#74B9FF',
  '#55EFC4',
]

const clampInt = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))

const hslToHex = (h: number, s: number, l: number) => {
  const sat = clampInt(Number(s), 0, 100) / 100
  const lig = clampInt(Number(l), 0, 100) / 100

  const c = (1 - Math.abs(2 * lig - 1)) * sat
  const hp = (((Number(h) % 360) + 360) % 360) / 60
  const x = c * (1 - Math.abs((hp % 2) - 1))

  let r1 = 0
  let g1 = 0
  let b1 = 0
  if (hp >= 0 && hp < 1) [r1, g1, b1] = [c, x, 0]
  else if (hp >= 1 && hp < 2) [r1, g1, b1] = [x, c, 0]
  else if (hp >= 2 && hp < 3) [r1, g1, b1] = [0, c, x]
  else if (hp >= 3 && hp < 4) [r1, g1, b1] = [0, x, c]
  else if (hp >= 4 && hp < 5) [r1, g1, b1] = [x, 0, c]
  else if (hp >= 5 && hp < 6) [r1, g1, b1] = [c, 0, x]

  const m = lig - c / 2
  const toHex = (v: number) =>
    Math.round((v + m) * 255)
      .toString(16)
      .padStart(2, '0')
  return `#${toHex(r1)}${toHex(g1)}${toHex(b1)}`
}

const deriveDownloaderColor = (seed: unknown) => {
  const value = String(seed || '').trim()
  if (!value) return predefinedDownloaderColors[0]
  let hash = 0
  for (let i = 0; i < value.length; i++) {
    hash = value.charCodeAt(i) + ((hash << 5) - hash)
  }
  // 避免纯红色：hue 取 20~339
  const hue = (Math.abs(hash) % 320) + 20
  return hslToHex(hue, 72, 45)
}

const randomDownloaderColor = () => {
  if (Math.random() < 0.35) {
    return predefinedDownloaderColors[Math.floor(Math.random() * predefinedDownloaderColors.length)]
  }
  const hue = Math.floor(Math.random() * 320) + 20
  return hslToHex(hue, 72, 45)
}

// 路径映射相关状态
const pathMappingDialogVisible = ref(false)
const currentDownloader = ref<DownloaderConfig | null>(null)
const currentPathMappings = ref<PathMapping[]>([])

onMounted(() => {
  fetchSettings()
})

const fetchSettings = async () => {
  isLoading.value = true
  try {
    const response = await axios.get<Record<string, unknown>>(`${API_BASE_URL}/settings`)
    const raw = isRecord(response.data) ? response.data : {}
    const rawDownloaders = Array.isArray(raw.downloaders) ? raw.downloaders : []

    const downloaders = rawDownloaders.map((item) => {
      const record = isRecord(item) ? item : {}
      const id = String(record.id || `client_${Date.now()}_${Math.random()}`)
      const pathMappings = Array.isArray(record.path_mappings)
        ? record.path_mappings
            .map((mapping) => {
              if (!isRecord(mapping)) return null
              return {
                remote: typeof mapping.remote === 'string' ? mapping.remote : '',
                local: typeof mapping.local === 'string' ? mapping.local : '',
              }
            })
            .filter((mapping): mapping is PathMapping => Boolean(mapping))
        : []

      return {
        ...record,
        id,
        enabled: typeof record.enabled === 'boolean' ? record.enabled : true,
        name: typeof record.name === 'string' ? record.name : '新下载器',
        type: typeof record.type === 'string' ? record.type : 'qbittorrent',
        host: typeof record.host === 'string' ? record.host : '',
        username: typeof record.username === 'string' ? record.username : '',
        password: typeof record.password === 'string' ? record.password : '',
        password_configured: record.password_configured === true,
        api_key: '',
        api_key_configured: record.api_key_configured === true,
        clear_api_key: false,
        proxy_token: '',
        proxy_token_configured: record.proxy_token_configured === true,
        clear_proxy_token: false,
        proxy_media_only: record.proxy_media_only === true,
        use_proxy: typeof record.use_proxy === 'boolean' ? record.use_proxy : false,
        proxy_port:
          typeof record.proxy_port === 'number'
            ? record.proxy_port
            : Number(record.proxy_port) || 19090,
        color:
          typeof record.color === 'string' && record.color.trim()
            ? record.color
            : deriveDownloaderColor(id),
        path_mappings: pathMappings,
        enable_ratio_limiter:
          typeof record.enable_ratio_limiter === 'boolean' ? record.enable_ratio_limiter : false,
      } satisfies DownloaderConfig
    })

    settings.value = {
      ...raw,
      downloaders,
      realtime_speed_enabled:
        typeof raw.realtime_speed_enabled === 'boolean' ? raw.realtime_speed_enabled : true,
    }
    draftDownloaderIds.value.clear()
  } catch (error) {
    ElMessage.error('加载设置失败！')
    console.error(error)
  } finally {
    isLoading.value = false
  }
}

const settingsPayload = (editedDownloader?: DownloaderConfig) => ({
  ...settings.value,
  downloaders: settings.value.downloaders.map((item) => {
    const downloader = editedDownloader?.id === item.id ? editedDownloader : item
    const payload = Object.fromEntries(
      Object.entries(downloader).filter(
        ([key]) =>
          !['password_configured', 'api_key_configured', 'proxy_token_configured'].includes(key),
      ),
    )
    if (downloader.use_proxy) payload.proxy_media_only = true
    return payload
  }),
})

const persistSettings = async (payload = settingsPayload()) => {
  const savedIds = settings.value.downloaders.map(({ id }) => id)
  await axios.post(`${API_BASE_URL}/settings`, payload)
  for (const id of savedIds) draftDownloaderIds.value.delete(id)
}

const saveSettings = async () => {
  if (isSaving.value) return
  isSaving.value = true
  try {
    await persistSettings()
    expandedDownloaderIds.value.clear()
    ElMessage.success('设置已成功保存并应用！')
    // 关键：删除/停用下载器后，TorrentsView 的下载器筛选列表使用缓存，
    // 不强制刷新会导致“已删除下载器仍可筛选”的错觉。
    await torrentsViewState.fetchDownloadersList(true)
    await fetchSettings()
  } catch (error) {
    ElMessage.error('保存设置失败！')
    console.error(error)
  } finally {
    isSaving.value = false
  }
}

const addDownloader = () => {
  const id = `new_${Date.now()}_${++nextDraftSequence}`
  draftDownloaderIds.value.add(id)
  expandedDownloaderIds.value.add(id)
  settings.value.downloaders.push({
    id,
    enabled: true,
    name: '新下载器',
    type: 'qbittorrent',
    host: '',
    username: '',
    password: '',
    password_configured: false,
    api_key: '',
    api_key_configured: false,
    clear_api_key: false,
    proxy_token: '',
    proxy_token_configured: false,
    clear_proxy_token: false,
    proxy_media_only: true,
    use_proxy: false,
    proxy_port: 19090,
    color: deriveDownloaderColor(id),
    path_mappings: [], // 初始化空的路径映射数组
    enable_ratio_limiter: false, // 默认关闭出种限速
  })
  void nextTick(() => {
    document
      .getElementById(downloaderEditorId(id))
      ?.querySelector<HTMLInputElement>('.name-input input')
      ?.focus()
  })
}

const confirmDeleteDownloader = (downloaderId: string) => {
  ElMessageBox.confirm('保存设置后会移除该下载器连接。', '删除下载器', {
    confirmButtonText: '确定删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
    .then(() => {
      deleteDownloader(downloaderId)
      ElMessage({
        type: 'success',
        message: '下载器已删除（尚未保存）。',
      })
    })
    .catch(() => {})
}

const deleteDownloader = (downloaderId: string) => {
  settings.value.downloaders = settings.value.downloaders.filter((d) => d.id !== downloaderId)
  draftDownloaderIds.value.delete(downloaderId)
  expandedDownloaderIds.value.delete(downloaderId)
}

const resetConnectionStatus = (downloaderId: string) => {
  if (connectionTestResults.value[downloaderId]) {
    delete connectionTestResults.value[downloaderId]
  }
}

const onConnectionModeChange = (downloader: DownloaderConfig) => {
  if (downloader.use_proxy) downloader.proxy_media_only = true
  resetConnectionStatus(downloader.id)
}

const testConnection = async (downloader: DownloaderConfig) => {
  resetConnectionStatus(downloader.id)
  const hasToken =
    Boolean(downloader.api_key.trim()) ||
    (downloader.api_key_configured && !downloader.clear_api_key)
  if (
    downloader.type === 'qbittorrent' &&
    !hasToken &&
    (!downloader.username.trim() || !downloader.password.trim())
  ) {
    ElMessage.warning('请填写用户名和密码，或提供 qBittorrent API Token。')
    return
  }
  testingConnectionId.value = downloader.id
  try {
    const testConfig = Object.fromEntries(
      Object.entries(downloader).filter(
        ([key]) =>
          !['password_configured', 'api_key_configured', 'proxy_token_configured'].includes(key),
      ),
    )
    if (downloader.use_proxy) testConfig.proxy_media_only = true
    const response = await axios.post(`${API_BASE_URL}/test_connection`, testConfig)
    const result = response.data as { success?: boolean; message?: string }
    if (result.success) {
      ElMessage.success(result.message || '连接成功')
      connectionTestResults.value[downloader.id] = 'success'
    } else {
      ElMessage.error(result.message || '连接失败')
      connectionTestResults.value[downloader.id] = 'error'
    }
  } catch (error) {
    ElMessage.error('测试连接请求失败，请检查网络或后端服务。')
    console.error('Test connection error:', error)
    connectionTestResults.value[downloader.id] = 'error'
  } finally {
    testingConnectionId.value = null
  }
}

const onApiKeyInput = (downloader: DownloaderConfig) => {
  downloader.clear_api_key = false
  resetConnectionStatus(downloader.id)
}

const onProxyTokenInput = (downloader: DownloaderConfig) => {
  downloader.clear_proxy_token = false
  resetConnectionStatus(downloader.id)
}

const onClearProxyTokenChange = (downloader: DownloaderConfig) => {
  if (downloader.clear_proxy_token) downloader.proxy_token = ''
  resetConnectionStatus(downloader.id)
}

const onClearApiKeyChange = (downloader: DownloaderConfig) => {
  if (downloader.clear_api_key) downloader.api_key = ''
  resetConnectionStatus(downloader.id)
}

// 路径映射相关函数
const openPathMappingDialog = (downloader: DownloaderConfig) => {
  currentDownloader.value = downloader
  // 初始化路径映射数据，如果不存在则创建空数组
  if (!downloader.path_mappings || !Array.isArray(downloader.path_mappings)) {
    downloader.path_mappings = []
  }
  // 拷贝映射数据，避免直接修改
  currentPathMappings.value = downloader.path_mappings.map((mapping) => ({
    remote: mapping.remote,
    local: mapping.local,
  }))
  pathMappingDialogVisible.value = true
}

const addPathMapping = () => {
  currentPathMappings.value.push({
    remote: '',
    local: '',
  })
}

const deletePathMapping = (mapping: (typeof currentPathMappings.value)[number]) => {
  currentPathMappings.value = currentPathMappings.value.filter((item) => item !== mapping)
}

const savePathMappings = async () => {
  if (isSaving.value) return
  // 过滤掉空的映射规则
  const validMappings = currentPathMappings.value.filter(
    (mapping) => mapping.remote.trim() !== '' && mapping.local.trim() !== '',
  )
  const downloader = currentDownloader.value
  if (!downloader) {
    ElMessage.error('未选择要保存的下载器')
    return
  }

  isSaving.value = true
  try {
    await persistSettings(settingsPayload({ ...downloader, path_mappings: validMappings }))
    downloader.path_mappings = validMappings
    pathMappingDialogVisible.value = false
    ElMessage.success('路径映射已保存！')
    await fetchSettings()
  } catch (error) {
    ElMessage.error('保存路径映射失败！')
    console.error(error)
  } finally {
    isSaving.value = false
  }
}
</script>

<style scoped>
.downloader-page {
  display: grid;
  min-width: 0;
  gap: 16px;
}

.downloader-page__header,
.top-actions,
.card-header,
.editor-controls {
  display: flex;
  min-width: 0;
  align-items: center;
}

.downloader-page__header {
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
  padding: 14px clamp(13px, 2vw, 20px);
  border-radius: 12px;
}

.top-actions,
.editor-controls {
  flex-wrap: wrap;
  gap: 8px;
}

.top-actions > .el-button,
.editor-controls > .el-button {
  min-height: 36px;
  margin-left: 0;
}

.downloader-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: start;
  gap: 14px;
}

.downloader-card {
  min-width: 0;
}

.downloader-card :deep(.el-card__header) {
  padding: 14px 17px;
}

.downloader-card :deep(.el-card__body) {
  padding: 15px 17px 0;
}

.card-header {
  justify-content: space-between;
  gap: 10px 14px;
}

.downloader-disclosure {
  flex: none;
  min-height: 36px;
}

.downloader-card:not(.downloader-card--expanded) :deep(.el-card__header) {
  border-bottom: 0;
}

.downloader-card:not(.downloader-card--expanded) :deep(.el-card__body) {
  display: none;
}

.downloader-editor {
  max-width: 820px;
}

.downloader-heading {
  min-width: 0;
  overflow-wrap: anywhere;
}

.downloader-heading h3 {
  margin: 0;
  color: var(--star-ink);
  font-size: 1rem;
  font-weight: 650;
  line-height: 1.3;
}

.downloader-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 4px;
  color: var(--star-muted);
  font-size: 0.75rem;
}

.downloader-config-state {
  font-weight: 600;
}

.downloader-config-state--ready {
  color: var(--el-color-success);
}

.downloader-config-state--missing,
.downloader-config-state--draft,
.downloader-config-hint {
  color: var(--el-color-warning);
}

.downloader-config-hint {
  margin: -4px 0 16px;
  font-size: 0.8125rem;
}

.downloader-status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.downloader-status::before {
  width: 6px;
  height: 6px;
  flex: none;
  border-radius: 50%;
  background-color: currentColor;
  content: '';
}

.downloader-status--enabled {
  color: var(--el-color-success);
}

.editor-controls {
  --settings-action-height: 36px;
  max-width: 100%;
  margin-bottom: 16px;
}

.downloader-color-controls {
  display: flex;
  align-items: center;
}

.editor-controls > .el-button,
.editor-controls :deep(.el-switch) {
  height: var(--settings-action-height);
  min-height: var(--settings-action-height);
}

.editor-controls > .el-button {
  padding-inline: 11px;
}

.downloader-color-trigger {
  display: grid;
  box-sizing: border-box;
  width: var(--settings-action-height);
  height: var(--settings-action-height);
  padding: 7px;
  place-items: center;
  border: 1px solid var(--star-border-strong);
  border-radius: 8px;
  background-clip: content-box;
  cursor: pointer;
}

.downloader-color-trigger:focus-visible {
  outline: 2px solid var(--star-accent);
  outline-offset: 2px;
}

.downloader-color-random-btn {
  margin-left: 8px;
}

.name-and-client-row,
.proxy-settings-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  width: 100%;
  gap: 8px;
}

.name-input {
  flex: 2 1 165px;
  min-width: 0;
}

.client-type-select {
  flex: 1 1 128px;
  min-width: 0;
}

.proxy-port-input {
  flex: 1 1 140px;
  min-width: 0;
}

.proxy-port-input :deep(.el-input-group__append) {
  padding: 0 7px;
}

.input-append-wrapper {
  display: flex;
  align-items: center;
}

.ratio-limiter-label,
.api-token-field {
  display: flex;
  min-width: 0;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 10px;
}

.ratio-limiter-label {
  color: var(--star-muted);
  font-size: 0.8125rem;
}

.api-token-field {
  width: 100%;
}

.api-token-field :deep(.el-input) {
  flex: 1 1 200px;
  min-width: 0;
}

.api-token-field :deep(.el-checkbox) {
  height: auto;
  max-width: 100%;
  margin-right: 0;
  white-space: normal;
}

.api-token-status,
.credential-hint {
  color: var(--star-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.connection-mode-title {
  color: var(--star-text);
  font-size: 0.875rem;
  font-weight: 600;
}

.credential-hint {
  flex-basis: 100%;
}

.path-mapping-container {
  display: grid;
  gap: 14px;
}

.mapping-list {
  display: grid;
  gap: 10px;
}

.mapping-item {
  --settings-action-height: var(--el-component-size);
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr)) auto;
  align-items: center;
  gap: 8px;
}

.mapping-input {
  min-width: 0;
}

.editor-controls > .delete-draft-button,
.mapping-item__remove {
  width: var(--settings-action-height);
  height: var(--settings-action-height);
  padding: 0;
}

.mapping-input :deep(.el-input-group__prepend) {
  padding: 0 9px;
}

.add-mapping {
  justify-self: start;
}

@media (max-width: 600px) {
  .top-actions {
    width: 100%;
  }

  .top-actions > .el-button {
    min-height: 44px;
    flex: 1 1 auto;
  }

  .downloader-disclosure {
    min-height: 44px;
  }

  .downloader-card :deep(.el-card__header),
  .downloader-card :deep(.el-card__body) {
    padding-inline: 12px;
  }

  .mapping-item {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .mapping-input {
    grid-column: 1;
  }

  .mapping-item__remove {
    grid-column: 2;
    grid-row: 1 / span 2;
  }
}
</style>
