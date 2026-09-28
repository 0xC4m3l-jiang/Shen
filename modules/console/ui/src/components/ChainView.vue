<script setup lang="ts">
import { ChevronRight } from 'lucide-vue-next'
import { ref } from 'vue'
import type { ChainNode, RequestGraph } from '@/lib/types'
import { fmtTime } from '@/lib/format'

// 单条请求链路：客户端 → 适配器 → 判定 → 决策 → 落点，每一跳可点开看输入 / 输出 / 依据。
defineProps<{ graph: RequestGraph }>()
const active = ref<ChainNode | null>(null)
const kindTone: Record<string, string> = {
  client: 'border-info/40 text-info',
  adapter: 'border-primary/40 text-primary',
  branch: 'border-border text-muted-foreground',
  judge: 'border-secondary/40 text-secondary',
  decision: 'border-secondary/40 text-secondary',
  origin: 'border-origin/40 text-origin',
  mirage: 'border-mirage/50 text-mirage',
  block: 'border-danger/40 text-danger',
  analysis: 'border-accent/40 text-accent',
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      <span class="font-mono">{{ fmtTime(graph.at) }}</span>
      <span class="rounded bg-muted px-1.5 py-0.5 font-mono">{{ graph.method }}</span>
      <span class="truncate font-mono text-foreground">{{ graph.path }}</span>
      <span>· {{ graph.source_ip }}</span>
      <span v-if="graph.real_alert" class="rounded-full bg-danger/15 px-2 text-danger">告警</span>
      <span v-else-if="graph.high_risk" class="rounded-full bg-warn/15 px-2 text-warn">高风险</span>
    </div>
    <div class="flex flex-wrap items-center gap-1.5">
      <template v-for="(node, i) in graph.chain" :key="node.id + i">
        <button
          type="button"
          class="group flex cursor-pointer flex-col rounded-lg border bg-background/40 px-3 py-1.5 text-left transition hover:-translate-y-0.5 hover:bg-primary/5"
          :class="[kindTone[node.kind] ?? 'border-border', node.alert && 'ring-1 ring-danger', node.warn && 'ring-1 ring-warn/60', active === node && 'glow-ring']"
          @click="active = active === node ? null : node"
        >
          <span class="text-[11px] opacity-80">{{ node.label }}</span>
          <span v-if="node.value" class="max-w-[180px] truncate font-mono text-xs text-foreground">{{ node.value }}</span>
        </button>
        <ChevronRight v-if="i < graph.chain.length - 1" class="h-4 w-4 text-muted-foreground/60" />
      </template>
    </div>
    <div v-if="active" class="grid gap-2 rounded-lg border border-primary/20 bg-background/50 p-3 text-xs animate-rise md:grid-cols-3">
      <div><div class="mb-1 text-muted-foreground">收到的输入</div><div class="whitespace-pre-wrap break-all font-mono">{{ active.request || '—' }}</div></div>
      <div><div class="mb-1 text-muted-foreground">给出的输出</div><div class="whitespace-pre-wrap break-all font-mono">{{ active.response || '—' }}</div></div>
      <div><div class="mb-1 text-muted-foreground">为什么走到这一步</div><div class="whitespace-pre-wrap">{{ active.why || '—' }}</div></div>
    </div>
  </div>
</template>
