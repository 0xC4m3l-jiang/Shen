<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

export type BadgeTone = 'primary' | 'mirage' | 'decoy' | 'origin' | 'warn' | 'danger' | 'info' | 'muted'

const props = withDefaults(defineProps<{ tone?: BadgeTone; dot?: boolean; class?: HTMLAttributes['class'] }>(), {
  tone: 'primary',
})

// 类名必须以完整字面量出现（Tailwind 按源码扫描生成样式，拼接出的类名不会生成）。
const tones: Record<BadgeTone, string> = {
  primary: 'bg-primary/10 text-primary border-primary/30',
  mirage: 'bg-mirage/10 text-mirage border-mirage/35',
  decoy: 'bg-decoy/10 text-decoy border-decoy/35',
  origin: 'bg-origin/10 text-origin border-origin/35',
  warn: 'bg-warn/10 text-warn border-warn/35',
  danger: 'bg-danger/10 text-danger border-danger/35',
  info: 'bg-info/10 text-info border-info/35',
  muted: 'bg-muted text-muted-foreground border-border',
}
const dots: Record<BadgeTone, string> = {
  primary: 'bg-primary',
  mirage: 'bg-mirage',
  decoy: 'bg-decoy',
  origin: 'bg-origin',
  warn: 'bg-warn',
  danger: 'bg-danger',
  info: 'bg-info',
  muted: 'bg-muted-foreground',
}
</script>

<template>
  <span
    :class="
      cn(
        'inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-xs font-medium leading-5',
        tones[props.tone],
        props.class,
      )
    "
  >
    <span v-if="props.dot" :class="cn('h-1.5 w-1.5 rounded-full', dots[props.tone])" />
    <slot />
  </span>
</template>
