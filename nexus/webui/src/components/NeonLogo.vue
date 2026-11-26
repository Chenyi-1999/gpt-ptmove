<!-- STARPT's compact CSS wordmark, inspired by React Bits NeonLogo. -->
<template>
  <RouterLink
    to="/"
    class="star-neon desktop-no-drag"
    aria-label="STARPT 首页"
    @pointermove="moveLight"
  >
    <span class="star-neon__art" aria-hidden="true">
      <img src="/starpt-wordmark.svg" width="114" height="26" alt="" />
    </span>
  </RouterLink>
</template>

<script setup lang="ts">
function moveLight(event: PointerEvent) {
  if (event.pointerType === 'touch') return
  const brand = event.currentTarget as HTMLElement
  const { left, width } = brand.getBoundingClientRect()
  brand.style.setProperty('--neon-x', `${(event.clientX - left) / width * 100}%`)
}
</script>

<style scoped>
.star-neon {
  display: inline-flex;
  flex: none;
  align-items: center;
  min-height: 44px;
  border-radius: 8px;
}

.star-neon__art {
  position: relative;
  display: block;
  filter: drop-shadow(0 0 4px rgb(166 120 255 / 36%));
  transition: filter 200ms ease-out;
}

.star-neon__art img {
  display: block;
}

.star-neon:is(:hover, :focus-visible) .star-neon__art {
  filter: drop-shadow(0 0 6px rgb(166 120 255 / 73%));
}

.star-neon:active .star-neon__art {
  filter: drop-shadow(0 0 8px #a569ff) brightness(1.28);
}

@media (hover: hover) and (prefers-reduced-motion: no-preference) {
  .star-neon__art::after {
    position: absolute;
    inset: 0;
    mask: url('/starpt-wordmark.svg') center / contain no-repeat;
    background: radial-gradient(ellipse 34px 21px at var(--neon-x, 24%) 50%, #fff 0%, #c69fff 45%, transparent 100%);
    opacity: 0;
    transition: opacity 200ms ease-out;
    pointer-events: none;
    content: '';
  }

  .star-neon:hover .star-neon__art::after {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .star-neon__art { transition: none; }
}

@media (forced-colors: active) {
  .star-neon__art { filter: none; }
}
</style>
