<template>
  <div ref="workspace" class="settings-workspace">
    <div class="settings-inner">
      <header class="settings-heading">
        <h1>设置</h1>
      </header>

      <div class="settings-layout">
        <nav class="settings-navigation star-surface" aria-label="设置分类">
          <RouterLink v-for="section in sections" :key="section.path" :to="section.path">
            <el-icon aria-hidden="true"><component :is="section.icon" /></el-icon>
            <span>{{ section.label }}</span>
          </RouterLink>
        </nav>

        <div class="settings-content">
          <router-view />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Connection, Download, Key, Refresh, Setting, Tickets } from '@element-plus/icons-vue'
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const workspace = ref<HTMLElement>()

watch(
  () => route.path,
  () => workspace.value?.scrollTo({ top: 0 }),
)

const sections = [
  { label: '通用', path: '/settings/general', icon: Setting },
  { label: '下载器', path: '/settings/downloader', icon: Download },
  { label: '站点管理', path: '/settings/cookie', icon: Tickets },
  { label: '自动转种', path: '/plugins?plugin=automation', icon: Refresh },
  { label: '站点配置', path: '/settings/site-profiles', icon: Connection },
  { label: 'API 管理', path: '/settings/api', icon: Key },
]
</script>

<style scoped>
.settings-workspace {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  scrollbar-gutter: stable;
  background:
    radial-gradient(ellipse at 10% 0%, rgb(69 45 123 / 25%), transparent 44%),
    var(--star-canvas);
}

.settings-inner {
  box-sizing: border-box;
  max-width: 1496px;
  margin: 0 auto;
  padding: clamp(16px, 3vw, 38px) clamp(12px, 3vw, 38px) 36px;
}

.settings-heading {
  margin-bottom: clamp(14px, 2vw, 23px);
}

.settings-heading h1 {
  margin: 0;
  color: var(--star-ink);
  font-size: clamp(1.55rem, 3vw, 2.15rem);
  line-height: 1.2;
  letter-spacing: -0.03em;
}

.settings-layout {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  align-items: start;
  gap: clamp(12px, 2vw, 24px);
}

.settings-navigation {
  position: sticky;
  top: 12px;
  display: grid;
  min-width: 0;
  gap: 4px;
  padding: 7px;
  border-radius: 12px;
}

.settings-navigation a {
  box-sizing: border-box;
  display: flex;
  min-width: 0;
  min-height: 44px;
  align-items: center;
  gap: 9px;
  padding: 9px 11px;
  border: 1px solid transparent;
  border-radius: 8px;
  color: var(--star-muted);
  font-size: 0.875rem;
  line-height: 1.25;
  text-decoration: none;
}

.settings-navigation .el-icon {
  flex: none;
  font-size: 16px;
}

.settings-navigation a:hover {
  background: var(--star-surface-muted);
  color: var(--star-ink);
}

.settings-navigation a.router-link-exact-active {
  border-color: color-mix(in srgb, var(--star-accent) 27%, var(--star-border));
  background: var(--star-accent-soft);
  color: var(--star-accent);
  font-weight: 650;
}

.settings-navigation a:focus-visible {
  outline: 2px solid var(--star-accent);
  outline-offset: -2px;
}

.settings-content,
.settings-content :deep(.el-form-item),
.settings-content :deep(.el-form-item__content) {
  min-width: 0;
}

.settings-content :deep(.settings-section-title) {
  margin: 0;
  color: var(--star-ink);
  font-size: clamp(1.15rem, 2vw, 1.35rem);
  font-weight: 650;
  line-height: 1.25;
  letter-spacing: -0.015em;
}

.settings-content :deep(.el-form-item__label),
.settings-content :deep(.el-alert__content) {
  overflow-wrap: anywhere;
}

.settings-content :deep(.el-input-number),
.settings-content :deep(.el-date-editor) {
  max-width: 100%;
}

@media (max-width: 900px) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-navigation {
    position: static;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .settings-navigation a {
    justify-content: center;
    gap: 5px;
    padding-inline: 4px;
    text-align: center;
  }
}

@media (max-width: 345px) {
  .settings-navigation {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>

<style>
.el-overlay-dialog .el-dialog.settings-dialog {
  display: flex;
  flex-direction: column;
  max-width: calc(100vw - 24px);
  max-height: calc(100dvh - 24px);
}

.settings-dialog .el-dialog__header,
.settings-dialog .el-dialog__footer {
  flex: none;
}

.settings-dialog .el-dialog__title {
  overflow-wrap: anywhere;
}

.settings-dialog .el-dialog__body {
  min-height: 0;
  overflow: auto;
  overflow-wrap: anywhere;
}

.settings-dialog .el-dialog__footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
}

.settings-dialog .el-dialog__footer > .el-button {
  margin-left: 0;
}

.settings-dialog .el-form-item__content,
.settings-dialog .el-input,
.settings-dialog .el-select {
  min-width: 0;
}
</style>
