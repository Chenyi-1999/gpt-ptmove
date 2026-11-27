<!-- Vue hold-to-confirm control based on the React Bits Hold Button interaction. -->
<template>
  <button
    type="button"
    class="star-hold"
    :class="{
      'star-hold--compact': compact,
      'star-hold--toolbar': appearance === 'toolbar',
      'star-hold--text': appearance === 'text',
    }"
    :data-phase="phase"
    :disabled="disabled"
    :aria-label="label + '（长按 2 秒）'"
    :aria-describedby="hintId"
    :title="label + ' · 长按 2 秒'"
    @pointerdown="startPointer"
    @pointermove="movePointer"
    @pointerup="endPointer"
    @pointercancel="endPointer"
    @lostpointercapture="endPointer"
    @keydown="startKey"
    @keyup="endKey"
    @blur="cancel"
    @click.stop.prevent
    @contextmenu.prevent
  >
    <span class="star-hold__fill" aria-hidden="true"></span>
    <span class="star-hold__label" aria-hidden="true">
      <Delete v-if="compact || appearance === 'text'" />
      <slot v-if="!compact">{{ phase === 'done' ? '已确认' : '长按' + label }}</slot>
    </span>
    <span :id="hintId" class="star-hold__hint">按住按钮、空格键或回车键 2 秒，提前松开会取消。</span>
  </button>
</template>

<script setup lang="ts">
import { Delete } from '@element-plus/icons-vue'
import { onMounted, onUnmounted, ref, useId, watch } from 'vue'

const props = defineProps<{
  label: string
  disabled?: boolean
  compact?: boolean
  appearance?: 'toolbar' | 'text'
}>()
const emit = defineEmits<{ hold: [] }>()
const hintId = useId()
const phase = ref<'idle' | 'holding' | 'done'>('idle')
const HOLD_MS = 2000
let timer: ReturnType<typeof setTimeout> | undefined
let pointer: { id: number; rect: DOMRect } | undefined
let heldKey: string | undefined

function begin() {
  phase.value = 'holding'
  timer = setTimeout(() => {
    if (props.disabled || document.hidden || !document.hasFocus()) {
      cancel()
      return
    }
    phase.value = 'done'
    timer = setTimeout(() => { phase.value = 'idle' }, 1200)
    emit('hold')
  }, HOLD_MS)
}

function cancel() {
  pointer = undefined
  heldKey = undefined
  if (phase.value !== 'holding') return
  clearTimeout(timer)
  phase.value = 'idle'
}

function startPointer(event: PointerEvent) {
  if (event.button !== 0 || !event.isPrimary || props.disabled || phase.value !== 'idle' || pointer || heldKey) return
  const button = event.currentTarget as HTMLButtonElement
  button.setPointerCapture(event.pointerId)
  pointer = { id: event.pointerId, rect: button.getBoundingClientRect() }
  begin()
}

function movePointer(event: PointerEvent) {
  if (pointer?.id !== event.pointerId) return
  const { left, right, top, bottom } = pointer.rect
  if (event.clientX < left - 10 || event.clientX > right + 10 ||
      event.clientY < top - 10 || event.clientY > bottom + 10) cancel()
}

function endPointer(event: PointerEvent) {
  if (pointer?.id === event.pointerId) cancel()
}

function startKey(event: KeyboardEvent) {
  if (event.key !== ' ' && event.key !== 'Enter') return
  event.preventDefault()
  if (event.repeat || props.disabled || phase.value !== 'idle' || pointer || heldKey) return
  heldKey = event.key
  begin()
}

function endKey(event: KeyboardEvent) {
  if (heldKey === event.key) cancel()
}

watch(() => props.disabled, disabled => { if (disabled) cancel() }, { flush: 'sync' })
watch(() => props.label, cancel, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('blur', cancel)
  document.addEventListener('visibilitychange', cancel)
})
onUnmounted(() => {
  clearTimeout(timer)
  window.removeEventListener('blur', cancel)
  document.removeEventListener('visibilitychange', cancel)
})
</script>

<style scoped>
.star-hold {
  --star-hold-size: 40px;
  --star-hold-progress: var(--star-action);
  position: relative;
  isolation: isolate;
  box-sizing: border-box;
  display: inline-flex;
  justify-content: center;
  align-items: center;
  min-height: var(--star-hold-size);
  max-width: 100%;
  padding: 0 12px;
  border: 1px solid var(--star-border-strong);
  border-radius: 8px;
  background: var(--star-surface-muted);
  color: var(--star-danger);
  font: inherit;
  font-size: 13px;
  font-weight: 650;
  white-space: nowrap;
  user-select: none;
  touch-action: none;
  cursor: pointer;
  overflow: hidden;
  transition: transform 200ms, border-color 200ms, box-shadow 200ms;
}

.star-hold--compact { width: var(--star-hold-size); padding: 0; flex: none; }
.star-hold__label { display: inline-flex; align-items: center; gap: 6px; line-height: 1; }
.star-hold__label svg { width: 1em; height: 1em; }
.star-hold--compact .star-hold__label { font-size: 16px; }
.star-hold:focus-visible { outline: 2px solid var(--star-accent); outline-offset: 2px; }
.star-hold:disabled { opacity: 0.5; cursor: not-allowed; }
.star-hold:not(:disabled):hover {
  border-color: var(--star-danger);
  background: var(--star-danger-soft);
}

.star-hold--toolbar {
  --star-hold-size: var(--settings-action-height, 36px);
  border-radius: 6px;
}

.star-hold--text {
  --star-hold-size: var(--settings-action-height, 32px);
  --star-hold-progress: color-mix(in srgb, var(--star-danger) 22%, var(--star-surface));
  padding: 0 4px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  font-size: var(--el-font-size-base);
  font-weight: 500;
}

.star-hold__fill {
  position: absolute;
  z-index: -1;
  inset: 0 -6px 0 0;
  background: var(--star-hold-progress);
  border-radius: 0 6px 6px 0;
  transform: translateX(calc(-100% - 12px));
  transition: transform 200ms ease-out;
}

.star-hold__fill::after {
  position: absolute;
  top: -3px;
  right: -6px;
  bottom: -3px;
  width: 12px;
  border-radius: 50%;
  background: inherit;
  content: '';
}

.star-hold[data-phase='holding'] {
  transform: scale(0.98);
}

.star-hold:not(.star-hold--text):is([data-phase='holding'], [data-phase='done']) { color: var(--star-ink); }

.star-hold[data-phase='holding'] .star-hold__fill {
  transform: translateX(0);
  transition: transform 2s linear;
}

.star-hold[data-phase='holding'] .star-hold__fill::after {
  animation: star-hold-wave 600ms ease-in-out infinite alternate;
}

@keyframes star-hold-wave {
  from { transform: translateY(-6px) scaleX(0.45); }
  to { transform: translateY(6px) scaleX(1); }
}

.star-hold[data-phase='done'] .star-hold__fill { transform: translateX(0); }
.star-hold[data-phase='done'] { border-color: var(--star-accent); }

.star-hold__hint {
  position: absolute;
  width: 1px;
  height: 1px;
  clip-path: inset(50%);
  overflow: hidden;
}

@media (pointer: coarse) {
  .star-hold:not(.star-hold--toolbar, .star-hold--text) { --star-hold-size: 44px; }
}

@media (prefers-reduced-motion: reduce) {
  .star-hold, .star-hold[data-phase='holding'] .star-hold__fill { transition: none; }
  .star-hold[data-phase='holding'] { transform: none; outline: 2px solid var(--star-accent); }
  .star-hold[data-phase='holding'] .star-hold__fill::after { animation: none; }
}

@media (forced-colors: active) {
  .star-hold[data-phase='holding'] { outline: 2px solid Highlight; }
}
</style>
