<script setup lang="ts">
import { ArrowLeft, Pencil, Trash2 } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import BarList from '@/components/charts/BarList.vue'
import TrendChart from '@/components/charts/TrendChart.vue'
import GeoTag from '@/components/GeoTag.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowDrawer from '@/components/RowDrawer.vue'
import ServiceForm from '@/components/ServiceForm.vue'
import StatePanel from '@/components/StatePanel.vue'
import TrafficTable from '@/components/TrafficTable.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import NumberTicker from '@/components/fx/NumberTicker.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { fmtDateTime, fmtPct } from '@/lib/format'
import type { ServiceTraffic, TrafficRow } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const live = useLiveStore()
const id = computed(() => String(route.params.id))
const editing = ref(false)
const confirmDelete = ref(false)
const deleting = ref(false)
const deleteError = ref('')
const selected = ref<TrafficRow | null>(null)

const { data, error, loading, refresh } = useLiveResource(
  (signal) => api.get<ServiceTraffic>(`/api/v1/services/${id.value}/traffic`, { window: live.span }, signal),
  { deps: [id] },
)
const svc = computed(() => data.value?.service)
const funnelMax = computed(() => Math.max(1, ...(data.value?.funnel ?? []).map((f) => f.count)))
const sources = computed(() =>
  (data.value?.top_sources ?? []).map((s) => ({ label: `${s.ip}  ${s.geo.label}`, value: s.count, tone: 'accent' as const })),
)

async function remove() {
  if (!svc.value) return
  deleting.value = true
  deleteError.value = ''
  await api
    .del(`/api/v1/services/${svc.value.id}`, { version: svc.value.version })
    .then(() => router.replace({ name: 'services' }))
    .catch((err: unknown) => {
      console.error('删除登记失败', err)
      deleteError.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (deleting.value = false))
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <RouterLink :to="{ name: 'services' }" class="flex w-fit items-center gap-1.5 text-sm text-muted-foreground transition hover:text-primary">
      <ArrowLeft class="h-4 w-4" /> 返回反向链接器
    </RouterLink>
    <PageHeader :title="svc?.name ?? '服务详情'" :subtitle="svc?.description || '该服务的流量是否流入蜃楼（幻境 / 诱饵），以及来源与归属地。'">
      <template v-if="auth.can('registry:write') && svc" #actions>
        <Button variant="outline" @click="editing = true"><Pencil /> 编辑</Button>
        <Button variant="ghost" class="text-danger" @click="confirmDelete = true"><Trash2 /> 删除登记</Button>
      </template>
    </PageHeader>
    <StatePanel :error="error" :window="data?.window" :loading="loading" @retry="refresh" />

    <SpotlightCard v-if="svc" :interactive="false" class="flex flex-wrap items-center gap-x-8 gap-y-3 p-5 text-sm">
      <Badge :tone="svc.enabled ? 'mirage' : 'muted'" dot>{{ svc.enabled ? '观测中' : '已停用' }}</Badge>
      <div><span class="text-muted-foreground">上游 </span><span class="font-mono">{{ svc.upstream }}</span></div>
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-muted-foreground">域名</span>
        <span v-for="h in svc.hosts" :key="h" class="rounded-md bg-muted px-2 py-0.5 font-mono text-xs">{{ h }}</span>
      </div>
      <div><span class="text-muted-foreground">负责人 </span>{{ svc.owner || '—' }}</div>
      <div class="text-xs text-muted-foreground">v{{ svc.version }} · {{ svc.updated_by }} 更新于 {{ fmtDateTime(svc.updated_at) }}</div>
    </SpotlightCard>

    <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" beam class="p-5">
        <h2 class="mb-4 text-base font-medium">流入蜃楼漏斗</h2>
        <ol class="flex flex-col gap-3">
          <li v-for="(f, i) in data?.funnel ?? []" :key="f.key" class="flex flex-col gap-1">
            <div class="flex items-center justify-between text-xs">
              <span class="text-muted-foreground">{{ i + 1 }}. {{ f.label }}</span>
              <span class="text-base font-semibold tabular-nums"><NumberTicker :value="f.count" /></span>
            </div>
            <div class="h-2 overflow-hidden rounded-full bg-muted">
              <div
                class="h-full rounded-full bg-gradient-to-r from-primary to-accent transition-[width] duration-700"
                :style="{ width: `${(f.count / funnelMax) * 100}%`, opacity: 1 - i * 0.15 }"
              />
            </div>
          </li>
        </ol>
        <p class="mt-4 text-xs text-muted-foreground">
          流入占比（含影子）<span class="ml-1 text-sm font-semibold text-mirage">{{ fmtPct(data?.stats.mirage_ratio) }}</span>
          · 诱饵投递失败 {{ data?.stats.decoy_failed ?? 0 }}
        </p>
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="p-5 xl:col-span-2">
        <h2 class="mb-2 text-base font-medium">流量趋势</h2>
        <TrendChart :buckets="data?.trend ?? []" height="240px" />
      </SpotlightCard>
    </section>

    <section class="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <SpotlightCard :interactive="false" class="p-5 xl:col-span-2">
        <h2 class="mb-3 text-base font-medium">流量明细</h2>
        <TrafficTable :rows="data?.rows ?? []" :show-service="false" empty="该服务在时间窗内没有流量" @select="selected = $event" />
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="flex flex-col gap-4 p-5">
        <h2 class="text-base font-medium">来源 IP 与归属地</h2>
        <BarList :items="sources" />
        <div class="flex flex-wrap gap-2">
          <GeoTag v-for="g in data?.geo ?? []" :key="g.label" :geo="{ label: `${g.label} ×${g.count}`, scope: g.scope, country: '', province: '', city: '', isp: '', iso: '' }" compact />
        </div>
      </SpotlightCard>
    </section>

    <ServiceForm v-model:open="editing" :service="svc" @saved="refresh" />
    <Sheet v-model:open="confirmDelete" side="center" title="删除这条登记？" description="只删除管控台登记，不影响引擎的路由与策略；该服务的流量之后显示为「未登记」。">
      <p v-if="deleteError" class="text-xs text-danger">{{ deleteError }}</p>
      <template #footer>
        <Button variant="ghost" @click="confirmDelete = false">取消</Button>
        <Button variant="destructive" :loading="deleting" @click="remove"><Trash2 /> 确认删除</Button>
      </template>
    </Sheet>
    <RowDrawer v-model:row="selected" />
  </div>
</template>
