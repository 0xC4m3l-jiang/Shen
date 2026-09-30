<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowDown, ArrowUp, BrainCircuit, GripVertical, LayoutTemplate, Plus, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import SaveBar from './SaveBar.vue'
import TemplatePicker, { type PickItem } from './TemplatePicker.vue'
import ValidationPanel from './ValidationPanel.vue'
import { useDomainDraft } from '@/composables/useDomainDraft'
import { injectKindLabel, type Inject } from '@/lib/config'
import { useConfigStore } from '@/stores/deceptionConfig'

const store = useConfigStore()
type Draft = { injects: Inject[]; injects_provided: boolean }
const d = useDomainDraft<Draft>(
  'injects',
  (ds) => ({ injects: ds.injects, injects_provided: ds.injects_provided }),
  (v) => ({ injects: v.injects_provided ? v.injects : [], injects_provided: v.injects_provided }),
)

const pickerOpen = ref(false)
const dragFrom = ref(-1)
const kindOptions = computed(() => [{ value: '', label: '未分类' }, ...(store.templates?.enums.inject_kinds ?? []).map((k) => ({ value: k, label: injectKindLabel[k] ?? k }))])
const pickItems = computed<PickItem[]>(() =>
  (store.templates?.templates.injects ?? []).map((t) => ({ id: t.id, title: t.name, tag: t.kind, description: t.description, hint: t.marker })),
)

function setList(list: Inject[]): void {
  d.draft.value = { ...d.draft.value, injects: list }
}
function patch(i: number, p: Partial<Inject>): void {
  setList(d.draft.value.injects.map((r, j) => (j === i ? { ...r, ...p } : r)))
}
function move(i: number, to: number): void {
  if (to < 0 || to >= d.draft.value.injects.length || i === to) return
  const list = [...d.draft.value.injects]
  const [x] = list.splice(i, 1)
  list.splice(to, 0, x)
  setList(list)
}
function fromTemplate(id: string): void {
  const t = store.templates?.templates.injects.find((x) => x.id === id)
  if (!t) return
  d.draft.value = { injects_provided: true, injects: [...d.draft.value.injects, { kind: t.kind, snippet: t.snippet, marker: t.marker }] }
}
function addBlank(): void {
  d.draft.value = { injects_provided: true, injects: [...d.draft.value.injects, { kind: '', snippet: '', marker: '</body>' }] }
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
    <div class="flex flex-col gap-4">
      <SpotlightCard :interactive="false" class="p-5" body-class="flex flex-col gap-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-base font-medium">注入规则 · 改道侧响应改写</h2>
            <p class="text-xs text-muted-foreground">只作用于已改道到幻境的响应（业务侧响应永不改写）。数组顺序即执行顺序，可拖拽或用箭头调整。</p>
          </div>
          <div v-if="d.canWrite.value" class="flex gap-2">
            <Button variant="outline" size="sm" @click="pickerOpen = true"><LayoutTemplate class="h-3.5 w-3.5" />从模板添加</Button>
            <Button size="sm" @click="addBlank"><Plus class="h-3.5 w-3.5" />空白规则</Button>
          </div>
        </div>
        <label class="flex items-center justify-between rounded-xl border border-border/60 px-4 py-3 text-sm">
          <span class="flex flex-col">
            <span>由管控台下发注入规则</span>
            <span class="text-[11px] text-muted-foreground">
              关闭 = 「未配置」：适配器沿用它本地的规则。开启且列表为空 = 「显式清空」：关掉全部注入。
            </span>
          </span>
          <Switch
            :model-value="d.draft.value.injects_provided" :disabled="!d.canWrite.value" label="由管控台下发注入规则"
            @update:model-value="(v: boolean) => (d.draft.value = { ...d.draft.value, injects_provided: v })"
          />
        </label>
        <TransitionGroup v-if="d.draft.value.injects_provided" tag="ol" name="inj" class="flex flex-col gap-3">
          <li
            v-for="(r, i) in d.draft.value.injects"
            :key="i + r.snippet.slice(0, 12)"
            :draggable="d.canWrite.value"
            class="flex gap-3 rounded-xl border p-3 transition"
            :class="[d.issuesAt(`injects[${i}]`).length ? 'border-danger/40 bg-danger/5' : 'border-border/60 hover:border-primary/40', dragFrom === i ? 'opacity-50' : '']"
            @dragstart="dragFrom = i"
            @dragend="dragFrom = -1"
            @dragover.prevent
            @drop="move(dragFrom, i)"
          >
            <div class="flex flex-col items-center gap-1 pt-1 text-muted-foreground">
              <GripVertical class="h-4 w-4 cursor-grab" />
              <span class="text-xs tabular-nums">{{ i + 1 }}</span>
            </div>
            <div class="flex flex-1 flex-col gap-2">
              <div class="flex flex-wrap items-center gap-2">
                <NativeSelect :model-value="r.kind" :options="kindOptions" label="分类" class="w-40" @update:model-value="(v?: string) => patch(i, { kind: v ?? '' })" />
                <Input :model-value="r.marker" class="w-40 font-mono text-xs" placeholder="</body>" @update:model-value="(v) => patch(i, { marker: String(v ?? '') })" />
                <Badge tone="muted">插入到标记之前</Badge>
              </div>
              <textarea
                :value="r.snippet"
                rows="3"
                spellcheck="false"
                :disabled="!d.canWrite.value"
                class="w-full resize-y rounded-lg border border-input bg-background/40 px-3 py-2 font-mono text-xs outline-none transition focus:border-primary/60 focus:ring-2 focus:ring-primary/20"
                placeholder="<!-- 注入片段：对人不可见、对自动化 Agent 可见 -->"
                @input="(e) => patch(i, { snippet: (e.target as HTMLTextAreaElement).value })"
              />
              <p v-for="iss in d.issuesAt(`injects[${i}]`)" :key="iss.field" class="text-[11px] text-danger">{{ iss.reason }}</p>
            </div>
            <div v-if="d.canWrite.value" class="flex flex-col gap-1">
              <Button variant="ghost" size="icon" aria-label="上移" :disabled="i === 0" @click="move(i, i - 1)"><ArrowUp class="h-3.5 w-3.5" /></Button>
              <Button variant="ghost" size="icon" aria-label="下移" :disabled="i === d.draft.value.injects.length - 1" @click="move(i, i + 1)"><ArrowDown class="h-3.5 w-3.5" /></Button>
              <Button variant="ghost" size="icon" aria-label="删除" @click="setList(d.draft.value.injects.filter((_, j) => j !== i))"><Trash2 class="h-3.5 w-3.5 text-danger" /></Button>
            </div>
          </li>
        </TransitionGroup>
        <p v-if="d.draft.value.injects_provided && !d.draft.value.injects.length" class="rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">
          显式清空：保存后适配器将不再注入任何静态片段。
        </p>
        <SaveBar
          :dirty="d.dirty.value" :errors="d.errors.value.length" :warnings="d.warnings.value.length" :saving="d.saving.value"
          :can-write="d.canWrite.value" :conflict="d.conflict.value" :save-error="d.saveError.value" :saved-at="d.savedAt.value"
          @save="d.save" @reset="d.reset" @keep-mine="d.keepMineAndRefresh"
        />
      </SpotlightCard>
      <SpotlightCard :interactive="false" class="border border-dashed border-border/60 p-5" body-class="flex flex-col gap-2">
        <h3 class="flex items-center gap-2 text-sm font-medium"><BrainCircuit class="h-4 w-4 text-primary" />AI 动态内容（只读）</h3>
        <p class="text-xs leading-relaxed text-muted-foreground">
          LLM 生成的欺骗内容走「生成 → 审核 → 发布」的独立链路（M4，尚未接入本数据集），当前仍由部署配置 <code class="font-mono">ai:</code> 段与离线清单驱动 ——
          为避免「看起来生效、其实没生效」，这里不提供编辑。当前状态见「欺骗层 → 策略快照」，模型提供方在「系统 → 大模型接入」。
        </p>
      </SpotlightCard>
    </div>
    <ValidationPanel
      :errors="d.errors.value" :warnings="d.warnings.value" :validating="d.validating.value" :dirty="d.dirty.value" :domains="['injects']"
      @locate="() => undefined"
    />
  </div>
  <TemplatePicker v-model:open="pickerOpen" title="从模板添加注入片段" description="四类内置片段：对人不可见，只把自动化 Agent 引向诱饵。" :items="pickItems" @pick="fromTemplate" />
</template>

<style scoped>
.inj-move,
.inj-enter-active {
  transition: all 0.25s ease;
}
.inj-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
