<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from 'reka-ui'

const model = defineModel<boolean>({ default: false })
defineProps<{ disabled?: boolean; label?: string }>()
</script>

<template>
  <SwitchRoot
    v-model="model"
    :disabled="disabled"
    :aria-label="label"
    class="peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-gradient-to-r data-[state=checked]:from-primary data-[state=checked]:to-accent data-[state=unchecked]:bg-muted"
  >
    <SwitchThumb
      class="pointer-events-none block h-4 w-4 rounded-full bg-foreground shadow-lg ring-0 transition-transform duration-200 data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0.5"
    />
  </SwitchRoot>
</template>
