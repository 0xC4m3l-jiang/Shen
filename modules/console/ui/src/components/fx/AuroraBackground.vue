<script setup lang="ts">
// 极光背景（Inspira UI「Aurora Background」的实现思路，MIT）：两层重复渐变 + 模糊 + 缓慢平移。
// 颜色取主题令牌 --aurora-a/b/c；页面不可见时暂停动画（省电、省 CPU）。
import { useDocumentVisibility } from '@vueuse/core'
import { computed } from 'vue'

const props = withDefaults(defineProps<{ intensity?: number }>(), { intensity: 1 })
const visibility = useDocumentVisibility()
const playState = computed(() => (visibility.value === 'visible' ? 'running' : 'paused'))
</script>

<template>
  <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
    <div
      class="aurora-layer absolute -inset-[10px] animate-aurora opacity-50 blur-[10px] will-change-transform"
      :style="{ animationPlayState: playState, opacity: 0.45 * props.intensity }"
    />
    <div class="absolute inset-0 bg-gradient-to-b from-transparent via-background/40 to-background" />
  </div>
</template>

<style scoped>
.aurora-layer {
  --stripes: repeating-linear-gradient(100deg, hsl(var(--background)) 0%, hsl(var(--background)) 7%, transparent 10%, transparent 12%, hsl(var(--background)) 16%);
  --rainbow: repeating-linear-gradient(
    100deg,
    hsl(var(--aurora-a)) 10%,
    hsl(var(--aurora-b)) 16%,
    hsl(var(--aurora-c)) 22%,
    hsl(var(--aurora-a)) 28%,
    hsl(var(--aurora-b)) 34%
  );
  background-image: var(--stripes), var(--rainbow);
  background-size: 300%, 200%;
  background-position: 50% 50%, 50% 50%;
  mask-image: radial-gradient(ellipse at 70% 0%, black 10%, transparent 72%);
}
.aurora-layer::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image: var(--stripes), var(--rainbow);
  background-size: 200%, 100%;
  background-attachment: fixed;
  mix-blend-mode: difference;
  animation: aurora 60s linear infinite;
  animation-play-state: inherit;
}
</style>
