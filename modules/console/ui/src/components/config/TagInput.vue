<script setup lang="ts">
import { ref } from 'vue'
import { X } from 'lucide-vue-next'

const model = defineModel<string[]>({ required: true })
const props = defineProps<{ placeholder?: string; disabled?: boolean; mono?: boolean; invalid?: (v: string) => boolean }>()

const text = ref('')

function commit(): void {
  const parts = text.value
    .split(/[\s,，]+/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (!parts.length) return
  const next = [...model.value]
  for (const p of parts) if (!next.includes(p)) next.push(p)
  model.value = next
  text.value = ''
}

function remove(i: number): void {
  model.value = model.value.filter((_, j) => j !== i)
}

function onKey(e: KeyboardEvent): void {
  if (e.key === 'Enter' || e.key === ',') {
    e.preventDefault()
    commit()
  } else if (e.key === 'Backspace' && !text.value && model.value.length) {
    remove(model.value.length - 1)
  }
}
</script>

<template>
  <div
    class="flex min-h-[40px] flex-wrap items-center gap-1.5 rounded-lg border border-input bg-background/40 px-2 py-1.5 transition focus-within:border-primary/60 focus-within:ring-2 focus-within:ring-primary/20"
    :class="props.disabled ? 'cursor-not-allowed opacity-60' : ''"
  >
    <span
      v-for="(tag, i) in model"
      :key="tag"
      class="group inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs transition"
      :class="[
        props.mono ? 'font-mono' : '',
        props.invalid?.(tag) ? 'border-danger/40 bg-danger/10 text-danger' : 'border-primary/25 bg-primary/10 text-foreground',
      ]"
    >
      {{ tag }}
      <button
        v-if="!props.disabled"
        type="button"
        class="cursor-pointer rounded text-muted-foreground transition hover:text-danger"
        :aria-label="`移除 ${tag}`"
        @click="remove(i)"
      >
        <X class="h-3 w-3" />
      </button>
    </span>
    <input
      v-model="text"
      :disabled="props.disabled"
      :placeholder="model.length ? '' : props.placeholder"
      class="min-w-[120px] flex-1 border-0 bg-transparent px-1 py-0.5 text-sm outline-none placeholder:text-muted-foreground/70 disabled:cursor-not-allowed"
      :class="props.mono ? 'font-mono' : ''"
      @keydown="onKey"
      @blur="commit"
    />
  </div>
</template>
