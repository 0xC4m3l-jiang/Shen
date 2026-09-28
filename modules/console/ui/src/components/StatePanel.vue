<script setup lang="ts">
import { AlertTriangle, CloudOff, Info, RefreshCw } from 'lucide-vue-next'
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import type { ApiError } from '@/lib/api'
import type { Window } from '@/lib/types'
import { fmtDateTime } from '@/lib/format'

// 统一的「错误 / 数据覆盖说明」条：让用户知道下一步该做什么，而不是只看到空白。
const props = defineProps<{ error?: ApiError | null; window?: Window | null; loading?: boolean }>()
const emit = defineEmits<{ retry: [] }>()

const advice = computed(() => {
  const e = props.error
  if (!e) return ''
  if (e.code === 'core_unavailable') return '核心不可达：检查 core 容器是否在运行（docker compose ps / logs core）。'
  if (e.status === 403) return '当前角色无权查看此内容；如需访问请联系管理员调整角色。'
  if (e.status === 0) return '无法连接管控台 API：检查 console-api 容器与 nginx 反代。'
  return '请稍后重试；若持续失败，请查看 console-api 日志。'
})
</script>

<template>
  <div v-if="error" class="glass flex flex-wrap items-center gap-3 rounded-xl border-danger/30 px-4 py-3 text-sm animate-rise">
    <component :is="error.code === 'core_unavailable' ? CloudOff : AlertTriangle" class="h-4 w-4 text-danger" />
    <span class="font-medium text-foreground">{{ error.message }}</span>
    <span class="text-muted-foreground">{{ advice }}</span>
    <Button size="sm" variant="outline" class="ml-auto" :loading="loading" @click="emit('retry')">
      <RefreshCw /> 重试
    </Button>
  </div>
  <div
    v-else-if="window?.truncated"
    class="flex items-center gap-2 rounded-xl border border-info/30 bg-info/10 px-4 py-2 text-xs text-info animate-rise"
  >
    <Info class="h-4 w-4" />
    <span>{{ window.note }}（实际覆盖自 {{ fmtDateTime(window.coverage_start) }}）</span>
  </div>
</template>
