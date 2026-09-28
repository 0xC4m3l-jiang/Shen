<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { Loader2 } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { buttonVariants, type ButtonVariants } from '.'

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariants['variant']
    size?: ButtonVariants['size']
    type?: 'button' | 'submit' | 'reset'
    loading?: boolean
    disabled?: boolean
    class?: HTMLAttributes['class']
  }>(),
  { type: 'button' },
)
</script>

<template>
  <button
    :type="props.type"
    :disabled="props.disabled || props.loading"
    :aria-busy="props.loading || undefined"
    :class="cn(buttonVariants({ variant: props.variant, size: props.size }), props.class)"
  >
    <Loader2 v-if="props.loading" class="animate-spin" />
    <slot />
  </button>
</template>
