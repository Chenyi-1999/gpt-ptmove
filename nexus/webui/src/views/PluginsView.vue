<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowRight, Box, FolderOpened, Refresh } from '@element-plus/icons-vue'

type Block = {
  kind: 'text' | 'link' | 'metric'
  title: string
  text?: string
  target?: string
  metric?: string
}
type Manifest = { id: string; name: string; version: string; description?: string; blocks: Block[] }
type Catalog = {
  items: Manifest[]
  issues: { file: string; message: string }[]
  metrics: Record<string, number | null>
  message?: string
}

const catalog = ref<Catalog>({ items: [], issues: [], metrics: {} })
const selected = ref('')
const busy = ref(false)
const error = ref('')
const notice = ref('')
const current = computed(() => catalog.value.items.find((plugin) => plugin.id === selected.value))

async function load(refresh = false) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await fetch(refresh ? '/api/plugins/refresh' : '/api/plugins', {
      method: refresh ? 'POST' : 'GET',
    })
    const data = (await response.json()) as Catalog
    if (!response.ok || !Array.isArray(data.items) || !Array.isArray(data.issues)) {
      throw new Error(data.message || '插件列表暂时不可用')
    }
    catalog.value = data
    if (!catalog.value.items.some((item) => item.id === selected.value)) {
      selected.value = catalog.value.items[0]?.id || ''
    }
    if (refresh) notice.value = `已刷新，共 ${data.items.length} 个插件`
  } catch (err) {
    error.value = err instanceof Error ? err.message : '未能读取插件目录'
  } finally {
    busy.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <main class="star-plugins" :aria-busy="busy">
    <header class="plugin-heading">
      <div>
        <h1>插件</h1>
        <p>管理本机扩展和常用工具入口。</p>
      </div>
      <button class="plugin-refresh" type="button" :disabled="busy" @click="load(true)">
        <Refresh class="plugin-icon" aria-hidden="true" />
        {{ busy ? '正在读取…' : '刷新插件' }}
      </button>
    </header>

    <p v-if="error" class="plugin-message plugin-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="plugin-message" role="status">{{ notice }}</p>

    <div v-if="current" class="plugin-layout">
      <nav class="plugin-list" aria-label="已安装插件">
        <button
          v-for="plugin in catalog.items"
          :key="plugin.id"
          type="button"
          :aria-current="plugin.id === selected ? 'true' : undefined"
          @click="selected = plugin.id"
        >
          <Box class="plugin-icon" aria-hidden="true" />
          <span>{{ plugin.name }}</span>
          <ArrowRight v-if="plugin.id === selected" class="plugin-chev" aria-hidden="true" />
        </button>
      </nav>
      <section class="plugin-content" :aria-label="current.name">
        <header>
          <h2>{{ current.name }}</h2>
          <span class="plugin-version">{{ current.version }}</span>
        </header>
        <p v-if="current.description" class="plugin-description">{{ current.description }}</p>
        <div class="plugin-blocks">
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
    <div v-else-if="!error && !busy" class="plugin-empty">
      <Box class="plugin-empty-icon" aria-hidden="true" />
      <h2>还没有安装插件</h2>
      <p>把插件的 JSON 文件放进 <code>data/plugins/</code>，然后点击刷新。</p>
    </div>
    <div v-else-if="busy && !current" class="plugin-empty" role="status">正在读取插件目录…</div>

    <details v-if="catalog.issues.length" class="plugin-details plugin-warning" open>
      <summary>{{ catalog.issues.length }} 个插件文件未加载</summary>
      <ul>
        <li v-for="issue in catalog.issues" :key="issue.file">
          <code>{{ issue.file }}</code> {{ issue.message }}
        </li>
      </ul>
    </details>
    <details v-if="current" class="plugin-details">
      <summary><FolderOpened class="plugin-icon" aria-hidden="true" /> 安装与移除</summary>
      <p>在部署的数据目录中放入或移除 <code>plugins/</code> 下的 JSON 文件，再点击「刷新插件」。</p>
    </details>
  </main>
</template>

<style scoped>
.star-plugins {
  max-width: 1180px;
  min-width: 0;
  margin: 0 auto;
  padding: clamp(18px, 4vw, 48px);
  color: var(--star-ink, #172333);
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
.plugin-heading p, .plugin-description, .plugin-block p, .plugin-block small, .plugin-empty p {
  color: var(--star-muted, #4b6078);
  margin: 6px 0 0;
  line-height: 1.55;
}

.plugin-refresh {
  display: inline-flex;
  min-height: 42px;
  align-items: center;
  gap: 8px;
  padding: 8px 15px;
  border: 1px solid var(--star-border, #d9e2ee);
  border-radius: 10px;
  background: var(--star-surface, #fff);
  color: var(--star-accent, #175f8e);
  font: inherit;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
}

.plugin-refresh:disabled { opacity: 0.68; cursor: wait; }
.plugin-icon, .plugin-chev { width: 18px; height: 18px; flex: 0 0 auto; }
.plugin-chev { width: 15px; height: 15px; }
.plugin-layout { display: grid; grid-template-columns: minmax(180px, 0.8fr) minmax(0, 2fr); gap: 16px; }
.plugin-list, .plugin-content, .plugin-empty, .plugin-details {
  border: 1px solid var(--star-border, #d9e2ee);
  border-radius: 16px;
  background: var(--star-surface, #fff);
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
.plugin-list button[aria-current='true'], .plugin-list button:hover {
  background: var(--star-accent-soft, #e3f1fb);
  color: var(--star-accent, #175f8e);
}
.plugin-content { padding: clamp(16px, 3vw, 28px); }
.plugin-content h2, .plugin-block { overflow-wrap: anywhere; }
.plugin-version { color: var(--star-muted, #4b6078); font-size: 0.82rem; }
.plugin-blocks { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 210px), 1fr)); gap: 10px; margin-top: 20px; }
.plugin-block {
  min-width: 0;
  border: 1px solid var(--star-border, #d9e2ee);
  border-radius: 12px;
  padding: 14px;
  background: var(--star-surface-muted, #f5f8fc);
}
.plugin-block h3 { margin: 0; font-size: 0.9rem; }
.plugin-block a { display: flex; gap: 10px; justify-content: space-between; align-items: center; text-decoration: none; color: inherit; min-height: 28px; }
.plugin-block a:hover strong { color: var(--star-accent, #175f8e); }
.plugin-block small { display: block; }
.plugin-block .plugin-metric { font-size: 1.55rem; font-weight: 650; color: var(--star-ink, #172333); }
.plugin-empty { padding: clamp(36px, 8vw, 90px) 18px; text-align: center; }
.plugin-empty-icon { width: 35px; height: 35px; margin-bottom: 10px; color: var(--star-accent, #175f8e); }
.plugin-empty h2 { font-size: 1.15rem; }
.plugin-details, .plugin-message { margin-top: 13px; padding: 12px 15px; overflow-wrap: anywhere; }
.plugin-details summary { cursor: pointer; font-weight: 600; }
.plugin-details summary svg { vertical-align: middle; margin-right: 5px; }
.plugin-details p { margin-bottom: 0; }
.plugin-warning, .plugin-error { color: var(--star-warning-ink, #8a4518); }
:where(button, a, summary):focus-visible { outline: 2px solid var(--star-accent, #175f8e); outline-offset: 2px; }

@media (max-width: 700px) {
  .plugin-layout { grid-template-columns: 1fr; }
  .plugin-heading { align-items: flex-start; }
}
</style>
