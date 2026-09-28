<script setup lang="ts">
import { TabsIndicator, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'

export interface TabItem {
  value: string
  label: string
  count?: number
}

const model = defineModel<string>({ required: true })
defineProps<{ items: TabItem[] }>()
</script>

<template>
  <TabsRoot v-model="model" class="flex flex-col gap-5">
    <TabsList class="glass relative flex w-fit items-center gap-1 rounded-xl p-1">
      <TabsIndicator
        class="absolute bottom-1 left-0 top-1 w-[--reka-tabs-indicator-size] translate-x-[--reka-tabs-indicator-position] rounded-lg bg-gradient-to-r from-primary/25 to-accent/20 shadow-[0_0_18px_-6px_hsl(var(--primary)/0.8)] transition-all duration-300"
      />
      <TabsTrigger
        v-for="item in items"
        :key="item.value"
        :value="item.value"
        class="relative z-10 flex cursor-pointer items-center gap-2 rounded-lg px-4 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground data-[state=active]:text-foreground"
      >
        {{ item.label }}
        <span v-if="item.count !== undefined" class="rounded-full bg-primary/15 px-1.5 text-[11px] text-primary">
          {{ item.count }}
        </span>
      </TabsTrigger>
    </TabsList>
    <slot />
  </TabsRoot>
</template>
