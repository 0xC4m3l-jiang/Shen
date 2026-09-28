<script setup lang="ts">
// 横向条形排行（归属地 / 后端等）：纯 CSS 实现，数据少时比图表更清晰也更轻。
import { computed } from 'vue'

export interface BarItem {
  label: string
  value: number
  tone?: 'primary' | 'accent' | 'muted'
  hint?: string
}

const props = defineProps<{ items: BarItem[]; empty?: string }>()
const max = computed(() => Math.max(1, ...props.items.map((i) => i.value)))
const fill: Record<string, string> = {
  primary: 'from-primary/80 to-secondary/60',
  accent: 'from-accent/80 to-primary/50',
  muted: 'from-muted-foreground/50 to-muted-foreground/20',
}
</script>

<template>
  <ul v-if="items.length" class="flex flex-col gap-2.5">
    <li v-for="(item, i) in items" :key="item.label" class="group flex flex-col gap-1 animate-rise" :style="{ animationDelay: `${i * 40}ms` }">
      <div class="flex items-center justify-between text-xs">
        <span class="truncate text-foreground/90" :title="item.hint ?? item.label">{{ item.label }}</span>
        <span class="tabular-nums text-muted-foreground">{{ item.value.toLocaleString('zh-CN') }}</span>
      </div>
      <div class="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          class="h-full rounded-full bg-gradient-to-r transition-[width] duration-700 ease-out group-hover:brightness-125"
          :class="fill[item.tone ?? 'primary']"
          :style="{ width: `${(item.value / max) * 100}%` }"
        />
      </div>
    </li>
  </ul>
  <p v-else class="py-6 text-center text-sm text-muted-foreground">{{ empty ?? '时间窗内暂无数据' }}</p>
</template>
