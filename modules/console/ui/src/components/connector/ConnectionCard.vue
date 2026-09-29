<script setup lang="ts">
import { KeyRound, Radio, RotateCcw, ShieldOff, Waypoints } from 'lucide-vue-next'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import { fmtAgo, fmtDateTime } from '@/lib/format'
import type { ConnectorCredential, ConnectorSession } from '@/lib/types'

// 实时连接卡片：状态呼吸点 + 主字段 + 运维字段 + 操作。
// 一张卡 = 一个凭证；会话字段取该凭证当前（或最近一次）的会话。
const props = defineProps<{
  credential: ConnectorCredential
  session?: ConnectorSession | null
  canManage: boolean
}>()
const emit = defineEmits<{ revoke: [c: ConnectorCredential]; reset: [c: ConnectorCredential] }>()
const router = useRouter()

const online = computed(() => !!props.session?.online)
const state = computed(() => {
  if (props.credential.revoked) return { text: '已吊销', dot: 'bg-danger', ring: 'shadow-[0_0_10px_hsl(var(--danger))]', breathe: false }
  if (online.value) return { text: '在线', dot: 'bg-mirage', ring: 'shadow-[0_0_10px_hsl(var(--mirage))]', breathe: true }
  if (props.credential.last_seen) return { text: '离线', dot: 'bg-muted-foreground', ring: '', breathe: false }
  return { text: '等待接入', dot: 'bg-warn', ring: '', breathe: true }
})

function viewTraffic(name: string) {
  void router.push({ name: 'services', query: { focus: name } })
}
</script>

<template>
  <SpotlightCard :interactive="false" :beam="online" class="flex flex-col gap-4 p-5 transition-opacity" :class="!online && 'opacity-[0.85]'">
    <div class="flex items-start justify-between gap-3">
      <div class="flex min-w-0 items-center gap-2.5">
        <span class="relative flex h-2.5 w-2.5 shrink-0">
          <span class="h-2.5 w-2.5 rounded-full" :class="[state.dot, state.ring, state.breathe && 'animate-breathe']" />
        </span>
        <h3 class="truncate text-base font-medium">{{ credential.name }}</h3>
        <Badge tone="muted">{{ online ? '连接器' : credential.last_seen ? '连接器' : '未接入' }}</Badge>
      </div>
      <span class="flex shrink-0 items-center gap-1 rounded-md bg-muted px-2 py-1 font-mono text-xs text-muted-foreground" title="密钥已哈希保存，明文不可再查看">
        <KeyRound class="h-3 w-3" />{{ credential.key_hint }}
      </span>
    </div>

    <div class="flex flex-wrap gap-1.5">
      <span
        v-for="h in credential.hosts"
        :key="h"
        class="rounded-full border border-border px-2.5 py-0.5 font-mono text-xs text-muted-foreground"
      >{{ h }}</span>
    </div>

    <dl class="grid grid-cols-[110px_1fr] gap-x-4 gap-y-2 text-sm">
      <template v-if="session">
        <dt class="text-muted-foreground">业务地址</dt>
        <dd class="break-all font-mono text-xs leading-5">{{ session.local_addr || '—' }}</dd>
        <dt class="text-muted-foreground">网关节点</dt>
        <dd class="font-mono text-xs leading-5">{{ session.gateway_node || '—' }}</dd>
        <dt class="text-muted-foreground">连接器</dt>
        <dd class="font-mono text-xs leading-5">{{ session.connector_version || '—' }} · {{ session.connector_ip }}</dd>
        <dt class="text-muted-foreground">最后心跳</dt>
        <dd class="font-mono text-xs leading-5" :title="fmtDateTime(session.last_heartbeat)">{{ fmtAgo(session.last_heartbeat) }}</dd>
      </template>
      <template v-else>
        <dt class="text-muted-foreground">最近使用</dt>
        <dd class="font-mono text-xs leading-5">{{ credential.last_seen ? fmtAgo(credential.last_seen) : '从未连接' }}</dd>
      </template>
      <dt class="text-muted-foreground">往返延迟</dt>
      <dd class="flex items-center gap-2 font-mono text-xs leading-5">
        <template v-if="session?.rtt_ms">
          <div class="h-1 w-24 overflow-hidden rounded-full bg-muted">
            <div class="h-full rounded-full bg-gradient-to-r from-primary to-mirage" :style="{ width: `${Math.min(100, Math.max(6, 100 - session.rtt_ms / 5))}%` }" />
          </div>
          {{ session.rtt_ms }} ms
        </template>
        <span v-else class="text-muted-foreground">—</span>
      </dd>
    </dl>

    <div class="flex flex-wrap items-center gap-2 border-t border-border/60 pt-3">
      <Button variant="outline" size="sm" @click="viewTraffic(credential.name)">
        <Waypoints class="h-3.5 w-3.5" />查看流量
      </Button>
      <Button v-if="canManage" variant="ghost" size="sm" @click="emit('reset', credential)">
        <RotateCcw class="h-3.5 w-3.5" />重置密钥
      </Button>
      <Button v-if="canManage && !credential.revoked" variant="ghost" size="sm" class="ml-auto hover:text-danger" @click="emit('revoke', credential)">
        <ShieldOff class="h-3.5 w-3.5" />吊销
      </Button>
      <Badge v-else-if="credential.revoked" tone="danger" dot>已吊销</Badge>
      <span v-else class="ml-auto flex items-center gap-1 text-xs text-muted-foreground"><Radio class="h-3 w-3" />{{ state.text }}</span>
    </div>
  </SpotlightCard>
</template>
