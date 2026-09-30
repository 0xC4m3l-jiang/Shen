<script setup lang="ts">
import { computed } from 'vue'
import { ArrowUpRight, ShieldAlert, SlidersHorizontal } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import type { ConfigSyncCard } from '@/lib/config'

const props = defineProps<{ card: ConfigSyncCard; systemAlerts: number }>()

const meta: Record<string, { label: string; tone: 'origin' | 'warn' | 'danger' | 'muted' }> = {
  synced: { label: '已同步', tone: 'origin' },
  pending: { label: '生效中', tone: 'warn' },
  error: { label: '被核心拒绝', tone: 'danger' },
  offline: { label: '核心离线', tone: 'danger' },
  unconfigured: { label: '未启用同步', tone: 'muted' },
}
const m = computed(() => meta[props.card.state] ?? meta.unconfigured)
const ratio = computed(() => (props.card.honeypots_total ? props.card.honeypots_healthy / props.card.honeypots_total : 0))
const R = 26
const C = 2 * Math.PI * R
</script>

<template>
  <RouterLink :to="{ name: 'config' }" class="block">
    <SpotlightCard class="group p-5" body-class="flex flex-wrap items-center gap-6">
      <div class="flex items-center gap-3">
        <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary"><SlidersHorizontal class="h-5 w-5" /></span>
        <div class="flex flex-col">
          <span class="text-xs uppercase tracking-wider text-muted-foreground">欺骗配置同步</span>
          <span class="flex items-center gap-2 text-base font-semibold">
            <Badge :tone="m.tone" dot>{{ m.label }}</Badge>
            <span v-if="!card.initialized" class="text-xs font-normal text-warn">数据集未初始化</span>
          </span>
        </div>
      </div>
      <div class="flex items-center gap-2 font-mono text-sm tabular-nums">
        <span class="rounded-lg bg-muted/40 px-2.5 py-1">数据集 v{{ card.dataset_version }} · r{{ card.dataset_rev }}</span>
        <span class="text-muted-foreground">→</span>
        <span class="rounded-lg px-2.5 py-1" :class="card.core_rev >= card.dataset_rev && card.core_rev ? 'bg-origin/10 text-origin' : 'bg-warn/10 text-warn'">核心 r{{ card.core_rev || '—' }}</span>
        <span class="text-muted-foreground">→</span>
        <span class="rounded-lg px-2.5 py-1" :class="card.edge_total && card.edge_in_sync === card.edge_total ? 'bg-origin/10 text-origin' : 'bg-muted/40'">
          边缘 {{ card.edge_total ? `${card.edge_in_sync}/${card.edge_total}` : '—' }}
        </span>
      </div>
      <div class="flex items-center gap-3">
        <svg viewBox="0 0 64 64" class="h-14 w-14 -rotate-90">
          <circle cx="32" cy="32" :r="R" fill="none" :stroke="card.honeypots_total && !ratio ? 'hsl(var(--danger) / 0.35)' : 'hsl(var(--muted))'" stroke-width="7" />
          <circle
            v-if="ratio > 0"
            cx="32" cy="32" :r="R" fill="none" stroke-linecap="round" stroke-width="7" class="transition-all duration-700"
            :stroke="ratio === 1 ? 'hsl(var(--origin))' : ratio > 0 ? 'hsl(var(--warn))' : 'hsl(var(--danger))'"
            :stroke-dasharray="`${C * ratio} ${C}`"
          />
        </svg>
        <div class="flex flex-col text-sm">
          <span class="font-semibold tabular-nums">{{ card.honeypots_healthy }}/{{ card.honeypots_total }}</span>
          <span class="text-xs text-muted-foreground">启用蜜罐健康</span>
        </div>
      </div>
      <div v-if="systemAlerts" class="flex items-center gap-1.5 rounded-lg border border-danger/30 bg-danger/10 px-3 py-1.5 text-xs text-danger">
        <ShieldAlert class="h-3.5 w-3.5" />{{ systemAlerts }} 条系统告警
      </div>
      <ArrowUpRight class="ml-auto h-4 w-4 text-muted-foreground transition group-hover:-translate-y-0.5 group-hover:translate-x-0.5 group-hover:text-primary" />
    </SpotlightCard>
  </RouterLink>
</template>
