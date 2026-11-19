<template>
  <div class="settings-workspace">
    <header class="star-page-heading">
      <h1>设置</h1>
    </header>
    <div class="settings-layout">
      <nav class="settings-navigation star-surface" aria-label="设置分类">
        <RouterLink v-for="section in sections" :key="section.path" :to="section.path">
          <el-icon aria-hidden="true"><component :is="section.icon" /></el-icon>
          <span>{{ section.label }}</span>
        </RouterLink>
      </nav>
      <main class="settings-content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Connection, Download, Key, Refresh, Setting, Tickets } from '@element-plus/icons-vue'

const sections = [
  { label: '通用', path: '/settings/general', icon: Setting },
  { label: '下载器', path: '/settings/downloader', icon: Download },
  { label: '站点管理', path: '/settings/cookie', icon: Tickets },
  { label: '自动化', path: '/settings/automation', icon: Refresh },
  { label: '站点配置', path: '/settings/site-profiles', icon: Connection },
  { label: 'API 管理', path: '/settings/api', icon: Key },
]
</script>

<style scoped>
.settings-workspace {
  display: grid;
  gap: 20px;
  min-width: 0;
  padding: clamp(16px, 3vw, 38px);
}

.settings-layout {
  display: grid;
  grid-template-columns: 170px minmax(0, 1fr);
  align-items: start;
  gap: 18px;
}

.settings-navigation {
  display: grid;
  gap: 4px;
  padding: 7px;
}

.settings-navigation a {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 12px;
  min-height: 42px;
  border-radius: 12px;
  color: var(--star-muted);
  text-decoration: none;
  white-space: nowrap;
}

.settings-navigation a:hover {
  background: var(--star-surface-muted);
  color: var(--star-ink);
}

.settings-navigation a.router-link-exact-active {
  background: var(--star-accent-soft);
  color: var(--star-accent);
  font-weight: 600;
}

.settings-content {
  min-width: 0;
}

@media (max-width: 750px) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-navigation {
    grid-auto-flow: column;
    grid-auto-columns: max-content;
    overflow-x: auto;
    padding: 5px;
  }
}
</style>
