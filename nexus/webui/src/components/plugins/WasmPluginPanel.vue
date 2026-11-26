<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  requestPlugin,
  type PluginActionResult,
  type PluginField,
  type PluginSetting,
  type WasmPlugin,
} from './pluginApi'

const props = defineProps<{ plugin: WasmPlugin; refreshing: boolean }>()
const emit = defineEmits<{
  updated: [plugin: WasmPlugin]
  busy: [busy: boolean]
}>()

const fields = computed(() => props.plugin.fields ?? [])
const pending = ref('')
const draft = ref<Record<string, PluginSetting>>({})
const error = ref('')
const notice = ref('')
const result = ref<PluginActionResult>()
const busy = computed(() => props.refreshing || Boolean(pending.value))

type ScheduleSettings = {
  enabled: boolean
  action: string
  interval_minutes: number
}

type PluginSchedule = ScheduleSettings & {
  next_at?: number
  last_run_at?: number
  last_status?: string
}

const schedule = ref<PluginSchedule>()
const scheduleDraft = ref<ScheduleSettings>({ enabled: false, action: '', interval_minutes: 60 })
const scheduleLoading = ref(false)
const scheduleError = ref('')
const scheduleStatuses: Record<string, string> = {
  running: '运行中',
  succeeded: '成功',
  failed: '失败',
  interrupted: '已中断',
}
const scheduleChanged = computed(() => {
  const current = schedule.value
  const edited = scheduleDraft.value
  return Boolean(
    current &&
      (edited.enabled !== current.enabled ||
        edited.action !== (current.action || props.plugin.actions[0]?.id || '') ||
        edited.interval_minutes !== (current.interval_minutes || 60)),
  )
})

function defaultValue(field: PluginField): PluginSetting {
  switch (field.type) {
    case 'boolean':
      return field.default === true
    case 'integer':
      return typeof field.default === 'number' ? field.default : ''
    case 'string':
      return typeof field.default === 'string' ? field.default : (field.options?.[0] ?? '')
  }
}

const saved = computed<Record<string, PluginSetting>>(() =>
  Object.fromEntries(
    fields.value.map((field) => [
      field.key,
      props.plugin.settings?.[field.key] ?? defaultValue(field),
    ]),
  ),
)
const dirty = computed(() =>
  fields.value.some(({ key }) => !Object.is(draft.value[key], saved.value[key])),
)
const savedSignature = computed(() => JSON.stringify([props.plugin.version, fields.value, saved.value]))

watch(
  savedSignature,
  () => {
    draft.value = { ...saved.value }
    result.value = undefined
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  if (pending.value) emit('busy', false)
})

function setInteger(key: string, event: Event) {
  const value = (event.target as HTMLInputElement).valueAsNumber
  draft.value[key] = Number.isFinite(value) ? value : ''
}

function fieldId(key: string) {
  return `plugin-field-${props.plugin.id}-${key}`
}

async function perform(operation: string, work: () => Promise<void>) {
  if (busy.value) return
  pending.value = operation
  emit('busy', true)
  error.value = ''
  notice.value = ''
  try {
    await work()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '操作失败，请稍后重试'
  } finally {
    pending.value = ''
    emit('busy', false)
  }
}

const pluginPath = computed(() => '/' + encodeURIComponent(props.plugin.id))

async function save() {
  if (!dirty.value) return
  const invalid = fields.value.find(
    ({ key, type }) => type === 'integer' && !Number.isSafeInteger(draft.value[key]),
  )
  if (invalid) {
    error.value = `「${invalid.label}」需要填写整数`
    return
  }
  await perform('save', async () => {
    const data = await requestPlugin<{ plugin: WasmPlugin }>(
      pluginPath.value + '/settings',
      'PUT',
      '保存设置失败',
      { settings: draft.value },
    )
    emit('updated', data.plugin)
    notice.value = '设置已保存'
  })
}

async function toggle() {
  const enabled = !props.plugin.enabled
  await perform('toggle', async () => {
    const data = await requestPlugin<{ plugin: WasmPlugin }>(
      pluginPath.value + '/enabled',
      'PUT',
      '更新插件状态失败',
      { enabled },
    )
    emit('updated', data.plugin)
    notice.value = enabled ? '插件已启用' : '插件已停用'
  })
}

async function run(action: string) {
  if (!props.plugin.enabled || dirty.value) return
  await perform('action:' + action, async () => {
    const data = await requestPlugin<{ result: PluginActionResult }>(
      pluginPath.value + '/actions/' + encodeURIComponent(action),
      'POST',
      '运行插件操作失败',
    )
    result.value = data.result
    notice.value = data.result.message || '操作已完成'
  })
}

function installSchedule(settings: PluginSchedule) {
  schedule.value = settings
  scheduleDraft.value = {
    enabled: settings.enabled,
    action: settings.action || props.plugin.actions[0]?.id || '',
    interval_minutes: settings.interval_minutes || 60,
  }
}

async function loadSchedule() {
  if (scheduleLoading.value) return
  scheduleLoading.value = true
  scheduleError.value = ''
  try {
    const data = await requestPlugin<{ schedule: PluginSchedule }>(
      pluginPath.value + '/schedule',
      'GET',
      '无法读取定时设置',
    )
    installSchedule(data.schedule)
  } catch (err) {
    scheduleError.value = err instanceof Error ? err.message : '无法读取定时设置'
  } finally {
    scheduleLoading.value = false
  }
}

function formatTime(seconds?: number) {
  return seconds && seconds > 0 ? new Date(seconds * 1000).toLocaleString() : '—'
}

async function saveSchedule() {
  if (!scheduleChanged.value) return
  const { enabled, action: selectedAction } = scheduleDraft.value
  const hasAction = props.plugin.actions.some((item) => item.id === selectedAction)
  const action = hasAction || enabled ? selectedAction : (props.plugin.actions[0]?.id ?? '')
  const interval = enabled
    ? scheduleDraft.value.interval_minutes
    : (schedule.value?.interval_minutes || 60)
  if (enabled && !hasAction) {
    scheduleError.value = '请选择可运行的定时操作'
    return
  }
  if (!Number.isSafeInteger(interval) || interval < 1 || interval > 10080) {
    scheduleError.value = '时间间隔需为 1 到 10080 分钟之间的整数'
    return
  }
  await perform('schedule', async () => {
    scheduleError.value = ''
    try {
      const data = await requestPlugin<{ schedule: PluginSchedule }>(
        pluginPath.value + '/schedule',
        'PUT',
        '保存定时设置失败',
        { enabled, action, interval_minutes: interval },
      )
      installSchedule(data.schedule)
      notice.value = '定时设置已保存'
    } catch (err) {
      scheduleError.value = err instanceof Error ? err.message : '保存定时设置失败'
    }
  })
}

onMounted(() => void loadSchedule())
</script>

<template>
  <div class="wasm-panel" :aria-busy="busy">
    <div class="package-state">
      <span class="status-label" :class="{ 'status-label--active': plugin.enabled }">
        {{ plugin.enabled ? '已启用' : '已停用' }}
      </span>
      <el-button :disabled="busy" :loading="pending === 'toggle'" @click="toggle">
        {{ plugin.enabled ? '停用插件' : '启用插件' }}
      </el-button>
    </div>

    <p v-if="error" class="panel-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="panel-note" role="status">{{ notice }}</p>

    <form v-if="fields.length" class="package-section" @submit.prevent="save">
      <h3>插件设置</h3>
      <div class="package-fields">
        <div v-for="field in fields" :key="field.key" class="package-field">
          <label :for="fieldId(field.key)">{{ field.label }}</label>
          <el-switch
            v-if="field.type === 'boolean'"
            :id="fieldId(field.key)"
            :model-value="draft[field.key] === true"
            :aria-label="field.label"
            :disabled="busy"
            @update:model-value="draft[field.key] = $event === true"
          />
          <input
            v-else-if="field.type === 'integer'"
            :id="fieldId(field.key)"
            :value="draft[field.key]"
            :disabled="busy"
            type="number"
            step="1"
            required
            @input="setInteger(field.key, $event)"
          />
          <select
            v-else-if="field.options?.length"
            :id="fieldId(field.key)"
            v-model="draft[field.key]"
            :disabled="busy"
          >
            <option v-for="option in field.options" :key="option" :value="option">{{ option }}</option>
          </select>
          <input
            v-else
            :id="fieldId(field.key)"
            v-model="draft[field.key]"
            :disabled="busy"
            type="text"
            autocomplete="off"
          />
        </div>
      </div>
      <el-button :disabled="busy || !dirty" :loading="pending === 'save'" type="primary" native-type="submit">
        保存设置
      </el-button>
    </form>

    <section v-if="plugin.actions.length" class="package-section">
      <h3>操作</h3>
      <p v-if="!plugin.enabled" class="panel-note">启用插件后可以运行操作。</p>
      <p v-else-if="dirty" class="panel-note">保存设置后可以运行操作。</p>
      <div class="package-actions">
        <el-button
          v-for="action in plugin.actions"
          :key="action.id"
          :disabled="busy || !plugin.enabled || dirty"
          :loading="pending === 'action:' + action.id"
          @click="run(action.id)"
        >
          {{ action.label }}
        </el-button>
      </div>
    </section>

    <section v-if="result?.blocks?.length" class="package-section">
      <h3>运行结果</h3>
      <div class="package-results">
        <article
          v-for="(block, index) in result.blocks"
          :key="index"
          class="package-result"
          :class="{ 'package-result--wide': block.kind === 'table' }"
        >
          <h4>{{ block.title }}</h4>
          <p v-if="block.kind === 'metric'" class="package-metric">{{ block.value }}</p>
          <div
            v-else-if="block.kind === 'table'"
            class="package-table"
            tabindex="0"
            role="region"
            :aria-label="block.title"
          >
            <table>
              <thead>
                <tr>
                  <th v-for="(column, columnIndex) in block.columns" :key="columnIndex" scope="col">
                    {{ column }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, rowIndex) in block.rows" :key="rowIndex">
                  <td v-for="(cell, cellIndex) in row" :key="cellIndex">{{ cell }}</td>
                </tr>
              </tbody>
            </table>
            <p v-if="!block.rows?.length" class="panel-note">暂无数据</p>
          </div>
          <p v-if="block.text" class="panel-note">{{ block.text }}</p>
        </article>
      </div>
    </section>

    <form class="package-section" @submit.prevent="saveSchedule">
      <h3>定时运行</h3>
      <p v-if="scheduleLoading" class="panel-note" role="status">正在读取定时设置…</p>
      <p v-if="scheduleError" class="panel-error" role="alert">{{ scheduleError }}</p>
      <el-button v-if="!schedule && scheduleError" :disabled="busy" @click="loadSchedule">
        重试读取
      </el-button>
      <template v-if="schedule">
        <div class="schedule-toggle">
          <el-switch
            v-model="scheduleDraft.enabled"
            :disabled="busy"
            aria-label="启用定时运行"
          />
          <span>启用定时运行</span>
        </div>
        <div v-if="scheduleDraft.enabled" class="package-fields">
          <div class="package-field">
            <label :for="'plugin-schedule-action-' + plugin.id">定时操作</label>
            <select
              :id="'plugin-schedule-action-' + plugin.id"
              v-model="scheduleDraft.action"
              :disabled="busy"
              required
            >
              <option v-for="action in plugin.actions" :key="action.id" :value="action.id">
                {{ action.label }}
              </option>
            </select>
          </div>
          <div class="package-field">
            <label :for="'plugin-schedule-interval-' + plugin.id">间隔（分钟）</label>
            <input
              :id="'plugin-schedule-interval-' + plugin.id"
              v-model.number="scheduleDraft.interval_minutes"
              :disabled="busy"
              type="number"
              min="1"
              max="10080"
              step="1"
              required
            />
          </div>
        </div>
        <div v-if="schedule.next_at || schedule.last_run_at" class="schedule-times">
          <p v-if="schedule.enabled && plugin.enabled" class="panel-note">
            下次运行 {{ formatTime(schedule.next_at) }}
          </p>
          <p v-if="schedule.last_run_at" class="panel-note">
            最近运行 {{ formatTime(schedule.last_run_at) }}
            {{ scheduleStatuses[schedule.last_status ?? ''] ?? '' }}
          </p>
        </div>
        <el-button
          :disabled="busy || !scheduleChanged"
          :loading="pending === 'schedule'"
          native-type="submit"
        >
          保存定时设置
        </el-button>
      </template>
    </form>
  </div>
</template>

<style scoped>
.wasm-panel { display: grid; min-width: 0; gap: 16px; margin-top: 18px; }
.package-state, .package-actions, .schedule-toggle { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; }
.schedule-times { display: grid; gap: 4px; }
.status-label { color: var(--star-muted); font-size: 0.875rem; font-weight: 600; }
.status-label--active { color: var(--star-accent); }
.package-section {
  display: grid;
  min-width: 0;
  justify-items: start;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--star-border);
  border-radius: 12px;
}
.package-section h3, .package-result h4 { margin: 0; font-size: 0.95rem; }
.package-fields, .package-results {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
}
.package-field { display: grid; min-width: 0; align-content: start; gap: 6px; }
.package-field label { color: var(--star-ink); overflow-wrap: anywhere; }
.package-field input, .package-field select {
  box-sizing: border-box;
  width: 100%;
  min-height: 38px;
  padding: 6px 9px;
  border: 1px solid var(--star-border);
  border-radius: 8px;
  background: var(--star-canvas);
  color: var(--star-ink);
  font: inherit;
}
.package-actions :deep(.el-button) { margin: 0; }
.package-result {
  min-width: 0;
  padding: 12px;
  border-radius: 9px;
  background: var(--star-surface-muted);
  overflow-wrap: anywhere;
}
.package-result--wide { grid-column: 1 / -1; }
.package-metric { margin: 6px 0 0; font-size: 1.45rem; font-weight: 650; }
.panel-note, .panel-error { margin: 0; color: var(--star-muted); overflow-wrap: anywhere; }
.package-result .panel-note { margin-top: 6px; }
.panel-error {
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--star-warning-soft, #302719);
  color: var(--star-warning-ink, #ffd27a);
}
.package-table { max-width: 100%; overflow-x: auto; margin-top: 8px; }
.package-table table { width: 100%; border-collapse: collapse; text-align: start; }
.package-table th, .package-table td {
  padding: 8px;
  border-bottom: 1px solid var(--star-border);
  text-align: start;
  vertical-align: top;
}
:where(input, select, .package-table):focus-visible { outline: 2px solid var(--star-accent); outline-offset: 2px; }
</style>
