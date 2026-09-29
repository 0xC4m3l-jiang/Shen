<script setup lang="ts">
import { ArrowRight, Castle, Network, Plus, Search, TriangleAlert, UserRound } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '@/components/PageHeader.vue'
import ServiceForm from '@/components/ServiceForm.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { fmtAgo, fmtNum, fmtPct } from '@/lib/format'
import type { ServiceSummary, Window } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'

const auth = useAuthStore()
const live = useLiveStore()
const router = useRouter()
const route = useRoute()
// 接入管理页「查看流量」跳转：用 focus 参数预填搜索（聚焦到该服务）。
const keyword = ref(typeof route.query.focus === 'string' ? route.query.focus : '')
const formOpen = ref(false)
const presetHost = ref('')

const { data, error, loading, refresh } = useLiveResource((signal) =>
  api.get<{ services: ServiceSummary[]; window: Window; traffic_error?: string }>('/api/v1/services', { window: live.span }, signal),
)
const unregistered = useLiveResource((signal) =>
  api.get<{ hosts: { host: string; count: number; last_seen: string }[] }>('/api/v1/services/unregistered', { window: live.span }, signal),
)

const list = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const all = data.value?.services ?? []
  if (!k) return all
  return all.filter((s) => [s.name, s.owner, s.upstream, ...s.hosts].some((f) => f.toLowerCase().includes(k)))
})

function spark(points: number[]): string {
  const max = Math.max(1, ...points)
  return points.map((p, i) => `${(i / Math.max(1, points.length - 1)) * 100},${28 - (p / max) * 26}`).join(' ')
}

function register(host = '') {
  presetHost.value = host
  formOpen.value = true
}
function onSaved() {
  void refresh()
  void unregistered.refresh()
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      title="反向链接器"
      subtitle="每个被保护的 Web 服务是一项独立登记。生效路由由核心策略面下发。"
    >
      <template #actions>
        <label class="glass flex h-9 items-center gap-2 rounded-md px-3">
          <Search class="h-4 w-4 text-muted-foreground" />
          <input v-model="keyword" class="w-56 border-0 bg-transparent text-sm outline-none placeholder:text-muted-foreground/70" placeholder="搜索名称 / 域名 / 负责人" />
        </label>
        <Button v-if="auth.can('registry:write')" @click="register()"><Plus /> 登记服务</Button>
      </template>
    </PageHeader>
    <StatePanel :error="error" :loading="loading" @retry="refresh" />
    <p v-if="data?.traffic_error" class="flex items-center gap-2 rounded-xl border border-warn/30 bg-warn/10 px-4 py-2 text-xs text-warn">
      <TriangleAlert class="h-4 w-4" /> 流量统计暂不可用（{{ data.traffic_error }}），登记信息仍可查看与编辑。
    </p>

    <section class="grid grid-cols-1 gap-4 md:grid-cols-2 2xl:grid-cols-3">
      <SpotlightCard
        v-for="(svc, i) in list"
        :key="svc.id"
        class="animate-rise cursor-pointer p-5"
        :style="{ animationDelay: `${i * 50}ms` }"
        :beam="svc.stats.in_mirage > 0"
        role="link"
        tabindex="0"
        @click="router.push({ name: 'service-detail', params: { id: svc.id } })"
        @keydown.enter="router.push({ name: 'service-detail', params: { id: svc.id } })"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="flex min-w-0 items-center gap-3">
            <div class="rounded-xl bg-gradient-to-br from-primary/25 to-accent/10 p-2.5 text-primary"><Network class="h-5 w-5" /></div>
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <h3 class="truncate text-base font-semibold">{{ svc.name }}</h3>
                <Badge v-if="svc.source === 'connector'" tone="primary">连接器</Badge>
              </div>
              <p class="truncate font-mono text-xs text-muted-foreground">{{ svc.upstream }}</p>
            </div>
          </div>
          <Badge :tone="svc.enabled ? 'mirage' : 'muted'" dot>{{ svc.enabled ? '观测中' : '已停用' }}</Badge>
        </div>
        <div class="mt-3 flex flex-wrap gap-1.5">
          <span v-for="h in svc.hosts.slice(0, 4)" :key="h" class="rounded-md bg-muted px-2 py-0.5 font-mono text-[11px] text-muted-foreground">{{ h }}</span>
          <span v-if="svc.hosts.length > 4" class="text-[11px] text-muted-foreground">+{{ svc.hosts.length - 4 }}</span>
        </div>
        <svg viewBox="0 0 100 30" preserveAspectRatio="none" class="mt-4 h-10 w-full" aria-hidden="true">
          <polyline :points="spark(svc.spark)" fill="none" stroke="hsl(var(--primary))" stroke-width="1.5" vector-effect="non-scaling-stroke" />
        </svg>
        <div class="mt-3 grid grid-cols-3 gap-2 text-center">
          <div><div class="text-lg font-semibold tabular-nums">{{ fmtNum(svc.stats.total) }}</div><div class="text-[11px] text-muted-foreground">请求</div></div>
          <div><div class="text-lg font-semibold tabular-nums text-mirage">{{ fmtNum(svc.stats.in_mirage) }}</div><div class="text-[11px] text-muted-foreground">流入蜃楼</div></div>
          <div><div class="text-lg font-semibold tabular-nums">{{ fmtPct(svc.stats.mirage_ratio) }}</div><div class="text-[11px] text-muted-foreground">占比（含影子）</div></div>
        </div>
        <div class="mt-4 flex items-center justify-between border-t border-border/50 pt-3 text-xs text-muted-foreground">
          <span class="flex items-center gap-1.5"><UserRound class="h-3.5 w-3.5" />{{ svc.owner || '未填负责人' }}</span>
          <span class="flex items-center gap-1 text-primary">最近流量 {{ fmtAgo(svc.last_seen) }} <ArrowRight class="h-3.5 w-3.5" /></span>
        </div>
      </SpotlightCard>

      <div v-if="!list.length && !loading" class="col-span-full flex flex-col items-center gap-3 rounded-2xl border border-dashed border-primary/30 p-10 text-center">
        <Castle class="h-10 w-10 text-primary/70" />
        <p class="text-sm text-muted-foreground">{{ keyword ? '没有匹配的服务' : '还没有登记任何反向链接服务。登记后即可按服务查看流量是否流入蜃楼。' }}</p>
        <Button v-if="auth.can('registry:write') && !keyword" @click="register()"><Plus /> 登记第一个服务</Button>
      </div>
    </section>

    <SpotlightCard v-if="unregistered.data.value?.hosts?.length" :interactive="false" class="p-5">
      <h2 class="mb-1 text-base font-medium">发现未登记的域名</h2>
      <p class="mb-3 text-xs text-muted-foreground">时间窗内有流量、但不属于任何已登记服务的域名。</p>
      <ul class="flex flex-col divide-y divide-border/50">
        <li v-for="h in unregistered.data.value.hosts" :key="h.host" class="flex items-center gap-3 py-2 text-sm">
          <span class="font-mono">{{ h.host }}</span>
          <span class="text-xs text-muted-foreground">{{ h.count }} 次 · {{ fmtAgo(h.last_seen) }}</span>
          <Button v-if="auth.can('registry:write')" size="sm" variant="outline" class="ml-auto" @click="register(h.host)"><Plus /> 登记</Button>
        </li>
      </ul>
    </SpotlightCard>

    <ServiceForm v-model:open="formOpen" :preset-host="presetHost" @saved="onSaved" />
  </div>
</template>
