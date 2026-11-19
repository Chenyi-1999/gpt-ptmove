<template>
  <div class="star-home">
    <div class="star-home__content">
      <section class="star-home__welcome star-surface" aria-labelledby="workspace-title">
        <div class="star-home__intro">
          <h1 id="workspace-title" class="star-page-heading">工作台</h1>
          <p>从下载器选择种子，查看转种预览并提交到目标站点。</p>
          <RouterLink to="/torrents" class="star-button star-button--primary">
            <Switch aria-hidden="true" />
            开始转种
            <ArrowRight aria-hidden="true" />
          </RouterLink>
        </div>
        <nav class="star-home__shortcuts" aria-label="工作台快捷入口">
          <RouterLink v-for="item in shortcuts" :key="item.to" :to="item.to">
            <span class="star-home__shortcut-icon">
              <component :is="item.icon" aria-hidden="true" />
            </span>
            <span>{{ item.label }}</span>
            <ArrowRight class="star-home__shortcut-arrow" aria-hidden="true" />
          </RouterLink>
        </nav>
      </section>

      <aside class="star-home__notice" aria-labelledby="publishing-note">
        <Warning aria-hidden="true" />
        <div>
          <h2 id="publishing-note">发布前核对预览</h2>
          <p>
            检查标题、简介、截图、MediaInfo / BDInfo
            和目标站点映射，并确认站点规则与重复状态。发种后核实种子信息。
          </p>
        </div>
      </aside>

      <section class="star-home__downloaders" aria-labelledby="downloaders-title">
        <div class="star-home__section-head">
          <div>
            <h2 id="downloaders-title">下载器</h2>
            <p v-if="downloaderInfo.length">{{ downloaderInfo.length }} 个下载器 · 每 30 秒刷新</p>
          </div>
          <RouterLink to="/settings/downloader" class="star-button">
            <Setting aria-hidden="true" />
            下载器设置
          </RouterLink>
        </div>

        <div v-if="!downloaderInfo.length" class="star-home__empty star-surface" role="status">
          <span class="star-home__shortcut-icon"><Monitor aria-hidden="true" /></span>
          <p v-if="loading">正在读取下载器…</p>
          <template v-else-if="loadError">
            <p>无法获取下载器信息</p>
            <button type="button" class="star-button" @click="fetchDownloaderInfo()">
              重新加载
            </button>
          </template>
          <template v-else>
            <p>暂无下载器配置</p>
            <span>在下载器设置中添加 qBittorrent 或 Transmission。</span>
          </template>
        </div>

        <template v-else>
          <p v-if="loadError" class="star-home__stale" role="status">
            暂时无法刷新，以下显示上次读取的数据。
          </p>
          <div class="star-home__grid">
            <article
              v-for="downloader in downloaderInfo"
              :key="downloader.id || downloader.name"
              class="star-home__downloader star-surface"
            >
              <div class="star-home__downloader-head">
                <span class="star-home__shortcut-icon"><Monitor aria-hidden="true" /></span>
                <div class="star-home__downloader-name">
                  <h3>{{ downloader.name }}</h3>
                  <p>
                    {{ downloaderTypeLabel(downloader.type)
                    }}{{ downloader.details?.版本 ? ' · ' + downloader.details.版本 : '' }}
                  </p>
                </div>
                <span
                  class="star-home__status"
                  :class="{
                    'star-home__status--active':
                      downloader.enabled &&
                      (downloader.status === '已连接' || downloader.status === '已配置'),
                  }"
                >
                  {{ downloader.enabled ? downloader.status || '状态未知' : '已禁用' }}
                </span>
              </div>
              <dl v-if="downloader.enabled" class="star-home__metrics">
                <div v-for="metric in metrics" :key="metric.key">
                  <dt>{{ metric.label }}</dt>
                  <dd>{{ downloader.details?.[metric.key] || '—' }}</dd>
                </div>
              </dl>
            </article>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import {
  ArrowRight,
  Document,
  Grid,
  Monitor,
  Operation,
  Setting,
  Switch,
  Warning,
} from '@element-plus/icons-vue'

interface DownloaderInfo {
  id?: string
  name: string
  type: string
  enabled: boolean
  status: string
  details: Record<string, string> | null
}

const shortcuts = [
  { to: '/seed-maintenance', label: '种子维护', icon: Operation },
  { to: '/publish-logs', label: '任务记录', icon: Document },
  { to: '/plugins', label: '插件', icon: Grid },
]
const metrics = [
  { key: '今日上传量', label: '今日上传' },
  { key: '今日下载量', label: '今日下载' },
  { key: '累计上传量', label: '累计上传' },
  { key: '累计下载量', label: '累计下载' },
]

const downloaderInfo = ref<DownloaderInfo[]>([])
const loading = ref(true)
const loadError = ref(false)
let activeRequest: AbortController | undefined
let refreshTimer: number | undefined

const downloaderTypeLabel = (type: string) => {
  if (type === 'qbittorrent') return 'qBittorrent'
  if (type === 'transmission') return 'Transmission'
  return type || '下载器'
}

const fetchDownloaderInfo = async (showLoading = true) => {
  if (activeRequest) return
  const request = new AbortController()
  activeRequest = request
  if (showLoading) loading.value = true
  try {
    const { data } = await axios.get<DownloaderInfo[]>('/api/downloader_info', {
      signal: request.signal,
    })
    if (request.signal.aborted) return
    if (!Array.isArray(data)) {
      loadError.value = true
      return
    }
    downloaderInfo.value = data
    loadError.value = false
  } catch {
    if (!request.signal.aborted) loadError.value = true
  } finally {
    activeRequest = undefined
    if (!request.signal.aborted) loading.value = false
  }
}

onMounted(() => {
  void fetchDownloaderInfo()
  refreshTimer = window.setInterval(() => void fetchDownloaderInfo(false), 30000)
})

onUnmounted(() => {
  window.clearInterval(refreshTimer)
  activeRequest?.abort()
})
</script>

<style scoped>
.star-home {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: clamp(22px, 3.3vw, 44px) max(16px, env(safe-area-inset-right, 0px))
    max(32px, env(safe-area-inset-bottom, 0px)) max(16px, env(safe-area-inset-left, 0px));
}

.star-home__content {
  width: min(100%, 1120px);
  margin: 0 auto;
}

.star-home__welcome {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(215px, 30%);
  align-items: center;
  gap: 32px;
  overflow: hidden;
  padding: clamp(24px, 4vw, 48px);
  background: radial-gradient(ellipse at 98% 5%, #e3f4f9, transparent 60%), var(--star-surface);
}

.star-home__intro p {
  max-width: 35ch;
  margin: 12px 0 22px;
  color: var(--star-muted);
  font-size: 16px;
  line-height: 1.75;
}

.star-home__intro .star-button {
  min-height: 46px;
}

.star-home__intro .star-button svg:last-child {
  width: 15px;
  height: 15px;
  margin-left: 4px;
}

.star-home__shortcuts {
  display: grid;
  gap: 7px;
}

.star-home__shortcuts a {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 46px;
  padding: 4px 12px 4px 6px;
  border: 1px solid var(--star-border);
  border-radius: 10px;
  color: var(--star-ink);
  background: var(--star-surface);
  font-weight: 600;
  text-decoration: none;
  transition:
    background-color 140ms ease,
    border-color 140ms ease;
}

.star-home__shortcuts a:hover {
  border-color: var(--star-border-strong);
  background: var(--star-surface-muted);
}

.star-home__shortcut-icon {
  display: inline-grid;
  flex: none;
  width: 34px;
  height: 34px;
  place-items: center;
  border-radius: 8px;
  color: var(--star-accent);
  background: var(--star-accent-soft);
}

.star-home__shortcut-icon svg,
.star-home__notice > svg {
  width: 18px;
  height: 18px;
}

.star-home__shortcut-arrow {
  width: 16px;
  height: 16px;
  margin-left: auto;
  color: var(--star-muted);
}

.star-home__notice {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  margin-top: 18px;
  padding: 15px 18px;
  border: 1px solid #ead7ae;
  border-radius: 12px;
  color: var(--star-warning);
  background: var(--star-warning-soft);
}

.star-home__notice > svg {
  flex: none;
  margin-top: 2px;
}

.star-home__notice h2 {
  margin: 0 0 4px;
  font-size: 14px;
  line-height: 1.45;
}

.star-home__notice p {
  margin: 0;
  font-size: 14px;
}

.star-home__downloaders {
  margin-top: clamp(25px, 3vw, 38px);
}

.star-home__section-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px 16px;
  margin-bottom: 12px;
}

.star-home__section-head h2 {
  margin: 0;
  font-size: 21px;
  letter-spacing: -0.02em;
}

.star-home__section-head p {
  margin: 5px 0 0;
  color: var(--star-muted);
}

.star-home__empty {
  display: grid;
  justify-items: center;
  padding: 26px 15px;
  gap: 8px;
  color: var(--star-muted);
  text-align: center;
}

.star-home__empty p {
  margin: 0;
  color: var(--star-ink);
  font-weight: 600;
}

.star-home__empty .star-button {
  margin-top: 5px;
}

.star-home__stale {
  margin: 0 0 10px;
  color: var(--star-warning);
}

.star-home__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 292px), 1fr));
  gap: 12px;
}

.star-home__downloader {
  min-width: 0;
  padding: 16px;
}

.star-home__downloader-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px 10px;
}

.star-home__downloader-name {
  flex: 1 1 100px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.star-home__downloader-name h3 {
  margin: 0;
  font-size: 16px;
  line-height: 1.35;
}

.star-home__downloader-name p {
  margin: 3px 0 0;
  color: var(--star-muted);
  font-size: 13px;
}

.star-home__status {
  padding: 3px 8px;
  border-radius: 999px;
  color: var(--star-muted);
  background: var(--star-surface-muted);
  font-size: 12px;
  white-space: nowrap;
}

.star-home__status--active {
  color: var(--star-success);
  background: var(--star-success-soft);
}

.star-home__metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 14px;
  padding-top: 12px;
  margin: 14px 0 0;
  border-top: 1px solid var(--star-border);
}

.star-home__metrics dt {
  color: var(--star-muted);
  font-size: 13px;
}

.star-home__metrics dd {
  margin: 2px 0 0;
  font-size: 14px;
  font-variant-numeric: tabular-nums;
  font-weight: 650;
  overflow-wrap: anywhere;
}

@media (max-width: 650px) {
  .star-home__welcome {
    grid-template-columns: minmax(0, 1fr);
    gap: 19px;
    padding: 20px;
  }

  .star-home__intro p {
    margin: 9px 0 15px;
    font-size: 15px;
  }

  .star-home__shortcuts {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .star-home__shortcuts a {
    justify-content: center;
    gap: 5px;
    min-height: 48px;
    padding: 4px;
    font-size: 13px;
    text-align: center;
  }

  .star-home__shortcuts .star-home__shortcut-icon {
    width: 24px;
    height: 24px;
    border-radius: 6px;
  }

  .star-home__shortcuts .star-home__shortcut-icon svg {
    width: 15px;
    height: 15px;
  }

  .star-home__shortcut-arrow {
    display: none;
  }

  .star-home__notice {
    padding: 12px;
  }
}

@media (max-width: 360px) {
  .star-home__shortcuts a {
    flex-direction: column;
    padding-block: 7px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .star-home__shortcuts a {
    transition: none;
  }
}
</style>
