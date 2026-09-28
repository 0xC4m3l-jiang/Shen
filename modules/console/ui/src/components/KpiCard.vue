<script setup lang="ts">
import type { Component } from 'vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import NumberTicker from '@/components/fx/NumberTicker.vue'

withDefaults(
  defineProps<{
    label: string
    value?: number
    /** 非数值指标（如「12 秒前」）：给出时显示文本而不是滚动数字 */
    text?: string
    icon: Component
    suffix?: string
    decimals?: number
    hint?: string
    tone?: 'primary' | 'mirage' | 'decoy' | 'warn' | 'info'
    beam?: boolean
  }>(),
  { tone: 'primary', decimals: 0, suffix: '', value: 0 },
)
const tones = {
  primary: 'bg-primary/10 text-primary',
  mirage: 'bg-mirage/10 text-mirage',
  decoy: 'bg-decoy/10 text-decoy',
  warn: 'bg-warn/10 text-warn',
  info: 'bg-info/10 text-info',
}
</script>

<template>
  <SpotlightCard :beam="beam" class="p-5">
    <div class="flex items-start justify-between gap-3">
      <div class="flex min-w-0 flex-col gap-2">
        <span class="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">{{ label }}</span>
        <span class="text-3xl font-semibold tracking-tight text-foreground">
          <span v-if="text !== undefined">{{ text }}</span>
          <NumberTicker v-else :value="value" :decimals="decimals" :suffix="suffix" />
        </span>
        <span v-if="hint" class="truncate text-xs text-muted-foreground">{{ hint }}</span>
        <slot />
      </div>
      <div class="rounded-lg p-2" :class="tones[tone]">
        <component :is="icon" class="h-5 w-5" />
      </div>
    </div>
  </SpotlightCard>
</template>
