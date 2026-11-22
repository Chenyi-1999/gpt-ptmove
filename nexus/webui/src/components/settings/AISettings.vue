<template>
  <section class="settings-page" v-loading="loading">
    <header class="page-heading">
      <div>
        <h2 class="settings-section-title">API 管理</h2>
        <p>管理机器密钥、权限与有效期，并为外部客户端准备安全的接入说明。</p>
      </div>
      <el-button type="primary" @click="openCreate">创建密钥</el-button>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-alert
      title="保护机器密钥"
      description="机器密钥等同于持有者凭据。使用 HTTPS 或可信 TLS 反向代理；只授予集成需要的权限，并在不再需要时撤销。"
      type="warning"
      show-icon
      :closable="false"
    />

    <el-card shadow="never" class="connection-card">
      <template #header>
        <div class="card-heading">
          <div>
            <strong>连接信息</strong>
            <p class="muted">仅用于生成接入说明和接入包，不会向此地址发送测试请求。</p>
          </div>
        </div>
      </template>
      <div class="connection-grid">
        <div class="deployment-info">
          <div class="info-row">
            <span>当前部署</span><code>{{ currentDeployment }}</code>
          </div>
          <div class="info-row">
            <span>当前 API 根地址</span><code>{{ currentApiRoot }}</code>
          </div>
          <div class="info-row">
            <span>OpenAPI 文档</span><code>{{ openApiUrl }}</code>
          </div>
          <div class="docs-action">
            <el-button :loading="downloadingOpenApi" @click="downloadOpenApi">
              下载机器 API OpenAPI JSON
            </el-button>
            <span class="muted">通过当前管理员会话下载；文档的认证方式是机器 Bearer 密钥。</span>
          </div>
        </div>
        <el-form label-position="top" class="api-base-form">
          <el-form-item label="接入说明使用的 API 根地址">
            <el-input
              v-model="apiBaseInput"
              autocomplete="url"
              placeholder="https://starpt.example.com"
            >
              <template #append>
                <el-button @click="resetApiBase">恢复当前地址</el-button>
              </template>
            </el-input>
          </el-form-item>
          <el-alert
            v-if="apiBaseError"
            :title="apiBaseError"
            type="error"
            :closable="false"
            show-icon
          />
          <div v-else class="base-note">
            <el-tag :type="apiBaseIsSameOrigin ? 'success' : 'warning'" size="small">
              {{ apiBaseIsSameOrigin ? '与当前部署同源' : '远程地址，仅用于导出' }}
            </el-tag>
            <span class="muted">路径会自动规范化；接口示例始终包含完整的 /api/v1 路径。</span>
          </div>
        </el-form>
      </div>
    </el-card>

    <el-card shadow="never" class="probe-card">
      <template #header>
        <div class="card-heading">
          <div>
            <strong>测试连接</strong>
            <p class="muted">使用机器密钥进行只读身份校验，不会访问可变更数据或执行任务。</p>
          </div>
        </div>
      </template>
      <div class="probe-controls">
        <el-input
          v-model="probeToken"
          type="password"
          show-password
          autocomplete="off"
          spellcheck="false"
          placeholder="临时粘贴机器密钥"
          aria-label="用于只读连接测试的临时机器密钥"
          @keyup.enter="testConnection"
        />
        <el-button
          :loading="testingConnection"
          :disabled="!probeToken.trim() || anyProbeTesting"
          @click="testConnection"
        >
          验证只读连接
        </el-button>
        <el-button :disabled="testingIssuedToken" @click="clearProbe">清除</el-button>
      </div>
      <el-alert v-if="probeError" :title="probeError" type="error" show-icon :closable="false" />
      <section v-if="probeResult" class="probe-result" aria-live="polite">
        <div class="result-heading">
          <strong>连接成功</strong><el-tag type="success">只读校验</el-tag>
        </div>
        <dl class="result-grid">
          <template v-if="probeResult.service"
            ><dt>服务</dt>
            <dd>{{ probeResult.service }}</dd></template
          >
          <template v-if="probeResult.apiVersion"
            ><dt>API 版本</dt>
            <dd>{{ probeResult.apiVersion }}</dd></template
          >
          <template v-if="probeResult.keyId"
            ><dt>密钥 ID</dt>
            <dd>{{ probeResult.keyId }}</dd></template
          >
          <template v-if="probeResult.keyName"
            ><dt>密钥名称</dt>
            <dd>{{ probeResult.keyName }}</dd></template
          >
          <template v-if="probeResult.scopes.length"
            ><dt>权限</dt>
            <dd>{{ probeResult.scopes.map(scopeLabel).join('、') }}</dd></template
          >
          <template v-if="probeResult.expiresAt"
            ><dt>有效期至</dt>
            <dd>{{ formatDate(probeResult.expiresAt) }}</dd></template
          >
          <template v-if="probeResult.management.length"
            ><dt>管理能力</dt>
            <dd>{{ probeResult.management.join('、') }}</dd></template
          >
          <template v-if="probeResult.idempotencyHeader"
            ><dt>幂等请求头</dt>
            <dd>{{ probeResult.idempotencyHeader }}</dd></template
          >
        </dl>
      </section>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <div class="card-heading key-heading">
          <div>
            <strong>机器密钥</strong>
            <p class="muted">密钥只在创建时显示一次。到期或撤销后无法继续调用 API。</p>
          </div>
          <el-button type="primary" @click="openCreate">创建密钥</el-button>
        </div>
      </template>
      <div class="key-summary" aria-live="polite">
        <span>全部 {{ keys.length }}</span>
        <span>有效 {{ statusCounts.active }}</span>
        <span>已到期 {{ statusCounts.expired }}</span>
        <span>已撤销 {{ statusCounts.revoked }}</span>
      </div>
      <div class="key-toolbar">
        <span class="muted">筛选状态</span>
        <el-radio-group v-model="statusFilter" size="small" aria-label="按状态筛选机器密钥">
          <el-radio-button label="all">全部</el-radio-button>
          <el-radio-button label="active">有效</el-radio-button>
          <el-radio-button label="expired">已到期</el-radio-button>
          <el-radio-button label="revoked">已撤销</el-radio-button>
        </el-radio-group>
      </div>
      <el-table :data="filteredKeys" row-key="id" empty-text="此状态下没有机器密钥">
        <el-table-column prop="name" label="名称" min-width="130" />
        <el-table-column label="权限" min-width="170">
          <template #default="scope">
            <el-tag
              v-for="permission in scope.row.scopes"
              :key="permission"
              class="scope-tag"
              :type="
                permission === 'publish' ? 'danger' : permission === 'write' ? 'warning' : 'info'
              "
              >{{ scopeLabel(permission) }}</el-tag
            >
            <span v-if="!scope.row.scopes.length" class="muted">无</span>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" min-width="155">
          <template #default="scope">{{ formatDate(scope.row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="到期时间" min-width="155">
          <template #default="scope">{{ formatDate(scope.row.expires_at) }}</template>
        </el-table-column>
        <el-table-column label="状态" min-width="95">
          <template #default="scope">
            <el-tag :type="statusType(keyStatus(scope.row))">{{
              statusLabel(keyStatus(scope.row))
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="scope">
            <el-button
              v-if="keyStatus(scope.row) !== 'revoked'"
              link
              type="danger"
              @click="revoke(scope.row)"
              >撤销</el-button
            >
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <div class="card-heading key-heading">
          <div>
            <strong>AI 接入指南</strong>
            <p class="muted">复制说明或下载不含真实凭据的 JSON 接入包。</p>
          </div>
          <div class="guide-actions">
            <el-button @click="copyGuide">复制说明</el-button>
            <el-button @click="downloadIntegrationPackage">下载 AI 接入包</el-button>
          </div>
        </div>
      </template>
      <el-input
        v-model="guide"
        type="textarea"
        :rows="21"
        readonly
        aria-label="可复制的 API 管理与 AI 接入说明"
      />
    </el-card>

    <el-dialog
      v-model="createOpen"
      title="创建机器密钥"
      width="560px"
      class="settings-dialog"
      @closed="clearCreate"
    >
      <el-form label-position="top">
        <el-form-item label="名称">
          <el-input
            v-model="name"
            maxlength="80"
            autocomplete="off"
            placeholder="例如：媒体信息读取器"
          />
        </el-form-item>
        <el-form-item label="权限预设">
          <el-radio-group v-model="permissionPreset" class="preset-list">
            <el-radio-button label="read">只读</el-radio-button>
            <el-radio-button label="manage">读取 + 写入</el-radio-button>
            <el-radio-button label="execute">读取 + 写入 + 发布</el-radio-button>
            <el-radio-button label="custom">自定义</el-radio-button>
          </el-radio-group>
          <div class="permission-summary">{{ selectedScopeSummary }}</div>
          <div v-if="permissionPreset === 'custom'" class="custom-scope-list">
            <el-checkbox v-model="customScopes.read" disabled>读取</el-checkbox>
            <el-checkbox v-model="customScopes.write">写入</el-checkbox>
            <el-checkbox v-model="customScopes.publish">发布</el-checkbox>
          </div>
          <div class="muted permission-help">
            写入与发布是独立能力。发布权限允许创建真实运行或进入下载/发布队列；运行不会被静默自动触发。仅当自动化已启用或存在待执行设置时，配置变更也需要发布权限。
          </div>
        </el-form-item>
        <el-form-item label="有效期">
          <el-radio-group v-model="days" class="expiry-presets">
            <el-radio-button :label="7">7 天</el-radio-button>
            <el-radio-button :label="30">30 天</el-radio-button>
            <el-radio-button :label="90">90 天</el-radio-button>
            <el-radio-button :label="365">365 天</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" :disabled="!name.trim()" @click="createKey"
          >创建密钥</el-button
        >
      </template>
    </el-dialog>

    <el-dialog
      v-model="tokenOpen"
      title="新机器密钥"
      width="600px"
      class="settings-dialog"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      @closed="clearIssuedToken"
    >
      <el-alert
        title="请现在复制并保存密钥。关闭窗口后将无法再次查看。"
        type="warning"
        :closable="false"
      />
      <el-input
        v-model="issuedToken"
        type="password"
        show-password
        :rows="2"
        readonly
        autocomplete="off"
        class="token-input"
        aria-label="新创建的机器密钥，仅显示一次"
      />
      <div class="issued-actions">
        <el-button type="primary" @click="copyIssuedToken">复制密钥</el-button>
        <el-button
          :loading="testingIssuedToken"
          :disabled="testingConnection || testingIssuedToken"
          @click="testIssuedToken"
          >只读测试此密钥</el-button
        >
        <el-button @click="tokenOpen = false">保存并关闭</el-button>
      </div>
      <el-alert
        v-if="issuedProbeError"
        :title="issuedProbeError"
        type="error"
        show-icon
        :closable="false"
        class="issued-probe-message"
      />
      <div v-if="issuedProbeResult" class="issued-probe-message result-heading">
        <strong>只读连接验证成功</strong
        ><el-tag type="success">{{
          issuedProbeResult.apiVersion || issuedProbeResult.service
        }}</el-tag>
      </div>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

type KeyStatus = 'active' | 'expired' | 'revoked' | 'unknown'
type MachineKey = {
  id: string
  name: string
  scopes: string[]
  expires_at: string
  created_at: string
  revoked?: boolean
  status?: string
}
type SafeProbeResult = {
  service: string
  apiVersion: string
  keyId: string
  keyName: string
  scopes: string[]
  expiresAt: string
  management: string[]
  idempotencyHeader: string
}
type ConnectionResponse = {
  service?: unknown
  api_version?: unknown
  key?: { id?: unknown; name?: unknown; scopes?: unknown; expires_at?: unknown }
  management?: unknown
  openapi_url?: unknown
  idempotency_header?: unknown
}

const MACHINE_API_PATH = '/api/v1'
const OPENAPI_PATH = `${MACHINE_API_PATH}/openapi.json`
const CONNECTION_PATH = `${MACHINE_API_PATH}/connection`
const loading = ref(false)
const creating = ref(false)
const downloadingOpenApi = ref(false)
const testingConnection = ref(false)
const testingIssuedToken = ref(false)
const error = ref('')
const keys = ref<MachineKey[]>([])
const createOpen = ref(false)
const tokenOpen = ref(false)
const name = ref('')
const days = ref(90)
const permissionPreset = ref<'read' | 'manage' | 'execute' | 'custom'>('read')
const customScopes = ref({ read: true, write: false, publish: false })
const issuedToken = ref('')
const probeToken = ref('')
const probeResult = ref<SafeProbeResult | null>(null)
const probeError = ref('')
const issuedProbeResult = ref<SafeProbeResult | null>(null)
const issuedProbeError = ref('')
const statusFilter = ref<'all' | Exclude<KeyStatus, 'unknown'>>('all')
const apiBaseInput = ref(typeof window === 'undefined' ? '' : window.location.origin)
const now = ref(Date.now())
let probeController: AbortController | null = null
let activeProbeKind: 'manual' | 'issued' | null = null
let statusClock: number | null = null
const anyProbeTesting = computed(() => testingConnection.value || testingIssuedToken.value)

const currentDeployment = computed(() => window.location.origin)
const currentApiRoot = computed(() => `${window.location.origin}${MACHINE_API_PATH}`)
const openApiUrl = computed(() => `${window.location.origin}${OPENAPI_PATH}`)

const normalizedApiBase = computed(() => {
  const raw = apiBaseInput.value.trim()
  if (!raw) return null
  try {
    const parsed = new URL(raw)
    if (!['http:', 'https:'].includes(parsed.protocol)) return null
    if (parsed.username || parsed.password || parsed.search || parsed.hash) return null
    let pathname = parsed.pathname.replace(/\/+$/, '')
    const apiPathIndex = pathname.lastIndexOf(MACHINE_API_PATH)
    if (
      apiPathIndex >= 0 &&
      (pathname.length === apiPathIndex + MACHINE_API_PATH.length ||
        pathname[apiPathIndex + MACHINE_API_PATH.length] === '/')
    ) {
      pathname = pathname.slice(0, apiPathIndex)
    }
    return `${parsed.origin}${pathname}`
  } catch {
    return null
  }
})
const apiBaseError = computed(() => {
  if (!apiBaseInput.value.trim()) return '请输入 HTTP 或 HTTPS 地址。'
  if (!normalizedApiBase.value)
    return '地址必须是有效的 HTTP(S) URL，且不能包含用户名、密码、查询参数或片段。'
  return ''
})
const apiBaseIsSameOrigin = computed(() => {
  if (!normalizedApiBase.value) return false
  return new URL(normalizedApiBase.value).origin === window.location.origin
})
const scopeLabel = (scope: string) => {
  const labels: Record<string, string> = { read: '读取', write: '写入', publish: '发布' }
  return labels[scope] || '未知权限'
}
const selectedScopes = computed(() => {
  if (permissionPreset.value === 'read') return ['read']
  if (permissionPreset.value === 'manage') return ['read', 'write']
  if (permissionPreset.value === 'execute') return ['read', 'write', 'publish']
  return [
    'read',
    ...(customScopes.value.write ? ['write'] : []),
    ...(customScopes.value.publish ? ['publish'] : []),
  ]
})
const selectedScopeSummary = computed(() => selectedScopes.value.map(scopeLabel).join(' + '))

const keyStatus = (key: MachineKey): KeyStatus => {
  if (key.status === 'revoked' || key.revoked === true) return 'revoked'
  if (key.status === 'expired') return 'expired'
  const expiry = Date.parse(key.expires_at)
  if (!Number.isFinite(expiry)) return 'unknown'
  if (now.value >= expiry) return 'expired'
  return 'active'
}
const statusLabel = (status: KeyStatus) =>
  ({ active: '有效', expired: '已到期', revoked: '已撤销', unknown: '状态未知' })[status]
const statusType = (status: KeyStatus) =>
  ({ active: 'success', expired: 'warning', revoked: 'danger', unknown: 'info' })[status]
const statusCounts = computed(() => ({
  active: keys.value.filter((key) => keyStatus(key) === 'active').length,
  expired: keys.value.filter((key) => keyStatus(key) === 'expired').length,
  revoked: keys.value.filter((key) => keyStatus(key) === 'revoked').length,
}))
const filteredKeys = computed(() =>
  statusFilter.value === 'all'
    ? keys.value
    : keys.value.filter((key) => keyStatus(key) === statusFilter.value),
)

const formatDate = (value: string) => {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return '—'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    timestamp,
  )
}

const guide = computed(() => {
  const base = normalizedApiBase.value || window.location.origin
  return `STARPT 机器 API 接入说明\n\n当前部署：${window.location.origin}\nAPI 根地址：${base}${MACHINE_API_PATH}\nOpenAPI JSON：${base}${OPENAPI_PATH}\n认证：Authorization: Bearer <machine-key>（使用 PTMOVE_API_KEY 环境变量注入真实密钥）\n\n权限独立且遵循最小授权：\n- read：读取仪表板、站点、任务与运行记录。\n- write：预览配置、导入/修改配置、取消任务等写入操作。\n- publish：创建运行或明确加入下载/发布队列。执行需要明确 API 请求；不会因密钥存在而静默自动运行。\n- 修改配置需要 write；自动化已启用或存在待执行内容时，还必须同时有 publish。\n- POST /tasks 或 /runs 会明确创建一次运行；如果已保存的自动化规则处于启用状态，扫描后可能创建真实任务并进入执行流程。selected_ids 或 enqueue=true 也会明确请求执行，即使规则未启用。\n\n只读示例：\ncurl -H "Authorization: Bearer $PTMOVE_API_KEY" \\\n  '${base}${MACHINE_API_PATH}/dashboard'\n\n写入示例（write；同一操作重试必须复用相同幂等键和请求体）：\ncurl -X PUT '${base}${MACHINE_API_PATH}/rules' \\\n  -H "Authorization: Bearer $PTMOVE_API_KEY" \\\n  -H 'Content-Type: application/json' \\\n  -H 'Idempotency-Key: rule-update-0001' \\\n  --data '{"enabled":false}'\n\n创建运行示例（需要 publish 权限；可能创建真实任务/队列项，请先确认请求内容）：\ncurl -X POST '${base}${MACHINE_API_PATH}/tasks' \\\n  -H "Authorization: Bearer $PTMOVE_API_KEY" \\\n  -H 'Content-Type: application/json' \\\n  -H 'Idempotency-Key: run-request-0001' \\\n  --data '{"selected_ids":[],"enqueue":false}'\n\n所有改变状态的请求都必须带 Idempotency-Key：8–128 个字母、数字、下划线、点、冒号或连字符。只对结果明确且服务端契约允许的操作重试；上传结果不明确时先对账，不要盲目重发。密钥有效期可选 7/30/90/365 天，也可随时撤销。`
})

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const response = await axios.get('/api/machine-keys')
    keys.value = Array.isArray(response.data?.keys) ? response.data.keys : []
  } catch (e) {
    error.value = axios.isAxiosError(e)
      ? '机器密钥列表加载失败，请检查管理员会话后重试。'
      : '无法加载机器密钥。'
  } finally {
    loading.value = false
  }
}

const loadOpenApi = async () => {
  downloadingOpenApi.value = true
  try {
    const response = await axios.get('/api/machine-openapi')
    const blob = new Blob([JSON.stringify(response.data, null, 2)], { type: 'application/json' })
    downloadBlob(blob, 'starpt-machine-openapi.json')
  } catch {
    ElMessage.error('OpenAPI 文档下载失败，请检查管理员会话后重试。')
  } finally {
    downloadingOpenApi.value = false
  }
}
const downloadOpenApi = loadOpenApi

const downloadBlob = (blob: Blob, filename: string) => {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

const integrationPackage = computed(() => ({
  name: 'STARPT AI API 接入包',
  version: 1,
  deployment: window.location.origin,
  base_url: normalizedApiBase.value || window.location.origin,
  api_root: `${normalizedApiBase.value || window.location.origin}${MACHINE_API_PATH}`,
  openapi_url: `${normalizedApiBase.value || window.location.origin}${OPENAPI_PATH}`,
  auth: {
    scheme: 'Bearer',
    header: 'Authorization: Bearer <machine-key>',
    token_placeholder: '${PTMOVE_API_KEY}',
    environment_variable: 'PTMOVE_API_KEY',
    token_value_included: false,
  },
  scopes: {
    read: '读取仪表板、站点、任务与运行记录。',
    write: '预览或修改配置、导入站点、取消任务。',
    publish: '明确创建运行或加入下载/发布队列；不代表静默自动执行。',
    execution_bounds:
      'POST /tasks 或 /runs 会明确创建运行；已启用的保存规则可能自动筛选并创建真实任务。selected_ids 或 enqueue=true 明确请求执行。配置修改需要 write；自动化启用或有待执行内容时还需要 publish。',
  },
  help: {
    idempotency_header: 'Idempotency-Key',
    idempotency_bounds:
      '8–128 个字母、数字、下划线、点、冒号或连字符；同一操作重试须复用相同键和请求体。',
    credential_safety:
      '通过可信 HTTPS 使用；密钥存入密钥管理器，不写入代码、命令历史、日志或提示词。',
    uncertain_publish: '发布结果不明确时先对账，禁止盲目重发。',
    openapi_download: '此页面通过管理员会话下载 OpenAPI；API 文档本身使用机器 Bearer 密钥。',
  },
}))
const downloadIntegrationPackage = () => {
  if (apiBaseError.value) {
    ElMessage.error(apiBaseError.value)
    return
  }
  downloadBlob(
    new Blob([JSON.stringify(integrationPackage.value, null, 2)], { type: 'application/json' }),
    'starpt-ai-integration.json',
  )
}

const copyGuide = async () => {
  try {
    await navigator.clipboard.writeText(guide.value)
    ElMessage.success('API 接入说明已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择说明文本')
  }
}
const resetApiBase = () => {
  apiBaseInput.value = window.location.origin
}

const openCreate = () => {
  clearCreate()
  createOpen.value = true
}
const createKey = async () => {
  creating.value = true
  try {
    const expiresAt = new Date(Date.now() + days.value * 86400000).toISOString()
    const response = await axios.post('/api/machine-keys', {
      name: name.value.trim(),
      scopes: selectedScopes.value,
      expires_at: expiresAt,
    })
    issuedToken.value = typeof response.data?.token === 'string' ? response.data.token : ''
    if (!issuedToken.value) throw new Error('服务器未返回密钥，请检查机器密钥列表')
    createOpen.value = false
    tokenOpen.value = true
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? '创建机器密钥失败，请检查权限或管理员会话。'
        : e instanceof Error
          ? e.message
          : '创建失败。',
    )
  } finally {
    creating.value = false
  }
}
const revoke = async (key: MachineKey) => {
  try {
    await ElMessageBox.confirm(
      `撤销“${key.name}”后，使用该密钥的客户端将无法继续访问。`,
      '撤销机器密钥',
      { type: 'warning', confirmButtonText: '撤销密钥' },
    )
    await axios.delete(`/api/machine-keys/${encodeURIComponent(key.id)}`)
    ElMessage.success('密钥已撤销')
    await load()
  } catch (e) {
    if (e !== 'cancel')
      ElMessage.error(axios.isAxiosError(e) ? '撤销失败，请检查管理员会话后重试。' : '撤销失败。')
  }
}

const sanitizeProbeResult = (payload: ConnectionResponse): SafeProbeResult => {
  const key = payload.key && typeof payload.key === 'object' ? payload.key : {}
  const safeString = (value: unknown) => (typeof value === 'string' ? value.slice(0, 160) : '')
  const safeStrings = (value: unknown) =>
    Array.isArray(value)
      ? value
          .filter((item): item is string => typeof item === 'string')
          .map((item) => item.slice(0, 80))
          .slice(0, 20)
      : []
  return {
    service: safeString(payload.service),
    apiVersion: safeString(payload.api_version),
    keyId: safeString(key.id),
    keyName: safeString(key.name),
    scopes: safeStrings(key.scopes),
    expiresAt: safeString(key.expires_at),
    management: safeStrings(payload.management),
    idempotencyHeader: payload.idempotency_header === 'Idempotency-Key' ? 'Idempotency-Key' : '',
  }
}

const requestConnection = async (token: string, issued = false) => {
  if (anyProbeTesting.value) return
  const controller = new AbortController()
  probeController = controller
  activeProbeKind = issued ? 'issued' : 'manual'
  const timeout = window.setTimeout(() => controller.abort('timeout'), 10000)
  if (issued) {
    testingIssuedToken.value = true
    issuedProbeError.value = ''
    issuedProbeResult.value = null
  } else {
    testingConnection.value = true
    probeError.value = ''
    probeResult.value = null
  }
  try {
    const response = await fetch(`${window.location.origin}${CONNECTION_PATH}`, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' },
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      signal: controller.signal,
    })
    if (response.status === 401) throw new Error('unauthorized')
    if (response.status === 403) throw new Error('forbidden')
    if (!response.ok) throw new Error('unavailable')
    const payload: unknown = await response.json()
    if (controller.signal.reason === 'cleared') return
    if (!payload || typeof payload !== 'object' || Array.isArray(payload))
      throw new Error('unavailable')
    const result = sanitizeProbeResult(payload as ConnectionResponse)
    if (result.service !== 'ptmove-nexus' || result.apiVersion !== 'v1')
      throw new Error('unavailable')
    if (issued) issuedProbeResult.value = result
    else probeResult.value = result
  } catch (cause) {
    if (controller.signal.reason === 'cleared') return
    let safeMessage = '连接失败，请确认部署可访问且网络未拦截请求。'
    if (controller.signal.reason === 'timeout') safeMessage = '连接测试超时（10 秒）。'
    else if (cause instanceof Error && cause.message === 'unauthorized')
      safeMessage = '密钥无效、已到期或已撤销。'
    else if (cause instanceof Error && cause.message === 'forbidden')
      safeMessage = '此密钥没有读取权限。'
    if (issued) issuedProbeError.value = safeMessage
    else probeError.value = safeMessage
  } finally {
    window.clearTimeout(timeout)
    if (probeController === controller) {
      probeController = null
      activeProbeKind = null
      if (issued) testingIssuedToken.value = false
      else testingConnection.value = false
    }
  }
}
const testConnection = async () => {
  if (anyProbeTesting.value) return
  const token = probeToken.value.trim()
  if (!token) return
  await requestConnection(token)
}
const testIssuedToken = async () => {
  if (anyProbeTesting.value || !issuedToken.value) return
  await requestConnection(issuedToken.value, true)
}
const clearProbe = () => {
  if (activeProbeKind === 'manual') {
    probeController?.abort('cleared')
    probeController = null
    activeProbeKind = null
    testingConnection.value = false
  }
  probeToken.value = ''
  probeResult.value = null
  probeError.value = ''
}

const copyIssuedToken = async () => {
  try {
    await navigator.clipboard.writeText(issuedToken.value)
    ElMessage.success('已复制。请将密钥存入安全的凭据管理器。')
  } catch {
    ElMessage.warning('复制失败，请手动选择并复制密钥。')
  }
}
const clearCreate = () => {
  name.value = ''
  days.value = 90
  permissionPreset.value = 'read'
  customScopes.value = { read: true, write: false, publish: false }
}
const clearIssuedToken = () => {
  if (activeProbeKind === 'issued') {
    probeController?.abort('cleared')
    probeController = null
    activeProbeKind = null
    testingIssuedToken.value = false
  }
  issuedToken.value = ''
  issuedProbeResult.value = null
  issuedProbeError.value = ''
}

const refreshStatusClock = () => {
  now.value = Date.now()
}
onMounted(() => {
  void load()
  statusClock = window.setInterval(refreshStatusClock, 30000)
  window.addEventListener('focus', refreshStatusClock)
})
onUnmounted(() => {
  if (statusClock !== null) window.clearInterval(statusClock)
  window.removeEventListener('focus', refreshStatusClock)
  clearProbe()
  clearIssuedToken()
})
</script>

<style scoped>
.settings-page {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 14px;
}
.page-heading,
.card-heading,
.key-heading,
.result-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.page-heading p,
.card-heading p {
  margin: 4px 0 0;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.55;
}
.connection-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 282px), 1fr));
  gap: 24px;
}
.deployment-info {
  min-width: 0;
}
.info-row {
  display: grid;
  grid-template-columns: 116px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  padding: 6px 0;
}
.info-row > span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.info-row code {
  overflow-wrap: anywhere;
  color: var(--el-text-color-primary);
}
.docs-action,
.base-note,
.probe-controls,
.guide-actions,
.issued-actions,
.key-toolbar,
.key-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
.docs-action {
  margin-top: 12px;
}
.api-base-form {
  min-width: 0;
}
.base-note {
  align-items: flex-start;
}
.probe-controls {
  margin-bottom: 12px;
}
.probe-controls :deep(.el-input) {
  flex: 1 1 260px;
  min-width: 0;
}
.probe-result {
  margin-top: 14px;
  padding: 14px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
}
.result-heading {
  justify-content: flex-start;
  margin-bottom: 8px;
}
.result-grid {
  display: grid;
  grid-template-columns: 130px minmax(0, 1fr);
  gap: 8px 12px;
  margin: 0;
  font-size: 13px;
}
.result-grid dt {
  color: var(--el-text-color-secondary);
}
.result-grid dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.key-summary {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin-bottom: 12px;
}
.key-summary span {
  padding: 5px 9px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
}
.key-toolbar {
  justify-content: space-between;
  margin-bottom: 12px;
}
.scope-tag {
  margin: 0 5px 4px 0;
}
.preset-list,
.expiry-presets {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.preset-list :deep(.el-radio-button__inner),
.expiry-presets :deep(.el-radio-button__inner) {
  border-radius: 4px;
}
.permission-summary {
  margin: 10px 0;
  font-size: 14px;
  font-weight: 600;
}
.custom-scope-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
}
.custom-scope-list :deep(.el-checkbox) {
  height: auto;
  max-width: 100%;
  margin-right: 0;
  white-space: normal;
}
.custom-scope-list :deep(.el-checkbox__label) {
  overflow-wrap: anywhere;
}
.permission-help {
  margin-top: 8px;
}
.token-input {
  margin: 14px 0;
}
.issued-actions {
  justify-content: flex-start;
}
.issued-probe-message {
  margin-top: 12px;
}
.settings-page :deep(.el-card),
.settings-page :deep(.el-alert) {
  min-width: 0;
  flex-shrink: 0;
}

.docs-action .el-button,
.probe-controls .el-button,
.issued-actions .el-button,
.guide-actions .el-button {
  margin-left: 0;
}
@media (max-width: 600px) {
  .settings-page :deep(.el-card__body),
  .settings-page :deep(.el-card__header) {
    padding-inline: 12px;
  }
  .page-heading {
    align-items: flex-start;
    flex-direction: column;
  }
  .page-heading > .el-button {
    align-self: stretch;
  }
  .key-heading {
    align-items: flex-start;
    flex-direction: column;
  }
  .guide-actions {
    width: 100%;
  }
  .guide-actions :deep(.el-button) {
    flex: 1 1 auto;
    margin-left: 0;
  }
  .info-row {
    grid-template-columns: 1fr;
    gap: 2px;
  }
  .result-grid {
    grid-template-columns: 1fr;
    gap: 3px;
  }
  .result-grid dd {
    margin-bottom: 7px;
  }
  .probe-controls > .el-button {
    flex: 1 1 auto;
  }
  .key-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .key-toolbar :deep(.el-radio-group) {
    max-width: 100%;
    display: flex;
    flex-wrap: wrap;
  }
  .preset-list,
  .expiry-presets {
    display: flex;
    flex-direction: column;
    align-items: stretch;
  }
  .preset-list :deep(.el-radio-button),
  .expiry-presets :deep(.el-radio-button) {
    width: 100%;
  }
  .preset-list :deep(.el-radio-button__inner),
  .expiry-presets :deep(.el-radio-button__inner) {
    width: 100%;
    border-left: 1px solid var(--el-border-color);
  }
  .issued-actions > .el-button {
    margin-left: 0;
  }
}
</style>
