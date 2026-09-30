<script setup lang="ts">
import { computed } from 'vue'
import { CircleAlert, CircleCheck, LoaderCircle, TriangleAlert } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { domainLabel, type Domain, type Issue } from '@/lib/config'

const props = defineProps<{ errors: Issue[]; warnings: Issue[]; validating?: boolean; domains: Domain[]; dirty?: boolean }>()
const emit = defineEmits<{ locate: [field: string] }>()

const mine = (list: Issue[]) => list.filter((i) => props.domains.includes(i.domain))
const others = (list: Issue[]) => list.filter((i) => !props.domains.includes(i.domain))
const errs = computed(() => mine(props.errors))
const warns = computed(() => mine(props.warnings))
const elsewhere = computed(() => others(props.errors))
</script>

<template>
  <SpotlightCard :interactive="false" class="sticky top-24 p-4" body-class="flex flex-col gap-3">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-medium">冲突预检</h3>
      <span class="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <LoaderCircle v-if="validating" class="h-3 w-3 animate-spin" />
        {{ validating ? '校验中…' : dirty ? '基于未保存的草稿' : '基于已保存的数据' }}
      </span>
    </div>

    <div
      v-if="!errs.length && !warns.length"
      class="flex items-center gap-2 rounded-lg border border-origin/30 bg-origin/10 px-3 py-2.5 text-xs text-origin"
    >
      <CircleCheck class="h-4 w-4" />没有发现冲突，可以保存
    </div>

    <TransitionGroup tag="ul" name="list" class="flex max-h-[60vh] flex-col gap-2 overflow-y-auto pr-1">
      <li v-for="(i, n) in errs" :key="'e' + n + i.field">
        <button
          type="button"
          class="flex w-full cursor-pointer items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-left text-xs text-danger transition hover:bg-danger/15"
          @click="emit('locate', i.field)"
        >
          <CircleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span class="flex flex-col gap-0.5">
            <span class="font-mono text-[11px] opacity-80">{{ i.field }}</span>
            <span class="text-foreground/90">{{ i.reason }}</span>
          </span>
        </button>
      </li>
      <li v-for="(i, n) in warns" :key="'w' + n + i.field">
        <button
          type="button"
          class="flex w-full cursor-pointer items-start gap-2 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-left text-xs text-warn transition hover:bg-warn/15"
          @click="emit('locate', i.field)"
        >
          <TriangleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span class="flex flex-col gap-0.5">
            <span class="font-mono text-[11px] opacity-80">{{ i.field }}</span>
            <span class="text-foreground/90">{{ i.reason }}</span>
          </span>
        </button>
      </li>
    </TransitionGroup>

    <p v-if="elsewhere.length" class="rounded-lg border border-border/60 px-3 py-2 text-[11px] text-muted-foreground">
      其他域还有 {{ elsewhere.length }} 个错误（{{ [...new Set(elsewhere.map((i) => domainLabel[i.domain]))].join('、') }}）——
      保存本域前需要一并修正。
    </p>
    <p class="text-[11px] leading-relaxed text-muted-foreground">
      错误会阻止保存；警告允许保存但请确认。核心在应用前还会做一次终检，未通过时保留上一份正确配置。
    </p>
  </SpotlightCard>
</template>

<style scoped>
.list-enter-active,
.list-leave-active {
  transition: all 0.25s ease;
}
.list-enter-from,
.list-leave-to {
  opacity: 0;
  transform: translateX(8px);
}
</style>
