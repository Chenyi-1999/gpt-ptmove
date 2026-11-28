<template>
  <section
    class="settings-card glass-card glass-rounded glass-transparent-header glass-transparent-body layout-panel"
    :aria-labelledby="`${layoutId}-title`"
  >
    <header class="card-header">
      <div class="header-content">
        <el-icon class="header-icon"><Grid /></el-icon>
        <h2 :id="`${layoutId}-title`">{{ title }}</h2>
      </div>
      <el-button
        type="primary"
        :loading="saving"
        :disabled="loading || Boolean(loadError) || !dirty"
        @click="saveLayout"
      >
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
            <span class="layout-label" :id="`${layoutId}-columns-label`">电脑端列数</span>
            <el-radio-group
              v-model="draftColumns"
              :aria-labelledby="`${layoutId}-columns-label`"
              :disabled="saving"
            >
              <el-radio-button :value="1">一列</el-radio-button>
              <el-radio-button :value="2">两列</el-radio-button>
            </el-radio-group>
          </div>
          <p class="layout-help">{{ description }}</p>
        </div>

        <p v-if="saveError" class="layout-feedback layout-feedback--error" role="alert">
          {{ saveError }}
        </p>
        <p v-else-if="savedMessage" class="layout-feedback" role="status">{{ savedMessage }}</p>

        <ol class="panel-order" :aria-label="`${title}模块顺序`">
          <li v-for="(panel, index) in draftOrder" :key="panel" class="panel-order__row">
            <span class="panel-order__rank" aria-hidden="true">{{ index + 1 }}</span>
            <label class="panel-order__toggle">
              <input
                type="checkbox"
                :checked="!draftHidden.includes(panel)"
                :disabled="saving"
                :aria-label="`显示${labels[panel]}`"
                @change="setPanelVisibility(panel, ($event.target as HTMLInputElement).checked)"
              />
              <span class="panel-order__name">{{ labels[panel] }}</span>
            </label>
            <span v-if="draftHidden.includes(panel)" class="panel-order__priority">已隐藏</span>
            <span v-else-if="panel === firstVisible" class="panel-order__priority">最高优先级</span>
            <div class="panel-order__actions" :aria-label="`${labels[panel]}排序`">
              <el-button
                :icon="ArrowUp"
                :aria-label="`上移${labels[panel]}`"
                :disabled="saving || index === 0"
                @click="movePanel(index, -1)"
              />
              <el-button
                :icon="ArrowDown"
                :aria-label="`下移${labels[panel]}`"
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

<script setup lang="ts" generic="PanelID extends string">
import { ArrowDown, ArrowUp, Grid } from '@element-plus/icons-vue'
import { computed, onMounted, ref, shallowRef } from 'vue'
import type { DashboardLayout } from '@/stores/dashboardLayout'

const props = defineProps<{
  layoutId: string
  title: string
  description: string
  labels: Readonly<Record<PanelID, string>>
  store: {
    columns: 1 | 2
    order: PanelID[]
    hidden: PanelID[]
    load(force?: boolean): Promise<void>
    save(draft: DashboardLayout<PanelID>): Promise<void>
  }
}>()

const draftColumns = ref<1 | 2>(2)
const draftOrder = shallowRef<PanelID[]>([])
const draftHidden = shallowRef<PanelID[]>([])
const saved = shallowRef<DashboardLayout<PanelID>>({ columns: 2, order: [], hidden: [] })
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const savedMessage = ref('')

const dirty = computed(
  () =>
    draftColumns.value !== saved.value.columns ||
    draftOrder.value.some((panel, index) => panel !== saved.value.order[index]) ||
    draftHidden.value.length !== saved.value.hidden.length ||
    draftHidden.value.some((panel) => !saved.value.hidden.includes(panel)),
)
const firstVisible = computed(() =>
  draftOrder.value.find((panel) => !draftHidden.value.includes(panel)),
)

function syncFromStore() {
  saved.value = {
    columns: props.store.columns,
    order: [...props.store.order],
    hidden: [...props.store.hidden],
  }
  draftColumns.value = saved.value.columns
  draftOrder.value = [...saved.value.order]
  draftHidden.value = [...saved.value.hidden]
}

function clearSaveFeedback() {
  saveError.value = ''
  savedMessage.value = ''
}

async function loadLayout() {
  loading.value = true
  loadError.value = ''
  clearSaveFeedback()
  try {
    await props.store.load(true)
    syncFromStore()
  } catch (error) {
    loadError.value =
      error instanceof Error ? `布局设置读取失败：${error.message}` : '布局设置读取失败，请重试。'
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
  clearSaveFeedback()
}

function setPanelVisibility(panel: PanelID, visible: boolean) {
  draftHidden.value = visible
    ? draftHidden.value.filter((id) => id !== panel)
    : [...draftHidden.value, panel]
  clearSaveFeedback()
}

async function saveLayout() {
  if (!dirty.value || saving.value) return
  saving.value = true
  clearSaveFeedback()
  try {
    await props.store.save({
      columns: draftColumns.value,
      order: [...draftOrder.value],
      hidden: [...draftHidden.value],
    })
    syncFromStore()
    savedMessage.value = `${props.title}已保存。`
  } catch (error) {
    saveError.value =
      error instanceof Error ? `保存失败：${error.message}` : '保存失败，请稍后重试。'
  } finally {
    saving.value = false
  }
}

onMounted(loadLayout)
</script>

<style scoped>
.layout-panel {
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

.header-content h2 {
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

.panel-order__toggle {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
  min-height: 32px;
  cursor: pointer;
}

.panel-order__toggle input {
  flex: none;
  width: 16px;
  height: 16px;
  margin: 0;
  accent-color: var(--star-accent);
}

.panel-order__toggle input:focus-visible {
  outline: 2px solid var(--star-accent);
  outline-offset: 3px;
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
  grid-column: -2;
  grid-row: 1;
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
  }

  .panel-order__row:has(.panel-order__priority) .panel-order__actions {
    grid-row: 1 / span 2;
  }
}
</style>
