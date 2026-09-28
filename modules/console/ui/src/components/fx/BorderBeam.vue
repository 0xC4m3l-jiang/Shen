<script setup lang="ts">
// 描边流光（Inspira UI / Magic UI「Border Beam」思路，MIT）：一段渐变光沿卡片边框匀速绕行。
// 靠 CSS offset-path + mask-composite 实现，无 JS 帧循环。
withDefaults(defineProps<{ size?: number; duration?: number; delay?: number }>(), {
  size: 160,
  duration: 9,
  delay: 0,
})
</script>

<template>
  <div
    class="beam pointer-events-none absolute inset-0 rounded-[inherit]"
    :style="{ '--size': size, '--duration': duration, '--delay': `-${delay}s` }"
    aria-hidden="true"
  />
</template>

<style scoped>
.beam {
  border: 1px solid transparent;
  -webkit-mask:
    linear-gradient(transparent, transparent) padding-box,
    linear-gradient(#000, #000) border-box;
  -webkit-mask-composite: source-in, xor;
  mask:
    linear-gradient(transparent, transparent) padding-box,
    linear-gradient(#000, #000) border-box;
  mask-composite: intersect;
}
.beam::after {
  content: '';
  position: absolute;
  aspect-ratio: 1;
  width: calc(var(--size) * 1px);
  offset-anchor: 90% 50%;
  offset-path: rect(0 auto auto 0 round calc(var(--size) * 1px));
  background: linear-gradient(to left, hsl(var(--primary)), hsl(var(--accent)), transparent);
  animation: border-beam calc(var(--duration) * 1s) infinite linear;
  animation-delay: var(--delay);
}
</style>
