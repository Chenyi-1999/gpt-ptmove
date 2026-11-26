<!-- Vue implementation of the pill/fill/dot configuration from React Bits Pill Nav. -->
<template>
  <nav ref="nav" class="star-pill-nav" aria-label="主导航">
    <div ref="track" class="star-pill-nav__track">
      <RouterLink
        v-for="item in items"
        :key="item.to"
        :to="item.to"
        class="star-pill-nav__link"
        :aria-current="activeHref === item.to ? 'page' : undefined"
      >
        <component :is="item.icon" class="star-pill-nav__icon" aria-hidden="true" />
        <span class="star-pill-nav__roll">
          <span>{{ item.label }}</span>
          <span aria-hidden="true">{{ item.label }}</span>
        </span>
      </RouterLink>
      <span v-if="dot !== null" class="star-pill-nav__dot" :style="{ left: dot + 'px' }" aria-hidden="true" />
    </div>
  </nav>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch, type Component } from 'vue'

const props = defineProps<{
  items: { to: string; label: string; icon: Component }[]
  activeHref: string
}>()

const nav = ref<HTMLElement>()
const track = ref<HTMLElement>()
const dot = ref<number | null>(null)
let observer: ResizeObserver | undefined

function moveDot() {
  const link = track.value?.querySelector<HTMLElement>('[aria-current="page"]')
  dot.value = link ? link.offsetLeft + link.offsetWidth / 2 : null
  if (link && nav.value) {
    const left = link.offsetLeft
    const right = left + link.offsetWidth
    if (left < nav.value.scrollLeft) nav.value.scrollLeft = left
    if (right > nav.value.scrollLeft + nav.value.clientWidth) {
      nav.value.scrollLeft = right - nav.value.clientWidth
    }
  }
}

watch(() => props.activeHref, moveDot, { flush: 'post' })
onMounted(() => {
  observer = new ResizeObserver(moveDot)
  if (track.value) observer.observe(track.value)
  if (nav.value) observer.observe(nav.value)
  moveDot()
})
onUnmounted(() => observer?.disconnect())
</script>

<style scoped>
.star-pill-nav {
  box-sizing: border-box;
  width: max-content;
  min-width: 0;
  margin-inline: auto;
  padding: 2px;
  border: 1px solid #43385f;
  border-radius: 28px;
  background: #1f1a32;
  box-shadow: inset 0 1px rgb(239 222 255 / 13%), 0 10px 22px -12px #05030e;
  overflow: auto;
  scrollbar-width: thin;
}

.star-pill-nav__track {
  position: relative;
  display: flex;
  gap: 3px;
  width: max-content;
  overflow: clip;
}

.star-pill-nav__link {
  position: relative;
  isolation: isolate;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 38px;
  padding: 0 clamp(7px, 0.8vw, 12px) 3px;
  border-radius: 999px;
  overflow: hidden;
  font-size: 14px;
  font-weight: 650;
  color: var(--star-muted);
  white-space: nowrap;
  text-decoration: none;
  box-sizing: border-box;
}

.star-pill-nav__link::before {
  position: absolute;
  z-index: -1;
  inset: 0;
  border-radius: inherit;
  background: linear-gradient(180deg, #7454cf, #4c338b);
  box-shadow: inset 0 1px rgb(255 255 255 / 22%);
  transform: translateY(105%);
  transition: transform 280ms cubic-bezier(0.22, 1, 0.36, 1);
  content: '';
}

.star-pill-nav__link[aria-current='page'] {
  background: #3a2a60;
  color: #f7edff;
}

.star-pill-nav__link:is(:hover, :focus-visible) {
  color: #fff;
}

.star-pill-nav__link:is(:hover, :focus-visible)::before {
  transform: translateY(0);
}

.star-pill-nav__link:focus-visible {
  outline: 2px solid var(--star-accent-hover);
  outline-offset: -2px;
}

.star-pill-nav__icon {
  width: 16px;
  height: 16px;
  flex: none;
}

.star-pill-nav__roll {
  display: grid;
  height: 1.35em;
  line-height: 1.35;
  overflow: hidden;
}

.star-pill-nav__roll span {
  transition: transform 280ms cubic-bezier(0.22, 1, 0.36, 1);
}

.star-pill-nav__link:is(:hover, :focus-visible) .star-pill-nav__roll span {
  transform: translateY(-100%);
}

.star-pill-nav__dot {
  position: absolute;
  bottom: 3px;
  width: 4px;
  height: 4px;
  border-radius: 50%;
  transform: translateX(-50%);
  background: #e5d6ff;
  box-shadow: 0 0 6px #bd9aff;
  transition: left 300ms cubic-bezier(0.22, 1, 0.36, 1);
  pointer-events: none;
}

@supports (backdrop-filter: blur(16px)) {
  .star-pill-nav {
    background: rgb(49 38 74 / 58%);
    backdrop-filter: blur(16px) saturate(140%);
  }
}

@media (prefers-reduced-transparency: reduce) {
  .star-pill-nav { background: #1f1a32; backdrop-filter: none; }
}

@media (max-width: 1280px) {
  .star-pill-nav__link { padding-inline: 8px; }
  .star-pill-nav__icon { display: none; }
}

@media (forced-colors: active) {
  .star-pill-nav, .star-pill-nav__link[aria-current='page'] { border: 1px solid Highlight; }
}
</style>
