<script setup lang="ts">
// 流星（Inspira UI「Meteors」思路，MIT）：少量、低频，位置与延迟在挂载时随机一次，之后不再计算。
import { onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{ count?: number }>(), { count: 14 })
const meteors = ref<{ left: string; top: string; delay: string; duration: string }[]>([])

onMounted(() => {
  meteors.value = Array.from({ length: props.count }, () => ({
    left: `${Math.floor(Math.random() * 110) - 5}%`,
    top: `${Math.floor(Math.random() * 40) - 10}%`,
    delay: `${(Math.random() * 8).toFixed(2)}s`,
    duration: `${(Math.random() * 6 + 5).toFixed(2)}s`,
  }))
})
</script>

<template>
  <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
    <span
      v-for="(m, i) in meteors"
      :key="i"
      class="meteor absolute h-px w-px rotate-[215deg] animate-meteor rounded-full bg-primary shadow-[0_0_0_1px_hsl(var(--primary)/0.08)]"
      :style="{ left: m.left, top: m.top, animationDelay: m.delay, animationDuration: m.duration }"
    />
  </div>
</template>

<style scoped>
.meteor::before {
  content: '';
  position: absolute;
  top: 50%;
  width: 70px;
  height: 1px;
  transform: translateY(-50%);
  background: linear-gradient(90deg, hsl(var(--primary)), transparent);
}
</style>
