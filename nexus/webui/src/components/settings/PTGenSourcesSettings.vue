<template>
  <section class="ptgen-sources glass-card glass-rounded" aria-labelledby="ptgen-sources-title">
    <header class="ptgen-sources__header">
      <h3 id="ptgen-sources-title">PTGen 来源</h3>
      <div class="ptgen-sources__controls">
        <el-button
          :icon="Plus"
          plain
          aria-label="添加来源"
          :disabled="loading || saving || Boolean(loadError)"
          @click="addSource"
        >
          添加
        </el-button>
        <el-button
          type="primary"
          :loading="saving"
          :disabled="loading || Boolean(loadError)"
          @click="saveSources"
        >
          保存来源
        </el-button>
      </div>
    </header>

    <div class="ptgen-sources__body" :aria-busy="loading || saving">
      <p v-if="loading" role="status" class="ptgen-sources__notice">正在读取来源…</p>
      <div v-else-if="loadError" class="ptgen-sources__notice" role="alert">
        <p>{{ loadError }}</p>
        <el-button link type="primary" @click="loadSources">重试</el-button>
      </div>
      <template v-else>
        <p class="ptgen-sources__guide">按列表顺序查询，未取得所需资料时继续尝试内置来源。</p>
        <p v-if="saveError" class="ptgen-sources__error" role="alert">{{ saveError }}</p>
        <p v-if="!sources.length" class="ptgen-sources__empty">尚未添加自定义来源</p>
        <ol v-else class="ptgen-sources__list">
          <li v-for="(row, index) in sources" :key="row.key" class="ptgen-row">
            <fieldset :disabled="saving" class="ptgen-row__fields">
              <legend class="ptgen-sr-only">第 {{ index + 1 }} 条来源</legend>
              <label class="ptgen-field">
                <span>接口类型</span>
                <select :value="row.type" @change="changeType(row, $event)">
                  <option v-for="provider in providers" :key="provider.type" :value="provider.type">
                    {{ provider.label }}
                  </option>
                </select>
              </label>
              <label class="ptgen-field">
                <span>API 地址</span>
                <input
                  v-model="row.url"
                  type="url"
                  inputmode="url"
                  autocomplete="off"
                  spellcheck="false"
                  :placeholder="providerExample(row.type)"
                />
              </label>
              <div class="ptgen-row__token">
                <label class="ptgen-field">
                  <span>{{ requiresToken(row) ? '令牌（必填）' : '令牌（可选）' }}</span>
                  <input
                    v-model="row.token"
                    type="password"
                    autocomplete="new-password"
                    :placeholder="tokenPlaceholder(row)"
                    :disabled="row.clearToken || row.useLegacyToken"
                  />
                </label>
                <el-button
                  v-if="row.tokenConfigured && !requiresToken(row)"
                  link
                  :type="row.clearToken ? 'primary' : 'danger'"
                  :disabled="saving"
                  @click="toggleTokenClear(row)"
                >
                  {{ row.clearToken ? '取消清除' : '清除令牌' }}
                </el-button>
                <label
                  v-if="!row.id && row.type === 'cspt' && legacyConfigured"
                  class="ptgen-row__reuse"
                >
                  <input v-model="row.useLegacyToken" type="checkbox" :disabled="!!row.token.trim()" />
                  使用已保存的财神令牌
                </label>
              </div>
              <div class="ptgen-row__actions" aria-label="排序与删除">
                <el-button
                  :icon="ArrowUp"
                  :aria-label="'上移第 ' + (index + 1) + ' 条来源'"
                  :title="'上移第 ' + (index + 1) + ' 条来源'"
                  :disabled="saving || index === 0"
                  @click="moveSource(index, -1)"
                />
                <el-button
                  :icon="ArrowDown"
                  :aria-label="'下移第 ' + (index + 1) + ' 条来源'"
                  :title="'下移第 ' + (index + 1) + ' 条来源'"
                  :disabled="saving || index === sources.length - 1"
                  @click="moveSource(index, 1)"
                />
                <el-button
                  :icon="Delete"
                  type="danger"
                  plain
                  :aria-label="'删除第 ' + (index + 1) + ' 条来源'"
                  :title="'删除第 ' + (index + 1) + ' 条来源'"
                  :disabled="saving"
                  @click="sources.splice(index, 1)"
                />
              </div>
              <p
                v-if="row.type === 'auto' && row.resolvedType && row.url.trim() === row.savedURL"
                class="ptgen-row__metadata"
              >
                识别结果：{{ providerLabel(row.resolvedType) }}
              </p>
            </fieldset>
          </li>
        </ol>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ArrowDown, ArrowUp, Delete, Plus } from '@element-plus/icons-vue'
import axios from 'axios'
import { onMounted, ref } from 'vue'
import { ElMessage } from '@/utils/uiNotify'

type ProviderType = 'auto' | 'cspt' | 'universal' | 'ourbits' | 'generic'

const providerDetails: Record<ProviderType, { label: string; example: string; defaultURL?: string }> = {
  auto: { label: '自动识别', example: 'https://服务地址/API 路径' },
  cspt: { label: '财神 PTGen', example: 'https://cspt.top', defaultURL: 'https://cspt.top' },
  universal: {
    label: 'PT-Gen Universal',
    example: 'https://你的域名/api/v2/info',
  },
  ourbits: {
    label: 'OurBits',
    example: 'https://api.ourhelp.club/infogen',
    defaultURL: 'https://api.ourhelp.club/infogen',
  },
  generic: { label: '通用 PTGen', example: 'https://你的域名/api' },
}

const providers = (Object.keys(providerDetails) as ProviderType[]).map((type) => ({
  type,
  label: providerDetails[type].label,
}))

interface SavedSource {
  id: string
  type: ProviderType
  url: string
  token_configured: boolean
  resolved_type?: ProviderType
}

interface SourcesResponse {
  sources: SavedSource[]
  legacy_cspt_configured: boolean
}

interface SourceDraft {
  key: number
  id?: string
  type: ProviderType
  resolvedType?: ProviderType
  url: string
  savedURL: string
  token: string
  tokenConfigured: boolean
  clearToken: boolean
  useLegacyToken: boolean
}

const sourcesURL = '/api/settings/ptgen'
const sources = ref<SourceDraft[]>([])
const legacyConfigured = ref(false)
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
let nextKey = 0

const providerLabel = (type: ProviderType) => providerDetails[type].label
const providerExample = (type: ProviderType) => providerDetails[type].example

const emptyDraft = (): SourceDraft => ({
  key: ++nextKey,
  type: 'auto',
  url: '',
  savedURL: '',
  token: '',
  tokenConfigured: false,
  clearToken: false,
  useLegacyToken: false,
})

const applyResponse = (response: SourcesResponse) => {
  if (!Array.isArray(response?.sources)) throw new Error('PTGen 来源响应格式无效。')
  sources.value = response.sources.map((row) => ({
    ...emptyDraft(),
    id: row.id,
    type: row.type,
    resolvedType: row.resolved_type,
    url: row.url,
    savedURL: row.url,
    tokenConfigured: row.token_configured,
  }))
  legacyConfigured.value = response.legacy_cspt_configured
}

const errorMessage = (error: unknown, fallback: string) => {
  if (axios.isAxiosError<{ message?: string; error?: string }>(error)) {
    return error.response?.data?.message || error.response?.data?.error || fallback
  }
  return error instanceof Error ? error.message : fallback
}

const loadSources = async () => {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await axios.get<SourcesResponse>(sourcesURL)
    applyResponse(data)
  } catch (error) {
    loadError.value = errorMessage(error, '读取 PTGen 来源失败。')
  } finally {
    loading.value = false
  }
}

const addSource = () => {
  sources.value.push(emptyDraft())
  saveError.value = ''
}

const changeType = (row: SourceDraft, event: Event) => {
  const type = (event.target as HTMLSelectElement).value as ProviderType
  const previousDefault = providerDetails[row.type].defaultURL
  if (!row.url.trim() || row.url.trim() === previousDefault) {
    row.url = providerDetails[type].defaultURL || ''
  }
  row.type = type
  row.useLegacyToken = false
  if (requiresToken(row)) row.clearToken = false
}

const moveSource = (index: number, delta: number) => {
  const other = index + delta
  if (other < 0 || other >= sources.value.length) return
  const moved = sources.value.splice(index, 1)[0]
  if (moved) sources.value.splice(other, 0, moved)
}

const requiresToken = (row: SourceDraft) =>
  row.type === 'cspt' ||
  (row.type === 'auto' && row.resolvedType === 'cspt' && row.url.trim() === row.savedURL)

const tokenPlaceholder = (row: SourceDraft) => {
  if (row.clearToken) return '保存后清除令牌'
  if (row.useLegacyToken) return '使用已保存的财神令牌'
  if (row.tokenConfigured) {
    return row.url.trim() === row.savedURL ? '留空保留现有令牌' : '地址已改，请重新填写令牌'
  }
  return requiresToken(row) ? '请填写接口令牌' : '按接口要求填写'
}

const toggleTokenClear = (row: SourceDraft) => {
  row.clearToken = !row.clearToken
  if (row.clearToken) row.token = ''
}

const saveSources = async () => {
  if (loading.value || saving.value || loadError.value) return
  saveError.value = ''
  for (const [index, row] of sources.value.entries()) {
    if (!row.url.trim()) {
      saveError.value = `请填写第 ${index + 1} 条来源的 API 地址。`
      return
    }
    if (
      requiresToken(row) &&
      !row.token.trim() &&
      !row.useLegacyToken &&
      (!row.tokenConfigured || row.url.trim() !== row.savedURL)
    ) {
      saveError.value = `请填写第 ${index + 1} 条来源的令牌。`
      return
    }
    if (row.tokenConfigured && row.url.trim() !== row.savedURL && !row.clearToken && !row.token.trim()) {
      saveError.value = `第 ${index + 1} 条来源的地址已更改，请填写新的令牌或选择清除。`
      return
    }
  }
  saving.value = true
  try {
    const payload = sources.value.map((row) => ({
      id: row.id,
      type: row.type,
      url: row.url.trim(),
      token: row.token.trim() || undefined,
      clear_token: row.clearToken || undefined,
      use_legacy_cspt_token: row.useLegacyToken || undefined,
    }))
    const { data } = await axios.put<SourcesResponse>(sourcesURL, { sources: payload })
    applyResponse(data)
    ElMessage.success('PTGen 来源已保存')
  } catch (error) {
    saveError.value = errorMessage(error, '保存 PTGen 来源失败。')
  } finally {
    saving.value = false
  }
}

onMounted(loadSources)
</script>

<style scoped>
.ptgen-sources {
  min-width: 0;
}

.ptgen-sources__header,
.ptgen-sources__controls,
.ptgen-row__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ptgen-sources__header {
  flex-wrap: wrap;
  justify-content: space-between;
  border-bottom: 1px solid var(--star-border);
  padding: 14px 16px;
}

.ptgen-sources__header h3 {
  margin: 0;
  color: var(--star-ink);
  font-size: 16px;
  font-weight: 550;
}

.ptgen-sources__header :deep(.el-button + .el-button),
.ptgen-row__actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.ptgen-sources__body {
  display: grid;
  gap: 12px;
  padding: 16px;
}

.ptgen-sources__guide,
.ptgen-sources__notice,
.ptgen-sources__empty,
.ptgen-sources__error,
.ptgen-row__metadata {
  margin: 0;
  color: var(--star-muted);
  font-size: 13px;
  line-height: 1.5;
}

.ptgen-sources__notice {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.ptgen-sources__notice p {
  margin: 0;
}

.ptgen-sources__empty {
  border: 1px dashed var(--star-border);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}

.ptgen-sources__error {
  border-radius: 6px;
  background: var(--star-danger-soft);
  color: var(--star-danger);
  padding: 9px 12px;
}

.ptgen-sources__list {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.ptgen-row {
  min-width: 0;
}

.ptgen-row__fields {
  display: grid;
  grid-template-columns: minmax(124px, 0.8fr) minmax(160px, 1.55fr) minmax(116px, 1fr) auto;
  align-items: start;
  gap: 10px;
  min-width: 0;
  margin: 0;
  border: 1px solid var(--star-border);
  border-radius: 8px;
  background: var(--star-surface-muted);
  padding: 12px;
}

.ptgen-field {
  display: grid;
  gap: 6px;
  min-width: 0;
  color: var(--star-muted);
  font-size: 13px;
  line-height: 18px;
}

.ptgen-field input,
.ptgen-field select {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  min-height: 40px;
  border: 1px solid var(--star-border);
  border-radius: 6px;
  background: var(--star-surface);
  padding: 7px 8px;
  color: var(--star-ink);
  font: inherit;
}

.ptgen-field input::placeholder {
  color: var(--star-muted);
}

.ptgen-field input:focus-visible,
.ptgen-field select:focus-visible {
  outline: 2px solid var(--star-accent);
  outline-offset: 1px;
}

.ptgen-row__actions {
  align-self: start;
  padding-top: 24px;
  flex-wrap: wrap;
}

.ptgen-row__actions :deep(.el-button) {
  width: 40px;
  min-height: 40px;
  padding: 0;
}

.ptgen-row__reuse {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  color: var(--star-muted);
  font-size: 13px;
  line-height: 1.4;
}

.ptgen-row__metadata {
  grid-column: 1 / -1;
}

.ptgen-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

@media (max-width: 1000px) {
  .ptgen-row__fields {
    grid-template-columns: minmax(126px, 1fr) minmax(160px, 2fr);
  }

  .ptgen-row__actions {
    align-self: end;
    padding-top: 0;
  }
}

@media (max-width: 620px) {
  .ptgen-sources__header,
  .ptgen-sources__body {
    padding: 12px;
  }

  .ptgen-row__fields {
    grid-template-columns: minmax(0, 1fr);
  }

  .ptgen-field input,
  .ptgen-field select,
  .ptgen-row__actions :deep(.el-button) {
    min-height: 44px;
  }

  .ptgen-row__actions :deep(.el-button) {
    width: 44px;
  }
}
</style>
