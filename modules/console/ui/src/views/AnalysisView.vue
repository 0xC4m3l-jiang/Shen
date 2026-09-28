<script setup lang="ts">
import { TabsContent } from 'reka-ui'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import PageHeader from '@/components/PageHeader.vue'
import ChatWorkspace from '@/components/analysis/ChatWorkspace.vue'
import L4Panel from '@/components/analysis/L4Panel.vue'
import ProvidersPanel from '@/components/analysis/ProvidersPanel.vue'
import UsagePanel from '@/components/analysis/UsagePanel.vue'
import Tabs from '@/components/ui/tabs/Tabs.vue'
import { useAuthStore } from '@/stores/auth'

// 分析：大模型对话分析（选流量 → 选模型 → 多轮追问）、模型管理、token 用量、L4 近线结论。
// 只读账号没有 llm:use —— 只能看 L4 结论（大模型调用会计费，只读意味着零副作用）。
const auth = useAuthStore()
const route = useRoute()
const canLLM = auth.can('llm:use')
const tabs = computed(() =>
  canLLM
    ? [
        { value: 'chat', label: '对话分析' },
        { value: 'models', label: '模型管理' },
        { value: 'usage', label: 'Token 用量' },
        { value: 'l4', label: '近线结论' },
      ]
    : [{ value: 'l4', label: '近线结论' }],
)
const tab = ref(canLLM ? (typeof route.query.tab === 'string' ? route.query.tab : 'chat') : 'l4')
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="分析" subtitle="选中流量交给大模型深度分析；模型答复只是建议，不会自动改策略。" />
    <Tabs v-model="tab" :items="tabs">
      <TabsContent v-if="canLLM" value="chat"><ChatWorkspace /></TabsContent>
      <TabsContent v-if="canLLM" value="models"><ProvidersPanel /></TabsContent>
      <TabsContent v-if="canLLM" value="usage"><UsagePanel /></TabsContent>
      <TabsContent value="l4"><L4Panel /></TabsContent>
    </Tabs>
  </div>
</template>
