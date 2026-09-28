<script setup lang="ts">
import { Eye, EyeOff, Lock, ShieldCheck, User } from 'lucide-vue-next'
import { motion } from 'motion-v'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuroraBackground from '@/components/fx/AuroraBackground.vue'
import BorderBeam from '@/components/fx/BorderBeam.vue'
import Meteors from '@/components/fx/Meteors.vue'
import MirageHorizon from '@/components/fx/MirageHorizon.vue'
import { Button } from '@/components/ui/button'
import { ApiError } from '@/lib/api'
import { firstAllowed } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { usePrefsStore } from '@/stores/prefs'

const auth = useAuthStore()
const prefs = usePrefsStore()
const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const reveal = ref(false)
const busy = ref(false)
const error = ref('')
const expired = computed(() => route.query.reason === 'expired')

async function submit() {
  if (!username.value || !password.value) {
    error.value = '请输入用户名与口令'
    return
  }
  busy.value = true
  error.value = ''
  await auth
    .login(username.value, password.value)
    .then(async (s) => {
      prefs.load(s.preferences)
      const next = typeof route.query.next === 'string' ? route.query.next : ''
      await router.replace(s.user.must_change ? { name: 'password' } : next || { name: firstAllowed() })
    })
    .catch((err: unknown) => {
      console.error('登录失败', err)
      error.value = err instanceof ApiError ? err.message : '无法连接管控台 API'
      password.value = ''
    })
    .finally(() => (busy.value = false))
}
</script>

<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden px-4">
    <div class="grid-floor absolute inset-0" />
    <AuroraBackground />
    <Meteors v-if="prefs.theme !== 'haze'" :count="8" />
    <div class="absolute inset-x-0 bottom-0 opacity-70"><MirageHorizon :height="260" /></div>

    <motion.div
      :initial="{ opacity: 0, y: 24, scale: 0.98 }"
      :animate="{ opacity: 1, y: 0, scale: 1 }"
      :transition="{ duration: 0.6, ease: [0.2, 0.8, 0.2, 1] }"
      class="relative z-10 w-full max-w-md"
    >
      <div class="glass relative overflow-hidden rounded-3xl p-8 shadow-2xl">
        <BorderBeam :duration="10" :size="220" />
        <div class="mb-8 flex flex-col items-center gap-3 text-center">
          <img src="/logo.svg" alt="蜃楼 Shen" class="h-14 w-14" />
          <h1 class="text-2xl font-semibold tracking-wide text-foreground">蜃楼 Shen 管控台</h1>
          <p class="text-sm text-muted-foreground">本地账号登录 · 全部操作记入审计</p>
        </div>

        <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
          <p v-if="expired && !error" class="rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-xs text-info">
            会话已过期或已在别处注销，请重新登录。
          </p>
          <label class="flex flex-col gap-1.5 text-sm">
            <span class="text-muted-foreground">用户名</span>
            <span class="group flex items-center gap-2 rounded-lg border border-input bg-background/40 px-3 transition focus-within:border-primary/70 focus-within:ring-2 focus-within:ring-primary/25">
              <User class="h-4 w-4 text-muted-foreground group-focus-within:text-primary" />
              <input
                v-model="username"
                autocomplete="username"
                autofocus
                class="h-11 w-full border-0 bg-transparent text-foreground outline-none placeholder:text-muted-foreground/60"
                placeholder="例如 admin"
              />
            </span>
          </label>
          <label class="flex flex-col gap-1.5 text-sm">
            <span class="text-muted-foreground">口令</span>
            <span class="group flex items-center gap-2 rounded-lg border border-input bg-background/40 px-3 transition focus-within:border-primary/70 focus-within:ring-2 focus-within:ring-primary/25">
              <Lock class="h-4 w-4 text-muted-foreground group-focus-within:text-primary" />
              <input
                v-model="password"
                :type="reveal ? 'text' : 'password'"
                autocomplete="current-password"
                class="h-11 w-full border-0 bg-transparent text-foreground outline-none placeholder:text-muted-foreground/60"
                placeholder="至少 12 位"
              />
              <button type="button" class="cursor-pointer text-muted-foreground hover:text-foreground" :aria-label="reveal ? '隐藏口令' : '显示口令'" @click="reveal = !reveal">
                <EyeOff v-if="reveal" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </button>
            </span>
          </label>
          <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
          <Button type="submit" size="lg" :loading="busy" class="mt-2 w-full">进入管控台</Button>
        </form>

        <div class="mt-8 flex items-start gap-2 border-t border-border/60 pt-4 text-[11px] leading-relaxed text-muted-foreground">
          <ShieldCheck class="mt-0.5 h-3.5 w-3.5 shrink-0 text-primary" />
          <span>仅限已获书面授权的站点与环境使用。所有登录与变更操作均写入审计日志；管控台只观测与登记，不下发处置策略。</span>
        </div>
      </div>
    </motion.div>
  </div>
</template>
