<template>
  <div
    v-if="backgroundImage"
    class="star-backdrop"
    :style="{ backgroundImage }"
    aria-hidden="true"
  />
  <a v-if="!isLoginPage" href="#main-content" class="star-skip-link" @click.prevent="focusMain">
    跳至主要内容
  </a>

  <header
    v-if="showDesktopCompactTitleBar"
    class="star-titlebar desktop-drag-region"
    @dblclick="handleDesktopNavDoubleClick"
  >
    <img class="star-titlebar__brand" src="/starpt-wordmark.svg" width="106" height="24" alt="STARPT" />
    <DesktopWindowControls
      compact
      :is-maximised="isMaximised"
      @minimise="minimiseWindow"
      @toggle-maximise="handleWindowMaximiseToggle"
      @close="hideWindowToTray"
    />
  </header>

  <header v-if="!isLoginPage" class="star-shell" :class="{ 'star-shell--mobile': showMobileNav }">
    <div class="star-shell__bar desktop-drag-region" @dblclick="handleDesktopNavDoubleClick">
      <NeonLogo />
      <span v-if="showMobileNav" class="star-shell__location">{{ currentRouteTitle }}</span>
      <PillNav v-else class="desktop-no-drag" :items="primaryNavigation" :active-href="activeRoute" />
      <div class="star-shell__actions desktop-no-drag">
        <button
          v-if="!showMobileNav && canRefresh"
          type="button"
          class="star-icon-button"
          aria-label="刷新当前页面缓存"
          title="刷新当前页面缓存"
          :aria-busy="isRefreshing"
          :disabled="isRefreshing"
          @click="handleGlobalRefresh"
        >
          <component
            :is="isRefreshing ? Loading : Refresh"
            :class="{ 'star-spinner': isRefreshing }"
            aria-hidden="true"
          />
        </button>
        <button
          type="button"
          class="star-icon-button star-shell__tasks"
          :aria-label="'后台任务，' + runningCount + ' 运行中，' + failedCount + ' 失败'"
          title="后台任务"
          aria-haspopup="dialog"
          :aria-expanded="taskDrawerVisible"
          @click="openTaskMonitor"
        >
          <Monitor aria-hidden="true" />
          <span v-if="taskMonitorBadge > 0" class="star-shell__task-count" aria-hidden="true">
            {{ taskMonitorBadge > 99 ? '99+' : taskMonitorBadge }}
          </span>
        </button>
        <button
          type="button"
          class="star-icon-button"
          :aria-label="showMobileNav ? '打开导航' : '打开工具'"
          :title="showMobileNav ? '导航' : '工具'"
          aria-haspopup="dialog"
          :aria-expanded="navigationDrawerVisible"
          @click="navigationDrawerVisible = true"
        >
          <component :is="showMobileNav ? Expand : More" aria-hidden="true" />
        </button>
      </div>
      <DesktopWindowControls
        v-if="isWindowsDesktop"
        class="star-shell__window-controls"
        :is-maximised="isMaximised"
        @minimise="minimiseWindow"
        @toggle-maximise="handleWindowMaximiseToggle"
        @close="hideWindowToTray"
      />
    </div>
  </header>

  <el-drawer
    v-if="!isLoginPage"
    v-model="navigationDrawerVisible"
    :title="showMobileNav ? '导航' : '工具'"
    :direction="showMobileNav ? 'ltr' : 'rtl'"
    size="min(360px, calc(100% - 24px))"
    append-to-body
    class="star-drawer"
    @closed="openQueuedVersionDialog"
  >
    <nav v-if="showMobileNav" aria-label="主导航" class="star-drawer__nav">
      <RouterLink
        v-for="item in primaryNavigation"
        :key="item.to"
        :to="item.to"
        class="star-drawer__action"
        :class="{ 'star-drawer__action--active': activeRoute === item.to }"
        :aria-current="activeRoute === item.to ? 'page' : undefined"
        @click="closeDrawerOnLink"
      >
        <component :is="item.icon" aria-hidden="true" />
        {{ item.label }}
      </RouterLink>
    </nav>
    <div class="star-drawer__tools" role="group" aria-label="工具操作">
      <button
        v-if="showMobileNav && canRefresh"
        type="button"
        class="star-drawer__action"
        :disabled="isRefreshing"
        @click="handleGlobalRefresh"
      >
        <component
          :is="isRefreshing ? Loading : Refresh"
          :class="{ 'star-spinner': isRefreshing }"
          aria-hidden="true"
        />
        {{ isRefreshing ? '正在刷新…' : '刷新当前页面缓存' }}
      </button>
      <button
        type="button"
        class="star-drawer__action"
        :disabled="exportingLogs"
        @click="handleExportLogs"
      >
        <component
          :is="exportingLogs ? Loading : Download"
          :class="{ 'star-spinner': exportingLogs }"
          aria-hidden="true"
        />
        {{ exportingLogs ? '正在导出…' : '导出后端日志' }}
      </button>
      <button type="button" class="star-drawer__action" @click="showVersionDialog">
        <InfoFilled aria-hidden="true" />
        版本与更新
        <span class="star-drawer__detail">{{ currentVersion }}</span>
      </button>
      <a :href="sourceUrl" target="_blank" rel="noopener noreferrer" class="star-drawer__action">
        <Link aria-hidden="true" />
        项目源码
        <TopRight class="star-drawer__detail" aria-hidden="true" />
      </a>
      <a :href="issuesUrl" target="_blank" rel="noopener noreferrer" class="star-drawer__action">
        <ChatDotRound aria-hidden="true" />
        问题反馈
        <TopRight class="star-drawer__detail" aria-hidden="true" />
      </a>
    </div>
  </el-drawer>

  <el-drawer
    v-if="!isLoginPage"
    v-model="taskDrawerVisible"
    title="后台任务"
    size="min(460px, calc(100% - 24px))"
    append-to-body
    class="star-drawer"
  >
    <div class="task-monitor-panel">
      <div class="task-monitor-panel__header">
        <span>{{ runningCount }} 运行中 · {{ failedCount }} 失败</span>
        <button
          v-if="hasFinishedTasks"
          type="button"
          class="star-button"
          @click="clearFinishedTasks"
        >
          清理已结束
        </button>
      </div>
      <p v-if="taskList.length === 0" class="task-monitor-panel__empty">暂无后台任务</p>
      <div v-else class="task-monitor-list">
        <article v-for="task in presentedTasks" :key="task.key" class="task-monitor-item">
          <div class="task-monitor-item__top">
            <h2>{{ task.title }}</h2>
            <div class="task-monitor-item__states">
              <span
                class="task-monitor-item__status"
                :class="'task-monitor-item__status--' + task.display.tone"
              >
                {{ task.display.label }}
              </span>
              <span
                v-if="task.autoAddStatus === 'failed'"
                class="task-monitor-item__status task-monitor-item__status--failed"
              >
                添加失败
              </span>
            </div>
          </div>
          <p v-if="task.progressText && task.progressText !== task.display.label">
            {{ task.progressText }}
          </p>
          <p v-if="task.message">{{ task.message }}</p>
          <p v-if="task.error" class="task-monitor-item__error">{{ task.error }}</p>
          <div class="task-monitor-item__footer">
            <span>{{ formatTaskTime(task.updatedAt) }}</span>
            <div class="task-monitor-item__actions">
              <el-button
                v-if="task.actions?.includes('finish')"
                link
                type="success"
                @click="handleTaskMonitorAction(task, 'finish')"
                >完成</el-button
              >
              <el-button
                v-if="task.actions?.includes('terminate')"
                link
                type="danger"
                @click="handleTaskMonitorAction(task, 'terminate')"
                >终止</el-button
              >
              <el-button link @click="clearTaskMonitorItem(task)">
                {{ task.status === 'running' ? '隐藏' : '清理' }}
              </el-button>
              <el-button
                v-if="task.routeTarget"
                link
                type="primary"
                @click="openTaskRoute(task.routeTarget)"
                >查看详情</el-button
              >
            </div>
          </div>
        </article>
      </div>
    </div>
  </el-drawer>

  <main
    id="main-content"
    ref="mainContent"
    class="main-content"
    :class="{ 'no-nav': isLoginPage }"
    tabindex="-1"
  >
    <router-view v-slot="{ Component }">
      <component :is="Component" @ready="handleComponentReady" />
    </router-view>
  </main>

  <VersionUpdate ref="versionUpdateRef" @version-loaded="(version) => (currentVersion = version)" />
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ChatDotRound,
  Connection,
  Document,
  Download,
  Expand,
  Grid,
  House,
  InfoFilled,
  Link,
  Loading,
  Monitor,
  More,
  Operation,
  Refresh,
  Search,
  Setting,
  Switch,
  TopRight,
} from '@element-plus/icons-vue'
import { ElMessageBox } from 'element-plus'
import axios from 'axios'
import DesktopWindowControls from '@/components/desktop/DesktopWindowControls.vue'
import NeonLogo from '@/components/NeonLogo.vue'
import PillNav from '@/components/PillNav.vue'
import { useDesktopWindowControls } from '@/desktop/windowControls'
import VersionUpdate from '@/components/VersionUpdate.vue'
import {
  useTaskMonitorStore,
  type TaskMonitorAction,
  type TaskMonitorItem,
  type TaskMonitorStatus,
} from '@/stores/taskMonitor'
import { ElMessage } from '@/utils/uiNotify'
import { getTaskDisplayStatus, redactPrivateText } from '@/utils/publishPresentation'

const route = useRoute()
const router = useRouter()
const taskMonitorStore = useTaskMonitorStore()
const { hideWindowToTray, isMaximised, isWindowsDesktop, minimiseWindow, toggleWindowMaximise } =
  useDesktopWindowControls()

const primaryNavigation = [
  { to: '/', label: '首页', icon: House },
  { to: '/torrents', label: '一种多站', icon: Switch },
  { to: '/data', label: '一键多种', icon: Connection },
  { to: '/seed-maintenance', label: '种子维护', icon: Operation },
  { to: '/publish-logs', label: '任务记录', icon: Document },
  { to: '/sites', label: '做种检索', icon: Search },
  { to: '/plugins', label: '插件', icon: Grid },
  { to: '/settings', label: '设置', icon: Setting },
]
const routeTitleMap: Record<string, string> = {
  ...Object.fromEntries(primaryNavigation.map(({ to, label }) => [to, label])),
  '/settings/general': '基础设置',
  '/settings/downloader': '下载器设置',
  '/settings/cookie': '站点管理',
  '/settings/transfer': '转种设置',
  '/settings/automation': '转种设置',
  '/settings/site-profiles': '站点配置',
  '/settings/api': 'API 管理',
  '/settings/ai': 'API 管理',
}

const sourceUrl = 'https://github.com/Chenyi-1999/gpt-ptmove'
const issuesUrl = sourceUrl + '/issues'
const mainContent = ref<HTMLElement>()
const currentVersion = ref('加载中…')
const navigationDrawerVisible = ref(false)
const taskDrawerVisible = ref(false)
const showMobileNav = ref(false)
const MOBILE_BREAKPOINT = 880
const WINDOWS_COMPACT_BREAKPOINT = 980
const backgroundUrl = ref('')
const backgroundImage = computed(() => {
  if (isLoginPage.value || !backgroundUrl.value) return ''
  return 'url(' + JSON.stringify(backgroundUrl.value) + ')'
})
const isLoginPage = computed(() => route.path === '/login')
const activeRoute = computed(() => route.matched[0]?.path || route.path)
const currentRouteTitle = computed(
  () => routeTitleMap[route.path] || routeTitleMap[activeRoute.value] || '导航',
)
const showDesktopCompactTitleBar = computed(() => isWindowsDesktop.value && isLoginPage.value)

const focusMain = () => mainContent.value?.focus()
const closeDrawerOnLink = (event: MouseEvent) => {
  if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
    navigationDrawerVisible.value = false
  }
}

const taskList = computed(() => taskMonitorStore.taskList)
const presentedTasks = computed(() =>
  taskList.value.map((task) => ({
    ...task,
    title: redactPrivateText(task.title),
    message: redactPrivateText(task.message),
    error: redactPrivateText(task.error),
    progressText: redactPrivateText(task.progressText),
    actionHint: redactPrivateText(task.actionHint || ''),
    display: getTaskDisplayStatus(task),
  })),
)
const hasFinishedTasks = computed(() => taskList.value.some((task) => task.status !== 'running'))
const runningCount = computed(() => taskMonitorStore.runningCount)
const failedCount = computed(() => taskMonitorStore.failedCount)
const taskMonitorBadge = computed(() => runningCount.value + failedCount.value)
let taskMonitorRefreshTimer: ReturnType<typeof window.setInterval> | null = null
let taskMonitorSyncing = false

type ServerTaskRouteTarget = {
  path?: string
  query?: Record<string, unknown>
}

type ServerTaskItem = {
  key?: string
  kind?: string
  raw_id?: string | null
  title?: string
  status?: TaskMonitorStatus
  message?: string
  error?: string
  progress_text?: string
  outcome?: string
  auto_add_status?: string
  started_at?: string | number | null
  updated_at?: string | number | null
  actions?: unknown[]
  action_hint?: string
  route_target?: ServerTaskRouteTarget
}

const parseServerTaskTime = (value: string | number | null | undefined) => {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value !== 'string' || !value.trim()) return undefined
  const timestamp = Date.parse(value.trim())
  return Number.isNaN(timestamp) ? undefined : timestamp
}

const formatTaskTime = (timestamp: number) => {
  if (!timestamp) return '--'
  return new Date(timestamp).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

const clearFinishedTasks = () => {
  taskMonitorStore.clearFinished()
}

const clearTaskMonitorItem = (task: TaskMonitorItem) => {
  taskMonitorStore.clearTask(task.key)
}

const normalizeTaskMonitorActions = (actions?: unknown[]) => {
  if (!Array.isArray(actions)) return undefined
  const normalized = actions.filter(
    (action): action is TaskMonitorAction => action === 'finish' || action === 'terminate',
  )
  return normalized.length > 0 ? normalized : undefined
}

const handleTaskMonitorAction = async (task: TaskMonitorItem, action: TaskMonitorAction) => {
  const actionText = action === 'finish' ? '完成' : '终止'
  try {
    await ElMessageBox.confirm(
      task.actionHint || '此操作会请求取消或将监控状态标记为结束，不保证强制终止底层进程。',
      `${actionText}后台任务`,
      { confirmButtonText: actionText, cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }

  try {
    const response = await axios.post('/api/migrate/task_monitor/action', {
      kind: task.kind,
      raw_id: task.rawId,
      action,
    })
    const payload = response.data
    if (payload?.success === false) {
      ElMessage.error(String(payload?.message || '任务操作失败'))
      return
    }
    ElMessage.success(String(payload?.message || '任务操作已提交'))
    await syncTaskMonitor()
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? String(error.response?.data?.message || error.message || '任务操作失败')
      : '任务操作失败'
    ElMessage.error(message)
  }
}

const syncTaskMonitor = async () => {
  const token = localStorage.getItem('token')
  if (isLoginPage.value || !token || taskMonitorSyncing || document.hidden) return
  taskMonitorSyncing = true
  try {
    const response = await axios.get('/api/migrate/task_monitor', { timeout: 8000 })
    if (isLoginPage.value || localStorage.getItem('token') !== token) return
    const payload = response.data
    if (payload?.success !== true || !Array.isArray(payload.tasks)) throw new Error('任务列表无效')
    const tasks = payload.tasks as ServerTaskItem[]
    const normalizeRouteQuery = (query?: Record<string, unknown>) => {
      if (!query) return undefined
      return Object.fromEntries(
        Object.entries(query)
          .filter(([, value]) => value !== undefined && value !== null && String(value).trim())
          .map(([key, value]) => [key, String(value).trim()]),
      ) as Record<string, string>
    }

    taskMonitorStore.replaceServerTasks(
      tasks.map((task) => ({
        key: String(task.key || '').trim(),
        kind: String(task.kind || 'generic').trim(),
        rawId: typeof task.raw_id === 'string' ? task.raw_id : null,
        source: 'server',
        title: String(task.title || '后台任务').trim(),
        status:
          task.status === 'running' || task.status === 'success' || task.status === 'failed'
            ? task.status
            : 'running',
        message: String(task.message || '').trim(),
        error: String(task.error || '').trim(),
        progressText: String(task.progress_text || '').trim(),
        outcome: typeof task.outcome === 'string' ? task.outcome : undefined,
        autoAddStatus: typeof task.auto_add_status === 'string' ? task.auto_add_status : undefined,
        startedAt: parseServerTaskTime(task.started_at),
        updatedAt: parseServerTaskTime(task.updated_at),
        actions: normalizeTaskMonitorActions(task.actions),
        actionHint: String(task.action_hint || '').trim(),
        routeTarget:
          task.route_target?.path && typeof task.route_target.path === 'string'
            ? {
                path: task.route_target.path,
                query: normalizeRouteQuery(task.route_target.query),
              }
            : undefined,
      })),
    )
  } catch (error) {
    if (isLoginPage.value || localStorage.getItem('token') !== token) return
    taskMonitorStore.serverState = 'unavailable'
    console.warn('同步任务监控失败:', error)
  } finally {
    taskMonitorSyncing = false
  }
}

const openTaskMonitor = () => {
  taskDrawerVisible.value = true
  void syncTaskMonitor()
}

const openTaskRoute = (routeTarget?: { path: string; query?: Record<string, string> }) => {
  if (!routeTarget?.path) return
  taskDrawerVisible.value = false
  // 旧版本缓存的自动化任务链接仍应打开自动转种插件。
  if (routeTarget.path === '/settings/automation') {
    void router.push({ path: '/plugins', query: { ...routeTarget.query, plugin: 'automation' } })
    return
  }
  void router.push(routeTarget.query ? { path: routeTarget.path, query: routeTarget.query } : routeTarget.path)
}

const isRefreshing = ref(false)
const exportingLogs = ref(false)

const isRefreshSupportedRoute = (path: string) => {
  return (
    path.startsWith('/torrents') ||
    path.startsWith('/sites') ||
    path.startsWith('/data') ||
    path.startsWith('/seed-maintenance') ||
    path.startsWith('/publish-logs') ||
    path.startsWith('/batch-fetch')
  )
}

const canRefresh = computed(() => isRefreshSupportedRoute(route.path))

// VersionUpdate 组件引用
const versionUpdateRef = ref()

const activeComponentRefresher = ref<(() => Promise<void>) | null>(null)

const handleComponentReady = (refreshMethod: () => Promise<void>) => {
  activeComponentRefresher.value = refreshMethod
}

const shouldDelegateRefreshToComponent = (path: string) => {
  return path.startsWith('/torrents') || path.startsWith('/publish-logs')
}

const handleGlobalRefresh = async () => {
  if (isRefreshing.value) return

  if (!isRefreshSupportedRoute(route.path)) {
    ElMessage.warning('当前页面不支持刷新操作。')
    return
  }

  const refreshPath = route.path
  const refreshTitle = currentRouteTitle.value
  const refreshComponent = activeComponentRefresher.value
  const refreshTaskKey = `global_refresh:${refreshPath}`
  const refreshRouteTarget = {
    path: refreshPath,
    query: Object.fromEntries(
      Object.entries(route.query)
        .filter(([, value]) => value !== undefined && value !== null && String(value).trim())
        .map(([key, value]) => [key, String(value).trim()]),
    ),
  }

  isRefreshing.value = true
  taskMonitorStore.markRunning({
    key: refreshTaskKey,
    kind: 'refresh',
    title: '刷新缓存',
    message: `正在刷新${refreshTitle}`,
    progressText: '已提交刷新请求',
    routeTarget: refreshRouteTarget,
  })
  ElMessage.info('后台正在刷新缓存...')

  try {
    if (shouldDelegateRefreshToComponent(refreshPath) && refreshComponent) {
      await refreshComponent()
    } else {
      await axios.post('/api/refresh_data')
      if (refreshComponent && route.path === refreshPath) {
        await refreshComponent()
      }
    }

    taskMonitorStore.markSuccess(refreshTaskKey, {
      kind: 'refresh',
      title: '刷新缓存',
      message: `${refreshTitle}已刷新`,
      progressText: '刷新完成',
      routeTarget: refreshRouteTarget,
    })
    ElMessage.success('数据已刷新！')
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string } | undefined)?.message || error.message
      : error instanceof Error
        ? error.message
        : '数据更新失败'
    taskMonitorStore.markFailed(refreshTaskKey, {
      kind: 'refresh',
      title: '刷新缓存',
      message: `刷新${refreshTitle}失败`,
      error: message || '数据更新失败',
      progressText: '刷新失败',
      routeTarget: refreshRouteTarget,
    })
    ElMessage.error(message || '数据更新失败')
  } finally {
    isRefreshing.value = false
  }
}

// handleExportLogs 调用后端导出接口并触发浏览器下载日志文件。
const handleExportLogs = async () => {
  if (exportingLogs.value) {
    return
  }
  exportingLogs.value = true
  try {
    const response = await axios.get('/api/logs/export', {
      responseType: 'blob',
      timeout: 120000,
    })

    const contentType = response.headers['content-type'] || 'text/plain; charset=utf-8'
    const blob = new Blob([response.data], { type: contentType })
    const fileName = parseFileName(response.headers['content-disposition'])

    const fileUrl = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = fileUrl
    link.download = fileName
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(fileUrl)

    ElMessage.success('最近24小时日志导出成功，请将文件发送给维护者')
  } catch (error) {
    const message = await resolveExportErrorMessage(error)
    ElMessage.error(`日志导出失败：${message}`)
  } finally {
    exportingLogs.value = false
  }
}

// parseFileName 从响应头中提取导出文件名，失败时回退默认名称。
const parseFileName = (contentDisposition?: string) => {
  const fallback = 'starpt-日志-24h.txt'
  if (!contentDisposition || typeof contentDisposition !== 'string') {
    return fallback
  }

  const utf8Match = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1])
    } catch {
      return utf8Match[1]
    }
  }

  const normalMatch = contentDisposition.match(/filename="?([^";]+)"?/i)
  if (normalMatch?.[1]) {
    return normalMatch[1]
  }
  return fallback
}

// resolveExportErrorMessage 兼容 blob/json 文本，提取后端返回的中文错误原因。
const resolveExportErrorMessage = async (error: unknown) => {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data

    if (data instanceof Blob) {
      try {
        const text = await data.text()
        if (!text) return '未知错误'
        try {
          const payload: unknown = JSON.parse(text)
          if (
            typeof payload === 'object' &&
            payload !== null &&
            'message' in payload &&
            typeof payload.message === 'string' &&
            payload.message.trim()
          ) {
            return payload.message
          }
        } catch {
          return text
        }
        return text
      } catch {
        return '无法读取错误详情'
      }
    }

    if (
      typeof data === 'object' &&
      data !== null &&
      'message' in data &&
      typeof data.message === 'string' &&
      data.message.trim()
    ) {
      return data.message
    }

    return error.message || '未知错误'
  }

  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  return '未知错误'
}

const handleRefreshLoadingChange = (event: Event) => {
  const customEvent = event as CustomEvent<{ refreshing?: boolean }>
  isRefreshing.value = !!customEvent.detail?.refreshing
}

// 加载背景设置
const loadBackgroundSettings = async () => {
  const token = localStorage.getItem('token')
  if (isLoginPage.value || !token) return
  try {
    const response = await axios.get('/api/settings')
    // A response from the previous session must not restore its custom background.
    if (isLoginPage.value || localStorage.getItem('token') !== token) return
    const configured = response.data?.ui_settings?.background_url
    backgroundUrl.value = typeof configured === 'string' ? configured.trim() : ''
  } catch (error) {
    console.error('加载背景设置失败:', error)
  }
}

type BackgroundUpdateDetail = { backgroundUrl: string }
const handleBackgroundUpdate = (event: Event) => {
  const newUrl = (event as CustomEvent<BackgroundUpdateDetail>).detail?.backgroundUrl
  if (!isLoginPage.value && typeof newUrl === 'string') backgroundUrl.value = newUrl.trim()
}

// Wait for the drawer to release focus before opening the version dialog.
let versionDialogQueued = false
const showVersionDialog = () => {
  versionDialogQueued = true
  navigationDrawerVisible.value = false
}
const openQueuedVersionDialog = () => {
  if (!versionDialogQueued) return
  versionDialogQueued = false
  if (!isLoginPage.value) versionUpdateRef.value?.show()
}

const handleWindowMaximiseToggle = () => {
  void toggleWindowMaximise()
}
const handleDesktopNavDoubleClick = (event: MouseEvent) => {
  if (!isWindowsDesktop.value) return
  if (event.target instanceof Element && event.target.closest('a, button')) return
  handleWindowMaximiseToggle()
}

const updateIsMobile = () => {
  const breakpoint = isWindowsDesktop.value ? WINDOWS_COMPACT_BREAKPOINT : MOBILE_BREAKPOINT
  showMobileNav.value = window.innerWidth <= breakpoint
}

watch(isWindowsDesktop, updateIsMobile)

watch(
  () => route.path,
  () => {
    navigationDrawerVisible.value = false
    taskDrawerVisible.value = false
    activeComponentRefresher.value = null
  },
  { flush: 'sync' },
)

watch(isLoginPage, (loginPage) => {
  if (loginPage) {
    backgroundUrl.value = ''
    versionDialogQueued = false
    return
  }
  void loadBackgroundSettings()
  void syncTaskMonitor()
})

onMounted(() => {
  updateIsMobile()
  loadBackgroundSettings()
  syncTaskMonitor()
  taskMonitorRefreshTimer = window.setInterval(() => {
    void syncTaskMonitor()
  }, 2000)
  window.addEventListener('starpt-open-task-monitor', openTaskMonitor)
  document.addEventListener('visibilitychange', syncTaskMonitor)
  window.addEventListener('background-updated', handleBackgroundUpdate)
  window.addEventListener('app-global-refresh-loading', handleRefreshLoadingChange as EventListener)
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('starpt-open-task-monitor', openTaskMonitor)
  document.removeEventListener('visibilitychange', syncTaskMonitor)
  if (taskMonitorRefreshTimer) {
    window.clearInterval(taskMonitorRefreshTimer)
    taskMonitorRefreshTimer = null
  }
  window.removeEventListener('background-updated', handleBackgroundUpdate)
  window.removeEventListener('resize', updateIsMobile)
  window.removeEventListener(
    'app-global-refresh-loading',
    handleRefreshLoadingChange as EventListener,
  )
})
</script>

<style src="@/assets/styles/star-shell.scss"></style>
