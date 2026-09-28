<script setup lang="ts">
import { MessagesSquare, Plus, Trash2 } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { Button } from '@/components/ui/button'
import { api, ApiError } from '@/lib/api'
import { fmtAgo, fmtNum } from '@/lib/format'
import type { Conversation, ConversationSummary, LLMProvider } from '@/lib/types'
import ChatPanel from './ChatPanel.vue'
import TrafficPicker from './TrafficPicker.vue'

// 对话分析工作区：左侧是我的会话，右侧是「新建分析」或当前会话。
// 从请求详情跳转来时（?decision=<id>）直接打开新建并预选那条流量。
const route = useRoute()
const router = useRouter()
const providers = ref<LLMProvider[]>([])
const list = ref<ConversationSummary[]>([])
const current = ref<Conversation | null>(null)
const creating = ref(true)
const preselect = ref<string[]>([])
const error = ref('')

const fail = (label: string) => (err: unknown) => {
  console.error(label, err)
  error.value = err instanceof ApiError ? err.message : '网络错误'
}

async function loadList() {
  await api.get<{ conversations: ConversationSummary[] }>('/api/v1/llm/conversations').then((r) => (list.value = r.conversations)).catch(fail('读取会话失败'))
}

async function open(id: string) {
  error.value = ''
  await api
    .get<Conversation>(`/api/v1/llm/conversations/${id}`)
    .then((c) => {
      current.value = c
      creating.value = false
    })
    .catch(fail('读取会话失败'))
}

function newAnalysis(ids: string[] = []) {
  preselect.value = ids
  current.value = null
  creating.value = true
}

function onStarted(c: Conversation) {
  current.value = c
  creating.value = false
  void loadList()
  if (route.query.decision) void router.replace({ query: {} })
}

function onUpdated(c: Conversation) {
  current.value = c
  void loadList()
}

async function remove(s: ConversationSummary) {
  if (!window.confirm(`删除会话「${s.title}」？`)) return
  await api
    .del(`/api/v1/llm/conversations/${s.id}`)
    .then(() => {
      if (current.value?.id === s.id) newAnalysis()
      void loadList()
    })
    .catch(fail('删除会话失败'))
}

onMounted(async () => {
  const decision = typeof route.query.decision === 'string' ? route.query.decision : ''
  newAnalysis(decision ? [decision] : [])
  await Promise.all([
    api.get<{ providers: LLMProvider[] }>('/api/v1/llm/providers').then((r) => (providers.value = r.providers)).catch(fail('读取模型失败')),
    loadList(),
  ])
})
</script>

<template>
  <div class="grid grid-cols-1 gap-4 lg:grid-cols-[260px_1fr]">
    <SpotlightCard :interactive="false" class="flex flex-col gap-2 self-start p-3">
      <Button class="w-full" @click="newAnalysis()"><Plus class="h-4 w-4" />新建分析</Button>
      <p v-if="!list.length" class="px-2 py-6 text-center text-xs text-muted-foreground">还没有会话</p>
      <ul class="flex max-h-[calc(100vh-18rem)] flex-col gap-1 overflow-y-auto">
        <li v-for="s in list" :key="s.id" class="group relative">
          <button
            type="button"
            class="flex w-full cursor-pointer flex-col gap-0.5 rounded-lg px-3 py-2 text-left transition hover:bg-primary/5"
            :class="current?.id === s.id && !creating && 'bg-primary/10'"
            @click="open(s.id)"
          >
            <span class="flex items-center gap-1.5 truncate pr-6 text-sm"><MessagesSquare class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />{{ s.title }}</span>
            <span class="truncate text-[11px] text-muted-foreground">{{ s.model }} · {{ s.traffic }} 条 · {{ fmtNum(s.total_tokens) }} token · {{ fmtAgo(s.updated_at) }}</span>
          </button>
          <button type="button" class="absolute right-2 top-2.5 hidden cursor-pointer p-1 text-muted-foreground hover:text-danger group-hover:block" :aria-label="`删除 ${s.title}`" @click="remove(s)">
            <Trash2 class="h-3.5 w-3.5" />
          </button>
        </li>
      </ul>
    </SpotlightCard>

    <div class="flex min-w-0 flex-col gap-3">
      <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
      <TrafficPicker v-if="creating" :key="preselect.join(',')" :providers="providers" :preselect="preselect" @started="onStarted" />
      <ChatPanel v-else-if="current" :conversation="current" @updated="onUpdated" />
    </div>
  </div>
</template>
