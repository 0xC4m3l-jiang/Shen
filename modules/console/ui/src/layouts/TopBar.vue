<script setup lang="ts">
import { KeyRound, LogOut, UserRound } from 'lucide-vue-next'
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from 'reka-ui'
import { useRouter } from 'vue-router'
import LiveIndicator from '@/components/LiveIndicator.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import { roleLabels, themeMeta, windowOptions } from '@/lib/format'
import type { ThemeId } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'
import { usePrefsStore } from '@/stores/prefs'

const auth = useAuthStore()
const live = useLiveStore()
const prefs = usePrefsStore()
const router = useRouter()
const themes = Object.entries(themeMeta) as [ThemeId, (typeof themeMeta)[ThemeId]][]

async function logout() {
  live.stop()
  await auth.logout()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <header class="glass fixed inset-x-0 top-0 z-40 flex h-16 items-center gap-4 border-x-0 border-t-0 px-5">
    <RouterLink to="/" class="flex items-center gap-2.5">
      <img src="/logo.svg" alt="" class="h-8 w-8" />
      <span class="text-base font-semibold tracking-wide text-foreground">蜃楼 Shen</span>
    </RouterLink>

    <div class="ml-auto flex items-center gap-3">
      <NativeSelect v-model="live.span" :options="windowOptions" label="时间窗" class="w-28" />
      <LiveIndicator />

      <div class="glass hidden items-center gap-1 rounded-full p-1 md:flex" role="radiogroup" aria-label="主题（深色 / 亮色）">
        <button
          v-for="[id, meta] in themes"
          :key="id"
          type="button"
          role="radio"
          :aria-checked="prefs.theme === id"
          :title="`${meta.name}：${meta.desc}`"
          class="h-6 w-6 cursor-pointer rounded-full border-2 transition-all hover:scale-110"
          :class="prefs.theme === id ? 'border-primary shadow-[0_0_10px_hsl(var(--primary))]' : 'border-transparent'"
          :style="{ background: `linear-gradient(135deg, ${meta.swatch[0]} 45%, ${meta.swatch[1]} 46%, ${meta.swatch[2]})` }"
          @click="prefs.save({ theme: id })"
        />
      </div>

      <DropdownMenuRoot>
        <DropdownMenuTrigger
          class="glass flex cursor-pointer items-center gap-2 rounded-full py-1 pl-1 pr-3 text-sm transition hover:border-primary/40"
        >
          <span class="flex h-7 w-7 items-center justify-center rounded-full bg-primary/15 text-primary">
            <UserRound class="h-4 w-4" />
          </span>
          <span class="hidden sm:inline">{{ auth.user?.username }}</span>
        </DropdownMenuTrigger>
        <DropdownMenuPortal>
          <DropdownMenuContent
            align="end"
            :side-offset="8"
            class="glass z-50 min-w-48 rounded-xl p-1.5 text-sm shadow-xl data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
          >
            <DropdownMenuLabel class="px-2 py-1.5">
              <div class="font-medium">{{ auth.user?.username }}</div>
              <div class="text-xs text-muted-foreground">{{ auth.user ? roleLabels[auth.user.role] : '' }}</div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator class="my-1 h-px bg-border" />
            <DropdownMenuItem
              class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 outline-none data-[highlighted]:bg-primary/10"
              @select="router.push({ name: 'password' })"
            >
              <KeyRound class="h-4 w-4" /> 修改口令
            </DropdownMenuItem>
            <DropdownMenuItem
              class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-danger outline-none data-[highlighted]:bg-danger/10"
              @select="logout"
            >
              <LogOut class="h-4 w-4" /> 退出登录
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuPortal>
      </DropdownMenuRoot>
    </div>
  </header>
</template>
