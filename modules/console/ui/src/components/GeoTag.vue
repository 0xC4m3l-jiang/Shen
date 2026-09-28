<script setup lang="ts">
import { computed } from 'vue'
import { Building2, House, MapPin, ShieldQuestion } from 'lucide-vue-next'
import type { GeoLocation } from '@/lib/types'

const props = defineProps<{ geo?: GeoLocation; compact?: boolean }>()
const icon = computed(() => {
  switch (props.geo?.scope) {
    case 'private':
    case 'loopback':
      return House
    case 'reserved':
    case 'unknown':
    case undefined:
      return ShieldQuestion
    default:
      return props.geo?.country === '中国' ? MapPin : Building2
  }
})
const tone = computed(() =>
  props.geo?.scope === 'public' ? 'text-foreground/90' : props.geo?.scope === 'private' || props.geo?.scope === 'loopback' ? 'text-secondary' : 'text-muted-foreground',
)
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5 text-xs" :class="tone" :title="geo?.isp ? `${geo.label} · ${geo.isp}` : geo?.label">
    <component :is="icon" class="h-3.5 w-3.5 shrink-0 opacity-80" />
    <span class="truncate">{{ geo?.label || '未知来源' }}</span>
    <span v-if="!compact && geo?.isp" class="truncate text-muted-foreground">· {{ geo.isp }}</span>
  </span>
</template>
