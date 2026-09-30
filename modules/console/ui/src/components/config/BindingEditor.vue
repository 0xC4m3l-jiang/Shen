<script setup lang="ts">
import { computed, ref } from 'vue'
import { Link2, Plus, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import SaveBar from './SaveBar.vue'
import { useDomainDraft } from '@/composables/useDomainDraft'
import { useConfigStore } from '@/stores/deceptionConfig'

const store = useConfigStore()
const d = useDomainDraft('bindings', (ds) => ds.bindings, (v) => ({ bindings: v }))
defineExpose({ draft: d })

const services = computed(() => store.view?.services ?? [])
const nameOf = (id: string) => services.value.find((s) => s.id === id)?.name ?? id
const unbound = computed(() => services.value.filter((s) => !d.draft.value.some((b) => b.service_id === s.id)))
const pick = ref('')
const svcOptions = computed(() => [{ value: '', label: '选择服务…' }, ...unbound.value.map((s) => ({ value: s.id, label: `${s.name}（${s.hosts.join(', ')}）` }))])
const decoys = computed(() => store.dataset?.decoys ?? [])

function add(): void {
  if (!pick.value) return
  d.draft.value = [...d.draft.value, { service_id: pick.value, decoy_ids: [] }]
  pick.value = ''
}

function toggle(i: number, id: string): void {
  d.draft.value = d.draft.value.map((b, j) =>
    j !== i ? b : { ...b, decoy_ids: b.decoy_ids.includes(id) ? b.decoy_ids.filter((x) => x !== id) : [...b.decoy_ids, id] },
  )
}
</script>

<template>
  <SpotlightCard :interactive="false" class="p-5" body-class="flex flex-col gap-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="flex items-center gap-2 text-base font-medium"><Link2 class="h-4 w-4 text-primary" />服务级诱饵绑定</h2>
        <p class="text-xs text-muted-foreground">
          <strong class="text-foreground">替换语义</strong>：服务一旦绑定，就只使用绑定的诱饵集，不再吃全局诱饵（下发时自动把服务域名并入被绑定诱饵、从其他诱饵中剔除）。
        </p>
      </div>
      <div v-if="d.canWrite.value && unbound.length" class="flex items-center gap-2">
        <NativeSelect v-model="pick" :options="svcOptions" label="选择服务" class="w-64" />
        <Button size="sm" :disabled="!pick" @click="add"><Plus class="h-3.5 w-3.5" />绑定</Button>
      </div>
    </div>
    <p v-if="!services.length" class="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
      还没有登记服务 —— 先在「反向链接器」登记被保护的站点，再为它绑定专属诱饵集。
    </p>
    <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
      <div
        v-for="(b, i) in d.draft.value"
        :key="b.service_id"
        class="flex flex-col gap-3 rounded-xl border p-4 transition"
        :class="d.issuesAt(`bindings[${i}]`).length ? 'border-danger/40 bg-danger/5' : 'border-border/60 hover:border-primary/40'"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="flex items-center gap-2 text-sm font-medium">{{ nameOf(b.service_id) }}<Badge tone="warn">替换全局</Badge></span>
          <Button v-if="d.canWrite.value" variant="ghost" size="icon" aria-label="解除绑定" @click="d.draft.value = d.draft.value.filter((_, j) => j !== i)">
            <Trash2 class="h-3.5 w-3.5 text-danger" />
          </Button>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="a in decoys"
            :key="a.id"
            type="button"
            :disabled="!d.canWrite.value"
            class="cursor-pointer rounded-md border px-2 py-1 font-mono text-[11px] transition disabled:cursor-not-allowed"
            :class="b.decoy_ids.includes(a.id) ? 'border-decoy/50 bg-decoy/15 text-foreground' : 'border-border/60 text-muted-foreground hover:border-primary/40'"
            @click="toggle(i, a.id)"
          >
            {{ a.id }}<span v-if="!a.enabled" class="ml-1 opacity-60">（停用）</span>
          </button>
        </div>
        <p v-if="!b.decoy_ids.length" class="text-[11px] text-warn">未选任何诱饵 = 该服务不投放任何诱饵（仍会从全局诱饵中剔除）。</p>
        <p v-for="iss in d.issuesAt(`bindings[${i}]`)" :key="iss.field + iss.reason" class="text-[11px] text-danger">{{ iss.reason }}</p>
      </div>
    </div>
    <ul v-if="d.errors.value.some((e) => e.domain === 'bindings')" class="flex flex-col gap-1.5">
      <li v-for="e in d.errors.value.filter((x) => x.domain === 'bindings')" :key="e.field + e.reason" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
        <span class="font-mono opacity-80">{{ e.field }}</span> · {{ e.reason }}
      </li>
    </ul>
    <SaveBar
      :dirty="d.dirty.value" :errors="d.errors.value.length" :warnings="d.warnings.value.length" :saving="d.saving.value"
      :can-write="d.canWrite.value" :conflict="d.conflict.value" :save-error="d.saveError.value" :saved-at="d.savedAt.value"
      @save="d.save" @reset="d.reset" @keep-mine="d.keepMineAndRefresh"
    />
  </SpotlightCard>
</template>
