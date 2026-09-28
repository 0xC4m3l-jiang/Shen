<script setup lang="ts">
import { TabsContent } from 'reka-ui'
import { computed, ref } from 'vue'
import PageHeader from '@/components/PageHeader.vue'
import AuditPanel from '@/components/system/AuditPanel.vue'
import ThemePanel from '@/components/system/ThemePanel.vue'
import UsersPanel from '@/components/system/UsersPanel.vue'
import Tabs from '@/components/ui/tabs/Tabs.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const tab = ref('theme')
const tabs = computed(() => [
  { value: 'theme', label: '主题' },
  ...(auth.can('users:admin') ? [{ value: 'users', label: '账号与角色' }] : []),
  ...(auth.can('audit:read') ? [{ value: 'audit', label: '审计日志' }] : []),
])
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="系统" subtitle="界面主题、账号与角色、操作审计。" />
    <Tabs v-model="tab" :items="tabs">
      <TabsContent value="theme"><ThemePanel /></TabsContent>
      <TabsContent v-if="auth.can('users:admin')" value="users"><UsersPanel /></TabsContent>
      <TabsContent v-if="auth.can('audit:read')" value="audit"><AuditPanel /></TabsContent>
    </Tabs>
  </div>
</template>
