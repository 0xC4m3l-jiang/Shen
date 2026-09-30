<script setup lang="ts">
// 聚光卡片（Inspira UI「Card Spotlight」思路，MIT）：玻璃底 + 跟随鼠标的径向光斑 + 可选描边流光。
import { ref, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'
import BorderBeam from './BorderBeam.vue'

const props = withDefaults(
  // bodyClass 作用在内层包裹上：插槽内容的 flex / grid / gap 要写在这里（写在 class 上够不到子元素）。
  defineProps<{ beam?: boolean; interactive?: boolean; class?: HTMLAttributes['class']; bodyClass?: HTMLAttributes['class']; as?: string }>(),
  { beam: false, interactive: true, as: 'div' },
)
const el = ref<HTMLElement | null>(null)
const pos = ref({ x: -999, y: -999 })
const hovering = ref(false)

function onMove(e: MouseEvent) {
  if (!props.interactive || !el.value) return
  const r = el.value.getBoundingClientRect()
  pos.value = { x: e.clientX - r.left, y: e.clientY - r.top }
}
</script>

<template>
  <component
    :is="props.as"
    ref="el"
    :class="
      cn(
        'glass group relative overflow-hidden rounded-2xl transition-all duration-300',
        props.interactive && 'hover:-translate-y-0.5 hover:border-primary/35 hover:shadow-[0_18px_40px_-20px_hsl(var(--primary)/0.55)]',
        props.class,
      )
    "
    @mousemove="onMove"
    @mouseenter="hovering = true"
    @mouseleave="hovering = false"
  >
    <div
      v-if="props.interactive"
      class="pointer-events-none absolute inset-0 transition-opacity duration-300"
      :style="{
        opacity: hovering ? 1 : 0,
        background: `radial-gradient(420px circle at ${pos.x}px ${pos.y}px, hsl(var(--primary) / 0.12), transparent 65%)`,
      }"
      aria-hidden="true"
    />
    <BorderBeam v-if="props.beam" />
    <div :class="cn('relative', props.bodyClass)">
      <slot />
    </div>
  </component>
</template>
