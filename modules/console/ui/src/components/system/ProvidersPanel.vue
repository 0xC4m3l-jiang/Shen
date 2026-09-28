<script setup lang="ts">
import { CheckCircle2, KeyRound, Pencil, Plus, PlugZap, Trash2, XCircle } from 'lucide-vue-next'
import { reactive, ref } from 'vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import StatePanel from '@/components/StatePanel.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import type { LLMProvider, LLMTestResult } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'
import ProviderForm from './ProviderForm.vue'

// 大模型接入（基础能力模块）：登记 / 编辑 / 删除 / 测试连通（管理员）；其他角色只读。
// 供分析模块与后续 AI 模块统一调用，不与任何消费方耦合。
const auth = useAuthStore()
const admin = auth.can('llm:admin')
const { data, error, loading, refresh } = useLiveResource(
  (signal) => api.get<{ providers: LLMProvider[] }>('/api/v1/llm/providers', undefined, signal),
  { live: false },
)
const formOpen = ref(false)
const editing = ref<LLMProvider | null>(null)
const testModel = reactive<Record<string, string>>({})
const testing = reactive<Record<string, boolean>>({})
const actionError = ref('')

function openForm(p: LLMProvider | null) {
  editing.value = p
  formOpen.value = true
}

async function test(p: LLMProvider) {
  testing[p.id] = true
  actionError.value = ''
  await api
    .post<{ result: LLMTestResult; provider: LLMProvider }>(`/api/v1/llm/providers/${p.id}/test`, { model: testModel[p.id] || p.default_model })
    .then(() => refresh())
    .catch((err: unknown) => {
      console.error('测试连通失败', err)
      actionError.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (testing[p.id] = false))
}

async function remove(p: LLMProvider) {
  if (!window.confirm(`删除「${p.name}」？使用它的历史会话将无法继续追问（记录保留）。`)) return
  await api
    .del(`/api/v1/llm/providers/${p.id}`)
    .then(() => refresh())
    .catch((err: unknown) => {
      console.error('删除提供方失败', err)
      actionError.value = err instanceof ApiError ? err.message : '网络错误'
    })
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between">
      <p class="text-sm text-muted-foreground">
        {{ admin ? '登记 OpenAI 兼容的大模型接口；密钥加密保存，只显示末 4 位。' : '已接入的模型（登记与修改需要管理员）。' }}
      </p>
      <Button v-if="admin" @click="openForm(null)"><Plus class="h-4 w-4" />登记大模型</Button>
    </div>
    <StatePanel :error="error" :loading="loading" @retry="refresh" />
    <p v-if="actionError" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ actionError }}</p>

    <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
      <SpotlightCard v-for="p in data?.providers ?? []" :key="p.id" :interactive="false" class="flex flex-col gap-4 p-5">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <h3 class="truncate text-base font-medium">{{ p.name }}</h3>
              <Badge :tone="p.enabled ? 'mirage' : 'muted'" dot>{{ p.enabled ? '启用' : '停用' }}</Badge>
            </div>
            <p class="mt-1 truncate font-mono text-xs text-muted-foreground">{{ p.base_url }}</p>
          </div>
          <span class="flex shrink-0 items-center gap-1 rounded-md bg-muted px-2 py-1 font-mono text-xs text-muted-foreground" title="密钥已加密保存">
            <KeyRound class="h-3 w-3" />{{ p.key_hint }}
          </span>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <span
            v-for="m in p.models"
            :key="m"
            class="rounded-full border px-2.5 py-0.5 font-mono text-xs"
            :class="m === p.default_model ? 'border-primary/50 bg-primary/10 text-primary' : 'border-border text-muted-foreground'"
          >
            {{ m }}{{ m === p.default_model ? ' · 默认' : '' }}
          </span>
        </div>
        <div class="rounded-lg border border-border/60 px-3 py-2.5 text-xs">
          <template v-if="p.last_test">
            <div class="flex items-center gap-2">
              <CheckCircle2 v-if="p.last_test.ok" class="h-4 w-4 text-mirage" />
              <XCircle v-else class="h-4 w-4 text-danger" />
              <span :class="p.last_test.ok ? 'text-mirage' : 'text-danger'">{{ p.last_test.ok ? '连通正常' : '连通失败' }}</span>
              <span class="text-muted-foreground">· {{ p.last_test.model }} · {{ p.last_test.latency_ms }} ms · {{ fmtDateTime(p.last_test.at) }}</span>
            </div>
            <p v-if="p.last_test.error" class="mt-1.5 break-all text-danger/90">{{ p.last_test.error }}</p>
            <p v-else-if="p.last_test.reply" class="mt-1.5 text-muted-foreground">模型回复：{{ p.last_test.reply }}</p>
          </template>
          <span v-else class="text-muted-foreground">尚未测试连通性</span>
        </div>
        <div v-if="admin" class="flex flex-wrap items-center gap-2">
          <NativeSelect
            :model-value="testModel[p.id] || p.default_model"
            :options="p.models.map((m) => ({ value: m, label: m }))"
            label="测试模型"
            class="w-48"
            @update:model-value="testModel[p.id] = $event ?? ''"
          />
          <Button variant="outline" size="sm" :loading="testing[p.id]" @click="test(p)"><PlugZap class="h-4 w-4" />测试连通</Button>
          <Button variant="ghost" size="sm" class="ml-auto" @click="openForm(p)"><Pencil class="h-4 w-4" />编辑</Button>
          <Button variant="ghost" size="sm" class="hover:text-danger" @click="remove(p)"><Trash2 class="h-4 w-4" />删除</Button>
        </div>
      </SpotlightCard>
    </div>
    <div v-if="!loading && !data?.providers?.length" class="glass flex flex-col items-center gap-3 rounded-2xl px-6 py-14 text-center">
      <PlugZap class="h-8 w-8 text-muted-foreground" />
      <p class="text-sm text-muted-foreground">{{ admin ? '还没有接入任何大模型。登记并测试连通后，分析等模块即可选用。' : '管理员尚未接入可用的大模型。' }}</p>
      <Button v-if="admin" @click="openForm(null)"><Plus class="h-4 w-4" />登记第一个大模型</Button>
    </div>
    <ProviderForm v-model:open="formOpen" :provider="editing" @saved="refresh()" />
  </div>
</template>
