<script setup lang="ts">
import { TabsContent } from 'reka-ui'
import { computed, ref } from 'vue'
import ChainView from '@/components/ChainView.vue'
import GeoTag from '@/components/GeoTag.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Tabs from '@/components/ui/tabs/Tabs.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { actionLabels, fmtTime } from '@/lib/format'
import type { CoreConfig, FlowRecord, RequestGraph } from '@/lib/types'

const tab = ref('flow')
const flow = useLiveResource((signal) => api.get<FlowRecord[]>('/api/v1/deception/flow', { limit: 300 }, signal))
const graphs = useLiveResource((signal) => api.get<RequestGraph[]>('/api/v1/deception/graphs', { limit: 60 }, signal))
const config = useLiveResource((signal) => api.get<CoreConfig>('/api/v1/deception/config', undefined, signal), { live: false })

// 点击判定流的一行，抽屉里回放这条请求的完整链路（客户端 → 适配器 → 判定 → 落点）。
const chainRow = ref<FlowRecord | null>(null)
const chainGraph = ref<RequestGraph | null>(null)
const chainError = ref('')
const chainLoading = ref(false)
const chainOpen = computed({
  get: () => chainRow.value !== null,
  set: (v) => {
    if (!v) chainRow.value = null
  },
})
async function openChain(f: FlowRecord) {
  chainRow.value = f
  chainGraph.value = null
  chainError.value = ''
  chainLoading.value = true
  await api
    .get<RequestGraph[]>('/api/v1/deception/graphs', { decision_id: f.decision_id })
    .then((gs) => {
      chainGraph.value = gs[0] ?? null
      if (!chainGraph.value) chainError.value = '没有可回放的链路：这条请求的执行记录可能已被核心内存缓冲挤出'
    })
    .catch((err: unknown) => {
      chainError.value = err instanceof ApiError ? err.message : '读取链路失败'
    })
    .finally(() => (chainLoading.value = false))
}

const tabs = computed(() => [
  { value: 'flow', label: '判定流', count: flow.data.value?.length },
  { value: 'graphs', label: '请求链路', count: graphs.data.value?.length },
  { value: 'config', label: '策略快照' },
])
const actionTone = (a: string) => (a === 'route_mirage' ? 'mirage' : a === 'block' ? 'danger' : 'origin')
const labels: Record<string, string> = {
  policy_id: '策略 ID', version: '策略版本', checksum: '校验和', rule_count: '规则数', whitelist_count: '白名单条数',
  enabled: 'AI 内容层', kinds: '内容类型', model: '生成模型', manifest_path: '内容清单', variants: '会话变体数',
  rotate_cooldown: '轮换冷却', manifest_loaded: '清单已装载', manifest_version: '清单版本',
  manifest_resources: '资源数', manifest_contents: '内容条数',
}
const show = (v: unknown) => (typeof v === 'boolean' ? (v ? '是' : '否') : Array.isArray(v) ? v.join('、') || '—' : String(v ?? '—') || '—')
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="欺骗层" subtitle="核心判定、逐请求链路与生效中的策略快照（只读）。" />
    <Tabs v-model="tab" :items="tabs">
      <TabsContent value="flow" class="flex flex-col gap-4">
        <StatePanel :error="flow.error.value" :loading="flow.loading.value" @retry="flow.refresh" />
        <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
          <table class="w-full min-w-[900px] text-sm">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
                <th class="py-2 font-medium">时间</th><th class="py-2 font-medium">来源</th><th class="py-2 font-medium">归属地</th>
                <th class="py-2 font-medium">请求</th><th class="py-2 font-medium">决策</th><th class="py-2 font-medium">信号</th>
                <th class="py-2 text-right font-medium">风险分</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="f in flow.data.value ?? []"
                :key="f.decision_id"
                class="group cursor-pointer border-t border-border/50 transition-colors hover:bg-primary/5"
                :title="`${f.decision_id}：点击查看这条请求的链路`"
                @click="openChain(f)"
              >
                <td class="py-2.5 font-mono text-xs text-muted-foreground">{{ fmtTime(f.at) }}</td>
                <td class="py-2.5 font-mono text-xs">{{ f.source_ip }}</td>
                <td class="max-w-[180px] py-2.5"><GeoTag :geo="f.geo" compact /></td>
                <td class="max-w-[280px] truncate py-2.5 font-mono text-xs" :title="f.path">{{ f.method }} {{ f.path }}</td>
                <td class="py-2.5"><Badge :tone="actionTone(f.action)">{{ actionLabels[f.action] ?? f.action }}</Badge></td>
                <td class="py-2.5">
                  <div class="flex flex-wrap gap-1">
                    <span v-for="s in f.signals ?? []" :key="s" class="rounded bg-secondary/10 px-1.5 py-0.5 font-mono text-[11px] text-secondary">{{ s }}</span>
                  </div>
                </td>
                <td class="py-2.5 text-right font-mono text-xs">
                  <div class="ml-auto flex w-24 items-center gap-2">
                    <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                      <div class="h-full rounded-full bg-gradient-to-r from-primary to-warn" :style="{ width: `${Math.round(f.score * 100)}%` }" />
                    </div>
                    {{ f.score.toFixed(2) }}
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="!flow.data.value?.length" class="py-10 text-center text-sm text-muted-foreground">核心尚无判定记录（白名单 / 缓存命中与专属诱饵路由不调用核心）</p>
          <p v-else class="text-xs text-muted-foreground">点击任意一行，回放这条请求的完整链路。</p>
        </SpotlightCard>
      </TabsContent>

      <TabsContent value="graphs" class="flex flex-col gap-3">
        <StatePanel :error="graphs.error.value" :loading="graphs.loading.value" @retry="graphs.refresh" />
        <SpotlightCard v-for="g in graphs.data.value ?? []" :key="g.decision_id + g.at" :interactive="false" class="p-4">
          <ChainView :graph="g" />
        </SpotlightCard>
        <p v-if="!graphs.data.value?.length" class="py-10 text-center text-sm text-muted-foreground">暂无请求链路</p>
      </TabsContent>

      <TabsContent value="config" class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <StatePanel class="lg:col-span-2" :error="config.error.value" :loading="config.loading.value" @retry="config.refresh" />
        <SpotlightCard v-for="(block, name) in config.data.value ?? {}" :key="name" :interactive="false" class="p-5">
          <h2 class="mb-3 text-base font-medium">{{ name === 'policy' ? '策略' : 'AI 欺骗内容' }}</h2>
          <dl class="grid grid-cols-[120px_1fr] gap-x-4 gap-y-2 text-sm">
            <template v-for="(v, k) in block" :key="k">
              <dt class="text-muted-foreground">{{ labels[k] ?? k }}</dt>
              <dd class="break-all font-mono text-xs leading-5">{{ show(v) }}</dd>
            </template>
          </dl>
        </SpotlightCard>
        <p class="text-xs text-muted-foreground lg:col-span-2">快照只读：阈值、灰度、影子模式等核心运行参数刻意不出观测面；修改策略请走核心配置与策略面。</p>
      </TabsContent>
    </Tabs>
    <Sheet v-model:open="chainOpen" title="请求链路" :description="chainRow ? `${chainRow.method} ${chainRow.path} · ${chainRow.decision_id}` : ''" width="max-w-2xl">
      <p v-if="chainLoading" class="py-10 text-center text-sm text-muted-foreground">正在读取链路……</p>
      <p v-else-if="chainError" class="py-10 text-center text-sm text-danger">{{ chainError }}</p>
      <ChainView v-else-if="chainGraph" :graph="chainGraph" />
    </Sheet>
  </div>
</template>
