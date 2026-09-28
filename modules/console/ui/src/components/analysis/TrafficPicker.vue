<script setup lang="ts">
import { Search, Sparkles } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import GeoTag from '@/components/GeoTag.vue'
import LayerBadge from '@/components/LayerBadge.vue'
import StatePanel from '@/components/StatePanel.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { fmtTime, layerMeta } from '@/lib/format'
import type { Conversation, Layer, LLMProvider, TrafficRow } from '@/lib/types'

// 发起一次分析：勾选流量 → 选模型 → 写第一个问题。流量快照在创建时冻结进会话。
const props = defineProps<{ providers: LLMProvider[]; preselect?: string[] }>()
const emit = defineEmits<{ started: [c: Conversation] }>()
const MAX = 50

const span = ref('1h')
const layer = ref('')
const keyword = ref('')
const { data, error, loading, refresh } = useLiveResource(
  (signal) => api.get<{ rows: TrafficRow[] }>('/api/v1/traffic', { window: span.value, limit: 500, layer: layer.value || undefined }, signal),
  { deps: [span, layer], live: false },
)
const selected = ref<Set<string>>(new Set(props.preselect ?? []))
const enabled = computed(() => props.providers.filter((p) => p.enabled))
const providerId = ref('')
const model = ref('')
const redact = ref(true)
const question = ref('')
const busy = ref(false)
const startError = ref('')

watch(enabled, (list) => {
  if (!list.find((p) => p.id === providerId.value)) providerId.value = list[0]?.id ?? ''
}, { immediate: true })
watch(providerId, (id) => (model.value = enabled.value.find((p) => p.id === id)?.default_model ?? ''), { immediate: true })

const rows = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const all = data.value?.rows ?? []
  if (!k) return all
  return all.filter((r) => [r.path, r.user_agent, r.source_ip, r.host, r.geo?.label].some((v) => v?.toLowerCase().includes(k)))
})
const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.slice(0, MAX).every((r) => selected.value.has(r.decision_id)))
const models = computed(() => (enabled.value.find((p) => p.id === providerId.value)?.models ?? []).map((m) => ({ value: m, label: m })))
const suggestions = ['这批请求是什么性质？是人工、扫描器还是 AI Agent？', '还原攻击链：对方处于哪个阶段、下一步可能做什么？', '欺骗是否生效？有没有漏判或误伤？', '给出防守侧的策略调整建议']

function toggle(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else if (next.size < MAX) next.add(id)
  selected.value = next
}
function toggleAll() {
  const next = new Set(selected.value)
  if (allVisibleSelected.value) rows.value.forEach((r) => next.delete(r.decision_id))
  else for (const r of rows.value) {
    if (next.size >= MAX) break
    next.add(r.decision_id)
  }
  selected.value = next
}

async function start() {
  busy.value = true
  startError.value = ''
  await api
    .post<Conversation>('/api/v1/llm/conversations', {
      provider_id: providerId.value, model: model.value, decision_ids: [...selected.value], redact_ip: redact.value, question: question.value,
    })
    .then((c) => emit('started', c))
    .catch((err: unknown) => {
      console.error('发起分析失败', err)
      startError.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}
const layerOptions = [{ value: '', label: '全部落点' }, ...(Object.keys(layerMeta) as Layer[]).map((l) => ({ value: l, label: layerMeta[l].label }))]
const spanOptions = [{ value: '15m', label: '15 分钟' }, { value: '1h', label: '1 小时' }, { value: '6h', label: '6 小时' }, { value: '24h', label: '24 小时' }]
</script>

<template>
  <div class="grid grid-cols-1 gap-4 2xl:grid-cols-[1fr_380px]">
    <SpotlightCard :interactive="false" class="flex min-w-0 flex-col gap-3 p-5">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="mr-auto text-base font-medium">① 选择要分析的流量 <span class="text-sm font-normal text-muted-foreground">已选 {{ selected.size }} / {{ MAX }}</span></h2>
        <NativeSelect v-model="span" :options="spanOptions" label="时间范围" class="w-28" />
        <NativeSelect v-model="layer" :options="layerOptions" label="落点" class="w-32" />
        <div class="relative w-56">
          <Search class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input v-model="keyword" placeholder="路径 / UA / IP / 域名" class="h-9 pl-9" />
        </div>
      </div>
      <StatePanel :error="error" :loading="loading" @retry="refresh" />
      <div class="max-h-[520px] overflow-auto rounded-lg border border-border/60">
        <table class="w-full min-w-[760px] text-sm">
          <thead class="sticky top-0 z-10 bg-card/95 backdrop-blur">
            <tr class="text-left text-xs text-muted-foreground">
              <th class="w-10 px-3 py-2"><input type="checkbox" class="h-4 w-4 cursor-pointer accent-[hsl(var(--primary))]" :checked="allVisibleSelected" aria-label="全选当前列表" @change="toggleAll" /></th>
              <th class="py-2 font-medium">时间</th><th class="py-2 font-medium">落点</th><th class="py-2 font-medium">请求</th>
              <th class="py-2 font-medium">来源</th><th class="py-2 font-medium">User-Agent</th><th class="py-2 pr-3 text-right font-medium">风险分</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in rows"
              :key="r.decision_id + r.at"
              class="cursor-pointer border-t border-border/50 transition-colors hover:bg-primary/5"
              :class="selected.has(r.decision_id) && 'bg-primary/10'"
              @click="toggle(r.decision_id)"
            >
              <td class="px-3 py-2" @click.stop><input type="checkbox" class="h-4 w-4 cursor-pointer accent-[hsl(var(--primary))]" :checked="selected.has(r.decision_id)" :aria-label="`选择 ${r.path}`" @change="toggle(r.decision_id)" /></td>
              <td class="py-2 font-mono text-xs text-muted-foreground">{{ fmtTime(r.at) }}</td>
              <td class="py-2"><LayerBadge :layer="r.layer" /></td>
              <td class="max-w-[240px] truncate py-2 font-mono text-xs" :title="`${r.host}${r.path}`">{{ r.method }} {{ r.path }}</td>
              <td class="py-2"><div class="flex flex-col"><span class="font-mono text-xs">{{ r.source_ip }}</span><GeoTag :geo="r.geo" compact /></div></td>
              <td class="max-w-[200px] truncate py-2 text-xs text-muted-foreground" :title="r.user_agent">{{ r.user_agent || '—' }}</td>
              <td class="py-2 pr-3 text-right font-mono text-xs">{{ r.score !== undefined ? r.score.toFixed(2) : '—' }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="!loading && !rows.length" class="py-10 text-center text-sm text-muted-foreground">该时间范围内没有流量</p>
      </div>
    </SpotlightCard>

    <SpotlightCard :interactive="false" class="flex flex-col gap-4 p-5">
      <h2 class="text-base font-medium">② 选择模型并提问</h2>
      <p v-if="!enabled.length" class="rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn">没有可用的大模型：请先在「模型管理」登记并启用。</p>
      <template v-else>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">大模型</span>
          <NativeSelect v-model="providerId" :options="enabled.map((p) => ({ value: p.id, label: p.name }))" label="大模型" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">模型</span>
          <NativeSelect v-model="model" :options="models" label="模型" />
        </label>
        <label class="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5 text-sm">
          <span class="flex flex-col"><span>脱敏来源 IP</span><span class="text-xs text-muted-foreground">发给模型前把末段改成 x</span></span>
          <Switch v-model="redact" label="脱敏来源 IP" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">第一个问题（可留空，进入对话后再问）</span>
          <textarea
            v-model="question"
            rows="4"
            maxlength="8000"
            class="rounded-md border border-input bg-background/40 px-3 py-2 text-sm outline-none transition hover:border-primary/40 focus:border-primary/70 focus:ring-2 focus:ring-primary/25"
            placeholder="例如：这批请求是什么性质？"
          />
        </label>
        <div class="flex flex-wrap gap-1.5">
          <button v-for="s in suggestions" :key="s" type="button" class="cursor-pointer rounded-full border border-border px-2.5 py-1 text-left text-xs text-muted-foreground transition hover:border-primary/50 hover:text-primary" @click="question = s">{{ s }}</button>
        </div>
        <p v-if="startError" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ startError }}</p>
        <Button :loading="busy" :disabled="!selected.size || !providerId" @click="start">
          <Sparkles class="h-4 w-4" />{{ question.trim() ? '开始分析' : '创建会话' }}（{{ selected.size }} 条）
        </Button>
        <p class="text-xs text-muted-foreground">所选流量会发送到该模型服务商；路径与 UA 由攻击者控制，已在提示词中标注为不可信数据。</p>
      </template>
    </SpotlightCard>
  </div>
</template>
