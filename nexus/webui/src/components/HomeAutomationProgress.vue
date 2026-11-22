<template>
  <section class="home-card home-panel task-progress" aria-labelledby="home-task-progress-title">
    <div class="home-panel__heading">
      <div>
        <h2 id="home-task-progress-title">自动化任务</h2>
        <p>{{ activeTasks.length }} 项正在处理 · 每 8 秒刷新</p>
      </div>
      <RouterLink to="/settings/automation" class="task-progress__link">
        查看任务 <ArrowRight aria-hidden="true" />
      </RouterLink>
    </div>

    <p v-if="error && hasLoaded" class="home-status--warning" role="status">
      刷新失败，当前为上次读取的任务。
    </p>

    <ul v-if="activeTasks.length" class="task-progress__list">
      <li v-for="task in activeTasks" :key="task.id" class="task-progress__item">
        <div class="task-progress__main">
          <div class="task-progress__identity">
            <strong>{{ task.title || task.source_id || '自动化任务' }}</strong>
            <span
              >{{ task.source }} → {{ task.target
              }}<template v-if="task.size"> · {{ formatBytes(task.size) }}</template></span
            >
          </div>
          <span
            class="task-progress__state"
            :class="`task-progress__state--${statusOf(task.state).tone}`"
          >
            {{ statusOf(task.state).label }}
          </span>
        </div>

        <div v-if="task.state === 'downloading'" class="task-progress__download">
          <div
            v-if="downloadPercent(task) !== null"
            class="task-progress__track"
            role="progressbar"
            :aria-label="`${task.title || '任务'}下载进度`"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-valuenow="downloadPercent(task) ?? 0"
          >
            <span :style="{ width: `${downloadPercent(task)}%` }" />
          </div>
          <div
            v-else
            class="task-progress__track task-progress__track--unknown"
            aria-label="下载进度暂不可用"
          >
            <span />
          </div>
          <span class="task-progress__percent">
            {{ downloadPercent(task) === null ? '下载中' : `${downloadPercent(task)}%` }}
          </span>
        </div>

        <p v-if="task.message" class="task-progress__message">{{ task.message }}</p>
      </li>
    </ul>

    <div v-else class="task-progress__empty" role="status">
      <p v-if="busy && !hasLoaded">正在读取自动化任务…</p>
      <p v-else-if="error && !hasLoaded">无法读取自动化任务</p>
      <p v-else>当前没有进行中的自动化任务</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import axios from 'axios'
import { ArrowRight } from '@element-plus/icons-vue'

interface AutomationTask {
  id: string
  source_id?: string
  source?: string
  target?: string
  title?: string
  size?: number
  state: string
  message?: string
  download_progress?: number
}

interface AutomationTasksResponse {
  tasks?: AutomationTask[]
}

const activeStates = new Set([
  'reserved',
  'downloading',
  'waiting_batch',
  'preparing',
  'waiting_media',
  'waiting_target',
  'ready',
  'queued_publish',
  'publishing',
  'published',
  'seeding',
  'reconciling',
  'duplicate_cleanup',
])

const taskBadges: Record<string, { label: string; tone: string }> = {
  reserved: { label: '等待下载', tone: 'muted' },
  downloading: { label: '下载中', tone: 'active' },
  waiting_batch: { label: '等待批量查重', tone: 'waiting' },
  preparing: { label: '准备发布资料', tone: 'active' },
  waiting_media: { label: '等待补充资料', tone: 'waiting' },
  waiting_target: { label: '等待查重', tone: 'waiting' },
  ready: { label: '资料就绪', tone: 'muted' },
  queued_publish: { label: '等待发布', tone: 'muted' },
  publishing: { label: '发布中', tone: 'active' },
  published: { label: '等待做种确认', tone: 'active' },
  seeding: { label: '确认做种中', tone: 'active' },
  reconciling: { label: '核对中', tone: 'active' },
  duplicate_cleanup: { label: '处理重复下载', tone: 'waiting' },
}

const tasks = ref<AutomationTask[]>([])
const busy = ref(false)
const error = ref(false)
const hasLoaded = ref(false)
const activeTasks = computed(() =>
  tasks.value
    .filter((task) => activeStates.has(task.state))
    .sort((left, right) => statePriority(left.state) - statePriority(right.state)),
)

let request: AbortController | undefined
let refreshTimer: number | undefined

function statePriority(state: string): number {
  if (state === 'downloading') return 0
  if (state === 'publishing' || state === 'seeding' || state === 'reconciling') return 1
  return 2
}

function statusOf(state: string): { label: string; tone: string } {
  return taskBadges[state] ?? { label: state || '处理中', tone: 'muted' }
}

function downloadPercent(task: AutomationTask): number | null {
  const value = task.download_progress
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  return Math.round(Math.min(1, Math.max(0, value)) * 100)
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** unit).toFixed(unit >= 3 ? 2 : 1)} ${units[unit]}`
}

async function refreshTasks() {
  if (busy.value || document.visibilityState !== 'visible') return
  const controller = new AbortController()
  request = controller
  busy.value = true
  try {
    const { data } = await axios.get<AutomationTasksResponse>('/api/automation/tasks', {
      params: { active: 1 },
      signal: controller.signal,
      timeout: 8000,
    })
    if (controller.signal.aborted || request !== controller) return
    if (!Array.isArray(data?.tasks)) throw new Error('Invalid active task response')
    tasks.value = data.tasks
    error.value = false
    hasLoaded.value = true
  } catch {
    if (!controller.signal.aborted) error.value = true
  } finally {
    if (request === controller) {
      request = undefined
      busy.value = false
    }
  }
}

onMounted(() => {
  void refreshTasks()
  refreshTimer = window.setInterval(refreshTasks, 8000)
})

onBeforeUnmount(() => {
  window.clearInterval(refreshTimer)
  request?.abort()
})
</script>

<style scoped>
.task-progress__link {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--star-data-muted);
  font-size: 12px;
  font-weight: 600;
  text-decoration: none;
}

.task-progress__link:hover {
  color: var(--star-data-ink);
}

.task-progress__link svg {
  width: 14px;
  height: 14px;
}

.task-progress__list {
  display: grid;
  gap: 8px;
  max-height: 356px;
  overflow: auto;
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
  scrollbar-color: var(--star-data-border) transparent;
  scrollbar-width: thin;
}

.task-progress__item {
  min-width: 0;
  padding: 11px 12px;
  border: 1px solid var(--star-data-border);
  border-radius: 10px;
  background: var(--star-data-panel-raised);
}

.task-progress__main,
.task-progress__download {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 9px;
}

.task-progress__identity {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.task-progress__identity strong {
  overflow: hidden;
  color: var(--star-data-ink);
  font-size: 13px;
  font-weight: 650;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-progress__identity > span,
.task-progress__message {
  color: var(--star-data-muted);
  font-size: 11px;
  line-height: 1.45;
}

.task-progress__state {
  flex: none;
  padding: 3px 7px;
  border: 1px solid currentColor;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 650;
}

.task-progress__state--active {
  color: var(--star-data-accent);
}

.task-progress__state--waiting {
  color: var(--star-data-blue);
}

.task-progress__state--muted {
  color: var(--star-data-muted);
}

.task-progress__download {
  margin-top: 10px;
}

.task-progress__track {
  position: relative;
  flex: 1;
  height: 5px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--star-data-border);
}

.task-progress__track > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--star-data-accent);
  transition: width 240ms ease;
}

.task-progress__track--unknown > span {
  width: 35%;
  animation: task-progress-sweep 1.8s ease-in-out infinite alternate;
}

.task-progress__percent {
  min-width: 43px;
  color: var(--star-data-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-align: end;
}

.task-progress__message {
  margin-top: 7px;
  overflow-wrap: anywhere;
}

.task-progress__empty {
  display: grid;
  min-height: 88px;
  align-content: center;
  color: var(--star-data-muted);
  font-size: 13px;
}

@keyframes task-progress-sweep {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(185%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .task-progress__track--unknown > span {
    animation: none;
  }
}
</style>
