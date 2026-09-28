<script setup lang="ts">
import { ChevronDown } from 'lucide-vue-next'
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

defineProps<{ options: readonly { value: string; label: string }[]; class?: HTMLAttributes['class']; label?: string }>()
const model = defineModel<string>()
</script>

<template>
  <div :class="cn('relative', $props.class)">
    <select
      v-model="model"
      :aria-label="label"
      class="h-9 w-full cursor-pointer appearance-none rounded-md border border-input bg-background/40 pl-3 pr-8 text-sm text-foreground outline-none transition hover:border-primary/40 focus:border-primary/70 focus:ring-2 focus:ring-primary/25"
    >
      <option v-for="o in options" :key="o.value" :value="o.value" class="bg-popover text-popover-foreground">
        {{ o.label }}
      </option>
    </select>
    <ChevronDown class="pointer-events-none absolute right-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
  </div>
</template>
