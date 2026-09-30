<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Pencil, Plus, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Switch from '@/components/ui/switch/Switch.vue'
import BindingEditor from './BindingEditor.vue'
import DecoyEditor from './DecoyEditor.vue'
import SaveBar from './SaveBar.vue'
import ValidationPanel from './ValidationPanel.vue'
import { useDomainDraft } from '@/composables/useDomainDraft'
import { decoyKindLabel, type DecoyAsset } from '@/lib/config'
import { useConfigStore } from '@/stores/deceptionConfig'

const store = useConfigStore()
const router = useRouter()
const d = useDomainDraft('decoys', (ds) => ds.decoys, (v) => ({ decoys: v }))

const honeypots = computed(() => store.dataset?.honeypots ?? [])
const projected = computed(() => new Map((store.view?.projection.decoys ?? []).map((p) => [p.id, p.hosts])))
const editing = ref<{ index: number; value: DecoyAsset | null } | null>(null)
const editorOpen = ref(false)
const flash = ref(-1)
const takenIds = computed(() => d.draft.value.filter((_, i) => i !== editing.value?.index).map((a) => a.id))

function openNew(): void {
  editing.value = { index: -1, value: null }
  editorOpen.value = true
}

function openEdit(i: number): void {
  editing.value = { index: i, value: d.draft.value[i] }
  editorOpen.value = true
}

function onSubmit(a: DecoyAsset): void {
  const i = editing.value?.index ?? -1
  d.draft.value = i < 0 ? [...d.draft.value, a] : d.draft.value.map((x, j) => (j === i ? a : x))
}

function setEnabled(i: number, v: boolean): void {
  d.draft.value = d.draft.value.map((x, j) => (j === i ? { ...x, enabled: v } : x))
}

function locate(field: string): void {
  const m = /^decoys\[(\d+)\]/.exec(field)
  if (!m) return
  flash.value = Number(m[1])
  void nextTick(() => document.getElementById(`decoy-row-${m[1]}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }))
  setTimeout(() => (flash.value = -1), 1600)
}

function hostsDiffer(a: DecoyAsset): boolean {
  const p = projected.value.get(a.id)
  if (!p) return a.enabled && !d.dirty.value
  return p.join(',') !== [...a.hosts].sort().join(',')
}

function backendState(a: DecoyAsset): 'ok' | 'off' | 'missing' | 'none' {
  if (!a.backend) return 'none'
  const h = honeypots.value.find((x) => x.name === a.backend)
  return !h ? 'missing' : h.enabled ? 'ok' : 'off'
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
    <div class="flex flex-col gap-4">
      <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-base font-medium">诱饵资产</h2>
            <p class="text-xs text-muted-foreground">投放在真实站点上的钩子。同一主机上的路径不能重复或嵌套；不得落在禁止欺骗路径之下。</p>
          </div>
          <Button v-if="d.canWrite.value" size="sm" @click="openNew"><Plus class="h-3.5 w-3.5" />新建诱饵（含模板向导）</Button>
        </div>
        <table class="w-full min-w-[880px] text-sm">
          <thead>
            <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
              <th class="py-2 font-medium">ID</th>
              <th class="py-2 font-medium">形态</th>
              <th class="py-2 font-medium">路径</th>
              <th class="py-2 font-medium">主机</th>
              <th class="py-2 font-medium">后端</th>
              <th class="py-2 font-medium">内容</th>
              <th class="py-2 font-medium">启用</th>
              <th class="py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(a, i) in d.draft.value"
              :id="`decoy-row-${i}`"
              :key="a.id + i"
              class="group border-t border-border/50 align-top transition-colors hover:bg-primary/5"
              :class="[d.issuesAt(`decoys[${i}]`).length ? 'shadow-[inset_3px_0_0_hsl(var(--danger))]' : '', flash === i ? 'bg-warn/15' : '']"
            >
              <td class="py-3 font-mono font-medium">{{ a.id }}<span v-if="a.note" class="block font-sans text-[11px] font-normal text-muted-foreground">{{ a.note }}</span></td>
              <td class="py-3"><Badge tone="decoy">{{ decoyKindLabel[a.kind] ?? a.kind }}</Badge></td>
              <td class="py-3 font-mono text-xs">{{ a.path }}</td>
              <td class="py-3">
                <div class="flex max-w-[220px] flex-wrap gap-1">
                  <span v-for="h in a.hosts" :key="h" class="rounded bg-muted/40 px-1.5 py-0.5 font-mono text-[11px]">{{ h }}</span>
                  <span v-if="!a.hosts.length" class="text-xs text-muted-foreground">未声明</span>
                </div>
                <span v-if="hostsDiffer(a)" class="mt-1 block text-[11px] text-info" :title="(projected.get(a.id) ?? []).join(', ')">
                  下发主机：{{ projected.get(a.id)?.join(', ') || '（被绑定剔除，不下发）' }}
                </span>
              </td>
              <td class="py-3">
                <button
                  v-if="a.backend"
                  type="button"
                  class="cursor-pointer font-mono text-xs underline-offset-2 hover:underline"
                  :class="backendState(a) === 'ok' ? 'text-foreground' : 'text-danger'"
                  @click="router.replace({ query: { tab: 'honeypots' } })"
                >
                  {{ a.backend }}<span v-if="backendState(a) !== 'ok'">（{{ backendState(a) === 'off' ? '未启用' : '不存在' }}）</span>
                </button>
                <span v-else class="text-xs text-muted-foreground">—</span>
              </td>
              <td class="py-3 font-mono text-[11px] text-muted-foreground">{{ a.content || '—' }}</td>
              <td class="py-3">
                <Switch :model-value="a.enabled" :disabled="!d.canWrite.value" :label="`启用 ${a.id}`" @update:model-value="(v: boolean) => setEnabled(i, v)" />
              </td>
              <td class="py-3 text-right">
                <span v-if="d.canWrite.value" class="inline-flex gap-1 opacity-60 transition group-hover:opacity-100">
                  <Button variant="ghost" size="icon" :aria-label="`编辑 ${a.id}`" @click="openEdit(i)"><Pencil class="h-3.5 w-3.5" /></Button>
                  <Button variant="ghost" size="icon" :aria-label="`删除 ${a.id}`" @click="d.draft.value = d.draft.value.filter((_, j) => j !== i)">
                    <Trash2 class="h-3.5 w-3.5 text-danger" />
                  </Button>
                </span>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="!d.draft.value.length" class="py-8 text-center text-sm text-muted-foreground">还没有诱饵：用模板向导投放第一条钩子（建议先停用保存，观察后再启用）。</p>
      </SpotlightCard>
      <SaveBar
        :dirty="d.dirty.value" :errors="d.errors.value.length" :warnings="d.warnings.value.length" :saving="d.saving.value"
        :can-write="d.canWrite.value" :conflict="d.conflict.value" :save-error="d.saveError.value" :saved-at="d.savedAt.value"
        @save="d.save" @reset="d.reset" @keep-mine="d.keepMineAndRefresh"
      />
      <BindingEditor />
    </div>
    <ValidationPanel
      :errors="d.errors.value" :warnings="d.warnings.value" :validating="d.validating.value" :dirty="d.dirty.value"
      :domains="['decoys', 'bindings', 'blacklist', 'whitelist']" @locate="locate"
    />
  </div>
  <DecoyEditor
    v-model:open="editorOpen" :initial="editing?.value ?? null" :is-new="(editing?.index ?? -1) < 0"
    :templates="store.templates?.templates.decoys ?? []" :honeypots="honeypots" :taken-ids="takenIds" @submit="onSubmit"
  />
</template>
