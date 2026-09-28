<script setup lang="ts">
import { Eye, EyeOff, KeyRound, ListPlus, Plus, Star, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { api, ApiError } from '@/lib/api'
import type { LLMModelProbeInput, LLMModelProbeResult, LLMProvider, LLMProviderInput } from '@/lib/types'
import { providerPresets } from './presets'

// 登记 / 编辑大模型提供方。密钥只在提交时离开浏览器，服务端加密落盘，之后只显示脱敏提示。
const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ provider?: LLMProvider | null }>()
const emit = defineEmits<{ saved: [p: LLMProvider] }>()

// 与后端 llm 包的 maxModels 一致（超限由后端最终把关，这里提前提示）
const maxModels = 20

const blank = (): LLMProviderInput => ({ name: '', base_url: '', api_key: '', models: [], default_model: '', enabled: true, note: '' })
const form = ref<LLMProviderInput>(blank())
const modelDraft = ref('')
const reveal = ref(false)
const busy = ref(false)
const error = ref('')
const editing = computed(() => !!props.provider)
const plainHttp = computed(() => form.value.base_url.startsWith('http://'))

// 「获取模型列表」：按已填的地址与密钥调上游 /models，结果作为输入行的下拉候选。
const probing = ref(false)
const probeError = ref('')
const fetchedModels = ref<string[]>([])
const probeable = computed(() => form.value.base_url.trim() !== '' && (form.value.api_key.trim() !== '' || editing.value))

// 输入行的内嵌下拉（tag-input 卡内部）：候选 = 探测到且未接入、且匹配当前输入的模型。
const panelOpen = ref(false)
const candidates = computed(() => {
  const added = new Set(form.value.models)
  const draft = modelDraft.value.trim().toLowerCase()
  return fetchedModels.value
    .filter((m) => !added.has(m))
    .filter((m) => draft === '' || m.toLowerCase().includes(draft))
})

watch(open, (v) => {
  if (!v) return
  error.value = ''
  modelDraft.value = ''
  reveal.value = false
  probeError.value = ''
  fetchedModels.value = []
  panelOpen.value = false
  const p = props.provider
  form.value = p
    ? { name: p.name, base_url: p.base_url, api_key: '', models: [...p.models], default_model: p.default_model, enabled: p.enabled, note: p.note ?? '' }
    : blank()
})

// 快速填充只替地址与名称（不方便手打的两个长串）；**不碰模型选择**——
// 模型是用户的显式决策（手输 / 下拉 / 探测后点选），预设只应是起点而不是覆盖。
function applyPreset(id: string) {
  const preset = providerPresets.find((x) => x.id === id)
  if (!preset) return
  form.value = { ...form.value, name: form.value.name || preset.name, base_url: preset.base_url }
}

function addModel() {
  for (const m of modelDraft.value.split(/[\s,，]+/)) {
    const v = m.trim()
    if (v && !form.value.models.includes(v)) {
      if (form.value.models.length >= maxModels) {
        probeError.value = `最多接入 ${maxModels} 个模型（当前上限）`
        break
      }
      form.value.models.push(v)
    }
  }
  if (!form.value.default_model && form.value.models.length) form.value.default_model = form.value.models[0]
  modelDraft.value = ''
}

// 从下拉候选接入一个：面板保持打开，可连续选多个。
function pickCandidate(m: string) {
  if (!m || form.value.models.includes(m)) return
  if (form.value.models.length >= maxModels) {
    probeError.value = `最多接入 ${maxModels} 个模型（当前上限）`
    return
  }
  form.value.models.push(m)
  if (!form.value.default_model) form.value.default_model = m
  modelDraft.value = ''
}

function removeModel(i: number) {
  const [gone] = form.value.models.splice(i, 1)
  if (form.value.default_model === gone) form.value.default_model = form.value.models[0] ?? ''
}

async function fetchModels() {
  if (!probeable.value) return
  probing.value = true
  probeError.value = ''
  const body: LLMModelProbeInput = { base_url: form.value.base_url.trim(), api_key: form.value.api_key.trim() || undefined }
  if (editing.value && !body.api_key) body.provider_id = props.provider?.id
  await api
    .post<LLMModelProbeResult>('/api/v1/llm/models/probe', body)
    .then((r) => {
      fetchedModels.value = r.models ?? []
      if (!fetchedModels.value.length) probeError.value = '上游没有返回任何模型（可手动输入模型名）'
    })
    .catch((err: unknown) => {
      console.error('获取模型列表失败', err)
      fetchedModels.value = []
      probeError.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (probing.value = false))
}

async function save() {
  addModel()
  busy.value = true
  error.value = ''
  const req = props.provider
    ? api.put<LLMProvider>(`/api/v1/llm/providers/${props.provider.id}`, { ...form.value, version: props.provider.version })
    : api.post<LLMProvider>('/api/v1/llm/providers', form.value)
  await req
    .then((p) => {
      form.value.api_key = '' // 提交后立刻从内存里清掉明文
      emit('saved', p)
      open.value = false
    })
    .catch((err: unknown) => {
      console.error('保存大模型提供方失败', err)
      error.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="editing ? `编辑：${provider?.name}` : '登记大模型'"
    description="支持任意 OpenAI 兼容接口。API Key 在服务端以 AES-256-GCM 加密保存，页面只显示脱敏提示。"
  >
    <form id="llm-provider-form" class="flex flex-col gap-4" @submit.prevent="save">
      <div v-if="!editing" class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">快速填充（只带出名称与接口地址，模型按需自己选）</span>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="p in providerPresets"
            :key="p.id"
            type="button"
            class="cursor-pointer rounded-full border border-border px-3 py-1 text-xs transition hover:border-primary/60 hover:text-primary"
            :title="p.note"
            @click="applyPreset(p.id)"
          >
            {{ p.name }}
          </button>
        </div>
      </div>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">名称 *</span>
        <Input v-model="form.name" placeholder="例如：DeepSeek 生产账号" maxlength="40" />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">接口地址 *</span>
        <Input v-model="form.base_url" placeholder="https://api.deepseek.com" />
        <span class="text-xs" :class="plainHttp ? 'text-warn' : 'text-muted-foreground'">
          {{ plainHttp ? '明文 http 只允许本机模型（127.0.0.1 / localhost / host.docker.internal）' : '会请求 {地址}/chat/completions；公网服务必须 https' }}
        </span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="flex items-center gap-1.5 text-muted-foreground"><KeyRound class="h-3.5 w-3.5" />API Key {{ editing ? '' : '*' }}</span>
        <div class="relative">
          <Input v-model="form.api_key" :type="reveal ? 'text' : 'password'" autocomplete="off" spellcheck="false" class="pr-10 font-mono" :placeholder="editing ? `当前 ${provider?.key_hint}，留空表示不修改` : 'sk-...'" />
          <button type="button" class="absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer p-1 text-muted-foreground hover:text-foreground" :aria-label="reveal ? '隐藏密钥' : '显示密钥'" @click="reveal = !reveal">
            <EyeOff v-if="reveal" class="h-4 w-4" /><Eye v-else class="h-4 w-4" />
          </button>
        </div>
      </label>
      <div class="flex flex-col gap-1.5 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-muted-foreground">模型 *</span>
          <Button type="button" variant="ghost" size="sm" class="h-7 px-2 text-xs" :loading="probing" :disabled="!probeable" @click="fetchModels">
            <ListPlus class="h-3.5 w-3.5" />获取模型列表
          </Button>
        </div>
        <div
          class="rounded-lg border border-input bg-background/40 shadow-inner transition-all duration-200 focus-within:border-primary/70 focus-within:ring-2 focus-within:ring-primary/25"
        >
          <div v-if="form.models.length" class="flex flex-wrap gap-1.5 p-2">
            <span
              v-for="(m, i) in form.models"
              :key="m"
              class="inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 font-mono text-xs transition"
              :class="form.default_model === m ? 'border-primary/60 bg-primary/15 text-primary' : 'border-border text-foreground hover:border-primary/40'"
            >
              <button
                type="button"
                class="inline-flex cursor-pointer items-center gap-1"
                :title="form.default_model === m ? '默认模型' : '点这里设为默认'"
                @click="form.default_model = m"
              >
                <Star v-if="form.default_model === m" class="h-3 w-3 fill-current" />
                {{ m }}
              </button>
              <button type="button" class="cursor-pointer opacity-50 transition hover:opacity-100 hover:text-danger" :aria-label="`移除 ${m}`" @click="removeModel(i)">
                <X class="h-3 w-3" />
              </button>
            </span>
          </div>
          <div class="relative flex items-center border-t border-border/60">
            <input
              v-model="modelDraft"
              class="h-10 w-full bg-transparent px-3 py-2 font-mono text-sm text-foreground outline-none placeholder:font-sans placeholder:text-muted-foreground/70"
              :placeholder="form.models.length ? '输入或从下拉选择模型…' : '输入模型名（可逗号分隔），或点右上「获取模型列表」'"
              @focus="panelOpen = true"
              @blur="panelOpen = false"
              @input="panelOpen = true"
              @keydown.enter.prevent="addModel"
              @keydown.escape="panelOpen = false"
            />
            <Button type="button" variant="ghost" size="icon" class="mr-1 h-8 w-8 shrink-0" aria-label="添加模型" @mousedown.prevent @click="addModel">
              <Plus class="h-4 w-4" />
            </Button>
            <div
              v-if="panelOpen && candidates.length"
              class="glass absolute inset-x-0 top-full z-10 max-h-56 overflow-y-auto rounded-b-lg border border-t-0 border-input p-1 shadow-xl"
            >
              <button
                v-for="m in candidates"
                :key="m"
                type="button"
                class="flex w-full cursor-pointer items-center justify-between rounded-md px-2.5 py-1.5 text-left font-mono text-xs text-foreground transition hover:bg-primary/10"
                @mousedown.prevent="pickCandidate(m)"
              >
                {{ m }}
                <Plus class="h-3 w-3 text-muted-foreground" />
              </button>
            </div>
          </div>
        </div>
        <p class="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span>已接入 {{ form.models.length }} / {{ maxModels }} · 点标签上的星设默认</span>
          <span v-if="fetchedModels.length">· 探测到 {{ fetchedModels.length }} 个可用</span>
        </p>
        <p v-if="probeError" role="alert" class="text-xs text-danger">{{ probeError }}</p>
      </div>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">备注</span>
        <Input v-model="form.note" placeholder="例如：计费账号归属、额度说明" maxlength="200" />
      </label>
      <label class="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5 text-sm">
        <span class="flex flex-col">
          <span>启用</span>
          <span class="text-xs text-muted-foreground">停用后不能用它发起新的分析</span>
        </span>
        <Switch v-model="form.enabled" label="启用" />
      </label>
      <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
    </form>
    <template #footer>
      <Button variant="ghost" @click="open = false">取消</Button>
      <Button type="submit" form="llm-provider-form" :loading="busy">保存</Button>
    </template>
  </Sheet>
</template>
