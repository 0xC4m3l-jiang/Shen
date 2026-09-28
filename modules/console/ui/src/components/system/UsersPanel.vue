<script setup lang="ts">
import { KeyRound, Plus, Trash2, UserPlus } from 'lucide-vue-next'
import { ref } from 'vue'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { useLiveResource } from '@/composables/useLiveResource'
import { api, ApiError } from '@/lib/api'
import { fmtAgo, roleLabels } from '@/lib/format'
import type { Role, UserView } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const users = useLiveResource((signal) => api.get<{ users: UserView[]; roles: Role[] }>('/api/v1/users', undefined, signal), { live: false })
const roleOptions = (Object.keys(roleLabels) as Role[]).map((r) => ({ value: r, label: roleLabels[r] }))
const creating = ref(false)
const resetting = ref<UserView | null>(null)
const form = ref({ username: '', role: 'viewer' as Role, password: '' })
const resetPass = ref('')
const error = ref('')
const busy = ref(false)

async function run(p: Promise<unknown>, done?: () => void) {
  busy.value = true
  error.value = ''
  await p
    .then(() => {
      done?.()
      void users.refresh()
    })
    .catch((err: unknown) => {
      console.error('账号操作失败', err)
      error.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}
const create = () => run(api.post('/api/v1/users', form.value), () => {
  creating.value = false
  form.value = { username: '', role: 'viewer', password: '' }
})
const setRole = (u: UserView, role: string) => run(api.patch(`/api/v1/users/${u.username}`, { role }))
const setDisabled = (u: UserView, disabled: boolean) => run(api.patch(`/api/v1/users/${u.username}`, { disabled }))
const remove = (u: UserView) => run(api.del(`/api/v1/users/${u.username}`))
const reset = () =>
  resetting.value && run(api.post(`/api/v1/users/${resetting.value.username}/password`, { password: resetPass.value }), () => {
    resetting.value = null
    resetPass.value = ''
  })
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between">
      <p class="text-sm text-muted-foreground">四种角色：管理员（全部）· 欺骗运维（欺骗层 + 登记）· 蜜罐运维（蜜罐层）· 只读。改角色 / 停用会立即注销该账号的全部会话。</p>
      <Button @click="creating = true"><UserPlus /> 新建账号</Button>
    </div>
    <p v-if="error && !creating && !resetting" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
    <SpotlightCard :interactive="false" class="overflow-x-auto p-5">
      <table class="w-full min-w-[760px] text-sm">
        <thead>
          <tr class="text-left text-xs uppercase tracking-wider text-muted-foreground">
            <th class="py-2 font-medium">账号</th><th class="py-2 font-medium">角色</th><th class="py-2 font-medium">状态</th>
            <th class="py-2 font-medium">最近登录</th><th class="py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users.data.value?.users ?? []" :key="u.username" class="border-t border-border/50 hover:bg-primary/5">
            <td class="py-3 font-medium">
              {{ u.username }}
              <Badge v-if="u.username === auth.user?.username" tone="info" class="ml-2">当前</Badge>
              <Badge v-if="u.must_change" tone="warn" class="ml-2">待改密</Badge>
            </td>
            <td class="py-3"><NativeSelect :model-value="u.role" :options="roleOptions" class="w-36" label="角色" @update:model-value="setRole(u, $event as string)" /></td>
            <td class="py-3">
              <label class="flex items-center gap-2 text-xs text-muted-foreground">
                <Switch :model-value="!u.disabled" :disabled="u.username === auth.user?.username" label="启用" @update:model-value="setDisabled(u, !$event)" />
                {{ u.disabled ? '已停用' : '启用' }}
              </label>
            </td>
            <td class="py-3 text-xs text-muted-foreground">{{ fmtAgo(u.last_login_at) }}</td>
            <td class="py-3 text-right">
              <Button size="sm" variant="ghost" @click="resetting = u"><KeyRound /> 重置口令</Button>
              <Button size="sm" variant="ghost" class="text-danger" :disabled="u.username === auth.user?.username" @click="remove(u)"><Trash2 /></Button>
            </td>
          </tr>
        </tbody>
      </table>
    </SpotlightCard>

    <Sheet v-model:open="creating" title="新建账号" description="新账号首次登录必须修改口令。">
      <form id="user-form" class="flex flex-col gap-4" @submit.prevent="create">
        <label class="flex flex-col gap-1.5 text-sm"><span class="text-muted-foreground">用户名</span><Input v-model="form.username" placeholder="例如 ops.deception" /></label>
        <label class="flex flex-col gap-1.5 text-sm"><span class="text-muted-foreground">角色</span><NativeSelect v-model="form.role" :options="roleOptions" /></label>
        <label class="flex flex-col gap-1.5 text-sm"><span class="text-muted-foreground">初始口令（≥12 位）</span><Input v-model="form.password" type="password" autocomplete="new-password" /></label>
        <p v-if="error" class="text-xs text-danger">{{ error }}</p>
      </form>
      <template #footer>
        <Button variant="ghost" @click="creating = false">取消</Button>
        <Button type="submit" form="user-form" :loading="busy"><Plus /> 创建</Button>
      </template>
    </Sheet>
    <Sheet :open="!!resetting" side="center" :title="`重置 ${resetting?.username ?? ''} 的口令`" description="重置后该账号的会话全部失效，下次登录必须修改口令。" @update:open="!$event && (resetting = null)">
      <Input v-model="resetPass" type="password" autocomplete="new-password" placeholder="新的临时口令（≥12 位）" />
      <p v-if="error" class="mt-2 text-xs text-danger">{{ error }}</p>
      <template #footer>
        <Button variant="ghost" @click="resetting = null">取消</Button>
        <Button :loading="busy" @click="reset">确认重置</Button>
      </template>
    </Sheet>
  </div>
</template>
