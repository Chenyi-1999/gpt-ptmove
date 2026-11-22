<template>
  <el-dialog
    v-model="visible"
    title="编辑已发布种子"
    width="min(920px, calc(100vw - 32px))"
    destroy-on-close
    class="published-edit-dialog"
  >
    <div v-loading="loadingDetails" class="edit-content" aria-live="polite">
      <p class="edit-intro">
        从 NovaHD 读取当前资料，可让飞牛补齐截图和 MediaInfo。修改内容会先预览，确认后才保存。
      </p>

      <el-alert
        v-if="errorMessage"
        :title="errorMessage"
        type="error"
        show-icon
        :closable="false"
      />
      <el-alert
        v-if="resultMessage"
        :title="resultMessage"
        :type="resultType"
        show-icon
        :closable="false"
      />
      <div v-if="needsReconciliation" class="reconcile-actions">
        <el-button :loading="loadingDetails" :disabled="busy" @click="reconcileFromSite"
          >重新读取 NovaHD 资料</el-button
        >
      </div>

      <template v-if="detailsLoaded">
        <div class="target-link-row">
          <span>NovaHD 种子详情</span>
          <a v-if="detailsUrl" :href="detailsUrl" target="_blank" rel="noopener noreferrer">
            打开详情页 <el-icon><TopRight /></el-icon>
          </a>
          <span v-else>链接暂不可用</span>
        </div>

        <el-radio-group
          v-model="editMode"
          class="edit-mode-switch"
          aria-label="选择已发布内容编辑方式"
          :disabled="busy"
        >
          <el-radio-button label="media">补截图与 MediaInfo</el-radio-button>
          <el-radio-button label="cover" :disabled="!currentCoverUrl">更换封面</el-radio-button>
        </el-radio-group>

        <el-alert
          v-if="!currentCoverUrl"
          title="站点没有返回可用的当前封面地址，封面编辑暂不可用。"
          type="info"
          :closable="false"
        />

        <section
          v-if="editMode === 'cover' && currentCoverUrl"
          class="edit-section cover-section"
          aria-labelledby="cover-heading"
        >
          <div class="section-heading">
            <div>
              <h2 id="cover-heading">更换封面</h2>
              <p>粘贴 Pixhost 展示页或图片链接。预览会显示服务端解析出的原图，确认后只更新封面。</p>
            </div>
          </div>
          <div class="cover-preview-grid">
            <article v-if="!coverPreview" class="cover-card">
              <h3>当前封面</h3>
              <a
                v-if="currentCoverImage"
                :href="currentCoverImage"
                target="_blank"
                rel="noopener noreferrer"
              >
                <img :src="currentCoverImage" alt="NovaHD 当前封面" referrerpolicy="no-referrer" />
              </a>
              <p v-else class="cover-help">可在 NovaHD 详情页查看当前封面。</p>
              <code>{{ currentCoverUrl }}</code>
            </article>
            <article class="cover-card">
              <h3>新的 Pixhost 地址</h3>
              <el-input
                v-model="coverUrlDraft"
                aria-label="新的 Pixhost 展示页或图片直链"
                placeholder="https://pixhost.to/show/... 或 https://img3.pixhost.to/images/..."
                clearable
                :disabled="busy"
                @input="invalidateCoverPreview"
              />
              <p class="cover-help">支持 HTTPS Pixhost 链接。</p>
            </article>
          </div>
          <div class="edit-actions">
            <el-button
              :icon="Refresh"
              :loading="coverPreviewing"
              :disabled="busy || !coverUrlDraft.trim()"
              @click="createCoverPreview"
            >
              解析并预览封面
            </el-button>
          </div>

          <section
            v-if="coverPreview"
            class="cover-confirm-panel"
            aria-labelledby="cover-confirm-heading"
          >
            <div class="section-heading">
              <div>
                <h2 id="cover-confirm-heading">封面保存预览</h2>
                <p>请确认新图片内容正确。保存操作仅修改封面链接。</p>
              </div>
              <el-tag type="warning" effect="plain">尚未保存</el-tag>
            </div>
            <div class="cover-preview-grid">
              <article class="cover-card">
                <h3>保存前</h3>
                <a
                  v-if="currentCoverImage"
                  :href="currentCoverImage"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <img :src="currentCoverImage" alt="保存前封面" referrerpolicy="no-referrer" />
                </a>
                <p v-else class="cover-help">可在 NovaHD 详情页查看当前封面。</p>
                <code>{{ coverPreview.coverUrlBefore }}</code>
              </article>
              <article class="cover-card">
                <h3>解析后的新封面</h3>
                <a :href="coverPreview.coverUrlAfter" target="_blank" rel="noopener noreferrer">
                  <img
                    :src="coverPreview.coverUrlAfter"
                    alt="解析后的新封面"
                    referrerpolicy="no-referrer"
                  />
                </a>
                <code>{{ coverPreview.coverUrlAfter }}</code>
              </article>
            </div>
            <el-alert
              v-if="coverPreview.coverUrlBefore === coverPreview.coverUrlAfter"
              title="解析结果与当前封面相同，没有需要保存的变化。"
              type="info"
              :closable="false"
            />
            <template v-else>
              <el-checkbox v-model="coverConfirmed" class="confirm-checkbox">
                我已核对新封面，确认只替换 NovaHD 的封面图片地址
              </el-checkbox>
              <div class="edit-actions save-actions">
                <el-button :disabled="busy" @click="clearCoverPreview">返回修改</el-button>
                <el-button
                  type="primary"
                  :loading="coverSaving"
                  :disabled="busy || !coverConfirmed"
                  @click="saveCover"
                >
                  确认并保存封面
                </el-button>
              </div>
            </template>
          </section>
        </section>

        <template v-if="editMode === 'media'">
          <section class="edit-section" aria-labelledby="screenshots-heading">
            <div class="section-heading">
              <div>
                <h2 id="screenshots-heading">截图</h2>
                <p>飞牛生成的有效截图会追加到现有资料，重复链接会自动去重。</p>
              </div>
              <el-checkbox v-model="fetchScreenshots" :disabled="busy">由飞牛补充</el-checkbox>
            </div>
            <el-input
              :model-value="existingScreenshotBBCode || '暂无现有截图'"
              type="textarea"
              :rows="4"
              resize="vertical"
              aria-label="NovaHD 当前截图"
              readonly
            />
            <el-input
              v-model="screenshotDraft"
              type="textarea"
              :rows="5"
              resize="vertical"
              aria-label="要追加的截图 BBCode"
              placeholder="飞牛返回的截图会显示在这里，也可以手动粘贴符合站点格式的图片链接。"
              :disabled="busy"
              @input="invalidatePreview"
            />
          </section>

          <section class="edit-section" aria-labelledby="mediainfo-heading">
            <div class="section-heading">
              <div>
                <h2 id="mediainfo-heading">MediaInfo</h2>
                <p>可手动修正，也可以由飞牛重新提取。</p>
              </div>
              <div class="mediainfo-controls">
                <el-checkbox v-model="fetchMediainfo" :disabled="busy">由飞牛补充</el-checkbox>
                <el-button
                  link
                  :disabled="busy || mediainfo === originalMediainfo"
                  @click="restoreMediainfo"
                >
                  恢复站点原文
                </el-button>
              </div>
            </div>
            <el-input
              v-model="mediainfo"
              type="textarea"
              :rows="10"
              resize="vertical"
              aria-label="MediaInfo 文本"
              placeholder="暂无 MediaInfo。可以手动粘贴，或选择由飞牛补充。"
              :disabled="busy"
              @input="invalidatePreview"
            />
          </section>

          <div class="edit-actions">
            <el-button
              :icon="Refresh"
              :loading="fetchingMedia"
              :disabled="busy || (!fetchScreenshots && !fetchMediainfo)"
              @click="fetchFromClient"
            >
              飞牛补充所选内容
            </el-button>
            <el-button
              type="primary"
              :loading="previewing"
              :disabled="busy || !hasChanges"
              @click="createPreview"
            >
              预览修改
            </el-button>
          </div>

          <section v-if="preview" class="preview-section" aria-labelledby="preview-heading">
            <div class="section-heading">
              <div>
                <h2 id="preview-heading">保存前预览</h2>
                <p>请核对 NovaHD 现有内容和本次修改。</p>
              </div>
              <el-tag type="warning" effect="plain">尚未保存</el-tag>
            </div>
            <div class="preview-grid">
              <article v-if="preview.screenshotBefore !== undefined" class="preview-card">
                <h3>截图</h3>
                <div class="preview-columns">
                  <div>
                    <strong>当前</strong>
                    <pre>{{ preview.screenshotBefore || '暂无内容' }}</pre>
                  </div>
                  <div>
                    <strong>修改后</strong>
                    <pre>{{ preview.screenshotAfter || '暂无内容' }}</pre>
                  </div>
                </div>
                <div
                  v-if="newScreenshotImages.length"
                  class="screenshot-gallery"
                  aria-label="新增截图预览"
                >
                  <a
                    v-for="imageUrl in newScreenshotImages"
                    :key="imageUrl"
                    :href="imageUrl"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <img
                      :src="imageUrl"
                      alt="新增截图"
                      loading="lazy"
                      referrerpolicy="no-referrer"
                    />
                  </a>
                </div>
              </article>
              <article v-if="preview.mediainfoBefore !== undefined" class="preview-card">
                <h3>MediaInfo</h3>
                <div class="preview-columns">
                  <div>
                    <strong>当前</strong>
                    <pre>{{ preview.mediainfoBefore || '暂无内容' }}</pre>
                  </div>
                  <div>
                    <strong>修改后</strong>
                    <pre>{{ preview.mediainfoAfter || '暂无内容' }}</pre>
                  </div>
                </div>
              </article>
              <article v-if="preview.descriptionBefore !== undefined" class="preview-card">
                <h3>简介完整内容</h3>
                <div class="preview-columns">
                  <div>
                    <strong>当前</strong>
                    <pre>{{ preview.descriptionBefore || '暂无内容' }}</pre>
                  </div>
                  <div>
                    <strong>修改后</strong>
                    <pre>{{ preview.descriptionAfter || '暂无内容' }}</pre>
                  </div>
                </div>
              </article>
            </div>
            <el-checkbox v-model="confirmed" class="confirm-checkbox">
              我已核对预览，确认将以上修改保存到 NovaHD
            </el-checkbox>
            <div class="edit-actions save-actions">
              <el-button :disabled="busy" @click="clearPreview">返回修改</el-button>
              <el-button
                type="primary"
                :loading="saving"
                :disabled="busy || !confirmed"
                @click="saveChanges"
              >
                确认并保存
              </el-button>
            </div>
          </section>
        </template>
      </template>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import axios from 'axios'
import { Refresh, TopRight } from '@element-plus/icons-vue'
import { ElMessage } from '@/utils/uiNotify'
import { safeTrackerResultUrl } from '@/utils/publishPresentation'

type PreviewData = {
  confirmation: string
  screenshotBefore?: string
  screenshotAfter?: string
  mediainfoBefore?: string
  mediainfoAfter?: string
  descriptionBefore?: string
  descriptionAfter?: string
}

type CoverPreviewData = {
  coverUrlBefore: string
  coverUrlAfter: string
  confirmation: string
}

const props = defineProps<{ modelValue: boolean; logId: number | string | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; saved: [] }>()
let editSession = 0

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => {
    if (!value) editSession++
    emit('update:modelValue', value)
  },
})
const busy = computed(
  () =>
    loadingDetails.value ||
    fetchingMedia.value ||
    previewing.value ||
    saving.value ||
    coverPreviewing.value ||
    coverSaving.value,
)

const loadingDetails = ref(false)
const fetchingMedia = ref(false)
const previewing = ref(false)
const saving = ref(false)
const coverPreviewing = ref(false)
const coverSaving = ref(false)
const detailsLoaded = ref(false)
const editMode = ref<'media' | 'cover'>('media')
const revision = ref('')
const detailsUrl = ref('')
const currentCoverUrl = ref('')
const currentCoverImage = computed(() => safePixhostImageUrl(currentCoverUrl.value))
const coverUrlDraft = ref('')
const existingScreenshotBBCode = ref('')
const screenshotDraft = ref('')
const mediainfo = ref('')
const originalMediainfo = ref('')
const fetchScreenshots = ref(true)
const fetchMediainfo = ref(true)
const preview = ref<PreviewData | null>(null)
const coverPreview = ref<CoverPreviewData | null>(null)
const confirmed = ref(false)
const coverConfirmed = ref(false)
const errorMessage = ref('')
const resultMessage = ref('')
const resultType = ref<'success' | 'warning'>('success')
const needsReconciliation = ref(false)

const isCurrentSession = (session: number) => session === editSession && visible.value

const hasChanges = computed(
  () =>
    screenshotDraft.value.trim() !== '' ||
    mediainfo.value.trim() !== originalMediainfo.value.trim(),
)
const newScreenshotImages = computed(() => {
  if (!preview.value) return []
  const before = new Set(extractPixhostImages(preview.value.screenshotBefore || ''))
  return extractPixhostImages(preview.value.screenshotAfter || '').filter((url) => !before.has(url))
})

const extractPixhostImages = (bbcode: string) => {
  const urls = new Set<string>()
  const imageTag = /\[img(?:=[^\]]*)?\]\s*(https?:\/\/[^\s\[]+?)\s*\[\/img\]/gi
  for (const match of bbcode.matchAll(imageTag)) {
    const rawUrl = match[1]?.trim()
    if (!rawUrl) continue
    try {
      const url = new URL(rawUrl)
      const allowedHost =
        url.hostname === 'pixhost.to' || /^(?:img|i|t)\d+\.pixhost\.to$/i.test(url.hostname)
      const allowedPath = /^\/(images|thumbs)\//i.test(url.pathname)
      if (url.protocol === 'https:' && allowedHost && allowedPath) urls.add(url.toString())
    } catch {
      continue
    }
  }
  return [...urls]
}

const endpoint = () =>
  `/api/publish_logs/${encodeURIComponent(String(props.logId || ''))}/published_edit`

const changedFields = () => {
  const fields: { screenshot_bbcode?: string; mediainfo?: string } = {}
  if (screenshotDraft.value.trim()) fields.screenshot_bbcode = screenshotDraft.value
  if (mediainfo.value.trim() !== originalMediainfo.value.trim()) fields.mediainfo = mediainfo.value
  return fields
}

const getResponseMessage = (error: unknown, fallback: string) => {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data as { message?: string; error?: string } | undefined
    return data?.message || data?.error || error.message || fallback
  }
  return error instanceof Error ? error.message : fallback
}

const loadDetails = async (session: number) => {
  if (!props.logId) return
  loadingDetails.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  needsReconciliation.value = false
  detailsLoaded.value = false
  preview.value = null
  coverPreview.value = null
  confirmed.value = false
  coverConfirmed.value = false
  try {
    const response = await axios.get(endpoint())
    if (!isCurrentSession(session)) return
    if (!response.data?.success) throw new Error(response.data?.message || '读取已发布资料失败')
    revision.value = String(response.data.revision || '')
    if (!revision.value) throw new Error('服务端没有返回资料版本标识，请刷新任务记录后重试')
    detailsUrl.value = safeTrackerResultUrl(response.data.details_url)
    currentCoverUrl.value =
      typeof response.data.cover_url === 'string' ? response.data.cover_url : ''
    coverUrlDraft.value = ''
    existingScreenshotBBCode.value = String(response.data.screenshot_bbcode || '')
    mediainfo.value = String(response.data.mediainfo || '')
    originalMediainfo.value = mediainfo.value
    detailsLoaded.value = true
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    errorMessage.value = getResponseMessage(error, '读取已发布资料失败')
  } finally {
    if (isCurrentSession(session)) loadingDetails.value = false
  }
}

const safePixhostImageUrl = (value: unknown): string | null => {
  if (typeof value !== 'string' || !value.trim()) return null
  try {
    const url = new URL(value.trim())
    const allowedHost =
      url.hostname === 'pixhost.to' || /^(?:img|i|t|s)\d+\.pixhost\.to$/i.test(url.hostname)
    const allowedPath = /^\/(?:images|thumbs)\//i.test(url.pathname)
    if (
      url.protocol !== 'https:' ||
      !allowedHost ||
      !allowedPath ||
      url.username ||
      url.password ||
      url.port
    )
      return null
    return url.toString()
  } catch {
    return null
  }
}

const invalidatePreview = () => {
  preview.value = null
  confirmed.value = false
  resultMessage.value = ''
}

const invalidateCoverPreview = () => {
  coverPreview.value = null
  coverConfirmed.value = false
  resultMessage.value = ''
}

const createCoverPreview = async () => {
  if (!props.logId || !revision.value || !currentCoverUrl.value || !coverUrlDraft.value.trim())
    return
  const session = editSession
  coverPreviewing.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  needsReconciliation.value = false
  coverConfirmed.value = false
  try {
    const response = await axios.post(`${endpoint()}/preview`, {
      revision: revision.value,
      cover_url: coverUrlDraft.value,
    })
    if (!isCurrentSession(session)) return
    if (!response.data?.success) throw new Error(response.data?.message || '解析封面失败')
    const coverUrlBefore = response.data.cover_url_before
    const coverUrlAfter = response.data.cover_url_after
    if (
      typeof coverUrlAfter !== 'string' ||
      !safePixhostImageUrl(coverUrlAfter) ||
      coverUrlBefore !== currentCoverUrl.value
    )
      throw new Error('无法核对原封面或解析后的原图，请重新读取站点资料')
    const confirmation = String(response.data.confirmation || '')
    if (!confirmation) throw new Error('服务端未返回确认凭据，请重新预览')
    coverPreview.value = { coverUrlBefore, coverUrlAfter, confirmation }
    if (coverUrlBefore === coverUrlAfter) {
      resultType.value = 'warning'
      resultMessage.value = '新旧封面相同，没有需要保存的变化。'
    }
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    errorMessage.value = getResponseMessage(error, '解析封面失败')
    coverPreview.value = null
  } finally {
    if (isCurrentSession(session)) coverPreviewing.value = false
  }
}

const saveCover = async () => {
  const activePreview = coverPreview.value
  if (
    !props.logId ||
    !revision.value ||
    !activePreview?.confirmation ||
    !coverConfirmed.value ||
    activePreview.coverUrlBefore === activePreview.coverUrlAfter
  )
    return
  const session = editSession
  coverSaving.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  try {
    const response = await axios.post(`${endpoint()}/save`, {
      revision: revision.value,
      cover_url: coverUrlDraft.value,
      resolved_cover_url: activePreview.coverUrlAfter,
      confirmation: activePreview.confirmation,
    })
    if (!isCurrentSession(session)) {
      if (response.data?.success && response.data?.status === 'verified') emit('saved')
      return
    }
    if (!response.data?.success || response.data?.status !== 'verified') {
      const status = String(response.data?.status || '')
      resultType.value = status === 'unknown' || status === 'conflict' ? 'warning' : 'success'
      resultMessage.value =
        response.data?.message || '封面保存结果尚未核实，请重新读取站点资料确认。'
      if (status === 'unknown' || status === 'conflict') coverPreview.value = null
      needsReconciliation.value = status === 'unknown' || status === 'conflict'
      return
    }
    resultType.value = 'success'
    resultMessage.value = response.data?.message || '站点已回读核实，封面更新成功。'
    ElMessage.success('NovaHD 封面已更新并核实')
    emit('saved')
    visible.value = false
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    const status = String(
      axios.isAxiosError(error)
        ? (error.response?.data as { status?: string } | undefined)?.status || ''
        : '',
    )
    resultType.value = 'warning'
    resultMessage.value =
      status === 'conflict'
        ? 'NovaHD 资料在预览后已变化，封面修改已拒绝保存。请重新读取资料后再次预览。'
        : `${getResponseMessage(error, '封面保存结果尚未核实')}。请重新读取 NovaHD 资料核对保存结果。`
    coverPreview.value = null
    coverConfirmed.value = false
    needsReconciliation.value = true
  } finally {
    if (isCurrentSession(session)) coverSaving.value = false
  }
}

const clearCoverPreview = () => invalidateCoverPreview()

const fetchFromClient = async () => {
  if (!props.logId || (!fetchScreenshots.value && !fetchMediainfo.value)) return
  const session = editSession
  fetchingMedia.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  invalidatePreview()
  try {
    const response = await axios.post(
      `${endpoint()}/media`,
      {
        screenshots: fetchScreenshots.value,
        mediainfo: fetchMediainfo.value,
      },
      { timeout: 300000 },
    )
    if (!isCurrentSession(session)) return
    if (!response.data?.success) throw new Error(response.data?.message || '飞牛补充失败')
    if (typeof response.data.screenshot_bbcode === 'string') {
      screenshotDraft.value = response.data.screenshot_bbcode
    }
    if (typeof response.data.mediainfo === 'string') {
      mediainfo.value = response.data.mediainfo
    }
    const fieldErrors = response.data.field_errors as Record<string, string> | undefined
    const errorSummary = fieldErrors
      ? Object.entries(fieldErrors)
          .map(
            ([field, message]) =>
              `${field === 'screenshot_bbcode' ? '截图' : field === 'mediainfo' ? 'MediaInfo' : field}：${message}`,
          )
          .join('；')
      : ''
    resultType.value = errorSummary ? 'warning' : 'success'
    resultMessage.value = errorSummary
      ? `部分内容未能获取，已保留可用结果和手动编辑内容。${errorSummary}`
      : '飞牛已返回所选内容。请检查后预览修改。'
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    errorMessage.value = getResponseMessage(error, '飞牛补充失败')
  } finally {
    if (isCurrentSession(session)) fetchingMedia.value = false
  }
}

const createPreview = async () => {
  if (!props.logId || !revision.value || !hasChanges.value) return
  const session = editSession
  previewing.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  needsReconciliation.value = false
  confirmed.value = false
  try {
    const response = await axios.post(`${endpoint()}/preview`, {
      revision: revision.value,
      ...changedFields(),
    })
    if (!isCurrentSession(session)) return
    if (!response.data?.success) throw new Error(response.data?.message || '生成预览失败')
    preview.value = {
      confirmation: String(response.data.confirmation || ''),
      screenshotBefore: response.data.screenshot_bbcode_before,
      screenshotAfter: response.data.screenshot_bbcode_after,
      mediainfoBefore: response.data.mediainfo_before,
      mediainfoAfter: response.data.mediainfo_after,
      descriptionBefore: response.data.before,
      descriptionAfter: response.data.after,
    }
    if (!preview.value.confirmation) throw new Error('服务端未返回确认凭据，请重新预览')
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    errorMessage.value = getResponseMessage(error, '生成预览失败')
    preview.value = null
  } finally {
    if (isCurrentSession(session)) previewing.value = false
  }
}

const clearPreview = () => invalidatePreview()

const saveChanges = async () => {
  if (!props.logId || !revision.value || !preview.value?.confirmation || !confirmed.value) return
  const session = editSession
  saving.value = true
  errorMessage.value = ''
  resultMessage.value = ''
  try {
    const response = await axios.post(`${endpoint()}/save`, {
      revision: revision.value,
      ...changedFields(),
      confirmation: preview.value.confirmation,
    })
    if (!isCurrentSession(session)) {
      if (response.data?.success && response.data?.status === 'verified') emit('saved')
      return
    }
    if (!response.data?.success || response.data?.status !== 'verified') {
      const status = String(response.data?.status || '')
      resultType.value = status === 'unknown' || status === 'conflict' ? 'warning' : 'success'
      resultMessage.value = response.data?.message || '保存结果尚未核实，请重新读取站点资料确认。'
      if (status === 'unknown' || status === 'conflict') preview.value = null
      needsReconciliation.value = status === 'unknown' || status === 'conflict'
      return
    }
    resultType.value = 'success'
    resultMessage.value = response.data?.message || '站点已回读核实，修改保存成功。'
    ElMessage.success('NovaHD 资料已更新并核实')
    emit('saved')
    visible.value = false
  } catch (error: unknown) {
    if (!isCurrentSession(session)) return
    const status = String(
      axios.isAxiosError(error)
        ? (error.response?.data as { status?: string } | undefined)?.status || ''
        : '',
    )
    resultType.value = 'warning'
    resultMessage.value =
      status === 'conflict'
        ? 'NovaHD 资料在预览后已变化，当前修改已拒绝保存。请重新读取资料后再次预览。'
        : `${getResponseMessage(error, '保存结果尚未核实')}。请重新读取 NovaHD 资料核对保存结果。`
    preview.value = null
    confirmed.value = false
    needsReconciliation.value = true
  } finally {
    if (isCurrentSession(session)) saving.value = false
  }
}

const reconcileFromSite = async () => {
  const session = editSession
  await loadDetails(session)
  if (!isCurrentSession(session)) return
  if (detailsLoaded.value) {
    resultType.value = 'warning'
    resultMessage.value = '已重新读取 NovaHD 当前资料。请核对内容后，再开始新的预览。'
    needsReconciliation.value = false
  } else {
    needsReconciliation.value = true
  }
}

const restoreMediainfo = () => {
  mediainfo.value = originalMediainfo.value
  invalidatePreview()
}

const reset = () => {
  loadingDetails.value = false
  fetchingMedia.value = false
  previewing.value = false
  saving.value = false
  coverPreviewing.value = false
  coverSaving.value = false
  detailsLoaded.value = false
  editMode.value = 'media'
  revision.value = ''
  detailsUrl.value = ''
  currentCoverUrl.value = ''
  coverUrlDraft.value = ''
  existingScreenshotBBCode.value = ''
  screenshotDraft.value = ''
  mediainfo.value = ''
  originalMediainfo.value = ''
  fetchScreenshots.value = true
  fetchMediainfo.value = true
  preview.value = null
  coverPreview.value = null
  confirmed.value = false
  coverConfirmed.value = false
  errorMessage.value = ''
  resultMessage.value = ''
  needsReconciliation.value = false
}

watch(
  () => [visible.value, props.logId] as const,
  ([isVisible]) => {
    const session = ++editSession
    if (isVisible) {
      void loadDetails(session)
    } else {
      reset()
    }
  },
)
</script>

<style scoped>
.edit-content {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
  min-height: 140px;
}

.edit-intro,
.section-heading p {
  margin: 0;
  color: var(--el-text-color-secondary);
  line-height: 1.55;
}

.mediainfo-controls {
  display: flex;
  align-items: center;
  gap: 10px;
}

.target-link-row,
.section-heading,
.edit-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.edit-mode-switch {
  align-self: flex-start;
  max-width: 100%;
}

.cover-section,
.cover-confirm-panel {
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.cover-preview-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.cover-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--star-border);
  border-radius: 10px;
  background: var(--star-surface-muted);
}

.cover-card h3 {
  margin: 0;
  font-size: 14px;
}

.cover-card img {
  display: block;
  width: 100%;
  max-height: 320px;
  aspect-ratio: 16 / 9;
  object-fit: contain;
  border-radius: 6px;
  background: var(--el-fill-color-light);
}

.cover-card code {
  overflow-wrap: anywhere;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.cover-help {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.target-link-row {
  padding: 10px 12px;
  border: 1px solid var(--star-border);
  border-radius: 10px;
  background: var(--star-surface-muted);
  color: var(--el-text-color-secondary);
}

.target-link-row a {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--star-accent);
}

.edit-section,
.preview-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.section-heading h2,
.preview-card h3 {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 650;
}

.preview-section {
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.preview-grid {
  display: grid;
  gap: 12px;
}

.preview-card {
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--star-border);
  border-radius: 10px;
  background: var(--star-surface-muted);
}

.screenshot-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 8px;
  margin-top: 14px;
}

.screenshot-gallery a {
  display: block;
  overflow: hidden;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  background: var(--el-fill-color-light);
}

.screenshot-gallery img {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
}

.preview-columns {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.preview-columns > div {
  min-width: 0;
}

.preview-columns strong {
  display: block;
  margin-bottom: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.preview-columns pre {
  max-height: 240px;
  margin: 0;
  overflow: auto;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  color: var(--el-text-color-primary);
  font: 12px/1.55 var(--el-font-family);
}

.confirm-checkbox {
  align-self: flex-start;
  align-items: flex-start;
  height: auto;
  min-width: 0;
  max-width: 100%;
  margin-right: 0;
}

.confirm-checkbox :deep(.el-checkbox__input) {
  flex: none;
  margin-top: 3px;
}

.confirm-checkbox :deep(.el-checkbox__label) {
  min-width: 0;
  line-height: 1.45;
  white-space: normal;
  overflow-wrap: anywhere;
}

.save-actions {
  justify-content: flex-end;
}

.reconcile-actions {
  display: flex;
  justify-content: flex-start;
}

@media (max-width: 640px) {
  .target-link-row,
  .section-heading,
  .edit-actions {
    align-items: flex-start;
    flex-direction: column;
  }

  .cover-preview-grid {
    grid-template-columns: 1fr;
  }

  .mediainfo-controls {
    flex-wrap: wrap;
  }

  .edit-mode-switch :deep(.el-radio-button__inner) {
    padding-inline: 10px;
    white-space: normal;
  }

  .edit-actions :deep(.el-button) {
    width: 100%;
    margin: 0;
  }

  .preview-columns {
    grid-template-columns: 1fr;
  }
}
</style>
