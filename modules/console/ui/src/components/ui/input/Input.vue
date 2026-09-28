<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: HTMLAttributes['class'] }>()
const model = defineModel<string | number>()
</script>

<template>
  <input
    v-model="model"
    :class="
      cn(
        'flex h-10 w-full rounded-md border border-input bg-background/40 px-3 py-2 text-sm text-foreground shadow-inner outline-none',
        'placeholder:text-muted-foreground/70 transition-all duration-200',
        'hover:border-primary/40 focus:border-primary/70 focus:bg-background/60 focus:ring-2 focus:ring-primary/25',
        'disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>
