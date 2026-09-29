<script setup lang="ts">
import { Cable, Check, KeyRound, Plus, ServerCog, ShieldCheck, Unplug } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import AccessGuide from '@/components/connector/AccessGuide.vue'
import ConnectionCard from '@/components/connector/ConnectionCard.vue'
import IssueWizard from '@/components/connector/IssueWizard.vue'
import KpiCard from '@/components/KpiCard.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { fmtAgo, fmtNum } from '@/lib/format'
import type { ConnectorCredential, ConnectorOverview } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'

// 接入管理：反向隧道连接器的凭证签发与实时连接观测。
// 三形态：冷启动（教学）→ 等待接入（凭证+接入代码）→ 运行态（KPI+连接卡片）。
const auth = useAuthStore()
const canManage = auth.can('registry:write')

const { data, error, loading, refresh } = useLiveResource(
  (signal) => api.get<ConnectorOverview>('/api/v1/connectors', undefined, signal),
)

const wizardOpen = ref(false)
const actionError = ref('')
const resetting = ref('')

const overview = computed(() => data.value)
const coldStart = computed(() => !overview.value?.gateway.configured && !overview.value?.credentials.length)
const waiting = computed(() => !!overview.value?.credentials.length && !overview.value?.kpi.online_connections)

// 凭证 → 当前（或最近）会话：同凭证取最新心跳的会话。
const sessionByCred = computed(() => {
  const map = new Map<string, ConnectorOverview['sessions'][number]>()
  for (const s of overview.value?.sessions ?? []) {
    const prev = map.get(s.credential_id)
    if (!prev || s.last_heartbeat > prev.last_heartbeat) map.set(s.credential_id, s)
  }
  return map
})

function onCreated() {
  void refresh()
}

async function revoke(c: ConnectorCredential) {
  if (!window.confirm(`吊销「${c.name}」的接入凭证？已建立的连接将在 30 秒内被断开，且不可恢复。`)) return
  actionError.value = ''
  await api
    .post(`/api/v1/connectors/credentials/${c.id}/revoke`, {})
    .then(() => refresh())
    .catch((err: unknown) => {
      console.error('吊销失败', err)
      actionError.value = err instanceof ApiError ? err.message : '网络错误'
    })
}

async function reset(c: ConnectorCredential) {
  if (!window.confirm(`重置「${c.name}」的密钥？旧密钥立即作废，存量连接在心跳周期（约 15 秒）内被拒；新密钥只显示一次。`)) return
  actionError.value = ''
  resetting.value = c.id
  await api
    .post<{ key: string }>(`/api/v1/connectors/credentials/${c.id}/reset`, {})
    .then((r) => {
      void refresh()
      // 重置出的新密钥同样只显示一次：直接弹窗提示复制。
      window.alert(`「${c.name}」的新密钥（只显示这一次，请立即保存）：\n\n${r.key}`)
    })
    .catch((err: unknown) => {
      console.error('重置失败', err)
      actionError.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (resetting.value = ''))
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="接入管理" subtitle="业务经连接器建立反向隧道接入，零入站暴露；密钥只在签发时显示一次。">
      <template #actions>
        <Button v-if="canManage" @click="wizardOpen = true"><Plus class="h-4 w-4" />签发接入凭证</Button>
      </template>
    </PageHeader>

    <StatePanel :error="error" :loading="loading" @retry="refresh" />
    <p v-if="actionError" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ actionError }}</p>

    <!-- 冷启动态：三步入门（教学） -->
    <template v-if="coldStart">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <SpotlightCard :interactive="false" class="flex flex-col gap-3 p-5">
          <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary"><ServerCog class="h-5 w-5" /></span>
          <h3 class="text-base font-medium">① 部署网关</h3>
          <p class="text-sm leading-6 text-muted-foreground">在平台侧启动连接器网关（compose 的 <code class="font-mono text-xs">gateway</code> 服务），配置 TLS 证书与集成令牌。</p>
          <pre class="overflow-auto rounded-lg border border-border/60 bg-background/60 p-3 font-mono text-xs leading-5">docker compose --profile gateway up -d</pre>
          <span class="mt-auto text-xs text-muted-foreground">网关就绪后会自动联系本控制台，本页离开冷启动状态。</span>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="flex flex-col gap-3 p-5">
          <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary"><KeyRound class="h-5 w-5" /></span>
          <h3 class="text-base font-medium">② 签发凭证</h3>
          <p class="text-sm leading-6 text-muted-foreground">为每个业务签发独立凭证：绑定服务名与域名白名单，明文只显示一次。</p>
          <Button v-if="canManage" variant="outline" size="sm" class="mt-auto w-fit" @click="wizardOpen = true"><Plus class="h-3.5 w-3.5" />签发第一个凭证</Button>
          <span v-else class="mt-auto text-xs text-muted-foreground">需要「登记写入」权限（管理员 / 欺骗运维）。</span>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="flex flex-col gap-3 p-5">
          <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary"><Cable class="h-5 w-5" /></span>
          <h3 class="text-base font-medium">③ 交给开发者</h3>
          <p class="text-sm leading-6 text-muted-foreground">签发向导最后一步生成带密钥的接入代码（Go SDK / 二进制 / Docker），整段转发即可，开发者零文档上手。</p>
          <span class="mt-auto flex items-center gap-1.5 text-xs text-muted-foreground"><ShieldCheck class="h-3.5 w-3.5" />业务全程零入站暴露</span>
        </SpotlightCard>
      </div>
      <AccessGuide />
    </template>

    <!-- 等待接入 / 运行态：KPI + 连接卡片 + 指南 -->
    <template v-else-if="overview">
      <!-- 网关未部署但已有凭证：温和提示（优雅降级） -->
      <div v-if="!overview.gateway.configured" class="flex items-center gap-2 rounded-lg border border-warn/40 bg-warn/10 px-3 py-2.5 text-xs text-warn">
        <Unplug class="h-4 w-4 shrink-0" />
        网关尚未联系控制台（未部署或集成令牌不对）；凭证可正常签发，连接器拨入前网关需先就绪。
      </div>
      <!-- 等待接入：实时脉冲指示 -->
      <div v-else-if="waiting" class="glass flex items-center gap-2.5 rounded-xl px-4 py-3 text-sm">
        <span class="relative flex h-2.5 w-2.5">
          <span class="h-2.5 w-2.5 rounded-full bg-warn shadow-[0_0_10px_hsl(var(--warn))] animate-breathe" />
        </span>
        等待连接器拨入——把接入代码交给业务侧，连接建立后这里会实时转为在线。
        <span v-if="overview.gateway.last_seen" class="ml-auto text-xs text-muted-foreground">网关最近联系：{{ fmtAgo(overview.gateway.last_seen) }}</span>
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <KpiCard label="在线连接" :value="overview.kpi.online_connections" :suffix="` / ${fmtNum(overview.kpi.total_credentials)}`" :icon="Cable" tone="mirage" :beam="overview.kpi.online_connections > 0" hint="在线会话 / 凭证总数" />
        <KpiCard label="离线凭证" :value="overview.kpi.total_credentials - overview.kpi.online_connections" :icon="Unplug" tone="info" hint="签发过但当前无连接" />
        <KpiCard label="今日断线" :value="overview.kpi.disconnects_today" :icon="Check" tone="warn" hint="含计划内下线与意外掉线" />
        <KpiCard label="隧道延迟" :value="overview.kpi.rtt_median_ms" suffix=" ms" :icon="ServerCog" tone="primary" hint="在线会话 RTT 中位数" />
      </div>

      <!-- 最近事件时间线（可展开） -->
      <details v-if="overview.events.length" class="glass rounded-xl">
        <summary class="cursor-pointer px-4 py-3 text-sm font-medium">最近事件（{{ overview.events.length }} 条）</summary>
        <ul class="flex flex-col gap-1.5 border-t border-border/60 px-4 py-3 text-xs">
          <li v-for="(e, i) in overview.events" :key="i" class="flex flex-wrap items-center gap-2">
            <span class="font-mono text-muted-foreground">{{ fmtAgo(e.at) }}</span>
            <Badge :tone="e.kind === 'online' || e.kind === 'reconnect' ? 'mirage' : 'warn'" dot>{{ e.kind === 'online' ? '上线' : e.kind === 'reconnect' ? '重连' : '下线' }}</Badge>
            <span>{{ e.name }}</span>
            <span v-if="e.detail" class="text-muted-foreground">· {{ e.detail }}</span>
          </li>
        </ul>
      </details>

      <!-- 连接卡片（每个凭证一张） -->
      <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <ConnectionCard
          v-for="c in overview.credentials"
          :key="c.id"
          :credential="c"
          :session="sessionByCred.get(c.id)"
          :can-manage="canManage"
          @revoke="revoke"
          @reset="reset"
        />
      </div>

      <AccessGuide />
    </template>

    <IssueWizard v-model:open="wizardOpen" @created="onCreated" />
  </div>
</template>
