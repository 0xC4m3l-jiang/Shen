<script setup lang="ts">
// 数字滚动（Inspira UI「Number Ticker」思路，MIT）：从旧值缓动到新值；动效关闭时直接显示终值。
import { onBeforeUnmount, ref, watch } from 'vue'
import { usePrefsStore } from '@/stores/prefs'

const props = withDefaults(defineProps<{ value: number; decimals?: number; suffix?: string; duration?: number }>(), {
  decimals: 0,
  suffix: '',
  duration: 900,
})
const prefs = usePrefsStore()
const shown = ref(0)
let frame = 0

const fmt = (n: number) =>
  n.toLocaleString('zh-CN', { minimumFractionDigits: props.decimals, maximumFractionDigits: props.decimals })

watch(
  () => props.value,
  (to) => {
    cancelAnimationFrame(frame)
    if (prefs.systemReduced) {
      shown.value = to
      return
    }
    const from = shown.value
    const start = performance.now()
    const step = (now: number) => {
      const t = Math.min(1, (now - start) / props.duration)
      const eased = 1 - Math.pow(1 - t, 4)
      shown.value = from + (to - from) * eased
      if (t < 1) frame = requestAnimationFrame(step)
    }
    frame = requestAnimationFrame(step)
  },
  { immediate: true },
)

onBeforeUnmount(() => cancelAnimationFrame(frame))
</script>

<template>
  <span class="tabular-nums">{{ fmt(shown) }}{{ suffix }}</span>
</template>
