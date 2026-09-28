<script setup lang="ts">
import { Coins, Gauge, MessageSquareText, TriangleAlert } from 'lucide-vue-next'
import { computed } from 'vue'
import VChart from 'vue-echarts'
import KpiCard from '@/components/KpiCard.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { baseOption, token } from '@/lib/charts'
import { fmtDateTime, fmtNum } from '@/lib/format'
import type { UsageResponse } from '@/lib/types'
import { usePrefsStore } from '@/stores/prefs'

// token 用量：以服务商返回的 usage 为准；服务商不返回时本地估算并标注。
const prefs = usePrefsStore()
const { data, error, loading, refresh } = useLiveResource(
  (signal) => api.get<UsageResponse>('/api/v1/llm/usage', { days: 14 }, signal),
  { live: false },
)
const sum = computed(() => data.value?.summary)
const t = computed(() => sum.value?.totals)

const option = computed(() => {
  void prefs.theme
  const days = sum.value?.by_day ?? []
  const bar = (name: string, key: 'prompt_tokens' | 'completion_tokens', color: string) => ({
    name, type: 'bar', stack: 'tokens', barMaxWidth: 26, data: days.map((d) => d[key]),
    itemStyle: { color, borderRadius: key === 'completion_tokens' ? [4, 4, 0, 0] : 0 },
  })
  return {
    ...baseOption(),
    tooltip: { ...baseOption().tooltip, trigger: 'axis' },
    legend: { top: 0, right: 0, icon: 'roundRect', itemWidth: 10, itemHeight: 6, textStyle: { color: token('muted-foreground') } },
    grid: { left: 8, right: 8, top: 32, bottom: 4, containLabel: true },
    xAxis: { type: 'category', data: days.map((d) => d.date.slice(5)), axisLine: { lineStyle: { color: token('border') } }, axisLabel: { color: token('muted-foreground'), fontSize: 11 } },
    yAxis: { type: 'value', splitLine: { lineStyle: { color: token('border', 0.5), type: 'dashed' } }, axisLabel: { color: token('muted-foreground'), fontSize: 11 } },
    series: [bar('输入 token', 'prompt_tokens', token('primary', 0.75)), bar('输出 token', 'completion_tokens', token('mirage'))],
  }
})
const failRate = computed(() => (t.value?.requests ? `${((t.value.failed / t.value.requests) * 100).toFixed(1)}%` : '0%'))
</script>

<template>
  <div class="flex flex-col gap-5">
    <div class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
      <span>{{ data?.scope === 'all' ? '全部账号的用量' : '我的用量' }}</span>
      <span v-if="sum?.window_start">· 统计自 {{ fmtDateTime(sum.window_start) }}（最近 {{ fmtNum(sum.records) }} 次调用）</span>
      <span v-if="data?.in_flight">· 当前 {{ data.in_flight }} 个调用进行中</span>
    </div>
    <StatePanel :error="error" :loading="loading" @retry="refresh" />

    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <KpiCard label="累计 token" :value="t?.total_tokens ?? 0" :icon="Coins" :hint="`输入 ${fmtNum(t?.prompt_tokens)} · 输出 ${fmtNum(t?.completion_tokens)}`" />
      <KpiCard label="调用次数" :value="t?.requests ?? 0" :icon="MessageSquareText" tone="info" :hint="t?.estimated_requests ? `其中 ${t.estimated_requests} 次为本地估算` : '以服务商返回为准'" />
      <KpiCard label="失败" :value="t?.failed ?? 0" :icon="TriangleAlert" tone="warn" :hint="`失败率 ${failRate}`" />
      <KpiCard label="模型数" :value="sum?.by_model?.length ?? 0" :icon="Gauge" tone="mirage" hint="有调用记录的 提供方 × 模型" />
    </section>

    <SpotlightCard :interactive="false" class="p-5">
      <h2 class="mb-2 text-base font-medium">近 14 天 token 用量</h2>
      <VChart :option="option" autoresize style="height: 240px" />
    </SpotlightCard>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" class="overflow-x-auto p-5 xl:col-span-2">
        <h2 class="mb-3 text-base font-medium">按模型</h2>
        <table class="w-full min-w-[560px] text-sm">
          <thead>
            <tr class="text-left text-xs text-muted-foreground">
              <th class="py-2 font-medium">提供方 / 模型</th><th class="py-2 text-right font-medium">调用</th>
              <th class="py-2 text-right font-medium">输入</th><th class="py-2 text-right font-medium">输出</th>
              <th class="py-2 text-right font-medium">合计</th><th class="py-2 text-right font-medium">最近</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in sum?.by_model ?? []" :key="m.provider_id + m.model" class="border-t border-border/50">
              <td class="py-2.5"><span class="text-foreground">{{ m.provider_name }}</span> <span class="font-mono text-xs text-muted-foreground">{{ m.model }}</span></td>
              <td class="py-2.5 text-right tabular-nums">{{ fmtNum(m.requests) }}<span v-if="m.failed" class="ml-1 text-xs text-warn">/{{ m.failed }} 失败</span></td>
              <td class="py-2.5 text-right tabular-nums">{{ fmtNum(m.prompt_tokens) }}</td>
              <td class="py-2.5 text-right tabular-nums">{{ fmtNum(m.completion_tokens) }}</td>
              <td class="py-2.5 text-right font-medium tabular-nums">{{ fmtNum(m.total_tokens) }}</td>
              <td class="py-2.5 text-right text-xs text-muted-foreground">{{ fmtDateTime(m.last_at) }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="!sum?.by_model?.length" class="py-6 text-center text-sm text-muted-foreground">还没有模型调用记录</p>
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="p-5">
        <h2 class="mb-3 text-base font-medium">{{ sum?.by_user ? '按账号' : '最近调用' }}</h2>
        <ul v-if="sum?.by_user" class="flex flex-col gap-2 text-sm">
          <li v-for="u in sum.by_user" :key="u.user" class="flex items-center justify-between">
            <span>{{ u.user }}</span><span class="tabular-nums text-muted-foreground">{{ fmtNum(u.total_tokens) }} token · {{ u.requests }} 次</span>
          </li>
        </ul>
        <ul v-else class="flex flex-col gap-2 text-xs">
          <li v-for="r in sum?.recent?.slice(0, 10) ?? []" :key="r.at + r.model" class="flex items-center gap-2">
            <Badge :tone="r.ok ? 'mirage' : 'danger'" dot>{{ r.kind === 'test' ? '测试' : '对话' }}</Badge>
            <span class="font-mono">{{ r.model }}</span>
            <span class="ml-auto tabular-nums text-muted-foreground">{{ fmtNum(r.total_tokens) }}</span>
          </li>
        </ul>
      </SpotlightCard>
    </div>

    <SpotlightCard v-if="sum?.by_user" :interactive="false" class="overflow-x-auto p-5">
      <h2 class="mb-3 text-base font-medium">最近调用</h2>
      <table class="w-full min-w-[720px] text-sm">
        <thead>
          <tr class="text-left text-xs text-muted-foreground">
            <th class="py-2 font-medium">时间</th><th class="py-2 font-medium">账号</th><th class="py-2 font-medium">类型</th>
            <th class="py-2 font-medium">模型</th><th class="py-2 text-right font-medium">token</th><th class="py-2 text-right font-medium">耗时</th><th class="py-2 font-medium">结果</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in sum.recent" :key="r.at + r.user + r.model" class="border-t border-border/50">
            <td class="py-2 font-mono text-xs text-muted-foreground">{{ fmtDateTime(r.at) }}</td>
            <td class="py-2">{{ r.user }}</td>
            <td class="py-2">{{ r.kind === 'test' ? '连通测试' : '对话分析' }}</td>
            <td class="py-2 font-mono text-xs">{{ r.provider_name }} / {{ r.model }}</td>
            <td class="py-2 text-right tabular-nums">{{ fmtNum(r.total_tokens) }}<span v-if="r.estimated" class="text-muted-foreground" title="服务商未返回用量，本地估算">*</span></td>
            <td class="py-2 text-right tabular-nums text-muted-foreground">{{ r.latency_ms }} ms</td>
            <td class="max-w-[260px] truncate py-2 text-xs" :class="r.ok ? 'text-mirage' : 'text-danger'" :title="r.error">{{ r.ok ? '成功' : r.error }}</td>
          </tr>
        </tbody>
      </table>
    </SpotlightCard>
  </div>
</template>
