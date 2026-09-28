<script setup lang="ts">
import { Eye, EyeOff, KeyRound, Plus, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { api, ApiError } from '@/lib/api'
import type { LLMProvider, LLMProviderInput } from '@/lib/types'
import { providerPresets } from './presets'

// 登记 / 编辑大模型提供方。密钥只在提交时离开浏览器，服务端加密落盘，之后只显示脱敏提示。
const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ provider?: LLMProvider | null }>()
const emit = defineEmits<{ saved: [p: LLMProvider] }>()

const blank = (): LLMProviderInput => ({ name: '', base_url: '', api_key: '', models: [], default_model: '', enabled: true, note: '' })
const form = ref<LLMProviderInput>(blank())
const modelDraft = ref('')
const reveal = ref(false)
const busy = ref(false)
const error = ref('')
const editing = computed(() => !!props.provider)
const plainHttp = computed(() => form.value.base_url.startsWith('http://'))

watch(open, (v) => {
  if (!v) return
  error.value = ''
  modelDraft.value = ''
  reveal.value = false
  const p = props.provider
  form.value = p
    ? { name: p.name, base_url: p.base_url, api_key: '', models: [...p.models], default_model: p.default_model, enabled: p.enabled, note: p.note ?? '' }
    : blank()
})

function applyPreset(id: string) {
  const preset = providerPresets.find((x) => x.id === id)
  if (!preset) return
  form.value = { ...form.value, name: form.value.name || preset.name, base_url: preset.base_url, models: [...preset.models], default_model: preset.models[0] }
}

function addModel() {
  for (const m of modelDraft.value.split(/[\s,，]+/)) {
    const v = m.trim()
    if (v && !form.value.models.includes(v)) form.value.models.push(v)
  }
  if (!form.value.default_model && form.value.models.length) form.value.default_model = form.value.models[0]
  modelDraft.value = ''
}

function removeModel(i: number) {
  const [gone] = form.value.models.splice(i, 1)
  if (form.value.default_model === gone) form.value.default_model = form.value.models[0] ?? ''
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
        <span class="text-muted-foreground">快速填充</span>
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
        <span class="text-muted-foreground">模型 *（点选设为默认）</span>
        <div class="flex flex-wrap gap-1.5">
          <span
            v-for="(m, i) in form.models"
            :key="m"
            class="inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 font-mono text-xs"
            :class="form.default_model === m ? 'border-primary bg-primary/15 text-primary' : 'border-border text-foreground'"
          >
            <button type="button" class="cursor-pointer" :title="form.default_model === m ? '默认模型' : '设为默认'" @click="form.default_model = m">{{ m }}</button>
            <button type="button" class="cursor-pointer hover:text-danger" :aria-label="`移除 ${m}`" @click="removeModel(i)"><X class="h-3 w-3" /></button>
          </span>
        </div>
        <div class="flex gap-2">
          <Input v-model="modelDraft" placeholder="模型名，回车添加（可逗号分隔多个）" @keydown.enter.prevent="addModel" />
          <Button variant="outline" size="icon" aria-label="添加模型" @click="addModel"><Plus /></Button>
        </div>
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
