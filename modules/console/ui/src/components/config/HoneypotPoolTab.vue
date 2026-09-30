<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { LayoutTemplate, Pencil, Plus, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import HoneypotEditor from './HoneypotEditor.vue'
import SaveBar from './SaveBar.vue'
import TemplatePicker, { type PickItem } from './TemplatePicker.vue'
import ValidationPanel from './ValidationPanel.vue'
import { useDomainDraft } from '@/composables/useDomainDraft'
import { honeypotTypeLabel, type Honeypot } from '@/lib/config'
import { useConfigStore } from '@/stores/deceptionConfig'

const store = useConfigStore()
const d = useDomainDraft('honeypots', (ds) => ds.honeypots, (v) => ({ honeypots: v }))

const health = computed(() => new Map((store.sync?.honeypots ?? []).map((p) => [p.name, p])))
const refs = computed(() => {
  const m = new Map<string, string[]>()
  for (const a of store.dataset?.decoys ?? []) {
    if (!a.enabled || !a.backend) continue
    m.set(a.backend, [...(m.get(a.backend) ?? []), a.id])
  }
  return m
})

const editing = ref<{ index: number; value: Honeypot | null } | null>(null)
const editorOpen = ref(false)
const pickerOpen = ref(false)
const blocked = ref<{ name: string; ids: string[] } | null>(null)
const flash = ref(-1)

const pickItems = computed<PickItem[]>(() =>
  (store.templates?.templates.honeypots ?? []).map((t) => ({
    id: t.id, title: t.name, tag: t.type, description: t.description, hint: `默认端口 ${t.default_port}`,
  })),
)
const takenNames = computed(() => d.draft.value.filter((_, i) => i !== editing.value?.index).map((h) => h.name))

function openNew(templateId?: string): void {
  const t = store.templates?.templates.honeypots.find((x) => x.id === templateId)
  const base = t
    ? { name: uniqueName(t.suggested_name), type: t.type, addr: `127.0.0.1:${t.default_port}`, enabled: false, description: t.name, template_id: t.id }
    : null
  editing.value = { index: -1, value: base }
  editorOpen.value = true
}

function uniqueName(base: string): string {
  const names = new Set(d.draft.value.map((h) => h.name))
  if (!names.has(base)) return base
  for (let i = 2; ; i++) if (!names.has(`${base}-${i}`)) return `${base}-${i}`
}

function openEdit(i: number): void {
  editing.value = { index: i, value: d.draft.value[i] }
  editorOpen.value = true
}

function onSubmit(h: Honeypot): void {
  const i = editing.value?.index ?? -1
  if (i < 0) d.draft.value = [...d.draft.value, h]
  else d.draft.value = d.draft.value.map((x, j) => (j === i ? h : x))
}

function remove(i: number): void {
  const h = d.draft.value[i]
  const ids = refs.value.get(h.name) ?? []
  if (ids.length) {
    blocked.value = { name: h.name, ids }
    return
  }
  d.draft.value = d.draft.value.filter((_, j) => j !== i)
}

function locate(field: string): void {
  const m = /^honeypots\[(\d+)\]/.exec(field)
  if (!m) return
  flash.value = Number(m[1])
  void nextTick(() => document.getElementById(`hp-row-${m[1]}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }))
  setTimeout(() => (flash.value = -1), 1600)
}

function healthOf(h: Honeypot): { tone: 'origin' | 'danger' | 'muted'; text: string; title: string } {
  if (!h.enabled) return { tone: 'muted', text: '未启用', title: '' }
  const p = health.value.get(h.name)
  if (!p) return { tone: 'muted', text: '待探测', title: '核心尚未上报该蜜罐的探测结果' }
  return p.healthy
    ? { tone: 'origin', text: `${p.latency_ms}ms`, title: `可用 · 探测于 ${p.checked_at}` }
    : { tone: 'danger', text: '不可用', title: p.error ?? '' }
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
    <div class="flex flex-col gap-4">
      <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-base font-medium">蜜罐池</h2>
            <p class="text-xs text-muted-foreground">改道与诱饵的落点。诱饵的后端必须指向这里<strong>已启用</strong>的蜜罐。</p>
          </div>
          <div v-if="d.canWrite.value" class="flex gap-2">
            <Button variant="outline" size="sm" @click="pickerOpen = true"><LayoutTemplate class="h-3.5 w-3.5" />从模板新建</Button>
            <Button size="sm" @click="openNew()"><Plus class="h-3.5 w-3.5" />手动登记</Button>
          </div>
        </div>
        <table class="w-full min-w-[760px] text-sm">
          <thead>
            <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
              <th class="py-2 font-medium">逻辑名</th>
              <th class="py-2 font-medium">类型</th>
              <th class="py-2 font-medium">地址</th>
              <th class="py-2 font-medium">健康</th>
              <th class="py-2 font-medium">被引用</th>
              <th class="py-2 font-medium">启用</th>
              <th class="py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(h, i) in d.draft.value"
              :id="`hp-row-${i}`"
              :key="h.name + i"
              class="group border-t border-border/50 transition-colors hover:bg-primary/5"
              :class="[d.issuesAt(`honeypots[${i}]`).length ? 'shadow-[inset_3px_0_0_hsl(var(--danger))]' : '', flash === i ? 'bg-warn/15' : '']"
            >
              <td class="py-3">
                <span class="font-mono font-medium">{{ h.name }}</span>
                <span v-if="h.description" class="block text-[11px] text-muted-foreground">{{ h.description }}</span>
              </td>
              <td class="py-3"><Badge tone="info">{{ honeypotTypeLabel[h.type] ?? h.type }}</Badge></td>
              <td class="py-3 font-mono text-xs">{{ h.addr }}</td>
              <td class="py-3">
                <Badge :tone="healthOf(h).tone" dot :title="healthOf(h).title">{{ healthOf(h).text }}</Badge>
              </td>
              <td class="py-3 tabular-nums">
                <span :class="(refs.get(h.name)?.length ?? 0) ? 'text-foreground' : 'text-muted-foreground'">{{ refs.get(h.name)?.length ?? 0 }}</span>
              </td>
              <td class="py-3">
                <Switch
                  :model-value="h.enabled"
                  :disabled="!d.canWrite.value"
                  :label="`启用 ${h.name}`"
                  @update:model-value="(v: boolean) => (d.draft.value = d.draft.value.map((x, j) => (j === i ? { ...x, enabled: v } : x)))"
                />
              </td>
              <td class="py-3 text-right">
                <span v-if="d.canWrite.value" class="inline-flex gap-1 opacity-60 transition group-hover:opacity-100">
                  <Button variant="ghost" size="icon" :aria-label="`编辑 ${h.name}`" @click="openEdit(i)"><Pencil class="h-3.5 w-3.5" /></Button>
                  <Button variant="ghost" size="icon" :aria-label="`删除 ${h.name}`" @click="remove(i)"><Trash2 class="h-3.5 w-3.5 text-danger" /></Button>
                </span>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="!d.draft.value.length" class="py-8 text-center text-sm text-muted-foreground">
          蜜罐池为空：先从模板登记一个蜜罐，诱饵才有落点。
        </p>
      </SpotlightCard>
      <SaveBar
        :dirty="d.dirty.value" :errors="d.errors.value.length" :warnings="d.warnings.value.length" :saving="d.saving.value"
        :can-write="d.canWrite.value" :conflict="d.conflict.value" :save-error="d.saveError.value" :saved-at="d.savedAt.value"
        @save="d.save" @reset="d.reset" @keep-mine="d.keepMineAndRefresh"
      />
    </div>
    <ValidationPanel
      :errors="d.errors.value" :warnings="d.warnings.value" :validating="d.validating.value" :dirty="d.dirty.value"
      :domains="['honeypots', 'decoys']" @locate="locate"
    />
  </div>

  <HoneypotEditor
    v-model:open="editorOpen" :initial="editing?.value ?? null" :is-new="(editing?.index ?? -1) < 0"
    :templates="store.templates?.templates.honeypots ?? []" :taken-names="takenNames"
    :types="store.templates?.enums.honeypot_types ?? []" @submit="onSubmit"
  />
  <TemplatePicker
    v-model:open="pickerOpen" title="从模板新建蜜罐" description="九类登记模板：预填类型、默认端口与建议逻辑名；地址按你的实际部署填写。"
    :items="pickItems" @pick="openNew"
  />
  <Sheet :open="!!blocked" side="center" title="无法删除：蜜罐仍被引用" @update:open="(v: boolean) => !v && (blocked = null)">
    <p class="text-sm text-muted-foreground">
      <span class="font-mono text-foreground">{{ blocked?.name }}</span> 被以下启用中的诱饵引用。删除后这些诱饵将没有落点 ——
      请先在「诱饵资产」里停用它们或改指向其他蜜罐。
    </p>
    <ul class="mt-3 flex flex-wrap gap-2">
      <li v-for="id in blocked?.ids" :key="id"><Badge tone="decoy">{{ id }}</Badge></li>
    </ul>
    <template #footer><Button @click="blocked = null">知道了</Button></template>
  </Sheet>
</template>
