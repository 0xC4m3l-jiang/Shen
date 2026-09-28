<script setup lang="ts">
import { CheckCircle2, KeyRound, XCircle } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import PageHeader from '@/components/PageHeader.vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import { ApiError } from '@/lib/api'
import { firstAllowed } from '@/router'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const oldPass = ref('')
const newPass = ref('')
const confirm = ref('')
const busy = ref(false)
const error = ref('')

const checks = computed(() => [
  { ok: [...newPass.value].length >= 12, text: '至少 12 个字符' },
  { ok: newPass.value !== '' && newPass.value !== oldPass.value, text: '与当前口令不同' },
  { ok: newPass.value.toLowerCase() !== (auth.user?.username ?? '').toLowerCase(), text: '不得与用户名相同' },
  { ok: newPass.value !== '' && newPass.value === confirm.value, text: '两次输入一致' },
])
const valid = computed(() => checks.value.every((c) => c.ok) && oldPass.value !== '')

async function submit() {
  if (!valid.value) return
  busy.value = true
  error.value = ''
  await auth
    .changePassword(oldPass.value, newPass.value)
    .then(() => router.replace({ name: firstAllowed() }))
    .catch((err: unknown) => {
      console.error('修改口令失败', err)
      error.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}
</script>

<template>
  <div class="mx-auto flex max-w-xl flex-col gap-6">
    <PageHeader
      title="修改口令"
      :subtitle="auth.mustChange ? '首次登录或口令已被管理员重置：修改后才能进入其他页面。' : '修改后当前账号的其他会话会全部失效。'"
    />
    <SpotlightCard :interactive="false" beam class="p-6">
      <form class="flex flex-col gap-4" @submit.prevent="submit">
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">当前口令</span>
          <Input v-model="oldPass" type="password" autocomplete="current-password" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">新口令</span>
          <Input v-model="newPass" type="password" autocomplete="new-password" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">确认新口令</span>
          <Input v-model="confirm" type="password" autocomplete="new-password" />
        </label>
        <ul class="grid grid-cols-2 gap-2 text-xs">
          <li v-for="c in checks" :key="c.text" class="flex items-center gap-1.5" :class="c.ok ? 'text-mirage' : 'text-muted-foreground'">
            <CheckCircle2 v-if="c.ok" class="h-3.5 w-3.5" />
            <XCircle v-else class="h-3.5 w-3.5" />
            {{ c.text }}
          </li>
        </ul>
        <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
        <Button type="submit" :disabled="!valid" :loading="busy" class="self-end"><KeyRound /> 保存新口令</Button>
      </form>
    </SpotlightCard>
  </div>
</template>
