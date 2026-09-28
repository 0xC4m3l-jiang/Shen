<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <header class="flex flex-wrap items-end justify-between gap-3 animate-rise">
    <div class="flex min-w-0 flex-col gap-1">
      <h1 class="text-foreground">{{ title }}</h1>
      <p v-if="subtitle" class="max-w-3xl truncate text-sm text-muted-foreground">{{ subtitle }}</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </header>
</template>
