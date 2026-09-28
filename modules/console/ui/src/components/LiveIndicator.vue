<script setup lang="ts">
import { computed } from 'vue'
import { useLiveStore } from '@/stores/live'
import { fmtAgo } from '@/lib/format'
import { useNow } from '@vueuse/core'

const live = useLiveStore()
const now = useNow({ interval: 1000 })
const view = computed(() => {
  switch (live.state) {
    case 'open':
      return { text: '实时', dot: 'bg-mirage', ring: 'shadow-[0_0_10px_hsl(var(--mirage))]', breathe: true }
    case 'connecting':
      return { text: '连接中', dot: 'bg-info', ring: '', breathe: true }
    case 'reconnecting':
      return { text: '重连中', dot: 'bg-warn', ring: '', breathe: true }
    default:
      return { text: '已断开', dot: 'bg-muted-foreground', ring: '', breathe: false }
  }
})
const title = computed(
  () =>
    `实时事件流：${view.value.text}${live.detail ? '（' + live.detail + '）' : ''}\n最新事件：${fmtAgo(live.lastEventAt, now.value.getTime())}\n核心侧丢弃：${live.dropped} 条`,
)
</script>

<template>
  <div class="glass flex items-center gap-2 rounded-full px-3 py-1.5 text-xs" :title="title" role="status" aria-live="polite">
    <span class="relative flex h-2 w-2">
      <span class="h-2 w-2 rounded-full" :class="[view.dot, view.ring, view.breathe && 'animate-breathe']" />
    </span>
    <span class="text-foreground/90">{{ view.text }}</span>
    <span class="hidden text-muted-foreground xl:inline">· {{ live.lastEventAt ? fmtAgo(live.lastEventAt, now.getTime()) : '等待新事件' }}</span>
    <span v-if="live.dropped > 0" class="rounded-full bg-warn/15 px-1.5 text-warn">丢弃 {{ live.dropped }}</span>
  </div>
</template>
