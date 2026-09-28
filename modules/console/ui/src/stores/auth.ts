import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, ApiError, request, setCsrfToken } from '@/lib/api'
import type { Permission, SessionView } from '@/lib/types'

export const useAuthStore = defineStore('auth', () => {
  const session = ref<SessionView | null>(null)
  const ready = ref(false)

  const user = computed(() => session.value?.user ?? null)
  const mustChange = computed(() => session.value?.user.must_change ?? false)
  const can = (perm: Permission) => session.value?.permissions.includes(perm) ?? false

  function apply(s: SessionView | null) {
    session.value = s
    setCsrfToken(s?.csrf_token ?? '')
  }

  /** 启动时恢复会话：Cookie 是 HttpOnly，只能问后端「我现在是谁」。 */
  async function bootstrap(): Promise<void> {
    const s = await request<SessionView>('/api/v1/auth/session', { silentAuth: true }).catch((err: unknown) => {
      if (!(err instanceof ApiError) || err.status !== 401) console.error('恢复会话失败', err)
      return null
    })
    apply(s)
    ready.value = true
  }

  async function login(username: string, password: string): Promise<SessionView> {
    const s = await api.post<SessionView>('/api/v1/auth/login', { username, password }, { silentAuth: true })
    apply(s)
    return s
  }

  async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
    const s = await api.post<SessionView>('/api/v1/auth/password', { old_password: oldPassword, new_password: newPassword })
    apply(s)
  }

  async function logout(): Promise<void> {
    await api.post('/api/v1/auth/logout').catch((err: unknown) => console.error('登出请求失败（本地会话仍会清除）', err))
    apply(null)
  }

  /** 会话失效（401）时由全局监听调用：只清本地状态，不再请求后端。 */
  function expire() {
    apply(null)
  }

  return { session, ready, user, mustChange, can, bootstrap, login, changePassword, logout, expire, apply }
})
