<template>
  <main class="settings-page" v-loading="loading">
    <header>
      <h1>站点配置档案</h1>
      <p>JSON 仅包含公开站点元数据（最大 64 KiB）；Cookie 与 RSS 凭据单独存储。</p>
    </header>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" /><el-alert
      title="announce_hosts 必须显式声明可信 tracker 域名"
      description="留空时无法核验目标站是否正在做种，自动化启用和下载执行会被拒绝；只读候选预览仍可使用。请在站点声明中填写准确的 tracker 主机名。"
      type="warning"
      show-icon
      :closable="false"
    /><el-card shadow="never"
      ><template #header
        ><div class="head">
          <strong>已登记站点</strong>
          <div>
            <el-button @click="download('/api/site-profiles/schema', 'site-profile.schema.json')"
              >下载 Schema</el-button
            ><el-button
              @click="download('/api/site-profiles/template', 'site-profile.template.json')"
              >下载模板</el-button
            ><el-button type="primary" @click="newProfile">导入配置</el-button>
          </div>
        </div></template
      ><el-table :data="profiles" empty-text="暂无配置"
        ><el-table-column prop="name" label="站点" /><el-table-column
          prop="id"
          label="ID"
        /><el-table-column prop="engine" label="引擎" /><el-table-column prop="roles" label="用途"
          ><template #default="s">{{ s.row.roles?.join(' / ') }}</template></el-table-column
        ><el-table-column label="状态"
          ><template #default="s"
            ><el-tag type="info">{{ s.row.health || 'declared / unverified' }}</el-tag
            ><el-tag v-if="s.row.builtin" class="ml">内置</el-tag></template
          ></el-table-column
        ><el-table-column prop="revision" label="版本" /><el-table-column label="操作" width="210"
          ><template #default="s"
            ><el-button link @click="editProfile(s.row)">编辑</el-button
            ><el-button link @click="exportProfile(s.row.id)">导出</el-button
            ><el-button link @click="editCredentials(s.row)">凭据</el-button></template
          ></el-table-column
        ></el-table
      ></el-card
    >
    <el-dialog
      v-model="editorOpen"
      :title="editing ? '检查配置' : '导入配置'"
      width="min(760px, 94vw)"
      @closed="clearEditor"
      ><div class="editor-actions">
        <el-upload
          :auto-upload="false"
          :show-file-list="false"
          accept="application/json,.json"
          :on-change="readFile"
          ><el-button>选择 JSON 文件</el-button></el-upload
        ><span>{{ byteCount }} / 65,536 字节</span>
      </div>
      <el-input
        v-model="jsonText"
        type="textarea"
        :rows="18"
        spellcheck="false"
        aria-label="站点配置 JSON"
      /><el-checkbox v-if="editing" v-model="updateExisting">明确更新现有配置</el-checkbox>
      <p class="note">
        engine 目前使用已知引擎映射，如 nexusphp /
        ptnexus_site；配置声明尚未验证站点可用性，也不会创建通用新协议。
      </p>
      <div v-if="previewProfile" class="preview">
        <strong>预览：{{ previewProfile.name }}</strong
        ><span
          >{{ previewProfile.base_url }} · {{ previewProfile.engine }} ·
          {{ previewProfile.roles?.join('/') }}</span
        >
      </div>
      <template #footer
        ><el-button @click="editorOpen = false">取消</el-button
        ><el-button :loading="previewing" @click="preview">校验并预览</el-button
        ><el-button
          type="primary"
          :disabled="!previewProfile || (editing && !updateExisting)"
          :loading="saving"
          @click="saveProfile"
          >{{ editing ? '确认更新' : '导入配置' }}</el-button
        ></template
      ></el-dialog
    >
    <el-dialog
      v-model="credentialsOpen"
      title="站点凭据"
      width="min(560px, 94vw)"
      @closed="clearCredentials"
      ><p>{{ credentialName }} · {{ configuredStatus }}</p>
      <el-input
        v-model="cookieInput"
        type="password"
        show-password
        clearable
        autocomplete="new-password"
        placeholder="留空保留现有 Cookie；点击清除后保存可清除"
      /><el-checkbox v-model="clearCookie">明确清除 Cookie</el-checkbox
      ><el-input
        v-model="rssInput"
        type="password"
        show-password
        clearable
        autocomplete="new-password"
        placeholder="留空保留现有 RSS 地址；输入新地址可替换"
      /><el-checkbox v-model="clearRss">明确清除 RSS 地址</el-checkbox
      ><template #footer
        ><el-button @click="credentialsOpen = false">取消</el-button
        ><el-button type="primary" :loading="savingCredentials" @click="saveCredentials"
          >保存凭据</el-button
        ></template
      ></el-dialog
    >
  </main>
</template>
<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, ref } from 'vue'
import { ElMessage, type UploadFile } from 'element-plus'
type Profile = {
  id: string
  name: string
  base_url: string
  engine: string
  roles: string[]
  health?: string
  builtin?: boolean
  revision?: number
  digest?: string
  [key: string]: unknown
}
const loading = ref(false),
  saving = ref(false),
  savingCredentials = ref(false),
  previewing = ref(false),
  error = ref(''),
  profiles = ref<Profile[]>([]),
  editorOpen = ref(false),
  credentialsOpen = ref(false),
  editing = ref(false),
  updateExisting = ref(false),
  expectedRevision = ref(0),
  jsonText = ref(''),
  previewProfile = ref<Profile | null>(null),
  credentialName = ref(''),
  credentialId = ref(''),
  cookieInput = ref(''),
  rssInput = ref(''),
  clearCookie = ref(false),
  clearRss = ref(false),
  hasCookie = ref(false),
  hasRss = ref(false)
const byteCount = computed(() => new TextEncoder().encode(jsonText.value).length),
  configuredStatus = computed(
    () =>
      `Cookie ${hasCookie.value ? '已配置' : '未配置'} · RSS ${hasRss.value ? '已配置' : '未配置'}`,
  )
const load = async () => {
  loading.value = true
  try {
    const r = await axios.get('/api/site-profiles')
    profiles.value = r.data.sites || []
  } catch (e) {
    error.value = axios.isAxiosError(e)
      ? e.response?.data?.message || e.response?.data?.error || e.message
      : '无法加载站点配置'
  } finally {
    loading.value = false
  }
}
const download = async (path: string, name: string) => {
  try {
    const r = await axios.get(path)
    const blob = new Blob([JSON.stringify(r.data, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = name
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '下载失败',
    )
  }
}
const newProfile = async () => {
  editing.value = false
  updateExisting.value = false
  expectedRevision.value = 0
  previewProfile.value = null
  try {
    const r = await axios.get('/api/site-profiles/template')
    jsonText.value = JSON.stringify(r.data, null, 2)
  } catch {
    jsonText.value = '{}'
  }
  editorOpen.value = true
}
const editProfile = async (p: Profile) => {
  editing.value = true
  updateExisting.value = false
  expectedRevision.value = p.revision || 0
  previewProfile.value = null
  try {
    const r = await axios.get(`/api/site-profiles/${encodeURIComponent(p.id)}/export`)
    jsonText.value = JSON.stringify(r.data, null, 2)
    editorOpen.value = true
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '无法导出配置',
    )
  }
}
const readFile = async (file: UploadFile) => {
  try {
    if (!file.raw) throw new Error('无法读取选中的文件')
    const text = await file.raw.text()
    if (new TextEncoder().encode(text).length > 65536) throw new Error('JSON 文件超过 64 KiB')
    jsonText.value = text
    previewProfile.value = null
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '读取失败')
  }
}
const parsed = () => {
  if (byteCount.value > 65536) throw new Error('配置超过 64 KiB')
  const value: unknown = JSON.parse(jsonText.value)
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('JSON 顶层必须是对象')
  const hasSecret = (item: unknown): boolean =>
    Array.isArray(item)
      ? item.some(hasSecret)
      : typeof item === 'object' && item !== null
        ? Object.entries(item as Record<string, unknown>).some(
            ([key, child]) =>
              /cookie|rss(_url)?|passkey|secret|token/i.test(key) || hasSecret(child),
          )
        : false
  if (hasSecret(value)) throw new Error('公开站点档案不能包含凭据字段')
  return value as Record<string, unknown>
}
const preview = async () => {
  previewing.value = true
  previewProfile.value = null
  try {
    const r = await axios.post('/api/site-profiles/preview', parsed())
    previewProfile.value = r.data.profile
    ElMessage.success('配置预览成功')
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : e instanceof Error
          ? e.message
          : '预览失败',
    )
  } finally {
    previewing.value = false
  }
}
const saveProfile = async () => {
  saving.value = true
  try {
    const profile = parsed()
    await axios.post('/api/site-profiles/import', {
      profile,
      update: editing.value && updateExisting.value,
      expected_revision: editing.value ? expectedRevision.value : undefined,
    })
    ElMessage.success(editing.value ? '配置已更新' : '配置已导入')
    editorOpen.value = false
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : e instanceof Error
          ? e.message
          : '导入失败',
    )
  } finally {
    saving.value = false
  }
}
const exportProfile = async (id: string) => {
  try {
    const r = await axios.get(`/api/site-profiles/${encodeURIComponent(id)}/export`)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(
      new Blob([JSON.stringify(r.data, null, 2)], { type: 'application/json' }),
    )
    a.download = `${id}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '导出失败',
    )
  }
}
const editCredentials = async (p: Profile) => {
  credentialId.value = p.id
  credentialName.value = p.name
  cookieInput.value = ''
  rssInput.value = ''
  clearCookie.value = false
  clearRss.value = false
  try {
    const r = await axios.get(`/api/site-profiles/${encodeURIComponent(p.id)}/credentials`)
    hasCookie.value = !!r.data.has_cookie
    hasRss.value = !!r.data.has_rss
    credentialsOpen.value = true
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '无法读取凭据状态',
    )
  }
}
const saveCredentials = async () => {
  savingCredentials.value = true
  try {
    const body: Record<string, string> = {}
    if (cookieInput.value) body.cookie = cookieInput.value
    else if (clearCookie.value) body.cookie = ''
    if (rssInput.value) body.rss = rssInput.value
    else if (clearRss.value) body.rss = ''
    await axios.put(
      `/api/site-profiles/${encodeURIComponent(credentialId.value)}/credentials`,
      body,
    )
    ElMessage.success('凭据已保存')
    credentialsOpen.value = false
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '保存凭据失败',
    )
  } finally {
    savingCredentials.value = false
  }
}
const clearEditor = () => {
  jsonText.value = ''
  previewProfile.value = null
}
const clearCredentials = () => {
  cookieInput.value = ''
  rssInput.value = ''
  clearCookie.value = false
  clearRss.value = false
}
onMounted(load)
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
p {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
}
.head,
.editor-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.head > div {
  display: flex;
  gap: 8px;
}
.ml {
  margin-left: 6px;
}
.editor-actions {
  margin-bottom: 10px;
}
.note {
  margin-top: 10px;
  line-height: 1.55;
}
.preview {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  margin-top: 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
}
@media (max-width: 600px) {
  .settings-page {
    padding: 12px;
  }
  .head {
    align-items: flex-start;
    flex-direction: column;
  }
  .head > div {
    flex-wrap: wrap;
  }
}
</style>
