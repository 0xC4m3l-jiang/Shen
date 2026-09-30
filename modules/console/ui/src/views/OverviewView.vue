<script setup lang="ts">
import { Activity, BellRing, Castle, Clock3, Globe2 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useNow } from '@vueuse/core'
import DonutChart from '@/components/charts/DonutChart.vue'
import BarList from '@/components/charts/BarList.vue'
import TrendChart from '@/components/charts/TrendChart.vue'
import GeoTag from '@/components/GeoTag.vue'
import ConfigSyncCard from '@/components/config/ConfigSyncCard.vue'
import KpiCard from '@/components/KpiCard.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowDrawer from '@/components/RowDrawer.vue'
import StatePanel from '@/components/StatePanel.vue'
import TrafficTable from '@/components/TrafficTable.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { layerColors } from '@/lib/charts'
import { fmtAgo, fmtPct, layerMeta } from '@/lib/format'
import type { Layer, Overview, TrafficRow } from '@/lib/types'
import { useLiveStore } from '@/stores/live'
import { usePrefsStore } from '@/stores/prefs'
import { useAuthStore } from '@/stores/auth'

const live = useLiveStore()
const prefs = usePrefsStore()
const auth = useAuthStore()
const now = useNow({ interval: 1000 })
const { data, error, loading, refresh } = useLiveResource((signal) =>
  api.get<Overview>('/api/v1/overview', { window: live.span }, signal),
)
const selected = ref<TrafficRow | null>(null)

const stats = computed(() => data.value?.stats)
const slices = computed(() => {
  void prefs.theme
  const colors = layerColors()
  return (Object.keys(layerMeta) as Layer[]).map((l) => ({
    name: layerMeta[l].label,
    value: stats.value?.by_layer[l] ?? 0,
    color: colors[l],
  }))
})
// 实时流里的新行优先（毫秒级），与接口返回的最近流量合并去重。
const recent = computed(() => {
  const seen = new Set<string>()
  const out: TrafficRow[] = []
  for (const r of [...live.rows, ...(data.value?.recent ?? [])]) {
    const key = r.decision_id + r.at
    if (seen.has(key)) continue
    seen.add(key)
    out.push(r)
    if (out.length >= 30) break
  }
  return out
})
const geoBars = computed(() =>
  (data.value?.geo ?? []).map((g) => ({
    label: g.label || '未知来源',
    value: g.count,
    tone: (g.scope === 'public' ? 'primary' : 'muted') as 'primary' | 'muted',
  })),
)
const freshness = computed(() => fmtAgo(live.lastEventAt || data.value?.window.freshest, now.value.getTime()))
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="流量总览" subtitle="新流量、来源与归属地，以及流入蜃楼的比例（幻境 / 诱饵）。" />
    <StatePanel :error="error" :window="data?.window" :loading="loading" @retry="refresh" />

    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-5">
      <KpiCard label="新流量" :value="stats?.total ?? 0" :icon="Activity" :hint="`时间窗 ${live.span}`" />
      <KpiCard
        label="流入蜃楼"
        :value="(stats?.mirage_ratio ?? 0) * 100"
        :decimals="1"
        suffix="%"
        :icon="Castle"
        tone="mirage"
        :hint="`实际 ${stats?.in_mirage ?? 0} · 影子 ${stats?.shadow_mirage ?? 0}`"
      />
      <KpiCard label="告警" :value="stats?.alerts ?? 0" :icon="BellRing" tone="warn" :hint="`诱饵投递失败 ${stats?.decoy_failed ?? 0}`" />
      <KpiCard label="来源 IP" :value="stats?.unique_ips ?? 0" :icon="Globe2" tone="info" :hint="`已登记服务 ${data?.services_registered ?? 0} 个`" />
      <KpiCard label="数据新鲜度" :text="freshness" :icon="Clock3" tone="decoy" :hint="live.healthy ? '实时流已连接' : '实时流未连接：定时刷新中'" />
    </section>

    <ConfigSyncCard v-if="data?.config_sync && auth.can('config:read')" :card="data.config_sync" :system-alerts="data.system_alerts ?? 0" />

    <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" class="p-5 xl:col-span-2">
        <h2 class="mb-2 text-base font-medium">流量趋势 · 按落点</h2>
        <TrendChart :buckets="data?.trend ?? []" />
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="p-5">
        <h2 class="mb-2 text-base font-medium">落点构成</h2>
        <DonutChart :slices="slices" :center="fmtPct(stats?.mirage_ratio)" sub="流入蜃楼" />
        <ul class="mt-2 grid grid-cols-2 gap-2 text-xs">
          <li v-for="s in slices.filter((x) => x.value > 0)" :key="s.name" class="flex items-center gap-2 text-muted-foreground">
            <span class="h-2 w-2 rounded-full" :style="{ background: s.color }" />
            {{ s.name }}<span class="ml-auto tabular-nums text-foreground">{{ s.value }}</span>
          </li>
        </ul>
      </SpotlightCard>
    </section>

    <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" class="p-5 xl:col-span-2">
        <h2 class="mb-3 text-base font-medium">来源 IP 排行</h2>
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
              <th class="py-2 font-medium">来源</th>
              <th class="py-2 font-medium">归属地</th>
              <th class="py-2 text-right font-medium">请求</th>
              <th class="py-2 text-right font-medium">流入蜃楼</th>
              <th class="py-2 text-right font-medium">最近</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in data?.top_sources ?? []" :key="s.ip" class="border-t border-border/50 transition-colors hover:bg-primary/5">
              <td class="py-2.5 font-mono text-xs">{{ s.ip || '（未知）' }}</td>
              <td class="max-w-[260px] py-2.5"><GeoTag :geo="s.geo" /></td>
              <td class="py-2.5 text-right tabular-nums">{{ s.count }}</td>
              <td class="py-2.5 text-right tabular-nums" :class="s.in_mirage ? 'text-mirage' : 'text-muted-foreground'">{{ s.in_mirage }}</td>
              <td class="py-2.5 text-right text-xs text-muted-foreground">{{ fmtAgo(s.last_seen, now.getTime()) }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="!data?.top_sources?.length" class="py-6 text-center text-sm text-muted-foreground">时间窗内暂无来源</p>
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="p-5">
        <h2 class="mb-3 text-base font-medium">归属地分布</h2>
        <BarList :items="geoBars" />
      </SpotlightCard>
    </section>

    <SpotlightCard :interactive="false" class="p-5">
      <div class="mb-3 flex items-center justify-between">
        <h2 class="text-base font-medium">实时流量</h2>
        <span class="text-xs text-muted-foreground">点击一行查看完整链路</span>
      </div>
      <TrafficTable :rows="recent" @select="selected = $event" />
    </SpotlightCard>
    <RowDrawer v-model:row="selected" />
  </div>
</template>
