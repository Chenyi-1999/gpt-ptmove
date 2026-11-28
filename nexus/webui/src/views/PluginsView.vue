<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRight, Box, Refresh } from '@element-plus/icons-vue'
import DownloaderInventoryPanel from '../components/plugins/DownloaderInventoryPanel.vue'
import TjuptRssPanel from '../components/plugins/TjuptRssPanel.vue'
import WasmPluginPanel from '../components/plugins/WasmPluginPanel.vue'
import { requestPlugin, type PluginCatalog, type PluginInfo } from '../components/plugins/pluginApi'

const AutomationSettings = defineAsyncComponent(
  () => import('../components/settings/AutomationSettings.vue'),
)
const route = useRoute()
const router = useRouter()
const catalog = ref<PluginCatalog>({ items: [], issues: [], metrics: {} })
const loading = ref<'list' | 'refresh'>()
const panelBusy = ref(false)
const automationBusy = ref(false)
const tjuptPackageBusy = ref(false)
const automationError = ref('')
const tjuptPackageError = ref('')
const error = ref('')
const notice = ref('')
const current = computed(
  () => catalog.value.items.find((plugin) => plugin.id === route.query.plugin) ?? catalog.value.items[0],
)
const busy = computed(() => Boolean(loading.value) || panelBusy.value || automationBusy.value || tjuptPackageBusy.value)

function selectPlugin(id: string) {
  void router.push({ name: 'plugins', query: { ...route.query, plugin: id } })
}

function updatePlugin(updated: PluginInfo) {
  const index = catalog.value.items.findIndex((plugin) => plugin.id === updated.id)
  if (index !== -1) catalog.value.items[index] = updated
}

async function toggleAutomation(enabled: boolean) {
  if (busy.value) return
  automationBusy.value = true
  automationError.value = ''
  try {
    const { plugin } = await requestPlugin<{ plugin: PluginInfo }>(
      '/automation/enabled',
      'PUT',
      '更新自动转种状态失败',
      { enabled },
    )
    updatePlugin(plugin)
  } catch (err) {
    automationError.value = err instanceof Error ? err.message : '更新自动转种状态失败'
  } finally {
    automationBusy.value = false
  }
}

async function toggleTjuptPackage(enabled: boolean) {
  if (busy.value) return
  tjuptPackageBusy.value = true
  tjuptPackageError.value = ''
  try {
    const { plugin } = await requestPlugin<{ plugin: PluginInfo }>(
      '/tjupt-rss/enabled',
      'PUT',
      '更新北洋园插件状态失败',
      { enabled },
    )
    updatePlugin(plugin)
  } catch (err) {
    tjuptPackageError.value = err instanceof Error ? err.message : '更新北洋园插件状态失败'
  } finally {
    tjuptPackageBusy.value = false
  }
}

async function load(refresh = false) {
  if (busy.value) return
  loading.value = refresh ? 'refresh' : 'list'
  error.value = ''
  notice.value = ''
  try {
    const data = await requestPlugin<PluginCatalog>(
      refresh ? '/refresh' : '',
      refresh ? 'POST' : 'GET',
      refresh ? '刷新插件失败' : '插件列表暂时不可用',
    )
    if (!Array.isArray(data.items) || !Array.isArray(data.issues)) {
      throw new Error('插件列表格式无效')
    }
    catalog.value = data
    if (refresh) notice.value = `已刷新，共 ${data.items.length} 个插件`
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取插件列表失败'
  } finally {
    loading.value = undefined
  }
}

onMounted(() => void load())
</script>

<template>
  <section class="star-plugins" aria-labelledby="plugins-heading" :aria-busy="Boolean(loading)">
    <header class="plugin-heading">
      <div>
        <h1 id="plugins-heading">插件</h1>
        <p class="plugin-install-hint">
          将插件 ZIP 或 JSON 放到 StarPT 数据目录下的 <code>plugins/</code>（容器内 <code>/app/data/plugins/</code>），再点击「刷新插件」。
        </p>
      </div>
      <button class="plugin-refresh" type="button" :disabled="busy" @click="load(true)">
        <Refresh class="plugin-icon" aria-hidden="true" />
        {{ loading === 'refresh' ? '正在刷新…' : '刷新插件' }}
      </button>
    </header>

    <p v-if="error" class="plugin-message plugin-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="plugin-message" role="status">{{ notice }}</p>

    <details v-if="catalog.issues.length" class="plugin-details plugin-warning" open>
      <summary>{{ catalog.issues.length }} 个插件文件需要处理</summary>
      <ul>
        <li v-for="(issue, index) in catalog.issues" :key="index">
          <code>{{ issue.file }}</code> {{ issue.message }}
        </li>
      </ul>
    </details>
    <div v-if="current" class="plugin-layout">
      <nav class="plugin-list" aria-label="已安装插件">
        <button
          v-for="plugin in catalog.items"
          :key="plugin.id"
          type="button"
          :disabled="panelBusy || automationBusy"
          :aria-current="plugin.id === current.id ? 'page' : undefined"
          @click="selectPlugin(plugin.id)"
        >
          <Box class="plugin-icon" aria-hidden="true" />
          <span>{{ plugin.name }}</span>
          <ArrowRight v-if="plugin.id === current.id" class="plugin-chev" aria-hidden="true" />
        </button>
      </nav>
      <section class="plugin-content" :aria-label="current.name">
        <header>
          <h2>{{ current.name }}</h2>
          <span class="plugin-version">{{ current.version }}</span>
        </header>
        <p v-if="current.description" class="plugin-description">{{ current.description }}</p>
        <template v-if="current.id === 'tjupt-rss' && current.kind === 'wasm'">
          <div class="automation-state">
            <span :class="{ 'automation-state__active': current.enabled }">
              {{ current.enabled ? '已启用' : '已停用' }}
            </span>
            <el-button
              :disabled="busy"
              :loading="tjuptPackageBusy"
              @click="toggleTjuptPackage(!current.enabled)"
            >
              {{ current.enabled ? '停用插件' : '启用插件' }}
            </el-button>
          </div>
          <p v-if="tjuptPackageError" class="plugin-message plugin-error" role="alert">
            {{ tjuptPackageError }}
          </p>
          <TjuptRssPanel :enabled="current.enabled" />
        </template>
        <template v-else-if="current.id === 'automation'">
          <div class="automation-state">
            <span :class="{ 'automation-state__active': current.enabled !== false }">
              {{ current.enabled === false ? '已停用' : '已启用' }}
            </span>
            <el-button
              :disabled="busy"
              :loading="automationBusy"
              @click="toggleAutomation(current.enabled === false)"
            >
              {{ current.enabled === false ? '启用插件' : '停用插件' }}
            </el-button>
          </div>
          <p v-if="automationError" class="plugin-message plugin-error" role="alert">
            {{ automationError }}
          </p>
          <p v-if="current.enabled === false" class="plugin-description" role="status">
            自动转种已暂停。仍可预览候选、查看历史和核对已提交的任务。
          </p>
          <AutomationSettings class="automation-settings" :plugin-enabled="current.enabled !== false" />
        </template>
        <DownloaderInventoryPanel
          v-else-if="current.id === 'downloader-summary' && current.kind === 'wasm' && current.capabilities?.includes('downloader.inventory')"
          :plugin="current"
          :refreshing="Boolean(loading)"
          @updated="updatePlugin"
          @busy="panelBusy = $event"
        />
        <WasmPluginPanel
          v-else-if="current.kind === 'wasm'"
          :key="current.id"
          :plugin="current"
          :refreshing="Boolean(loading)"
          @updated="updatePlugin"
          @busy="panelBusy = $event"
        />
        <div v-else-if="current.blocks?.length" class="plugin-blocks">
          <article v-for="(block, index) in current.blocks" :key="index" class="plugin-block">
            <RouterLink v-if="block.kind === 'link' && block.target" :to="block.target">
              <span>
                <strong>{{ block.title }}</strong>
                <small v-if="block.text">{{ block.text }}</small>
              </span>
              <ArrowRight class="plugin-chev" aria-hidden="true" />
            </RouterLink>
            <template v-else>
              <h3>{{ block.title }}</h3>
              <p v-if="block.kind === 'metric'" class="plugin-metric">
                {{ catalog.metrics?.[block.metric || '']?.toLocaleString('zh-CN') ?? '暂不可用' }}
              </p>
              <p v-if="block.text">{{ block.text }}</p>
            </template>
          </article>
        </div>
      </section>
    </div>
    <div v-else-if="!error && !loading" class="plugin-empty">
      <Box class="plugin-empty-icon" aria-hidden="true" />
      <h2>还没有安装插件</h2>
    </div>
    <div v-else-if="loading" class="plugin-empty" role="status">正在读取插件列表…</div>
  </section>
</template>

<style scoped>
.star-plugins {
  box-sizing: border-box;
  flex: 1 1 auto;
  width: 100%;
  max-width: 1180px;
  min-width: 0;
  min-height: 0;
  margin: 0 auto;
  padding: clamp(18px, 4vw, 48px);
  color: var(--star-ink, #f4f3fb);
  overflow-y: auto;
  scrollbar-gutter: stable;
}

.plugin-heading,
.plugin-content header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.plugin-heading { margin-bottom: 24px; }
.plugin-heading h1, .plugin-content h2, .plugin-empty h2 { margin: 0; letter-spacing: -0.035em; }
.plugin-heading h1 { font-size: clamp(28px, 4vw, 40px); line-height: 1.14; }
.plugin-install-hint, .plugin-description, .plugin-block p, .plugin-block small {
  color: var(--star-muted, #a8b3cf);
  margin: 6px 0 0;
  line-height: 1.55;
}
.plugin-install-hint { font-size: 0.875rem; }

.plugin-refresh {
  display: inline-flex;
  min-height: 42px;
  align-items: center;
  gap: 8px;
  padding: 8px 15px;
  border: 1px solid var(--star-border, #293553);
  border-radius: 10px;
  background: var(--star-surface, #12182b);
  color: var(--star-accent, #b8a4ff);
  font: inherit;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
}

.plugin-refresh:disabled { opacity: 0.68; cursor: wait; }
.plugin-icon, .plugin-chev { width: 18px; height: 18px; flex: 0 0 auto; }
.plugin-chev { width: 15px; height: 15px; }
.plugin-layout { display: grid; grid-template-columns: clamp(160px, 22%, 226px) minmax(0, 1fr); gap: 16px; }
.plugin-list, .plugin-content, .plugin-empty {
  border: 1px solid var(--star-border, #293553);
  border-radius: 16px;
  background: var(--star-surface, #12182b);
  box-shadow: 0 14px 38px rgb(0 0 0 / 12%);
}
.plugin-list { padding: 8px; align-self: start; }
.plugin-list button {
  appearance: none;
  border: 0;
  background: transparent;
  color: inherit;
  width: 100%;
  min-height: 44px;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-radius: 9px;
  text-align: start;
  font: inherit;
  cursor: pointer;
}
.plugin-list button span { flex: 1; overflow-wrap: anywhere; }
.plugin-list button[aria-current='page'], .plugin-list button:hover {
  background: var(--star-accent-soft, #251e42);
  color: var(--star-accent, #b8a4ff);
}
.plugin-list button:disabled { opacity: 0.72; cursor: wait; }
.plugin-content { min-width: 0; padding: clamp(16px, 3vw, 28px); }
.plugin-content h2, .plugin-block { overflow-wrap: anywhere; }
.plugin-version { color: var(--star-muted, #a8b3cf); font-size: 0.82rem; }
.plugin-blocks { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 210px), 1fr)); gap: 10px; margin-top: 20px; }
.plugin-block {
  min-width: 0;
  border: 1px solid var(--star-border, #293553);
  border-radius: 12px;
  padding: 14px;
  background: var(--star-surface-muted, #17203a);
}
.plugin-block h3 { margin: 0; font-size: 0.9rem; }
.plugin-block a { display: flex; gap: 10px; justify-content: space-between; align-items: center; text-decoration: none; color: inherit; min-height: 28px; }
.plugin-block a:hover strong { color: var(--star-accent-hover, #d4c8ff); }
.plugin-block small { display: block; }
.plugin-block .plugin-metric { font-size: 1.55rem; font-weight: 650; color: var(--star-ink, #f4f3fb); }
.plugin-empty { padding: clamp(36px, 8vw, 90px) 18px; text-align: center; }
.plugin-empty-icon { width: 35px; height: 35px; margin-bottom: 10px; color: var(--star-accent, #b8a4ff); }
.plugin-empty h2 { font-size: 1.15rem; }
.plugin-message { margin-top: 13px; padding: 12px 15px; overflow-wrap: anywhere; }
.plugin-warning { margin-bottom: 16px; }
.automation-state { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; margin-top: 16px; color: var(--star-muted); }
.automation-state__active { color: var(--star-accent); }
.automation-settings { margin-top: 18px; }
.plugin-warning, .plugin-error {
  border: 1px solid rgb(255 198 98 / 26%);
  color: var(--star-warning-ink, #ffd27a);
  background: var(--star-warning-soft, #302719);
}
:where(button, a):focus-visible { outline: 2px solid var(--star-accent, #b8a4ff); outline-offset: 2px; }

@media (max-width: 780px) {
  .plugin-layout { grid-template-columns: minmax(0, 1fr); }
  .plugin-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .plugin-heading { align-items: flex-start; }
}
</style>
