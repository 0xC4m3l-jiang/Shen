<script setup lang="ts">
import { AlertOctagon, AlertTriangle, Info } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import GeoTag from '@/components/GeoTag.vue'
import LayerBadge from '@/components/LayerBadge.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowDrawer from '@/components/RowDrawer.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import type { AlertItem, TrafficRow, Window } from '@/lib/types'
import { useLiveStore } from '@/stores/live'

type Level = AlertItem['level']
const live = useLiveStore()
const level = ref<Level | 'all'>('all')
const selected = ref<TrafficRow | null>(null)
const { data, error, loading, refresh } = useLiveResource((signal) =>
  api.get<{ alerts: AlertItem[]; counts: Partial<Record<Level, number>>; window: Window; alert_score: number }>(
    '/api/v1/alerts',
    { window: live.span },
    signal,
  ),
)
const items = computed(() => (data.value?.alerts ?? []).filter((a) => level.value === 'all' || a.level === level.value))
const meta: Record<Level, { label: string; icon: typeof Info; cls: string }> = {
  critical: { label: '真实告警', icon: AlertOctagon, cls: 'text-danger border-danger/40 bg-danger/10' },
  warning: { label: '诱饵异常', icon: AlertTriangle, cls: 'text-warn border-warn/40 bg-warn/10' },
  info: { label: '高风险分值', icon: Info, cls: 'text-info border-info/40 bg-info/10' },
}
const filters = computed(() => [
  { value: 'all' as const, label: '全部', count: data.value?.alerts.length ?? 0 },
  ...(Object.keys(meta) as Level[]).map((l) => ({ value: l, label: meta[l].label, count: data.value?.counts[l] ?? 0 })),
])
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="告警" :subtitle="`需要关注的请求：拦截或 severity≠none（真实告警）、诱饵投递失败、风险分 ≥ ${data?.alert_score ?? 0.9}（仅显示，不改变处置）。`" />
    <StatePanel :error="error" :window="data?.window" :loading="loading" @retry="refresh" />
    <div class="flex flex-wrap gap-2">
      <button
        v-for="f in filters"
        :key="f.value"
        type="button"
        class="glass flex cursor-pointer items-center gap-2 rounded-full px-4 py-1.5 text-sm transition hover:border-primary/40"
        :class="level === f.value && 'glow-ring text-foreground'"
        @click="level = f.value"
      >
        {{ f.label }}<span class="rounded-full bg-primary/15 px-1.5 text-xs text-primary">{{ f.count }}</span>
      </button>
    </div>
    <TransitionGroup tag="div" name="list" class="flex flex-col gap-3">
      <SpotlightCard v-for="a in items" :key="a.decision_id + a.at" class="cursor-pointer p-4" @click="selected = a">
        <div class="flex flex-wrap items-center gap-3">
          <span class="flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs" :class="meta[a.level].cls">
            <component :is="meta[a.level].icon" class="h-3.5 w-3.5" />{{ meta[a.level].label }}
          </span>
          <LayerBadge :layer="a.layer" />
          <span class="font-mono text-xs">{{ a.method }} {{ a.path }}</span>
          <span class="text-xs text-muted-foreground">{{ a.host }}</span>
          <span class="ml-auto text-xs text-muted-foreground">{{ fmtDateTime(a.at) }}</span>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          <span class="font-mono text-foreground/90">{{ a.source_ip }}</span>
          <GeoTag :geo="a.geo" />
          <span v-for="r in a.reasons" :key="r" class="rounded bg-muted px-2 py-0.5">{{ r }}</span>
        </div>
      </SpotlightCard>
    </TransitionGroup>
    <p v-if="!items.length && !loading" class="py-12 text-center text-sm text-muted-foreground">时间窗内没有需要关注的请求</p>
    <RowDrawer v-model:row="selected" />
  </div>
</template>

<style scoped>
.list-enter-active {
  transition: all 0.4s ease;
}
.list-enter-from {
  opacity: 0;
  transform: translateY(10px);
}
</style>
