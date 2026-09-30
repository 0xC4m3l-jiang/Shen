<script setup lang="ts">
import { computed } from 'vue'
import { ArrowRight, Database, Radio, Server, TriangleAlert } from 'lucide-vue-next'
import type { SeedOutcome, SyncStatus } from '@/lib/config'
import { fmtAgo } from '@/lib/format'

const props = defineProps<{ sync: SyncStatus | null; datasetVersion: number; initialized: boolean; seed: SeedOutcome | null }>()

type Tone = 'ok' | 'pending' | 'error' | 'idle'
const toneClass: Record<Tone, string> = {
  ok: 'border-origin/40 bg-origin/10 text-origin',
  pending: 'border-warn/40 bg-warn/10 text-warn',
  error: 'border-danger/40 bg-danger/10 text-danger',
  idle: 'border-border/60 bg-muted/30 text-muted-foreground',
}

const stages = computed(() => {
  const s = props.sync
  const coreTone: Tone = !s || s.state === 'unconfigured' || s.state === 'offline' ? 'idle' : !s.applied ? 'error' : s.core_rev < s.dataset_rev ? 'pending' : 'ok'
  const edgeTone: Tone = !s || !s.edge_total ? 'idle' : s.edge_in_sync < s.edge_total ? (s.edge_acks.some((a) => !a.applied) ? 'error' : 'pending') : 'ok'
  return [
    { icon: Database, label: '数据集', value: `v${props.datasetVersion}`, sub: s ? `投影 r${s.dataset_rev}` : '', tone: (props.initialized ? 'ok' : 'idle') as Tone },
    { icon: Server, label: '核心已应用', value: s && s.core_rev ? `r${s.core_rev}` : '—', sub: s?.policy_version ? `策略 v${s.policy_version}` : '', tone: coreTone },
    { icon: Radio, label: '边缘已确认', value: s && s.edge_total ? `${s.edge_in_sync}/${s.edge_total}` : '—', sub: s?.edge_min_version ? `最低 v${s.edge_min_version}` : '暂无适配器回执', tone: edgeTone },
  ]
})

const banner = computed(() => {
  const s = props.sync
  if (!props.initialized) return { tone: 'warn', text: '数据集尚未初始化：未找到部署配置可供导入。核心继续按部署配置运行；在任一 Tab 保存即视为由管控台接管。' }
  if (props.seed?.drifted) return { tone: 'warn', text: '部署配置中的欺骗域在接管后被修改过 —— 这些修改不会生效，请在此处修改。' }
  if (!s || s.state === 'unconfigured') return { tone: 'warn', text: '核心同步未启用（未设置 SHEN_CORE_SYNC_TOKEN）：此处的修改会保存，但不会下发到核心。' }
  if (s.state === 'offline') return { tone: 'danger', text: '核心未上报同步状态：核心继续按最后一份正确配置运行，新修改暂不生效。' }
  if (!s.applied) return { tone: 'danger', text: `核心拒绝了最新配置并保留上一份：${s.reason ?? ''}` }
  return null
})

const stateText: Record<string, string> = { synced: '已同步', pending: '生效中', error: '被拒绝', offline: '核心离线', unconfigured: '未启用同步' }
</script>

<template>
  <section class="glass flex flex-col gap-3 rounded-2xl p-4">
    <div class="flex flex-wrap items-center gap-3">
      <template v-for="(st, i) in stages" :key="st.label">
        <div
          class="flex min-w-[180px] flex-1 items-center gap-3 rounded-xl border px-4 py-2.5 transition-all duration-500"
          :class="toneClass[st.tone]"
        >
          <span class="relative flex h-8 w-8 items-center justify-center rounded-lg bg-background/60">
            <component :is="st.icon" class="h-4 w-4" />
            <span v-if="st.tone === 'pending'" class="absolute -right-0.5 -top-0.5 h-2 w-2 animate-ping rounded-full bg-warn" />
          </span>
          <div class="flex flex-col leading-tight">
            <span class="text-[11px] uppercase tracking-wider opacity-80">{{ st.label }}</span>
            <span class="text-base font-semibold tabular-nums text-foreground">{{ st.value }}</span>
            <span class="text-[11px] opacity-80">{{ st.sub }}</span>
          </div>
        </div>
        <ArrowRight v-if="i < stages.length - 1" class="hidden h-4 w-4 shrink-0 text-muted-foreground md:block" />
      </template>
      <div class="flex flex-col items-end gap-0.5 text-right text-xs text-muted-foreground">
        <span class="font-medium text-foreground">{{ stateText[sync?.state ?? 'unconfigured'] }}</span>
        <span v-if="sync?.last_report_at">核心上报 {{ fmtAgo(sync.last_report_at) }}</span>
        <span v-if="sync?.source">来源：{{ sync.source === 'console' ? '管控台' : sync.source === 'cache' ? '本地缓存' : '部署配置' }}</span>
      </div>
    </div>
    <p
      v-if="banner"
      class="flex items-start gap-2 rounded-lg border px-3 py-2 text-xs"
      :class="banner.tone === 'danger' ? 'border-danger/30 bg-danger/10 text-danger' : 'border-warn/30 bg-warn/10 text-warn'"
    >
      <TriangleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0" />{{ banner.text }}
    </p>
  </section>
</template>
