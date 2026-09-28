<script setup lang="ts">
import { computed } from 'vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { deliveryLabels, mirageState } from '@/lib/format'
import type { TrafficRow } from '@/lib/types'

// 「是否流入蜃楼」：已进入 / 影子（本会进入）/ 失败（诱饵 502 或回落业务）/ 未进入。
const props = defineProps<{ row: TrafficRow }>()
const state = computed(() => mirageState(props.row))
const failText = computed(() =>
  props.row.layer === 'fallback' ? '幻境不可用，回落业务' : deliveryLabels[props.row.delivery_result ?? ''] ?? '投递失败',
)
</script>

<template>
  <Badge v-if="state === 'in'" tone="mirage" dot>已流入蜃楼</Badge>
  <Badge v-else-if="state === 'shadow'" tone="info" dot title="影子模式：只观测不处置；放开后该请求会被改道">影子·本会流入</Badge>
  <Badge v-else-if="state === 'failed'" tone="warn" dot :title="failText">{{ failText }}</Badge>
  <span v-else class="text-xs text-muted-foreground">—</span>
</template>
