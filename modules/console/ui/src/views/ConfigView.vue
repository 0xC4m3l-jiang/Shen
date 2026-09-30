<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { TabsContent } from 'reka-ui'
import PageHeader from '@/components/PageHeader.vue'
import StatePanel from '@/components/StatePanel.vue'
import Tabs from '@/components/ui/tabs/Tabs.vue'
import DecoyAssetsTab from '@/components/config/DecoyAssetsTab.vue'
import HoneypotPoolTab from '@/components/config/HoneypotPoolTab.vue'
import InjectsTab from '@/components/config/InjectsTab.vue'
import ListsTab from '@/components/config/ListsTab.vue'
import SyncBar from '@/components/config/SyncBar.vue'
import VersionsTab from '@/components/config/VersionsTab.vue'
import { useConfigStore } from '@/stores/deceptionConfig'

// 欺骗管控工作区：蜜罐池 / 诱饵资产 / 黑白名单 / 注入规则 / 版本与回滚。
// 数据归管控台所有（方案 B），保存后由核心拉取、终检并热替换；判定策略（规则 / 阈值 / 灰度）不在这里。
const store = useConfigStore()
const route = useRoute()
const router = useRouter()

const valid = ['honeypots', 'decoys', 'lists', 'injects', 'versions']
const tab = ref(typeof route.query.tab === 'string' && valid.includes(route.query.tab) ? route.query.tab : 'honeypots')
watch(tab, (t) => void router.replace({ query: { ...route.query, tab: t } }))
watch(
  () => route.query.tab,
  (t) => {
    if (typeof t === 'string' && valid.includes(t) && t !== tab.value) tab.value = t
  },
)

const errCount = (domains: string[]) => (store.view?.report.errors ?? []).filter((e) => domains.includes(e.domain)).length
const tabs = computed(() => {
  const ds = store.dataset
  return [
    { value: 'honeypots', label: '蜜罐池', count: ds?.honeypots.length },
    { value: 'decoys', label: '诱饵资产', count: ds?.decoys.length },
    { value: 'lists', label: '黑白名单', count: ds ? ds.whitelist.source_cidrs.length + ds.whitelist.user_agents.length + ds.whitelist.path_prefixes.length + ds.blacklist.length : undefined },
    { value: 'injects', label: '注入规则', count: ds?.injects_provided ? ds.injects.length : undefined },
    { value: 'versions', label: '版本与回滚', count: ds?.version },
  ]
})
const brokenSaved = computed(() => errCount(['honeypots', 'decoys', 'whitelist', 'blacklist', 'injects', 'bindings']))

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void store.load()
  void store.loadTemplates()
  timer = setInterval(() => void store.refreshSync(), 8000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="欺骗管控" subtitle="蜜罐池 · 诱饵资产 · 黑白名单 · 注入规则 · 版本 —— 保存后由核心终检并热替换，判定策略不在此处" />
    <StatePanel v-if="!store.view" :error="store.error" :loading="store.loading" @retry="store.load" />
    <template v-if="store.view">
      <SyncBar :sync="store.sync" :dataset-version="store.view.dataset.version" :initialized="store.view.dataset.initialized" :seed="store.view.seed" />
      <p v-if="brokenSaved" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
        已保存的数据集当前有 {{ brokenSaved }} 个错误（例如被绑定的服务已被删除）：核心会拒收并保留上一份正确配置，请在对应 Tab 修正。
      </p>
      <Tabs v-model="tab" :items="tabs">
        <TabsContent value="honeypots"><HoneypotPoolTab /></TabsContent>
        <TabsContent value="decoys"><DecoyAssetsTab /></TabsContent>
        <TabsContent value="lists"><ListsTab /></TabsContent>
        <TabsContent value="injects"><InjectsTab /></TabsContent>
        <TabsContent value="versions"><VersionsTab /></TabsContent>
      </Tabs>
    </template>
  </div>
</template>
