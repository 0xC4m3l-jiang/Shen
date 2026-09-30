<script setup lang="ts">
import Sheet from '@/components/ui/sheet/Sheet.vue'

export interface PickItem {
  id: string
  title: string
  tag: string
  description: string
  hint?: string
}

const open = defineModel<boolean>('open', { default: false })
defineProps<{ title: string; description: string; items: PickItem[] }>()
const emit = defineEmits<{ pick: [id: string] }>()

function pick(id: string): void {
  emit('pick', id)
  open.value = false
}
</script>

<template>
  <Sheet v-model:open="open" side="center" width="max-w-4xl" :title="title" :description="description">
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <button
        v-for="(it, i) in items"
        :key="it.id"
        type="button"
        class="group flex cursor-pointer flex-col gap-2 rounded-xl border border-border/60 bg-background/40 p-4 text-left transition-all duration-200 hover:-translate-y-0.5 hover:border-primary/50 hover:bg-primary/5 hover:shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        :style="{ animationDelay: `${i * 30}ms` }"
        @click="pick(it.id)"
      >
        <span class="flex items-center justify-between gap-2">
          <span class="text-sm font-medium">{{ it.title }}</span>
          <span class="rounded-md bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] text-primary">{{ it.tag }}</span>
        </span>
        <span class="text-xs leading-relaxed text-muted-foreground">{{ it.description }}</span>
        <span v-if="it.hint" class="mt-auto font-mono text-[11px] text-muted-foreground/80">{{ it.hint }}</span>
      </button>
    </div>
  </Sheet>
</template>
