<template>
  <main class="settings-page" v-loading="loading">
    <header class="page-heading">
      <div>
        <h1>自动下载与跨站管理</h1>
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
        <el-form-item label="月度任务数上限"
          ><el-input-number v-model="form.monthly_limit" :min="1" :max="100"
        /></el-form-item>
        <el-form-item label="单种体积范围 (GiB)"
          ><div class="range">
            <el-input-number v-model="form.min_size_gib" :min="0.1" :precision="1" /><span>至</span
            ><el-input-number v-model="form.max_size_gib" :min="0.1" :precision="1" /></div
        ></el-form-item>
        <el-form-item label="月度体积预算 (GiB)"
          ><el-input-number v-model="form.monthly_budget_gib" :min="0.1" :precision="1"
        /></el-form-item>
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
        <el-form-item label="官方组策略"
          ><el-switch v-model="form.prefer_official" active-text="优先官方组"
        /></el-form-item>
        <el-form-item label="目标缺失偏好"
          ><el-switch v-model="form.target_absent_priority" active-text="优先目标站尚无种子"
        /></el-form-item>
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
      <div class="actions">
        <el-button type="primary" :loading="saving" @click="save">保存规则</el-button
        ><el-button :loading="previewing" @click="preview">预览候选</el-button
        ><el-button type="success" :disabled="!form.enabled" :loading="running" @click="run"
          >运行计划</el-button
        >
      </div>
      <el-alert
        title="发布策略：此规则不自动发布。目标限制未知或重复检查未知时，服务端应拒绝执行；请在任务详情中确认状态。"
        type="warning"
        :closable="false"
        show-icon
      />
    </el-card>
    <el-card v-if="candidates.length" shadow="never" class="panel"
      ><template #header
        ><div class="card-head">
          <strong>候选预览</strong
          ><el-button
            size="small"
            :disabled="!selected.length"
            :loading="running"
            @click="enqueueSelected"
            >将选中项目加入队列</el-button
          >
        </div></template
      ><el-table :data="candidates" @selection-change="(rows: Candidate[]) => (selected = rows)"
        ><el-table-column type="selection" width="48" /><el-table-column
          prop="title"
          label="候选"
          min-width="180"
        /><el-table-column prop="size" label="大小" width="90" /><el-table-column
          prop="seeders"
          label="做种"
          width="80"
        /><el-table-column label="资格" width="100"
          ><template #default="s"
            ><el-tag :type="s.row.eligible ? 'success' : 'danger'">{{
              s.row.eligible ? '可处理' : '跳过'
            }}</el-tag></template
          ></el-table-column
        ><el-table-column label="原因" min-width="200"
          ><template #default="s">{{ s.row.reasons?.join('；') || '—' }}</template></el-table-column
        ></el-table
      ></el-card
    >
    <el-card shadow="never" class="panel"
      ><template #header><strong>任务与运行记录</strong></template
      ><el-tabs
        ><el-tab-pane label="下载任务"
          ><el-table :data="tasks" empty-text="暂无任务"
            ><el-table-column prop="title" label="种子" min-width="150" /><el-table-column
              prop="source"
              label="源站"
              width="90"
            /><el-table-column prop="target" label="目标站" width="90" /><el-table-column
              prop="state"
              label="状态"
              width="110"
            /><el-table-column prop="message" label="进度 / 信息" min-width="160" /><el-table-column
              label="操作"
              width="230"
              ><template #default="s"
                ><el-button link type="danger" @click="act(s.row, 'cancel')">取消</el-button
                ><el-button link @click="act(s.row, 'retry')">重试</el-button
                ><el-button link @click="reconcile(s.row)">对账链接</el-button
                ><el-link v-if="s.row.result_url" :href="s.row.result_url" target="_blank"
                  >上游</el-link
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
  </main>
</template>
<script setup lang="ts">
import axios from 'axios'
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
type Site = { id: string; name: string }
type Candidate = {
  id: string
  title: string
  size: number
  seeders: number
  eligible: boolean
  reasons: string[]
}
type Row = Record<string, unknown>
const loading = ref(false),
  saving = ref(false),
  previewing = ref(false),
  running = ref(false),
  error = ref(''),
  sites = ref<Site[]>([]),
  downloaders = ref<{ id: string; name: string }[]>([]),
  candidates = ref<Candidate[]>([]),
  selected = ref<Candidate[]>([]),
  tasks = ref<Row[]>([]),
  runs = ref<Row[]>([]),
  quota = ref<Record<string, unknown>>({})
const form = reactive({
  enabled: false,
  auto_publish: false,
  schedule_enabled: false,
  schedule_day: 1,
  schedule_time: '02:00',
  timezone: 'Asia/Shanghai',
  monthly_limit: 6,
  min_size_gib: 1,
  max_size_gib: 10,
  monthly_budget_gib: 60,
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
const quotaLabel = computed(
  () =>
    `本月 ${quota.value.count ?? 0} / ${form.monthly_limit} 项 · ${quota.value.bytes ? (Number(quota.value.bytes) / 1024 ** 3).toFixed(1) : '0.0'} GiB`,
)
const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const [s, t, r, p, d] = await Promise.all([
      axios.get('/api/automation/settings'),
      axios.get('/api/automation/tasks'),
      axios.get('/api/automation/runs'),
      axios.get('/api/site-profiles'),
      axios.get('/api/settings'),
    ])
    Object.assign(form, s.data.settings || {})
    minLeechers.value = s.data.settings?.min_leechers ?? 0
    reserveSpace.value = s.data.settings?.reserve_space_gib ?? 0
    quota.value = s.data.quota || {}
    tasks.value = t.data.tasks || []
    runs.value = r.data.runs || []
    sites.value = (p.data.sites || []).map((x: Record<string, unknown>) => ({
      id: String(x.id ?? ''),
      name: String(x.name ?? x.id ?? ''),
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
const payload = () => ({
  ...form,
  min_leechers: minLeechers.value,
  reserve_space_gib: reserveSpace.value,
})
const save = async () => {
  if (!form.source_site || !form.target_site || form.source_site === form.target_site)
    return ElMessage.warning('请选择不同的源站和目标站')
  if (form.enabled && (!form.downloader_id || !form.save_path.trim()))
    return ElMessage.warning('启用模块前请选择下载器并填写保存目录')
  if (form.min_size_gib <= 0 || form.max_size_gib <= 0 || form.monthly_budget_gib <= 0)
    return ElMessage.warning('体积范围与月度预算必须大于零')
  if (form.min_size_gib > form.max_size_gib) return ElMessage.warning('最小体积不能大于最大体积')
  if (form.max_size_gib > form.monthly_budget_gib)
    return ElMessage.warning('最大种子体积不能超过月度体积预算')
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
  try {
    const r = await axios.post('/api/automation/preview', payload())
    candidates.value = r.data.candidates || []
    ElMessage.success(`预览完成，共 ${candidates.value.length} 个候选`)
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
const runRequest = async (ids?: string[]) => {
  running.value = true
  try {
    const r = await axios.post(
      '/api/automation/runs',
      ids ? { selected_ids: ids, enqueue: true } : {},
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
const enqueueSelected = () => runRequest(selected.value.map((x) => x.id))
const act = async (task: Row, action: string) => {
  try {
    await axios.post(`/api/automation/tasks/${String(task.id ?? '')}/${action}`)
    ElMessage.success(action === 'cancel' ? '已取消' : '已请求重试')
    await load()
  } catch (e) {
    ElMessage.error(
      axios.isAxiosError(e)
        ? e.response?.data?.message || e.response?.data?.error || e.message
        : '操作失败',
    )
  }
}
const reconcile = async (task: Row) => {
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
    ElMessage.success('对账完成')
    await load()
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
  padding: 20px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.page-heading,
.card-head,
.actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.page-heading h1 {
  font-size: 22px;
  margin: 0 0 6px;
}
.page-heading p,
.hint {
  color: var(--el-text-color-secondary);
  margin: 0;
  font-size: 13px;
}
.panel {
  background: rgba(255, 255, 255, 0.78);
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 14px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(150px, 1fr));
  gap: 0 14px;
}
.range {
  display: flex;
  align-items: center;
  gap: 8px;
}
.range :deep(.el-input-number) {
  width: calc(50% - 16px);
}
.actions {
  justify-content: flex-start;
  margin-bottom: 14px;
}
@media (max-width: 900px) {
  .form-grid {
    grid-template-columns: repeat(2, minmax(140px, 1fr));
  }
}
@media (max-width: 600px) {
  .settings-page {
    padding: 12px;
  }
  .form-grid {
    grid-template-columns: 1fr;
  }
  .page-heading {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
