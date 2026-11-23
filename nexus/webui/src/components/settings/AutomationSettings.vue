<template>
  <section class="settings-page" v-loading="loading">
    <header class="page-heading">
      <div>
        <h2 class="settings-section-title">自动下载与跨站管理</h2>
        <p>规则默认关闭。预览只读取候选和限制状态；确认后才会将选中项目加入下载队列。</p>
      </div>
      <el-tag type="info">{{ quotaLabel }}</el-tag>
    </header>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
    <el-card shadow="never" class="panel">
      <template #header
        ><div class="card-head">
          <strong>自动化规则</strong
          ><el-switch
            v-model="form.enabled"
            aria-label="启用自动化模块"
            active-text="启用"
            inactive-text="停用新任务"
          /></div
      ></template>
      <el-form :model="form" label-position="top" class="form-grid">
        <el-form-item label="源站标识"
          ><el-select v-model="form.source_site" filterable placeholder="选择已有站点"
            ><el-option
              v-for="site in sites"
              :key="site.id"
              :label="`${site.name} (${site.id})`"
              :value="site.id" /></el-select
        ></el-form-item>
        <el-form-item label="目标站标识"
          ><el-select v-model="form.target_site" filterable placeholder="选择已有站点"
            ><el-option
              v-for="site in sites"
              :key="site.id"
              :label="`${site.name} (${site.id})`"
              :value="site.id" /></el-select
        ></el-form-item>
        <el-form-item label="下载器"
          ><el-select v-model="form.downloader_id" clearable placeholder="选择已配置下载器"
            ><el-option
              v-for="d in downloaders"
              :key="d.id"
              :label="d.name || d.id"
              :value="d.id" /></el-select
        ></el-form-item>
        <el-form-item label="保存目录"
          ><el-input v-model="form.save_path" placeholder="下载器可访问的路径"
        /></el-form-item>
        <el-form-item label="月度计划"
          ><el-switch v-model="form.schedule_enabled" /><span class="hint"
            >启用模块后按上海时区设置执行月度扫描</span
          ></el-form-item
        >
        <el-form-item label="每月日期"
          ><el-input-number v-model="form.schedule_day" :min="1" :max="28"
        /></el-form-item>
        <el-form-item label="执行时间"
          ><el-time-picker v-model="form.schedule_time" format="HH:mm" value-format="HH:mm"
        /></el-form-item>
        <el-form-item label="每月成功发种数上限"
          ><el-input-number v-model="form.monthly_limit" :min="1" :max="100"
        /></el-form-item>
        <el-form-item label="单种体积范围 (GiB)"
          ><div class="range">
            <el-input-number v-model="form.min_size_gib" :min="0.1" :precision="1" /><span>至</span
            ><el-input-number v-model="form.max_size_gib" :min="0.1" :precision="1" /></div
        ></el-form-item>
        <template v-if="isZmptSource">
          <el-form-item label="每批下载数量">
            <el-input-number v-model="form.batch_size" :min="1" :max="100" />
            <span class="hint">本批下载完成后统一用 IYUU 查重，名额不足时按剩余数量下载。</span>
          </el-form-item>
          <el-form-item label="IYUU 确认重复时">
            <el-select v-model="form.duplicate_action">
              <el-option label="保留下载" value="keep" />
              <el-option label="移到指定目录" value="move" />
              <el-option label="删除 qB 任务，保留文件" value="delete" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="form.duplicate_action === 'move'" label="重复种子目录">
            <el-input v-model="form.duplicate_directory" placeholder="下载器内的绝对路径" />
            <span class="hint">qB 会将每个重复下载移到该目录下的独立子目录。</span>
          </el-form-item>
        </template>
        <el-form-item label="最少做种人数"
          ><el-input-number v-model="form.min_seeders" :min="0"
        /></el-form-item>
        <el-form-item label="最少下载人数"
          ><el-input-number v-model="minLeechers" :min="0"
        /></el-form-item>
        <el-form-item label="队列推进"
          ><el-tag type="info">顺序检查；下载并发由 qB 控制</el-tag></el-form-item
        >
        <el-form-item label="磁盘预留空间 (GiB)"
          ><el-input-number v-model="reserveSpace" :min="0" :precision="1" /><span class="hint"
            >仅在保存目录与 qBittorrent 默认目录一致时可验证；其他路径按未知处理。</span
          ></el-form-item
        >
        <el-form-item label="自动发布"
          ><el-switch
            v-model="form.auto_publish"
            active-text="启用自动发布"
            inactive-text="关闭"
          /><span class="hint"
            >默认关闭；发布会产生外部影响。开启前确认站点和账号权限。</span
          ></el-form-item
        >
      </el-form>
      <div class="policy-summary" role="note" aria-label="固定筛选规则">
        <div class="policy-summary__heading">
          <strong>固定筛选规则</strong>
          <el-tag type="success" effect="plain">必须全部满足</el-tag>
        </div>
        <ul>
          <li>源站只选官方种子，并按完整种子体积排序后匹配大小范围。</li>
          <li>禁转、限制转载、分集标签或已下载的资源会跳过。</li>
          <li v-if="isZmptSource">
            下载前检查织梦「辅种」栏，下载完成后按批用 IYUU 复查。需要在 IYUU 设置中配置令牌。
          </li>
          <li v-else>下载和发布前核对目标站。已有对应种子会跳过，结果不明时等待核对。</li>
        </ul>
        <p>这些条件由服务端强制执行，不能在页面或旧版 API 字段中关闭。</p>
      </div>
      <div class="actions">
        <el-button type="primary" :loading="saving" @click="save">保存规则</el-button
        ><el-button :loading="previewing" @click="preview">预览候选</el-button
        ><el-button type="success" :disabled="!form.enabled" :loading="running" @click="run"
          >运行计划</el-button
        >
      </div>
      <el-alert
        title="自动发布开启后，每条种子在文件完整、资料齐备并再次通过查重后独立发布。目标站返回已存在时不计发布成功。"
        type="warning"
        :closable="false"
        show-icon
      />
    </el-card>
    <el-card v-if="hasPreview" shadow="never" class="panel"
      ><template #header
        ><div class="card-head">
          <strong>候选预览</strong
          ><el-button
            size="small"
            :disabled="!selected.length || previewSitesMismatch || running"
            :loading="running"
            @click="enqueueSelected"
            >将选中项目加入队列</el-button
          >
        </div></template
      >
      <div class="preview-meta">
        最近成功预览：{{ formatPreviewTime(previewedAt) }} · 入队按已保存规则复核
      </div>
      <el-alert
        v-if="previewRulesChanged"
        title="当前部分筛选规则与预览时不同，候选仅供参考；加入队列时服务端仍会按当前规则复核。"
        type="warning"
        :closable="false"
        show-icon
      />
      <el-alert
        v-if="previewSitesMismatch"
        title="候选预览的源站或目标站与当前已保存站点不同。请保存规则并重新预览后再加入队列。"
        type="error"
        :closable="false"
        show-icon
      />
      <el-empty v-if="!candidates.length" description="上次成功预览没有符合条件的候选" />
      <el-table
        v-else
        :key="previewedAt ?? 'unknown'"
        row-key="id"
        :data="candidates"
        @selection-change="(rows: Candidate[]) => (selected = rows)"
        ><el-table-column
          type="selection"
          width="48"
          reserve-selection
          :selectable="(row: Candidate) => row.eligible"
        /><el-table-column label="候选" min-width="220"
          ><template #default="s">
            <div class="candidate-title">{{ s.row.title }}</div>
            <a
              v-if="safeDetailUrl(s.row.detail_url)"
              class="candidate-detail-link"
              :href="safeDetailUrl(s.row.detail_url)"
              :aria-label="`在新标签页打开 ${s.row.title || `#${s.row.id}`} 的种子详情`"
              target="_blank"
              rel="noopener noreferrer"
              @click.stop
              >详情</a
            >
            <div class="candidate-badges">
              <el-tag v-if="s.row.official" size="small" type="success" effect="plain">官方</el-tag>
              <el-tag v-if="s.row.episode" size="small" type="warning" effect="plain">分集</el-tag>
              <el-tag v-if="s.row.previously_downloaded" size="small" type="info" effect="plain"
                >已下载</el-tag
              >
            </div>
          </template></el-table-column
        ><el-table-column label="大小" width="100"
          ><template #default="s">{{ formatSize(s.row.size) }}</template></el-table-column
        ><el-table-column prop="seeders" label="做种" width="80" /><el-table-column
          label="资格"
          width="120"
          ><template #default="s"
            ><el-tag :type="candidateStatus(s.row).type">{{
              candidateStatus(s.row).label
            }}</el-tag></template
          ></el-table-column
        ><el-table-column label="原因" min-width="200"
          ><template #default="s">{{ s.row.reasons?.join('；') || '—' }}</template></el-table-column
        ></el-table
      ></el-card
    >
    <el-card shadow="never" class="panel"
      ><template #header>
        <div class="card-head">
          <strong>任务与运行记录</strong>
          <el-button size="small" :loading="refreshingRecords" @click="refreshRecords"
            >刷新记录</el-button
          >
        </div> </template
      ><el-tabs
        ><el-tab-pane label="下载任务"
          ><el-table :data="tasks" empty-text="暂无任务"
            ><el-table-column prop="title" label="种子" min-width="150" /><el-table-column
              prop="source"
              label="源站"
              width="90"
            /><el-table-column prop="target" label="目标站" width="90" /><el-table-column
              label="状态"
              width="140"
              ><template #default="s"
                ><el-tag :type="taskStatus(s.row).type" effect="plain">{{
                  taskStatus(s.row).label
                }}</el-tag></template
              ></el-table-column
            ><el-table-column prop="message" label="进度 / 信息" min-width="160" /><el-table-column
              label="操作"
              width="230"
              ><template #default="s"
                ><el-button v-if="canCancel(s.row)" link type="danger" @click="act(s.row, 'cancel')"
                  >取消</el-button
                ><el-button v-if="canRetry(s.row)" link @click="act(s.row, 'retry')">{{
                  s.row.state === 'waiting_target' ? '重新核对' : '重试'
                }}</el-button
                ><el-button v-if="canReconcile(s.row)" link @click="reconcile(s.row)"
                  >提交核对链接</el-button
                ><el-link
                  v-if="s.row.result_url"
                  :href="s.row.result_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  >目标详情</el-link
                ></template
              ></el-table-column
            ></el-table
          ></el-tab-pane
        ><el-tab-pane label="运行记录"
          ><el-table :data="runs" empty-text="暂无运行"
            ><el-table-column prop="id" label="运行 ID" /><el-table-column
              prop="status"
              label="状态" /><el-table-column prop="message" label="信息" /><el-table-column
              prop="created_at"
              label="开始时间" /></el-table></el-tab-pane></el-tabs
    ></el-card>
  </section>
</template>
<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
type Site = { id: string; name: string; baseURL: string }
type Candidate = {
  id: string
  title: string
  detail_url?: string
  size: number
  seeders: number
  official?: boolean
  episode?: boolean
  previously_downloaded?: boolean
  duplicate_known?: boolean
  duplicate?: boolean
  eligible: boolean
  reasons: string[]
}
type PreviewSnapshot = {
  candidates: Candidate[]
  settings?: Record<string, unknown>
  previewed_at?: number
  source_digest?: string | null
  target_digest?: string | null
}
type AutomationTask = {
  id: string
  state: string
  publish_attempted?: boolean
  result_url?: string
}
type StatusBadge = {
  label: string
  type: 'primary' | 'success' | 'warning' | 'info' | 'danger'
}
type Row = Record<string, unknown>
const safeDetailUrl = (value?: string): string => {
  if (!value) return ''
  try {
    const url = new URL(value)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return ''
    return url.href
  } catch {
    return ''
  }
}
const candidateStatus = (candidate: Candidate): StatusBadge => {
  if (candidate.eligible) return { label: '可处理', type: 'success' }
  if (candidate.duplicate_known && candidate.duplicate) return { label: '目标已存在', type: 'info' }
  if (candidate.reasons?.includes('目标查重未知')) return { label: '查重待核对', type: 'warning' }
  return { label: '跳过', type: 'info' }
}
const taskBadges: Record<string, StatusBadge> = {
  reserved: { label: '待下载', type: 'info' },
  downloading: { label: '下载中', type: 'primary' },
  preparing: { label: '准备资料', type: 'primary' },
  waiting_batch: { label: '等待 IYUU 批量查重', type: 'warning' },
  duplicate_cleanup: { label: '处理重复下载', type: 'info' },
  waiting_media: { label: '待补资料', type: 'warning' },
  waiting_target: { label: '查重待核对', type: 'warning' },
  ready: { label: '资料已齐', type: 'info' },
  queued_publish: { label: '待发布', type: 'info' },
  publishing: { label: '发布中', type: 'primary' },
  published: { label: '已发布待做种', type: 'primary' },
  seeding: { label: '做种校验中', type: 'primary' },
  reconciling: { label: '正在核对', type: 'primary' },
  succeeded: { label: '已发布并做种', type: 'success' },
  already_exists: { label: '已存在', type: 'info' },
  needs_review: { label: '结果待核对', type: 'warning' },
  cancelled: { label: '已取消', type: 'info' },
  failed: { label: '失败', type: 'danger' },
}
const taskStatus = (task: AutomationTask): StatusBadge =>
  taskBadges[task.state] ?? { label: task.state || '未知', type: 'info' }
const cancellableStates = new Set([
  'reserved',
  'downloading',
  'waiting_batch',
  'preparing',
  'waiting_media',
  'waiting_target',
  'ready',
  'queued_publish',
  'needs_review',
  'failed',
])
const canCancel = (task: AutomationTask) =>
  !task.publish_attempted && cancellableStates.has(task.state)
const canRetry = (task: AutomationTask) =>
  !task.publish_attempted && (task.state === 'waiting_target' || task.state === 'failed')
const canReconcile = (task: AutomationTask) =>
  task.publish_attempted === true &&
  ['needs_review', 'published', 'seeding', 'reconciling'].includes(task.state)
const loading = ref(false),
  refreshingRecords = ref(false),
  saving = ref(false),
  previewing = ref(false),
  running = ref(false),
  error = ref(''),
  sites = ref<Site[]>([]),
  downloaders = ref<{ id: string; name: string }[]>([]),
  candidates = ref<Candidate[]>([]),
  hasPreview = ref(false),
  previewedAt = ref<number | null>(null),
  previewSettings = ref<Record<string, unknown> | null>(null),
  previewSourceDigest = ref<string | null | undefined>(undefined),
  previewTargetDigest = ref<string | null | undefined>(undefined),
  savedSourceSite = ref(''),
  savedTargetSite = ref(''),
  selected = ref<Candidate[]>([]),
  tasks = ref<AutomationTask[]>([]),
  runs = ref<Row[]>([]),
  quota = ref<Record<string, unknown>>({})
let previewRequestId = 0
const form = reactive({
  enabled: false,
  auto_publish: false,
  schedule_enabled: false,
  schedule_day: 1,
  schedule_time: '02:00',
  timezone: 'Asia/Shanghai',
  monthly_limit: 6,
  batch_size: 20,
  duplicate_action: 'keep',
  duplicate_directory: '',
  min_size_gib: 1,
  max_size_gib: 10,
  min_seeders: 3,
  prefer_official: true,
  target_absent_priority: true,
  source_site: '',
  target_site: '',
  downloader_id: '',
  save_path: '',
})
const minLeechers = ref(0),
  reserveSpace = ref(0)
const isZmptSource = computed(() => {
  const source = sites.value.find((site) => site.id === form.source_site)
  if (!source?.baseURL) return form.source_site === 'zmpt'
  try {
    const url = new URL(source.baseURL)
    return url.protocol === 'https:' && url.host === 'zmpt.cc'
  } catch {
    return false
  }
})
const formatSize = (value: unknown) => {
  const bytes = Number(value)
  if (!Number.isFinite(bytes) || bytes <= 0) return '未知'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const amount = bytes / 1024 ** index
  return `${amount.toFixed(index >= 3 ? 2 : 1)} ${units[index]}`
}
const formatPreviewTime = (value: number | null) => {
  if (!value || !Number.isFinite(value)) return '时间未知'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}
const previewSnapshotKey = (preview: PreviewSnapshot) =>
  JSON.stringify({
    candidates: preview.candidates,
    settings: preview.settings ?? null,
    source_digest: preview.source_digest,
    target_digest: preview.target_digest,
  })
const applyPreview = (preview: PreviewSnapshot | null) => {
  hasPreview.value = preview !== null
  candidates.value = preview?.candidates || []
  previewedAt.value = preview?.previewed_at || null
  previewSettings.value = preview?.settings || null
  previewSourceDigest.value = preview?.source_digest
  previewTargetDigest.value = preview?.target_digest
  selected.value = []
}
const restorePreview = async (allowDuringPreview = false) => {
  if (previewing.value && !allowDuringPreview) return false
  const requestId = ++previewRequestId
  try {
    const response = await axios.get('/api/automation/preview')
    if (requestId !== previewRequestId || response.data?.success !== true) return false
    const preview = response.data.preview
    if (preview === null) {
      applyPreview(null)
      return true
    }
    if (!preview || !Array.isArray(preview.candidates)) return false
    const previewTime = Number(preview.previewed_at) || undefined
    const snapshot: PreviewSnapshot = {
      candidates: preview.candidates,
      settings: preview.settings,
      previewed_at: previewTime,
      source_digest: preview.source_digest,
      target_digest: preview.target_digest,
    }
    if (
      hasPreview.value &&
      previewTime !== undefined &&
      previewTime === previewedAt.value &&
      previewSnapshotKey(snapshot) ===
        previewSnapshotKey({
          candidates: candidates.value,
          settings: previewSettings.value ?? undefined,
          previewed_at: previewedAt.value ?? undefined,
          source_digest: previewSourceDigest.value,
          target_digest: previewTargetDigest.value,
        })
    )
      return true
    applyPreview(snapshot)
    return true
  } catch {
    // A failed refresh keeps the last successfully loaded preview visible.
    return false
  }
}
const quotaLabel = computed(
  () =>
    `本月已发布 ${quota.value.published ?? 0} / ${form.monthly_limit} · 占用名额 ${quota.value.pending ?? 0}`,
)
const load = async () => {
  loading.value = true
  error.value = ''
  void restorePreview()
  try {
    const [s, t, r, p, d] = await Promise.all([
      axios.get('/api/automation/settings'),
      axios.get('/api/automation/tasks'),
      axios.get('/api/automation/runs'),
      axios.get('/api/site-profiles'),
      axios.get('/api/settings'),
    ])
    Object.assign(form, s.data.settings || {})
    savedSourceSite.value = String(s.data.settings?.source_site ?? '')
    savedTargetSite.value = String(s.data.settings?.target_site ?? '')
    minLeechers.value = s.data.settings?.min_leechers ?? 0
    reserveSpace.value = s.data.settings?.reserve_space_gib ?? 0
    quota.value = s.data.quota || {}
    tasks.value = t.data.tasks || []
    runs.value = r.data.runs || []
    sites.value = (p.data.sites || []).map((x: Record<string, unknown>) => ({
      id: String(x.id ?? ''),
      name: String(x.name ?? x.id ?? ''),
      baseURL: String(x.base_url ?? ''),
    }))
    downloaders.value = (d.data.downloaders || []).map((x: Record<string, unknown>) => ({
      id: String(x.id ?? ''),
      name: String(x.name ?? x.id ?? ''),
    }))
  } catch (e) {
    error.value = axios.isAxiosError(e)
      ? e.response?.data?.message || e.response?.data?.error || e.message
      : '加载设置失败'
  } finally {
    loading.value = false
  }
}
const refreshRecords = async () => {
  refreshingRecords.value = true
  try {
    const [t, r] = await Promise.all([
      axios.get('/api/automation/tasks'),
      axios.get('/api/automation/runs'),
    ])
    tasks.value = t.data.tasks || []
    runs.value = r.data.runs || []
    quota.value = t.data.quota || quota.value
    void restorePreview()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '刷新记录失败',
    )
  } finally {
    refreshingRecords.value = false
  }
}
const payload = () => ({
  ...form,
  min_leechers: minLeechers.value,
  reserve_space_gib: reserveSpace.value,
})
const previewRulesChanged = computed(() => {
  const previous = previewSettings.value
  if (!previous) return false
  const current = {
    source_site: form.source_site,
    target_site: form.target_site,
    min_size_gib: form.min_size_gib,
    max_size_gib: form.max_size_gib,
    min_seeders: form.min_seeders,
    min_leechers: minLeechers.value,
    monthly_limit: form.monthly_limit,
    downloader_id: form.downloader_id,
  }
  return Object.entries(current).some(([key, value]) => {
    const oldValue = previous[key]
    if (oldValue === undefined || oldValue === null) return false
    return String(oldValue) !== String(value)
  })
})
const previewSitesMismatch = computed(() => {
  const previous = previewSettings.value
  if (!previous?.source_site || !previous?.target_site) return true
  return (
    String(previous.source_site) !== savedSourceSite.value ||
    String(previous.target_site) !== savedTargetSite.value
  )
})
const save = async () => {
  if (!form.source_site || !form.target_site || form.source_site === form.target_site)
    return ElMessage.warning('请选择不同的源站和目标站')
  if (form.enabled && (!form.downloader_id || !form.save_path.trim()))
    return ElMessage.warning('启用模块前请选择下载器并填写保存目录')
  if (form.min_size_gib <= 0 || form.max_size_gib <= 0)
    return ElMessage.warning('单种体积范围必须大于零')
  if (form.min_size_gib > form.max_size_gib) return ElMessage.warning('最小体积不能大于最大体积')
  if (isZmptSource.value && form.duplicate_action === 'move' && !form.duplicate_directory.trim())
    return ElMessage.warning('请填写下载器内的重复种子目录')
  saving.value = true
  try {
    await axios.put('/api/automation/settings', payload())
    ElMessage.success('规则已保存')
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '保存失败',
    )
  } finally {
    saving.value = false
  }
}
const preview = async () => {
  previewing.value = true
  const requestId = ++previewRequestId
  try {
    const r = await axios.post('/api/automation/preview', payload())
    if (requestId !== previewRequestId) return
    if (r.data?.success === false || !Array.isArray(r.data?.candidates))
      throw new Error(r.data?.message || '预览结果无效')
    if (r.data.preview_saved === false) {
      const restored = await restorePreview(true)
      ElMessage.info(
        restored
          ? '预览已完成，但有更新的结果先保存；已加载最新保存的候选。'
          : '预览已完成，但没有保存为最新结果；保留当前显示的候选。',
      )
      return
    }
    const rows = r.data.candidates
    applyPreview({
      candidates: rows,
      settings: r.data.settings,
      previewed_at: Number(r.data.previewed_at) || Date.now(),
      source_digest: r.data.source_digest,
      target_digest: r.data.target_digest,
    })
    ElMessage.success(`预览完成，共 ${rows.length} 个候选`)
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '预览失败',
    )
  } finally {
    previewing.value = false
  }
}
const runRequest = async (
  ids?: string[],
  previewSites?: {
    source_site: string
    target_site: string
    source_digest?: string | null
    target_digest?: string | null
  },
) => {
  running.value = true
  try {
    const r = await axios.post(
      '/api/automation/runs',
      ids
        ? {
            selected_ids: ids,
            enqueue: true,
            preview_source_site: previewSites?.source_site,
            preview_target_site: previewSites?.target_site,
            preview_source_digest: previewSites?.source_digest,
            preview_target_digest: previewSites?.target_digest,
          }
        : {},
    )
    tasks.value = [...(r.data.tasks || []), ...tasks.value]
    runs.value = [r.data.run, ...runs.value].filter(Boolean)
    ElMessage.success(r.data.run?.message || '已提交')
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '提交失败',
    )
  } finally {
    running.value = false
  }
}
const run = () => runRequest()
const enqueueSelected = () => {
  const snapshotSettings = previewSettings.value
  if (running.value || !selected.value.length || previewSitesMismatch.value || !snapshotSettings)
    return
  return runRequest(
    selected.value.map((x) => x.id),
    {
      source_site: String(snapshotSettings.source_site),
      target_site: String(snapshotSettings.target_site),
      source_digest: previewSourceDigest.value,
      target_digest: previewTargetDigest.value,
    },
  )
}
const act = async (task: AutomationTask, action: 'cancel' | 'retry') => {
  try {
    await axios.post(`/api/automation/tasks/${String(task.id ?? '')}/${action}`)
    ElMessage.success(
      action === 'cancel'
        ? '已取消'
        : task.state === 'waiting_target'
          ? '已请求核对'
          : '已请求重试',
    )
    await refreshRecords()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '操作失败',
    )
  }
}
const reconcile = async (task: AutomationTask) => {
  try {
    const result = await ElMessageBox.prompt('粘贴目标站详情页链接用于人工对账', '对账链接', {
      inputPattern: /^https:\/\//,
      inputErrorMessage: '请输入 HTTPS 链接',
    })
    const value =
      typeof result === 'string'
        ? result
        : typeof result === 'object' && result !== null && 'value' in result
          ? String(result.value ?? '')
          : ''
    if (!value) return ElMessage.warning('请输入目标站详情页链接')
    await axios.post(`/api/automation/tasks/${String(task.id ?? '')}/reconcile`, { url: value })
    ElMessage.success('已登记核对')
    await refreshRecords()
  } catch (e) {
    if (e !== 'cancel')
      ElMessage.error(
        axios.isAxiosError(e)
          ? e.response?.data?.message || e.response?.data?.error || e.message
          : '对账失败',
      )
  }
}
onMounted(load)
</script>
<style scoped>
.settings-page {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 14px;
}

.page-heading,
.card-head,
.actions {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}

.page-heading p,
.hint {
  margin: 4px 0 0;
  color: var(--star-muted);
  font-size: 0.8125rem;
  line-height: 1.55;
}

.hint {
  flex-basis: 100%;
}

.panel {
  min-width: 0;
  border-radius: 12px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 205px), 1fr));
  align-items: start;
  gap: 0 14px;
}

.form-grid :deep(.el-select),
.form-grid :deep(.el-date-editor) {
  width: 100%;
}

.range {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 6px;
}

.range :deep(.el-input-number) {
  width: 100%;
}

.actions {
  justify-content: flex-start;
  margin-bottom: 14px;
}

.actions > .el-button,
.card-head > .el-button {
  margin-left: 0;
}

.policy-summary {
  margin: 2px 0 16px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-lighter);
  color: var(--el-text-color-regular);
  line-height: 1.6;
}

.policy-summary__heading,
.candidate-badges {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.policy-summary ul {
  margin: 10px 0 6px;
  padding-left: 20px;
}

.policy-summary p {
  margin: 0;
  color: var(--star-muted);
  font-size: 0.8125rem;
}

.candidate-title {
  overflow-wrap: anywhere;
}

.candidate-detail-link {
  display: inline-block;
  margin-top: 3px;
  color: var(--el-color-primary);
  font-size: 0.8125rem;
  text-decoration: none;
}

.candidate-detail-link:hover {
  text-decoration: underline;
}

.candidate-badges {
  margin-top: 5px;
}

@media (max-width: 600px) {
  .page-heading > .el-tag {
    height: auto;
    line-height: 1.4;
    white-space: normal;
  }

  .panel :deep(.el-card__body),
  .panel :deep(.el-card__header) {
    padding-inline: 12px;
  }
}
</style>
