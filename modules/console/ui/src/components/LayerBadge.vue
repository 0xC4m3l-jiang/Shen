<script setup lang="ts">
import { computed } from 'vue'
import { Ban, Castle, Globe, Radar, Undo2 } from 'lucide-vue-next'
import Badge, { type BadgeTone } from '@/components/ui/badge/Badge.vue'
import { layerMeta } from '@/lib/format'
import type { Layer } from '@/lib/types'

// 落点徽章：颜色 + 图标双重编码（色觉差异时仍可区分）。
const props = defineProps<{ layer: Layer }>()
const icons = { origin: Globe, fallback: Undo2, mirage: Castle, decoy: Radar, block: Ban }
const meta = computed(() => layerMeta[props.layer] ?? layerMeta.origin)
</script>

<template>
  <Badge :tone="meta.tone as BadgeTone" :title="meta.hint">
    <component :is="icons[props.layer] ?? Globe" class="h-3 w-3" />
    {{ meta.label }}
  </Badge>
</template>
