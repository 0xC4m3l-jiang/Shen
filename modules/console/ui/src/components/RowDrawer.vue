<script setup lang="ts">
import { Sparkles } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import GeoTag from './GeoTag.vue'
import LayerBadge from './LayerBadge.vue'
import MirageState from './MirageState.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import { Button } from '@/components/ui/button'
import { api, ApiError } from '@/lib/api'
import { actionLabels, fmtDateTime } from '@/lib/format'
import type { TrafficRow } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'

// 单条请求详情：基本信息 + （有欺骗层权限时）核心判定与适配器执行的原始记录。
const row = defineModel<TrafficRow | null>('row', { default: null })
const auth = useAuthStore()
const router = useRouter()
function analyze(id: string) {
  row.value = null
  void router.push({ name: 'analysis', query: { tab: 'chat', decision: id } })
}
const trace = ref<{ judged?: unknown; decision?: unknown; notes: string[] } | null>(null)
const traceError = ref('')
const open = computed({
  get: () => row.value !== null,
  set: (v) => {
    if (!v) row.value = null
  },
})

watch(row, async (r) => {
  trace.value = null
  traceError.value = ''
  if (!r || !auth.can('deception:read')) return
  await api
    .get<{ judged?: unknown; decision?: unknown; notes: string[] }>('/api/v1/deception/trace', { decision_id: r.decision_id })
    .then((t) => (trace.value = t))
    .catch((err: unknown) => {
      console.error('读取链路失败', err)
      traceError.value = err instanceof ApiError ? err.message : '读取失败'
    })
})

const fields = computed(() => {
  const r = row.value
  if (!r) return []
  return [
    ['时间', fmtDateTime(r.at)],
    ['请求', `${r.method} ${r.path}`],
    ['域名', r.host || '（无）'],
    ['所属服务', r.service_name || '未登记'],
    ['来源 IP', r.source_ip || '（未知）'],
    ['User-Agent', r.user_agent || '—'],
    ['判定意图', actionLabels[r.action] ?? (r.action || '未经核心判定')],
    ['实际落点', r.executed === 'cache' ? `缓存命中 → ${r.dispatched || '未记录'}` : r.executed || '—'],
    ['后端', r.backend || '—'],
    ['投递结果', r.delivery_result || '—'],
    ['状态码 / 耗时', r.status ? `${r.status} · ${r.duration_ms?.toFixed(1)} ms` : '—'],
    ['风险分 / 信号', r.score !== undefined ? `${r.score.toFixed(2)} · ${(r.signals ?? []).join('、') || '无'}` : '—'],
  ]
})
</script>

<template>
  <Sheet v-model:open="open" title="请求详情" :description="row?.decision_id" width="max-w-xl">
    <div v-if="row" class="flex flex-col gap-5">
      <div class="flex flex-wrap items-center gap-2">
        <LayerBadge :layer="row.layer" />
        <MirageState :row="row" />
        <GeoTag :geo="row.geo" />
        <Button v-if="auth.can('llm:use')" size="sm" variant="outline" class="ml-auto" @click="analyze(row.decision_id)"><Sparkles class="h-3.5 w-3.5" />用大模型分析</Button>
      </div>
      <dl class="grid grid-cols-[110px_1fr] gap-x-4 gap-y-2.5 text-sm">
        <template v-for="[k, v] in fields" :key="k">
          <dt class="text-muted-foreground">{{ k }}</dt>
          <dd class="break-all font-mono text-xs leading-5">{{ v }}</dd>
        </template>
      </dl>
      <div v-if="auth.can('deception:read')" class="flex flex-col gap-2">
        <h3 class="text-sm font-medium">链路原始记录</h3>
        <p v-if="traceError" class="text-xs text-danger">{{ traceError }}</p>
        <ul v-if="trace?.notes?.length" class="list-disc pl-5 text-xs text-muted-foreground">
          <li v-for="n in trace.notes" :key="n">{{ n }}</li>
        </ul>
        <pre v-if="trace?.decision" class="max-h-56 overflow-auto rounded-lg bg-background/60 p-3 text-[11px] leading-5">{{ JSON.stringify(trace.decision, null, 2) }}</pre>
        <pre v-if="trace?.judged" class="max-h-56 overflow-auto rounded-lg bg-background/60 p-3 text-[11px] leading-5">{{ JSON.stringify(trace.judged, null, 2) }}</pre>
      </div>
    </div>
  </Sheet>
</template>
