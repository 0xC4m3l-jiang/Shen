<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import StatePanel from '@/components/StatePanel.vue'
import SyncBar from '@/components/config/SyncBar.vue'
import SystemAlertList from '@/components/config/SystemAlertList.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import type { SeedOutcome, SyncStatus, SystemAlert } from '@/lib/config'
import { fmtAgo, fmtDateTime } from '@/lib/format'

// 同步与健康：管控台 → 核心 → 边缘 的版本对账、核心拉取 / 上报时间、逐适配器回执、蜜罐探测。
type Resp = { sync: SyncStatus; alerts: SystemAlert[]; seed: SeedOutcome; dataset_version: number; initialized: boolean }
const { data, error, loading, refresh } = useLiveResource((signal) => api.get<Resp>('/api/v1/config/sync', undefined, signal), { live: false })
// 同步状态不随流量事件变化：固定 8 秒刷新一次。
const timer = setInterval(() => void refresh(), 8000)
onBeforeUnmount(() => clearInterval(timer))
const s = computed(() => data.value?.sync)
</script>

<template>
  <div class="flex flex-col gap-4">
    <StatePanel :error="error" :loading="loading && !data" @retry="refresh" />
    <template v-if="data && s">
      <SyncBar :sync="s" :dataset-version="data.dataset_version" :initialized="data.initialized" :seed="data.seed" />
      <section class="grid grid-cols-1 gap-4 md:grid-cols-4">
        <SpotlightCard :interactive="false" class="p-4">
          <span class="text-xs text-muted-foreground">核心最近拉取</span>
          <p class="mt-1 text-lg font-semibold">{{ fmtAgo(s.last_pull_at) }}</p>
          <p class="text-[11px] text-muted-foreground">{{ s.last_pull_at ? fmtDateTime(s.last_pull_at) : '尚未拉取' }}</p>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="p-4">
          <span class="text-xs text-muted-foreground">核心最近上报</span>
          <p class="mt-1 text-lg font-semibold">{{ fmtAgo(s.last_report_at) }}</p>
          <p class="text-[11px] text-muted-foreground">超过 90 秒未上报视为离线</p>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="p-4">
          <span class="text-xs text-muted-foreground">生效策略版本</span>
          <p class="mt-1 font-mono text-lg font-semibold">v{{ s.policy_version || '—' }}</p>
          <p class="text-[11px] text-muted-foreground">= 部署配置 version × 10⁶ + 投影修订号</p>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="p-4">
          <span class="text-xs text-muted-foreground">核心应用结论</span>
          <p class="mt-1"><Badge :tone="s.applied ? 'origin' : 'danger'" dot>{{ s.applied ? '已应用' : '已拒绝（保留 last-good）' }}</Badge></p>
          <p class="mt-1 line-clamp-2 text-[11px] text-muted-foreground" :title="s.reason">{{ s.reason || '—' }}</p>
        </SpotlightCard>
      </section>

      <SystemAlertList v-if="data.alerts.length" :alerts="data.alerts" />

      <section class="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
          <h2 class="mb-3 text-base font-medium">边缘适配器回执</h2>
          <table class="w-full min-w-[480px] text-sm">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
                <th class="py-2 font-medium">适配器</th><th class="py-2 font-medium">已确认版本</th><th class="py-2 font-medium">结果</th><th class="py-2 text-right font-medium">回执时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="a in s.edge_acks" :key="a.adapter_id" class="border-t border-border/50 hover:bg-primary/5">
                <td class="py-2.5 font-mono text-xs">{{ a.adapter_id }}</td>
                <td class="py-2.5 font-mono text-xs" :class="a.version < s.policy_version ? 'text-warn' : ''">v{{ a.version }}</td>
                <td class="py-2.5"><Badge :tone="a.applied ? 'origin' : 'danger'" dot :title="a.reason">{{ a.applied ? '已应用' : '失败' }}</Badge></td>
                <td class="py-2.5 text-right text-xs text-muted-foreground">{{ fmtAgo(a.received_at) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!s.edge_acks.length" class="py-6 text-center text-sm text-muted-foreground">暂无适配器回执（边缘尚未拉取策略面）</p>
        </SpotlightCard>
        <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
          <h2 class="mb-3 text-base font-medium">蜜罐健康探测 <span class="text-xs text-muted-foreground">· TCP 拨号，间隔 15s，超时 1s</span></h2>
          <table class="w-full min-w-[480px] text-sm">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
                <th class="py-2 font-medium">蜜罐</th><th class="py-2 font-medium">地址</th><th class="py-2 font-medium">状态</th><th class="py-2 text-right font-medium">探测时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="h in s.honeypots" :key="h.name" class="border-t border-border/50 hover:bg-primary/5">
                <td class="py-2.5 font-mono text-xs">{{ h.name }}</td>
                <td class="py-2.5 font-mono text-xs text-muted-foreground">{{ h.addr }}</td>
                <td class="py-2.5"><Badge :tone="h.healthy ? 'origin' : 'danger'" dot :title="h.error">{{ h.healthy ? `${h.latency_ms}ms` : '不可用' }}</Badge></td>
                <td class="py-2.5 text-right text-xs text-muted-foreground">{{ fmtAgo(h.checked_at) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!s.honeypots.length" class="py-6 text-center text-sm text-muted-foreground">暂无探测结果（未启用同步或蜜罐池中没有启用的蜜罐）</p>
        </SpotlightCard>
      </section>
    </template>
  </div>
</template>
