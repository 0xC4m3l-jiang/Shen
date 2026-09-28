<script setup lang="ts">
import { BrainCircuit, CheckCircle2, XCircle } from 'lucide-vue-next'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import type { AnalysisItem } from '@/lib/types'

// L4 近线分析（analysis 服务）按会话自动产出的结论。只作数据，不改策略、不干预请求。
const { data, error, loading, refresh } = useLiveResource((signal) => api.get<AnalysisItem[]>('/api/v1/analysis', { limit: 100 }, signal))
const kinds: Record<string, string> = { intent: '意图识别', chain: '攻击链还原', strategy: '策略建议' }
</script>

<template>
  <div class="flex flex-col gap-4">
    <StatePanel :error="error" :loading="loading" @retry="refresh" />
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <SpotlightCard v-for="(a, i) in data ?? []" :key="a.event_id" class="animate-rise p-5" :style="{ animationDelay: `${i * 40}ms` }">
        <div class="flex items-center gap-3">
          <div class="rounded-lg bg-accent/10 p-2 text-accent"><BrainCircuit class="h-5 w-5" /></div>
          <div class="flex flex-col">
            <span class="font-medium">{{ kinds[a.kind] ?? a.kind }}</span>
            <span class="text-xs text-muted-foreground">{{ fmtDateTime(a.at) }} · 分析 {{ a.analyzed }} 条观测</span>
          </div>
          <Badge class="ml-auto" :tone="a.accepted ? 'mirage' : 'warn'">
            <CheckCircle2 v-if="a.accepted" class="h-3 w-3" /><XCircle v-else class="h-3 w-3" />
            {{ a.accepted ? '已采纳' : '已作废' }}
          </Badge>
        </div>
        <p v-if="a.rejected_reason" class="mt-3 text-xs text-warn">作废原因：{{ a.rejected_reason }}</p>
        <pre v-if="a.data" class="mt-3 max-h-48 overflow-auto rounded-lg bg-background/60 p-3 text-[11px] leading-5">{{ JSON.stringify(a.data, null, 2) }}</pre>
        <div v-if="a.evidence_ids?.length" class="mt-3 flex flex-wrap gap-1">
          <span class="text-xs text-muted-foreground">证据</span>
          <span v-for="e in a.evidence_ids.slice(0, 8)" :key="e" class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{{ e }}</span>
        </div>
      </SpotlightCard>
    </div>
    <p v-if="!data?.length && !loading" class="py-12 text-center text-sm text-muted-foreground">暂无近线分析结论（analysis 服务未启动或尚未产生结论）</p>
  </div>
</template>
