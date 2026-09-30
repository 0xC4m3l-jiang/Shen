<script setup lang="ts">
import { computed } from 'vue'
import { CircleCheck, RotateCcw, Save } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{
  dirty: boolean
  errors: number
  warnings: number
  saving: boolean
  canWrite: boolean
  conflict: boolean
  saveError: string
  savedAt: number
}>()
const emit = defineEmits<{ save: []; reset: []; keepMine: [] }>()

const justSaved = computed(() => props.savedAt > 0)
</script>

<template>
  <Transition name="bar">
    <div
      v-if="dirty || conflict || saveError || justSaved"
      class="glass sticky bottom-4 z-20 flex flex-wrap items-center gap-3 rounded-2xl px-4 py-3 shadow-xl"
    >
      <template v-if="conflict">
        <p class="flex-1 text-sm text-warn">数据集已被他人修改（版本不一致）。你的修改仍保留在本页。</p>
        <Button variant="outline" size="sm" @click="emit('keepMine')">刷新并保留我的修改</Button>
        <Button variant="ghost" size="sm" @click="emit('reset')">放弃我的修改</Button>
      </template>
      <template v-else-if="dirty">
        <p class="flex flex-1 flex-wrap items-center gap-2 text-sm">
          <span class="font-medium">有未保存的修改</span>
          <span class="text-muted-foreground">·</span>
          <span :class="errors ? 'text-danger' : 'text-origin'">校验 {{ errors }} 错</span>
          <span :class="warnings ? 'text-warn' : 'text-muted-foreground'">{{ warnings }} 警告</span>
          <span v-if="saveError" class="text-xs text-danger">· {{ saveError }}</span>
          <span v-if="!canWrite" class="text-xs text-muted-foreground">· 当前角色无权保存本域</span>
        </p>
        <Button variant="ghost" size="sm" :disabled="saving" @click="emit('reset')"><RotateCcw class="h-3.5 w-3.5" />撤销</Button>
        <Button size="sm" :loading="saving" :disabled="!canWrite || errors > 0" @click="emit('save')">
          <Save class="h-3.5 w-3.5" />保存并下发
        </Button>
      </template>
      <p v-else-if="saveError" class="flex-1 text-sm text-danger">{{ saveError }}</p>
      <p v-else class="flex flex-1 items-center gap-2 text-sm text-origin">
        <CircleCheck class="h-4 w-4" />已保存：核心将在下一个同步周期内终检并热替换，顶部同步条会依次点亮。
      </p>
    </div>
  </Transition>
</template>

<style scoped>
.bar-enter-active,
.bar-leave-active {
  transition: all 0.3s ease;
}
.bar-enter-from,
.bar-leave-to {
  opacity: 0;
  transform: translateY(12px);
}
</style>
