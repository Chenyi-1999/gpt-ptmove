<template>
  <section class="settings-card glass-card glass-rounded glass-transparent-header glass-transparent-body sites-layout" aria-labelledby="sites-layout-title">
    <header class="card-header">
      <div class="header-content">
        <el-icon class="header-icon"><Grid /></el-icon>
        <h3 id="sites-layout-title">做种检索布局</h3>
      </div>
      <el-button type="primary" :loading="saving" :disabled="loading || Boolean(loadError) || !dirty" @click="saveLayout">
        保存布局
      </el-button>
    </header>

    <div class="card-content" :aria-busy="loading || saving">
      <p v-if="loading" class="layout-notice" role="status">正在读取布局设置…</p>
      <div v-else-if="loadError" class="layout-notice layout-notice--error" role="alert">
        <span>{{ loadError }}</span>
        <el-button link type="primary" :loading="loading" @click="loadLayout">重试</el-button>
      </div>
      <template v-else>
        <div class="layout-columns">
          <div class="layout-field">
            <span class="layout-label" id="sites-layout-columns-label">电脑端列数</span>
            <el-radio-group v-model="draftColumns" aria-labelledby="sites-layout-columns-label" :disabled="saving">
              <el-radio-button :value="1">一列</el-radio-button>
              <el-radio-button :value="2">两列</el-radio-button>
            </el-radio-group>
          </div>
          <p class="layout-help">
            手机端固定单列。双列按从左到右、从上到下排列；列表中越靠前的模块优先级越高。
          </p>
        </div>

        <p v-if="saveError" class="layout-feedback layout-feedback--error" role="alert">{{ saveError }}</p>
        <p v-else-if="savedMessage" class="layout-feedback" role="status">{{ savedMessage }}</p>

        <ol class="panel-order" aria-label="做种检索模块优先级">
          <li v-for="(panel, index) in draftOrder" :key="panel" class="panel-order__row">
            <span class="panel-order__rank" aria-hidden="true">{{ index + 1 }}</span>
            <span class="panel-order__name">{{ SITES_PANEL_LABELS[panel] }}</span>
            <span class="panel-order__priority">{{ index === 0 ? '最高优先级' : `优先级 ${index + 1}` }}</span>
            <div class="panel-order__actions" :aria-label="`${SITES_PANEL_LABELS[panel]}排序`">
              <el-button
                :icon="ArrowUp"
                :aria-label="`上移${SITES_PANEL_LABELS[panel]}`"
                :disabled="saving || index === 0"
                @click="movePanel(index, -1)"
              />
              <el-button
                :icon="ArrowDown"
                :aria-label="`下移${SITES_PANEL_LABELS[panel]}`"
                :disabled="saving || index === draftOrder.length - 1"
                @click="movePanel(index, 1)"
              />
            </div>
          </li>
        </ol>
        <p v-if="dirty" class="layout-help layout-help--pending">有未保存的布局更改。</p>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ArrowDown, ArrowUp, Grid } from '@element-plus/icons-vue'
import { computed, onMounted, ref } from 'vue'
import { useSitesLayoutStore, SITES_PANEL_LABELS, type SitesPanelId } from '@/stores/sitesLayout'

const layoutStore = useSitesLayoutStore()
const draftColumns = ref<1 | 2>(2)
const draftOrder = ref<SitesPanelId[]>([])
const savedColumns = ref<1 | 2>(2)
const savedOrder = ref<SitesPanelId[]>([])
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const savedMessage = ref('')

const dirty = computed(
  () => draftColumns.value !== savedColumns.value || draftOrder.value.some((panel, index) => panel !== savedOrder.value[index]),
)

function syncFromStore() {
  savedColumns.value = layoutStore.columns
  draftColumns.value = layoutStore.columns
  savedOrder.value = [...layoutStore.order]
  draftOrder.value = [...layoutStore.order]
}

async function loadLayout() {
  loading.value = true
  loadError.value = ''
  saveError.value = ''
  savedMessage.value = ''
  try {
    await layoutStore.load(true)
    syncFromStore()
  } catch (error) {
    loadError.value = error instanceof Error ? `布局设置读取失败：${error.message}` : '布局设置读取失败，请重试。'
  } finally {
    loading.value = false
  }
}

function movePanel(index: number, direction: -1 | 1) {
  const target = index + direction
  if (target < 0 || target >= draftOrder.value.length || saving.value) return
  const next = [...draftOrder.value]
  ;[next[index], next[target]] = [next[target], next[index]]
  draftOrder.value = next
  saveError.value = ''
  savedMessage.value = ''
}

async function saveLayout() {
  if (!dirty.value || saving.value) return
  saving.value = true
  saveError.value = ''
  savedMessage.value = ''
  try {
    await layoutStore.save({ columns: draftColumns.value, order: [...draftOrder.value] })
    syncFromStore()
    savedMessage.value = '做种检索布局已保存。'
  } catch (error) {
    saveError.value = error instanceof Error ? `保存失败：${error.message}` : '保存失败，请稍后重试。'
  } finally {
    saving.value = false
  }
}

onMounted(loadLayout)
</script>

<style scoped>
.sites-layout {
  min-width: 0;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--star-border);
}

.header-content {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.header-content h3 {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 16px;
  font-weight: 600;
}

.header-icon {
  color: var(--el-color-primary);
}

.card-content {
  display: grid;
  gap: 14px;
  min-width: 0;
  padding: 16px;
}

.layout-columns {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px 24px;
}

.layout-field {
  display: grid;
  gap: 8px;
}

.layout-label {
  color: var(--star-ink);
  font-size: 13px;
  font-weight: 600;
}

.layout-help,
.layout-notice,
.layout-feedback {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.layout-feedback {
  color: var(--star-success, #63c7a5);
}

.layout-notice--error,
.layout-feedback--error {
  color: var(--el-color-danger);
}

.layout-notice--error {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.layout-help--pending {
  color: var(--el-color-warning);
}

.panel-order {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.panel-order__row {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 9px 10px;
  border: 1px solid var(--star-border);
  border-radius: 10px;
  background: color-mix(in srgb, var(--star-surface-muted) 72%, transparent);
}

.panel-order__rank {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  border-radius: 8px;
  background: var(--star-accent-soft, rgba(145, 103, 255, 0.16));
  color: var(--el-color-primary-light-3);
  font-size: 13px;
  font-weight: 700;
}

.panel-order__name {
  min-width: 0;
  color: var(--el-text-color-primary);
  font-size: 14px;
}

.panel-order__priority {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;
}

.panel-order__actions {
  display: flex;
  gap: 4px;
}

.panel-order__actions :deep(.el-button) {
  margin: 0;
}

@media (max-width: 600px) {
  .card-header,
  .card-content {
    padding-inline: 12px;
  }

  .panel-order__row {
    grid-template-columns: 30px minmax(0, 1fr) auto;
    gap: 8px;
    padding-inline: 8px;
  }

  .panel-order__rank {
    width: 26px;
    height: 26px;
  }

  .panel-order__priority {
    grid-column: 2;
    grid-row: 2;
  }

  .panel-order__actions {
    grid-column: 3;
    grid-row: 1 / span 2;
  }
}
</style>
