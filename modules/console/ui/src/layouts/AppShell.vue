<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { useRouter } from 'vue-router'
import AuroraBackground from '@/components/fx/AuroraBackground.vue'
import Meteors from '@/components/fx/Meteors.vue'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'
import { usePrefsStore } from '@/stores/prefs'
import SideNav from './SideNav.vue'
import TopBar from './TopBar.vue'

const auth = useAuthStore()
const live = useLiveStore()
const prefs = usePrefsStore()
const router = useRouter()

// 权限是会变的：首次登录改密前只有 self:manage，改密后外壳不会重新挂载 ——
// 所以按权限**响应式**启停实时流，而不是只在挂载时判断一次。
watch(
  () => auth.can('stream:read'),
  (allowed) => {
    if (!allowed) {
      live.stop()
      return
    }
    live.start(() => {
      auth.expire()
      void router.replace({ name: 'login', query: { reason: 'expired' } })
    })
  },
  { immediate: true },
)
onBeforeUnmount(() => live.stop())
</script>

<template>
  <div class="relative min-h-screen">
    <div class="fixed inset-0 -z-10 overflow-hidden">
      <div class="grid-floor absolute inset-0" />
      <AuroraBackground :intensity="prefs.theme === 'haze' ? 0.7 : 1" />
      <Meteors v-if="prefs.theme !== 'haze'" :count="4" />
    </div>
    <TopBar />
    <div class="flex pt-16">
      <SideNav />
      <main class="min-w-0 flex-1 px-6 pb-10 pt-6">
        <RouterView v-slot="{ Component, route }">
          <Transition name="page" mode="out-in">
            <component :is="Component" :key="route.fullPath" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>
</template>

<style scoped>
.page-enter-active,
.page-leave-active {
  transition:
    opacity 0.28s ease,
    transform 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.page-enter-from {
  opacity: 0;
  transform: translateY(8px);
}
.page-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
