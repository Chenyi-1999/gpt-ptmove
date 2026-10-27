<template>
  <main class="settings-page" v-loading="loading">
    <header>
      <h1>AI 与机器 API</h1>
      <p>为外部客户端创建具名机器密钥。密钥只在创建后显示一次，不会保存到浏览器本地存储。</p>
    </header>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-alert
      title="传输安全"
      description="机器密钥是持有者凭据。通过 HTTPS 使用 API；若当前部署使用 HTTP，请先在受信任的 TLS 反向代理后访问，避免密钥在网络中明文传输。"
      type="warning"
      show-icon
      :closable="false"
    />

    <el-card shadow="never">
      <template #header>
        <div class="head">
          <strong>机器密钥</strong
          ><el-button type="primary" @click="createOpen = true">创建密钥</el-button>
        </div>
      </template>
      <el-table :data="keys" empty-text="暂无机器密钥">
        <el-table-column prop="name" label="名称" />
        <el-table-column label="权限">
          <template #default="scope">
            <el-tag
              v-for="permission in scope.row.scopes"
              :key="permission"
              class="scope"
              :type="
                permission === 'publish' ? 'danger' : permission === 'write' ? 'warning' : 'info'
              "
              >{{ scopeLabel(permission) }}</el-tag
            >
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" />
        <el-table-column prop="expires_at" label="到期时间" />
        <el-table-column label="状态">
          <template #default="scope">
            <el-tag :type="scope.row.revoked ? 'danger' : 'success'">{{
              scope.row.revoked ? '已撤销' : '有效'
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="scope">
            <el-button v-if="!scope.row.revoked" link type="danger" @click="revoke(scope.row)"
              >撤销</el-button
            >
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <div class="head">
          <strong>AI 调用说明</strong><el-button @click="copyGuide">复制说明</el-button>
        </div>
      </template>
      <el-form label-position="top">
        <el-form-item label="机器 API 根地址">
          <el-input v-model="apiBaseUrl" aria-label="机器 API 根地址" />
        </el-form-item>
      </el-form>
      <el-input
        v-model="guide"
        type="textarea"
        :rows="15"
        readonly
        aria-label="可复制的中文 AI API 调用说明"
      />
      <div class="docs-action">
        <el-button :loading="downloadingOpenApi" @click="downloadOpenApi"
          >下载带 JWT 认证的 OpenAPI 文档</el-button
        >
        <span class="muted">包含真实部署路由；此下载使用当前管理员登录会话。</span>
      </div>
    </el-card>

    <el-dialog
      v-model="createOpen"
      title="创建机器密钥"
      width="min(520px, 94vw)"
      @closed="clearCreate"
    >
      <el-form label-position="top">
        <el-form-item label="名称"
          ><el-input v-model="name" maxlength="80" placeholder="例如：媒体信息读取器"
        /></el-form-item>
        <el-form-item label="权限范围">
          <el-checkbox-group v-model="scopes">
            <el-checkbox label="read" disabled>读取（必选）</el-checkbox>
            <el-checkbox label="write">写入</el-checkbox>
            <el-checkbox label="publish">发布</el-checkbox>
          </el-checkbox-group>
          <div class="muted">
            read 必须启用。write 与 publish
            是独立权限；发布权限不会自动授予写入权限。发布可产生外部影响，请谨慎授予。
          </div>
        </el-form-item>
        <el-form-item label="有效天数"
          ><el-input-number v-model="days" :min="1" :max="365"
        /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button
          type="primary"
          :loading="creating"
          :disabled="!name.trim() || !scopes.includes('read')"
          @click="createKey"
          >创建密钥</el-button
        >
      </template>
    </el-dialog>

    <el-dialog
      v-model="tokenOpen"
      title="复制机器密钥"
      width="min(600px, 94vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      @closed="clearToken"
    >
      <el-alert
        title="现在复制此密钥。离开窗口后将无法再次查看。"
        type="warning"
        :closable="false"
      />
      <el-input
        v-model="issuedToken"
        type="textarea"
        :rows="3"
        readonly
        class="token"
        aria-label="新创建的机器密钥，仅显示一次"
      />
      <div class="actions">
        <el-button type="primary" @click="copyToken">复制密钥</el-button>
        <el-button @click="tokenOpen = false">已妥善保存并关闭</el-button>
      </div>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

type MachineKey = {
  id: string
  name: string
  scopes: string[]
  expires_at: string
  revoked: boolean
  created_at: string
}
type OpenApiDocument = { servers?: { url?: string }[] }

const loading = ref(false)
const creating = ref(false)
const downloadingOpenApi = ref(false)
const error = ref('')
const keys = ref<MachineKey[]>([])
const createOpen = ref(false)
const tokenOpen = ref(false)
const name = ref('')
const scopes = ref<string[]>(['read'])
const days = ref(90)
const issuedToken = ref('')
const apiBaseUrl = ref('')

const guide = computed(() => {
  const base = apiBaseUrl.value.trim().replace(/\/$/, '') || 'https://your-ptnexus-host/api/v1'
  return `PTNexus 机器 API 调用说明

API 根地址：${base}
认证方式：Authorization: Bearer <machine-key>
密钥用途：机器 API 专用，不可用于网页登录；请存入安全的凭据管理器。

权限范围相互独立：
- read：读取 API 数据（创建密钥时必选）。
- write：校验/导入站点配置、修改规则、取消任务等写入操作。
- publish：创建运行或加入下载/发布队列；不会自动获得 write 权限。
只申请集成实际需要的范围。密钥有效期为 1–365 天，管理员可随时撤销。

幂等要求：所有会改变状态的请求都必须携带 Idempotency-Key。每个新操作生成唯一值，长度 8–128，只使用字母、数字、下划线、点、冒号或连字符。重试同一操作时复用原值和完全相同的请求体；新操作必须使用新值。

读取示例（需要 read）：
curl -H 'Authorization: Bearer <machine-key>' \\
  '${base}/dashboard'

修改规则示例（需要 write；幂等键示例值仅供格式参考）：
curl -X PUT '${base}/rules' \\
  -H 'Authorization: Bearer <machine-key>' \\
  -H 'Content-Type: application/json' \\
  -H 'Idempotency-Key: rule-update-0001' \\
  --data '{"enabled":false}'

查看接口路径和请求字段：使用管理员会话下载 OpenAPI JSON，或用 read 密钥读取 ${base}/openapi.json。不要把真实密钥写入提示词、源代码、命令历史或日志。`
})

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const response = await axios.get('/api/machine-keys')
    keys.value = response.data.keys || []
  } catch (e) {
    error.value = axios.isAxiosError(e)
      ? e.response?.data?.message || e.response?.data?.error || e.message
      : '无法加载机器密钥'
  } finally {
    loading.value = false
  }
}

const scopeLabel = (scope: string) => {
  const labels: Record<string, string> = { read: '读取', write: '写入', publish: '发布' }
  return labels[scope] || scope
}

const loadOpenApiBaseUrl = async () => {
  try {
    const response = await axios.get<OpenApiDocument>('/api/machine-openapi')
    const path = response.data.servers?.[0]?.url || '/api/v1'
    apiBaseUrl.value = new URL(path, window.location.origin).toString().replace(/\/$/, '')
  } catch {
    apiBaseUrl.value = `${window.location.origin}/api/v1`
  }
}

const downloadOpenApi = async () => {
  downloadingOpenApi.value = true
  try {
    const response = await axios.get('/api/machine-openapi')
    const blob = new Blob([JSON.stringify(response.data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'ptnexus-machine-openapi.json'
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : 'OpenAPI 下载失败',
    )
  } finally {
    downloadingOpenApi.value = false
  }
}

const copyGuide = async () => {
  try {
    await navigator.clipboard.writeText(guide.value)
    ElMessage.success('AI 调用说明已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择说明文本')
  }
}

const createKey = async () => {
  creating.value = true
  try {
    const expiresAt = new Date(Date.now() + days.value * 86400000).toISOString()
    const response = await axios.post('/api/machine-keys', {
      name: name.value.trim(),
      scopes: [...new Set(['read', ...scopes.value])],
      expires_at: expiresAt,
    })
    issuedToken.value = response.data.token || ''
    if (!issuedToken.value) throw new Error('服务器未返回密钥，请检查机器密钥列表')
    createOpen.value = false
    tokenOpen.value = true
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : e instanceof Error
          ? e.message
          : '创建失败',
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
      {
        type: 'warning',
        confirmButtonText: '撤销密钥',
      },
    )
    await axios.delete(`/api/machine-keys/${encodeURIComponent(key.id)}`)
    ElMessage.success('密钥已撤销')
    await load()
  } catch (e) {
    if (e !== 'cancel')
      ElMessage.error(
        axios.isAxiosError(e)
          ? e.response?.data?.message || e.response?.data?.error || e.message
          : '撤销失败',
      )
  }
}

const copyToken = async () => {
  try {
    await navigator.clipboard.writeText(issuedToken.value)
    ElMessage.success('已复制。请将密钥存入安全的凭据管理器。')
  } catch {
    ElMessage.warning('复制失败，请手动选择并复制密钥')
  }
}

const clearCreate = () => {
  name.value = ''
  scopes.value = ['read']
  days.value = 90
}
const clearToken = () => {
  issuedToken.value = ''
}

onMounted(() => {
  void load()
  void loadOpenApiBaseUrl()
})
</script>

<style scoped>
.settings-page {
  padding: 20px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
h1 {
  font-size: 22px;
  margin: 0 0 6px;
}
header p,
.muted {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.55;
}
.head,
.actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.scope {
  margin-right: 6px;
}
.token {
  margin: 14px 0;
}
.actions {
  justify-content: flex-start;
}
.docs-action {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}
.muted {
  margin: 0;
}
@media (max-width: 600px) {
  .settings-page {
    padding: 12px;
  }
}
</style>
