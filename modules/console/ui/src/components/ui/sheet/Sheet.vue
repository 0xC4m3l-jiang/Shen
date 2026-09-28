<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { cn } from '@/lib/utils'

const open = defineModel<boolean>('open', { default: false })
const props = withDefaults(
  defineProps<{ title: string; description?: string; side?: 'right' | 'center'; width?: string }>(),
  { side: 'right', width: 'max-w-lg' },
)
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay
        class="fixed inset-0 z-50 bg-background/70 backdrop-blur-sm data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0"
      />
      <DialogContent
        :class="
          cn(
            'glass fixed z-50 flex flex-col gap-5 p-6 shadow-2xl outline-none',
            props.side === 'right'
              ? 'inset-y-0 right-0 h-full w-full border-l data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right data-[state=open]:animate-in data-[state=open]:slide-in-from-right'
              : 'left-1/2 top-1/2 w-full -translate-x-1/2 -translate-y-1/2 rounded-2xl data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95',
            props.width,
          )
        "
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex flex-col gap-1">
            <DialogTitle class="text-lg font-semibold">{{ props.title }}</DialogTitle>
            <DialogDescription v-if="props.description" class="text-sm text-muted-foreground">
              {{ props.description }}
            </DialogDescription>
          </div>
          <DialogClose
            class="cursor-pointer rounded-md p-1 text-muted-foreground transition hover:bg-primary/10 hover:text-foreground"
            aria-label="关闭"
          >
            <X class="h-4 w-4" />
          </DialogClose>
        </div>
        <div class="flex-1 overflow-y-auto">
          <slot />
        </div>
        <div v-if="$slots.footer" class="flex justify-end gap-2 border-t border-border/60 pt-4">
          <slot name="footer" />
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
