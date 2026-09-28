<script setup lang="ts">
import { CheckCircle2, ShieldAlert, XCircle } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import type { AuditEntry } from '@/lib/types'

const audit = useLiveResource((signal) => api.get<{ entries: AuditEntry[] }>('/api/v1/audit', { limit: 200 }, signal), { live: false, pollMs: 15_000 })
const actions: Record<string, string> = {
  'auth.login': '登录', 'auth.logout': '登出', 'auth.password_change': '修改口令', 'user.create': '新建账号',
  'user.update': '修改账号', 'user.password_reset': '重置口令', 'user.delete': '删除账号',
  'registry.create': '登记服务', 'registry.update': '修改登记', 'registry.delete': '删除登记', 'request.denied': '请求被拒',
}
</script>

<template>
  <SpotlightCard :interactive="false" class="p-5">
    <ol class="relative ml-3 border-l border-primary/25">
      <li v-for="(e, i) in audit.data.value?.entries ?? []" :key="i" class="relative mb-4 pl-6">
        <span class="absolute -left-[9px] top-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-background">
          <CheckCircle2 v-if="e.result === 'ok'" class="h-4 w-4 text-mirage" />
          <ShieldAlert v-else-if="e.result === 'denied' || e.result === 'locked'" class="h-4 w-4 text-warn" />
          <XCircle v-else class="h-4 w-4 text-danger" />
        </span>
        <div class="flex flex-wrap items-center gap-2 text-sm">
          <span class="font-medium">{{ actions[e.action] ?? e.action }}</span>
          <span class="text-muted-foreground">{{ e.actor || '（匿名）' }}</span>
          <span v-if="e.target" class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{{ e.target }}</span>
          <span class="ml-auto text-xs text-muted-foreground">{{ fmtDateTime(e.at) }}</span>
        </div>
        <div class="mt-0.5 text-xs text-muted-foreground">来源 {{ e.source || '—' }}<span v-if="e.detail"> · {{ e.detail }}</span></div>
      </li>
    </ol>
    <p v-if="!audit.data.value?.entries?.length" class="py-8 text-center text-sm text-muted-foreground">暂无审计记录</p>
  </SpotlightCard>
</template>
