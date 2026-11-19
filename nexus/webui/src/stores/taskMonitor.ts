import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { redactPrivateText } from '@/utils/publishPresentation'

export type TaskMonitorStatus = 'running' | 'success' | 'failed'
export type TaskMonitorAction = 'finish' | 'terminate'

export type TaskMonitorItem = {
  key: string
  kind: string
  rawId?: string | null
  source?: 'local' | 'server'
  title: string
  status: TaskMonitorStatus
  outcome?: string
  autoAddStatus?: string
  message: string
  error: string
  progressText: string
  startedAt: number
  updatedAt: number
  actions?: TaskMonitorAction[]
  actionHint?: string
  routeTarget?: {
    path: string
    query?: Record<string, string>
  }
}

type UpsertTaskInput = {
  key: string
  kind: string
  rawId?: string | null
  source?: 'local' | 'server'
  title: string
  status?: TaskMonitorStatus
  outcome?: string
  autoAddStatus?: string
  message?: string
  error?: string
  progressText?: string
  startedAt?: number
  updatedAt?: number
  actions?: TaskMonitorAction[]
  actionHint?: string
  routeTarget?: {
    path: string
    query?: Record<string, string>
  }
}

type ClearedServerTaskState = {
  updatedAt: number
  status: TaskMonitorStatus
  outcome?: string
  autoAddStatus?: string
  message: string
  error: string
  progressText: string
}

const normalizeText = (value: unknown) => (typeof value === 'string' ? value.trim() : '')
const normalizeActions = (value: unknown): TaskMonitorAction[] | undefined => {
  if (!Array.isArray(value)) {
    return undefined
  }
  const actions = value.filter(
    (action): action is TaskMonitorAction => action === 'finish' || action === 'terminate',
  )
  return actions.length > 0 ? actions : undefined
}
const privateText = (value: unknown) => redactPrivateText(normalizeText(value))
const TASK_MONITOR_STORAGE_KEY = 'ptnexus.taskMonitor.v1'
const TASK_MONITOR_CLEARED_STORAGE_KEY = 'ptnexus.taskMonitor.cleared.v1'

const loadPersistedTasks = (): Record<string, TaskMonitorItem> => {
  if (typeof window === 'undefined') {
    return {}
  }

  try {
    const raw = window.localStorage.getItem(TASK_MONITOR_STORAGE_KEY)
    if (!raw) {
      return {}
    }

    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') {
      return {}
    }

    const restoredEntries = Object.entries(parsed).flatMap(([key, value]) => {
      if (!value || typeof value !== 'object') {
        return []
      }

      const task = value as Partial<TaskMonitorItem>
      return [
        [
          key,
          {
            key,
            kind: normalizeText(task.kind) || 'generic',
            rawId: normalizeText(task.rawId) || null,
            source: task.source === 'server' ? 'server' : 'local',
            outcome: normalizeText(task.outcome) || undefined,
            autoAddStatus: normalizeText(task.autoAddStatus) || undefined,
            title: privateText(task.title) || '后台任务',
            status:
              task.status === 'running' || task.status === 'success' || task.status === 'failed'
                ? task.status
                : 'running',
            message: privateText(task.message),
            error: privateText(task.error),
            progressText: privateText(task.progressText),
            actions: normalizeActions(task.actions),
            actionHint: privateText(task.actionHint),
            startedAt: typeof task.startedAt === 'number' ? task.startedAt : Date.now(),
            updatedAt: typeof task.updatedAt === 'number' ? task.updatedAt : Date.now(),
            routeTarget:
              task.routeTarget && typeof task.routeTarget.path === 'string'
                ? {
                    path: task.routeTarget.path,
                    query: task.routeTarget.query,
                  }
                : undefined,
          } satisfies TaskMonitorItem,
        ],
      ]
    })

    const restored: Record<string, TaskMonitorItem> = Object.fromEntries(restoredEntries)
    persistTasks(restored)
    return restored
  } catch {
    return {}
  }
}

const persistTasks = (tasks: Record<string, TaskMonitorItem>) => {
  if (typeof window === 'undefined') {
    return
  }

  try {
    window.localStorage.setItem(TASK_MONITOR_STORAGE_KEY, JSON.stringify(tasks))
  } catch {
    // ignore storage failures to avoid blocking task updates
  }
}

const loadClearedServerTasks = (): Record<string, ClearedServerTaskState> => {
  if (typeof window === 'undefined') {
    return {}
  }

  try {
    const raw = window.localStorage.getItem(TASK_MONITOR_CLEARED_STORAGE_KEY)
    if (!raw) {
      return {}
    }

    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') {
      return {}
    }

    return Object.fromEntries(
      Object.entries(parsed).flatMap(([key, value]) => {
        if (!value || typeof value !== 'object') {
          return []
        }
        const task = value as Partial<ClearedServerTaskState>
        if (typeof task.updatedAt !== 'number') {
          return []
        }
        const status =
          task.status === 'running' || task.status === 'success' || task.status === 'failed'
            ? task.status
            : 'success'
        return [
          [
            key,
            {
              updatedAt: task.updatedAt,
              status,
              outcome: normalizeText(task.outcome) || undefined,
              autoAddStatus: normalizeText(task.autoAddStatus) || undefined,
              message: privateText(task.message),
              error: privateText(task.error),
              progressText: privateText(task.progressText),
            } satisfies ClearedServerTaskState,
          ],
        ]
      }),
    )
  } catch {
    return {}
  }
}

const persistClearedServerTasks = (clearedTasks: Record<string, ClearedServerTaskState>) => {
  if (typeof window === 'undefined') {
    return
  }

  try {
    window.localStorage.setItem(TASK_MONITOR_CLEARED_STORAGE_KEY, JSON.stringify(clearedTasks))
  } catch {
    // ignore storage failures to avoid blocking task updates
  }
}

export const useTaskMonitorStore = defineStore('taskMonitor', () => {
  const tasks = ref<Record<string, TaskMonitorItem>>(loadPersistedTasks())
  const clearedServerTasks = ref<Record<string, ClearedServerTaskState>>(loadClearedServerTasks())

  const taskList = computed(() =>
    Object.values(tasks.value).sort((a, b) => b.updatedAt - a.updatedAt),
  )

  const runningTasks = computed(() => taskList.value.filter((task) => task.status === 'running'))
  const failedTasks = computed(() =>
    taskList.value.filter(
      (task) =>
        task.status === 'failed' || (task.status !== 'running' && task.autoAddStatus === 'failed'),
    ),
  )
  const runningCount = computed(() => runningTasks.value.length)
  const failedCount = computed(() => failedTasks.value.length)
  const hasAttention = computed(() => runningCount.value > 0 || failedCount.value > 0)

  const upsertTask = (input: UpsertTaskInput) => {
    const now = Date.now()
    const existing = tasks.value[input.key]
    tasks.value[input.key] = {
      key: input.key,
      kind: input.kind,
      rawId: input.rawId ?? existing?.rawId ?? null,
      source: input.source ?? existing?.source ?? 'local',
      title: privateText(input.title) || existing?.title || '后台任务',
      status: input.status ?? existing?.status ?? 'running',
      outcome: input.outcome ?? (input.status ? undefined : existing?.outcome),
      autoAddStatus: input.autoAddStatus ?? (input.status ? undefined : existing?.autoAddStatus),
      message: privateText(input.message) || '',
      error: privateText(input.error) || '',
      progressText: privateText(input.progressText) || '',
      actions: Object.prototype.hasOwnProperty.call(input, 'actions')
        ? normalizeActions(input.actions)
        : existing?.actions,
      actionHint: Object.prototype.hasOwnProperty.call(input, 'actionHint')
        ? privateText(input.actionHint)
        : existing?.actionHint,
      startedAt: input.startedAt ?? existing?.startedAt ?? now,
      updatedAt: input.updatedAt ?? now,
      routeTarget: Object.prototype.hasOwnProperty.call(input, 'routeTarget')
        ? input.routeTarget
        : existing?.routeTarget,
    }
    persistTasks(tasks.value)
  }

  const markRunning = (input: Omit<UpsertTaskInput, 'status'>) => {
    upsertTask({ ...input, status: 'running', error: '' })
  }

  const markSuccess = (
    key: string,
    patch: Partial<Omit<UpsertTaskInput, 'key' | 'status'>> = {},
  ) => {
    const existing = tasks.value[key]
    if (!existing && !patch.title) return
    upsertTask({
      key,
      kind: patch.kind ?? existing?.kind ?? 'generic',
      title: patch.title ?? existing?.title ?? '后台任务',
      rawId: patch.rawId ?? existing?.rawId,
      actions: patch.actions ?? existing?.actions,
      actionHint: patch.actionHint ?? existing?.actionHint,
      routeTarget:
        Object.prototype.hasOwnProperty.call(patch, 'routeTarget') || !existing
          ? patch.routeTarget
          : existing?.routeTarget,
      message: patch.message ?? existing?.message ?? '',
      progressText: patch.progressText ?? existing?.progressText ?? '',
      error: '',
      status: 'success',
      outcome: patch.outcome,
      autoAddStatus: patch.autoAddStatus,
    })
  }

  const markFailed = (
    key: string,
    patch: Partial<Omit<UpsertTaskInput, 'key' | 'status'>> = {},
  ) => {
    const existing = tasks.value[key]
    if (!existing && !patch.title) return
    upsertTask({
      key,
      kind: patch.kind ?? existing?.kind ?? 'generic',
      title: patch.title ?? existing?.title ?? '后台任务',
      rawId: patch.rawId ?? existing?.rawId,
      actions: patch.actions ?? existing?.actions,
      actionHint: patch.actionHint ?? existing?.actionHint,
      routeTarget:
        Object.prototype.hasOwnProperty.call(patch, 'routeTarget') || !existing
          ? patch.routeTarget
          : existing?.routeTarget,
      message: patch.message ?? existing?.message ?? '',
      progressText: patch.progressText ?? existing?.progressText ?? '',
      error: patch.error ?? existing?.error ?? patch.message ?? '',
      status: 'failed',
      outcome: patch.outcome,
      autoAddStatus: patch.autoAddStatus,
    })
  }

  const removeTask = (key: string) => {
    if (!tasks.value[key]) return
    const nextTasks = { ...tasks.value }
    delete nextTasks[key]
    tasks.value = nextTasks
    persistTasks(tasks.value)
  }

  const rememberClearedServerTask = (task: TaskMonitorItem) => {
    if (task.source !== 'server') {
      return
    }
    clearedServerTasks.value = {
      ...clearedServerTasks.value,
      [task.key]: {
        updatedAt: task.updatedAt,
        status: task.status,
        outcome: task.outcome,
        autoAddStatus: task.autoAddStatus,
        message: task.message,
        error: task.error,
        progressText: task.progressText,
      },
    }
    persistClearedServerTasks(clearedServerTasks.value)
  }

  const clearTask = (key: string) => {
    const task = tasks.value[key]
    if (!task) return
    rememberClearedServerTask(task)
    removeTask(key)
  }

  const clearFinished = () => {
    Object.values(tasks.value).forEach((task) => {
      if (task.status !== 'running') {
        rememberClearedServerTask(task)
      }
    })
    const nextTasks = Object.fromEntries(
      Object.entries(tasks.value).filter(([, task]) => task.status === 'running'),
    )
    tasks.value = nextTasks
    persistTasks(tasks.value)
  }

  const replaceServerTasks = (serverTasks: UpsertTaskInput[]) => {
    const preservedLocalTasks = Object.fromEntries(
      Object.entries(tasks.value).filter(([, task]) => task.source !== 'server'),
    )

    const nextTasks: Record<string, TaskMonitorItem> = { ...preservedLocalTasks }
    const nextClearedServerTasks: Record<string, ClearedServerTaskState> = {}
    for (const task of serverTasks) {
      const key = normalizeText(task.key)
      if (!key) continue
      const existing = tasks.value[key]
      const normalizedTask: TaskMonitorItem = {
        key,
        kind: normalizeText(task.kind) || existing?.kind || 'generic',
        rawId: task.rawId ?? existing?.rawId ?? null,
        source: 'server',
        title: privateText(task.title) || existing?.title || '后台任务',
        status: task.status ?? existing?.status ?? 'running',
        outcome: normalizeText(task.outcome) || undefined,
        autoAddStatus: normalizeText(task.autoAddStatus) || undefined,
        message: privateText(task.message) || '',
        error: privateText(task.error) || '',
        progressText: privateText(task.progressText) || '',
        actions: normalizeActions(task.actions),
        actionHint: privateText(task.actionHint),
        startedAt: task.startedAt ?? existing?.startedAt ?? task.updatedAt ?? Date.now(),
        updatedAt: task.updatedAt ?? existing?.updatedAt ?? Date.now(),
        routeTarget: Object.prototype.hasOwnProperty.call(task, 'routeTarget')
          ? task.routeTarget
          : existing?.routeTarget,
      }
      const clearedState = clearedServerTasks.value[key]
      const shouldKeepCleared =
        !!clearedState &&
        normalizedTask.status !== 'running' &&
        clearedState.status === normalizedTask.status &&
        clearedState.outcome === normalizedTask.outcome &&
        clearedState.autoAddStatus === normalizedTask.autoAddStatus &&
        normalizedTask.message === clearedState.message &&
        normalizedTask.error === clearedState.error &&
        normalizedTask.progressText === clearedState.progressText

      if (shouldKeepCleared) {
        nextClearedServerTasks[key] = clearedState
        continue
      }

      nextTasks[key] = normalizedTask
    }

    clearedServerTasks.value = nextClearedServerTasks
    persistClearedServerTasks(clearedServerTasks.value)
    tasks.value = nextTasks
    persistTasks(tasks.value)
  }

  return {
    tasks,
    taskList,
    runningTasks,
    failedTasks,
    runningCount,
    failedCount,
    hasAttention,
    upsertTask,
    markRunning,
    markSuccess,
    markFailed,
    removeTask,
    clearTask,
    clearFinished,
    replaceServerTasks,
  }
})
