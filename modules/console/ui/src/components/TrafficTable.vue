<script setup lang="ts">
import { RouterLink } from 'vue-router'
import GeoTag from './GeoTag.vue'
import LayerBadge from './LayerBadge.vue'
import MirageState from './MirageState.vue'
import { fmtTime } from '@/lib/format'
import type { TrafficRow } from '@/lib/types'

// 请求明细表：新行从「地平线」下方浮起（animate-rise）；悬停时左侧出现青色竖条。
withDefaults(defineProps<{ rows: TrafficRow[]; showService?: boolean; empty?: string; dense?: boolean }>(), {
  showService: true,
})
defineEmits<{ select: [row: TrafficRow] }>()
</script>

<template>
  <div class="overflow-x-auto">
    <table class="w-full min-w-[860px] border-separate border-spacing-0 text-sm">
      <thead>
        <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
          <th class="px-3 py-2 font-medium">时间</th>
          <th class="px-3 py-2 font-medium">来源</th>
          <th class="px-3 py-2 font-medium">归属地</th>
          <th v-if="showService" class="px-3 py-2 font-medium">服务 / 域名</th>
          <th class="px-3 py-2 font-medium">请求</th>
          <th class="px-3 py-2 font-medium">落点</th>
          <th class="px-3 py-2 font-medium">蜃楼</th>
          <th class="px-3 py-2 text-right font-medium">分值</th>
        </tr>
      </thead>
      <TransitionGroup tag="tbody" name="row">
        <tr
          v-for="row in rows"
          :key="row.decision_id + row.at"
          class="group cursor-pointer transition-colors hover:bg-primary/5"
          @click="$emit('select', row)"
        >
          <td class="relative border-t border-border/50 px-3 py-2.5 font-mono text-xs text-muted-foreground">
            <span class="absolute inset-y-1 left-0 w-0.5 rounded-full bg-primary opacity-0 shadow-[0_0_8px_hsl(var(--primary))] transition-opacity group-hover:opacity-100" />
            {{ fmtTime(row.at) }}
          </td>
          <td class="border-t border-border/50 px-3 py-2.5 font-mono text-xs">{{ row.source_ip || '—' }}</td>
          <td class="max-w-[180px] border-t border-border/50 px-3 py-2.5"><GeoTag :geo="row.geo" compact /></td>
          <td v-if="showService" class="max-w-[200px] border-t border-border/50 px-3 py-2.5 text-xs">
            <RouterLink
              v-if="row.service_id"
              :to="{ name: 'service-detail', params: { id: row.service_id } }"
              class="block truncate text-primary hover:underline"
              @click.stop
            >
              {{ row.service_name }}
            </RouterLink>
            <span class="block truncate text-muted-foreground">{{ row.host || '（无域名）' }}</span>
          </td>
          <td class="max-w-[260px] border-t border-border/50 px-3 py-2.5">
            <span class="mr-1.5 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">{{ row.method }}</span>
            <span class="truncate font-mono text-xs" :title="row.path">{{ row.path }}</span>
          </td>
          <td class="border-t border-border/50 px-3 py-2.5"><LayerBadge :layer="row.layer" /></td>
          <td class="border-t border-border/50 px-3 py-2.5"><MirageState :row="row" /></td>
          <td class="border-t border-border/50 px-3 py-2.5 text-right font-mono text-xs">
            <span :class="(row.score ?? 0) >= 0.9 ? 'text-warn' : 'text-muted-foreground'">
              {{ row.score !== undefined ? row.score.toFixed(2) : '—' }}
            </span>
          </td>
        </tr>
      </TransitionGroup>
    </table>
    <p v-if="!rows.length" class="py-10 text-center text-sm text-muted-foreground">{{ empty ?? '时间窗内暂无流量' }}</p>
  </div>
</template>

<style scoped>
.row-enter-active {
  transition: all 0.5s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.row-enter-from {
  opacity: 0;
  transform: translateY(12px);
  background: hsl(var(--primary) / 0.12);
}
.row-leave-active {
  display: none;
}
</style>
