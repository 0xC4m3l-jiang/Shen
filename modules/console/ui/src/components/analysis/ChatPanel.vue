<script setup lang="ts">
import { Bot, ChevronDown, Loader2, RotateCcw, SendHorizontal, UserRound } from 'lucide-vue-next'
import { computed, nextTick, ref, watch } from 'vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { Button } from '@/components/ui/button'
import { api, ApiError } from '@/lib/api'
import { fmtNum, fmtTime } from '@/lib/format'
import type { ChatMessage, Conversation } from '@/lib/types'
import MarkdownText from './MarkdownText'

// 与大模型的多轮深度分析。模型答复是给人看的建议：不会被自动执行、不改任何策略。
const props = defineProps<{ conversation: Conversation }>()
const emit = defineEmits<{ updated: [c: Conversation] }>()

const draft = ref('')
const pending = ref<ChatMessage | null>(null) // 乐观显示：发出去的问题立刻出现在列表里
const sendError = ref('')
const showContext = ref(false)
const scroller = ref<HTMLElement | null>(null)
const messages = computed(() => (pending.value ? [...props.conversation.messages, pending.value] : props.conversation.messages))
const trafficLines = computed(() => props.conversation.context.split('\n').filter(Boolean).length)

async function scrollToEnd() {
  await nextTick()
  scroller.value?.scrollTo({ top: scroller.value.scrollHeight, behavior: 'smooth' })
}
watch(() => props.conversation.id, () => {
  draft.value = ''
  sendError.value = ''
  pending.value = null
  void scrollToEnd()
}, { immediate: true })

async function send(content = draft.value) {
  const text = content.trim()
  if (!text || pending.value) return
  pending.value = { role: 'user', content: text, at: new Date().toISOString() }
  draft.value = ''
  sendError.value = ''
  void scrollToEnd()
  await api
    .post<Conversation>(`/api/v1/llm/conversations/${props.conversation.id}/messages`, { content: text })
    .then((c) => emit('updated', c))
    .catch((err: unknown) => {
      console.error('发送消息失败', err)
      sendError.value = err instanceof ApiError ? err.message : '网络错误'
      draft.value = text // 没发出去：把内容还给输入框
    })
    .finally(() => {
      pending.value = null
      void scrollToEnd()
    })
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    void send()
  }
}
const retryOf = (i: number) => messages.value[i - 1]?.content ?? ''
</script>

<template>
  <SpotlightCard :interactive="false" class="flex h-[calc(100vh-15rem)] min-h-[520px] flex-col p-0">
    <header class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-5 py-3">
      <h2 class="mr-auto truncate text-base font-medium">{{ conversation.title }}</h2>
      <span class="font-mono text-xs text-muted-foreground">{{ conversation.provider_name }} / {{ conversation.model }}</span>
      <span class="text-xs text-muted-foreground">· {{ conversation.decision_ids.length }} 条流量{{ conversation.redact_ip ? '（IP 已脱敏）' : '' }}</span>
      <span class="text-xs text-muted-foreground">· {{ fmtNum(conversation.total_tokens) }} token</span>
      <button type="button" class="flex cursor-pointer items-center gap-1 text-xs text-primary hover:underline" @click="showContext = !showContext">
        发送给模型的流量快照 <ChevronDown class="h-3 w-3 transition" :class="showContext && 'rotate-180'" />
      </button>
    </header>
    <pre v-if="showContext" class="max-h-56 overflow-auto border-b border-border/60 bg-background/50 px-5 py-3 font-mono text-[11px] leading-5 text-muted-foreground">{{ conversation.context }}</pre>

    <div ref="scroller" class="flex-1 overflow-y-auto px-5 py-4">
      <p v-if="!messages.length" class="py-16 text-center text-sm text-muted-foreground">
        已载入 {{ trafficLines }} 条流量。向模型提问，例如「这批请求是什么性质？」
      </p>
      <div class="flex flex-col gap-4">
        <div v-for="(m, i) in messages" :key="i + m.at" class="flex gap-3" :class="m.role === 'user' && 'flex-row-reverse'">
          <span class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full" :class="m.role === 'user' ? 'bg-primary/15 text-primary' : 'bg-mirage/15 text-mirage'">
            <UserRound v-if="m.role === 'user'" class="h-4 w-4" /><Bot v-else class="h-4 w-4" />
          </span>
          <div class="flex max-w-[82%] flex-col gap-1" :class="m.role === 'user' && 'items-end'">
            <div
              class="rounded-2xl px-4 py-2.5"
              :class="m.role === 'user' ? 'rounded-tr-sm bg-primary/10 text-foreground' : m.error ? 'rounded-tl-sm border border-danger/30 bg-danger/10' : 'rounded-tl-sm border border-border/60 bg-card/60'"
            >
              <p v-if="m.role === 'user'" class="whitespace-pre-wrap break-words text-sm leading-6">{{ m.content }}</p>
              <div v-else-if="m.error" class="flex flex-col gap-2 text-sm text-danger">
                <span>模型调用失败：{{ m.error }}</span>
                <Button variant="outline" size="sm" class="w-fit" :disabled="!!pending" @click="send(retryOf(i))"><RotateCcw class="h-3.5 w-3.5" />重试</Button>
              </div>
              <MarkdownText v-else :text="m.content" />
            </div>
            <span class="text-[11px] text-muted-foreground">
              {{ fmtTime(m.at) }}
              <template v-if="m.usage"> · {{ fmtNum(m.usage.total_tokens) }} token{{ m.usage.estimated ? '（估算）' : '' }}</template>
              <template v-if="m.latency_ms"> · {{ (m.latency_ms / 1000).toFixed(1) }} s</template>
              <template v-if="m.truncated"> · 回答因长度上限被截断，可追问「继续」</template>
            </span>
          </div>
        </div>
        <div v-if="pending" class="flex gap-3">
          <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-mirage/15 text-mirage"><Bot class="h-4 w-4" /></span>
          <div class="flex items-center gap-2 rounded-2xl rounded-tl-sm border border-border/60 bg-card/60 px-4 py-2.5 text-sm text-muted-foreground">
            <Loader2 class="h-4 w-4 animate-spin" />模型正在分析…
          </div>
        </div>
      </div>
    </div>

    <footer class="border-t border-border/60 p-4">
      <p v-if="sendError" role="alert" class="mb-2 text-xs text-danger">{{ sendError }}</p>
      <div class="flex items-end gap-2">
        <textarea
          v-model="draft"
          rows="2"
          maxlength="8000"
          class="max-h-40 min-h-[44px] flex-1 resize-y rounded-xl border border-input bg-background/40 px-3 py-2.5 text-sm outline-none transition hover:border-primary/40 focus:border-primary/70 focus:ring-2 focus:ring-primary/25"
          placeholder="继续追问（Enter 发送，Shift+Enter 换行）"
          :disabled="!!pending"
          @keydown="onKey"
        />
        <Button size="icon" class="h-11 w-11" :disabled="!draft.trim() || !!pending" aria-label="发送" @click="send()"><SendHorizontal class="h-4 w-4" /></Button>
      </div>
    </footer>
  </SpotlightCard>
</template>
