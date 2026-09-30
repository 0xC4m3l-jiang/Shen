<script setup lang="ts">
import { ArrowUpRight, Castle, PlugZap, Radar, Undo2 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import DonutChart from '@/components/charts/DonutChart.vue'
import KpiCard from '@/components/KpiCard.vue'
import LayerBadge from '@/components/LayerBadge.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowDrawer from '@/components/RowDrawer.vue'
import StatePanel from '@/components/StatePanel.vue'
import TrafficTable from '@/components/TrafficTable.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { token } from '@/lib/charts'
import { deliveryLabels, fmtAgo } from '@/lib/format'
import type { BackendView, Deliveries, TrafficRow, Window } from '@/lib/types'
import { honeypotTypeLabel, type RegisteredBackend } from '@/lib/config'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'
import { usePrefsStore } from '@/stores/prefs'

const live = useLiveStore()
const prefs = usePrefsStore()
const auth = useAuthStore()
const selected = ref<TrafficRow | null>(null)
const deliveries = useLiveResource((signal) => api.get<Deliveries>('/api/v1/honeypot/deliveries', { window: live.span }, signal))
const backends = useLiveResource((signal) =>
  api.get<{ backends: BackendView[]; registered?: RegisteredBackend[]; window: Window; note: string }>('/api/v1/honeypot/backends', { window: live.span }, signal),
)
const d = computed(() => deliveries.data.value)
const registered = computed(() => backends.data.value?.registered ?? [])
const healthBadge: Record<RegisteredBackend['health'], { tone: 'origin' | 'danger' | 'muted'; text: string }> = {
  healthy: { tone: 'origin', text: '可用' },
  unhealthy: { tone: 'danger', text: '不可用' },
  unknown: { tone: 'muted', text: '待探测' },
  disabled: { tone: 'muted', text: '未启用' },
}
const slices = computed(() => {
  void prefs.theme
  const tones: Record<string, string> = { delivered: 'mirage', backend_unavailable: 'warn', delivery_failed: 'danger', tombstoned: 'muted-foreground' }
  return Object.entries(d.value?.by_result ?? {}).map(([k, v]) => ({ name: deliveryLabels[k] ?? k, value: v, color: token(tones[k] ?? 'info') }))
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="蜜罐层" subtitle="投递结果、后端与失败原因；专属诱饵故障固定 502，绝不回源生产。" />
    <StatePanel :error="deliveries.error.value" :window="d?.window" :loading="deliveries.loading.value" @retry="deliveries.refresh" />

    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <KpiCard label="进入 / 尝试进入" :value="d?.total ?? 0" :icon="Castle" tone="mirage" beam />
      <KpiCard label="幻境（评分改道）" :value="d?.by_layer.mirage ?? 0" :icon="Castle" tone="primary" />
      <KpiCard label="专属诱饵" :value="d?.by_layer.decoy ?? 0" :icon="Radar" tone="decoy" :hint="`投递成功 ${d?.by_result.delivered ?? 0}`" />
      <KpiCard label="回落业务" :value="d?.by_layer.fallback ?? 0" :icon="Undo2" tone="warn" hint="幻境不可用时回落（评分改道的失败语义）" />
    </section>

    <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" class="p-5">
        <h2 class="mb-2 text-base font-medium">诱饵投递结果</h2>
        <DonutChart :slices="slices" :center="String(d?.by_result.delivered ?? 0)" sub="已投递" />
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="p-5 xl:col-span-2">
        <h2 class="mb-1 text-base font-medium">后端（按实际流量推导）</h2>
        <p class="mb-3 text-xs text-muted-foreground">{{ backends.data.value?.note }}</p>
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
              <th class="py-2 font-medium">后端</th><th class="py-2 font-medium">类型</th><th class="py-2 text-right font-medium">请求</th>
              <th class="py-2 text-right font-medium">成功</th><th class="py-2 text-right font-medium">失败</th><th class="py-2 text-right font-medium">最近</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in backends.data.value?.backends ?? []" :key="b.name + b.layer" class="border-t border-border/50 hover:bg-primary/5">
              <td class="py-2.5 font-mono text-xs">{{ b.name }}</td>
              <td class="py-2.5"><LayerBadge :layer="b.layer" /></td>
              <td class="py-2.5 text-right tabular-nums">{{ b.requests }}</td>
              <td class="py-2.5 text-right tabular-nums text-mirage">{{ b.ok }}</td>
              <td class="py-2.5 text-right tabular-nums" :class="b.failed ? 'text-warn' : 'text-muted-foreground'" :title="b.unconfirmed ? `另有 ${b.unconfirmed} 条落点未证实（旧版适配器缓存命中）` : undefined">
                {{ b.failed }}<span v-if="b.unconfirmed" class="ml-1 text-[11px] text-muted-foreground">+{{ b.unconfirmed }}?</span>
              </td>
              <td class="py-2.5 text-right text-xs text-muted-foreground">{{ fmtAgo(b.last_seen) }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="!backends.data.value?.backends?.length" class="py-6 text-center text-sm text-muted-foreground">时间窗内没有请求进入幻境或诱饵</p>
      </SpotlightCard>
    </section>

    <SpotlightCard v-if="registered.length" :interactive="false" class="overflow-x-auto p-5">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-base font-medium">蜜罐池 · 登记与健康</h2>
          <p class="text-xs text-muted-foreground">来自欺骗管控数据集；健康由核心 TCP 探测上报。被启用诱饵引用却不可用的蜜罐会让命中请求回落业务。</p>
        </div>
        <RouterLink v-if="auth.can('config:read')" :to="{ name: 'config', query: { tab: 'honeypots' } }" class="flex items-center gap-1 text-xs text-primary hover:underline">
          去配置<ArrowUpRight class="h-3.5 w-3.5" />
        </RouterLink>
      </div>
      <table class="w-full min-w-[720px] text-sm">
        <thead>
          <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th class="py-2 font-medium">蜜罐</th><th class="py-2 font-medium">类型</th><th class="py-2 font-medium">地址</th><th class="py-2 font-medium">健康</th>
            <th class="py-2 text-right font-medium">被引用</th><th class="py-2 text-right font-medium">时间窗请求</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in registered" :key="b.name" class="border-t border-border/50 hover:bg-primary/5" :class="b.health === 'unhealthy' && b.references ? 'bg-danger/5' : ''">
            <td class="py-2.5 font-mono text-xs">{{ b.name }}</td>
            <td class="py-2.5"><Badge tone="info">{{ honeypotTypeLabel[b.type] ?? b.type }}</Badge></td>
            <td class="py-2.5 font-mono text-xs text-muted-foreground">{{ b.addr }}</td>
            <td class="py-2.5"><Badge :tone="healthBadge[b.health].tone" dot :title="b.error">{{ b.health === 'healthy' ? `${b.latency_ms}ms` : healthBadge[b.health].text }}</Badge></td>
            <td class="py-2.5 text-right tabular-nums">{{ b.references }}</td>
            <td class="py-2.5 text-right tabular-nums">
              {{ b.requests }}<span v-if="b.idle && b.enabled" class="ml-1.5 rounded bg-muted/50 px-1.5 py-0.5 text-[10px] text-muted-foreground">空闲</span>
            </td>
          </tr>
        </tbody>
      </table>
    </SpotlightCard>

    <SpotlightCard :interactive="false" class="p-5">
      <h2 class="mb-3 text-base font-medium">失败与回落 <span class="text-xs text-muted-foreground">（最近 100 条）</span></h2>
      <TrafficTable :rows="d?.failures ?? []" empty="没有失败的投递 —— 诱饵后端工作正常" @select="selected = $event" />
    </SpotlightCard>

    <div class="flex items-start gap-3 rounded-2xl border border-dashed border-primary/30 p-5 text-sm">
      <PlugZap class="mt-0.5 h-5 w-5 shrink-0 text-primary" />
      <div>
        <div class="font-medium">合成交互事件（登录 / 浏览 / 受限写）· 尚未接入</div>
        <p class="mt-1 text-xs text-muted-foreground">{{ d?.interaction_events.note ?? '合成管理台的交互事件目前只进蜃楼后端本地日志，尚未回流核心。' }} 接入后这里将展示每个诱饵会话的操作时间线。</p>
      </div>
    </div>
    <RowDrawer v-model:row="selected" />
  </div>
</template>
