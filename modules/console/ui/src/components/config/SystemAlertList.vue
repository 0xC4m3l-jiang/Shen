<script setup lang="ts">
import { AlertOctagon, AlertTriangle, ArrowUpRight } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import type { SystemAlert } from '@/lib/config'
import { fmtAgo } from '@/lib/format'

defineProps<{ alerts: SystemAlert[]; compact?: boolean }>()

/** 告警 id 前缀 → 去处置的页面（欺骗管控的哪个 Tab / 系统页）。 */
function target(id: string): { name: string; query?: Record<string, string> } {
  if (id.startsWith('honeypot_unhealthy')) return { name: 'config', query: { tab: 'honeypots' } }
  if (id.startsWith('edge_') || id === 'core_unreachable' || id === 'core_sync_unconfigured') return { name: 'system', query: { tab: 'sync' } }
  if (id === 'merge_rejected' || id === 'core_behind') return { name: 'config', query: { tab: 'versions' } }
  return { name: 'config' }
}
</script>

<template>
  <section class="flex flex-col gap-2">
    <h2 v-if="!compact" class="text-base font-medium">系统告警 <span class="text-xs text-muted-foreground">· 配置同步 / 蜜罐健康 / 边缘回执（与流量告警分开）</span></h2>
    <TransitionGroup tag="div" name="sys" class="flex flex-col gap-2">
      <RouterLink v-for="a in alerts" :key="a.id" :to="target(a.id)" class="block">
        <SpotlightCard
          class="group border-l-4 p-4" body-class="flex items-start gap-3"
          :class="a.severity === 'critical' ? 'border-l-danger' : 'border-l-warn'"
        >
          <component :is="a.severity === 'critical' ? AlertOctagon : AlertTriangle" class="mt-0.5 h-4 w-4 shrink-0" :class="a.severity === 'critical' ? 'text-danger' : 'text-warn'" />
          <div class="flex min-w-0 flex-1 flex-col gap-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium">{{ a.title }}</span>
              <span class="rounded px-1.5 py-0.5 text-[10px]" :class="a.severity === 'critical' ? 'bg-danger/15 text-danger' : 'bg-warn/15 text-warn'">
                {{ a.severity === 'critical' ? '严重' : '警告' }}
              </span>
              <span v-if="a.since && !a.since.startsWith('0001')" class="ml-auto text-xs text-muted-foreground">{{ fmtAgo(a.since) }}</span>
            </div>
            <p class="text-xs leading-relaxed text-muted-foreground">{{ a.detail }}</p>
          </div>
          <ArrowUpRight class="h-4 w-4 shrink-0 text-muted-foreground transition group-hover:text-primary" />
        </SpotlightCard>
      </RouterLink>
    </TransitionGroup>
  </section>
</template>

<style scoped>
.sys-enter-active {
  transition: all 0.35s ease;
}
.sys-enter-from {
  opacity: 0;
  transform: translateY(8px);
}
</style>
