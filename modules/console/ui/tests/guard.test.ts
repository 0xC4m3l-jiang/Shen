import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { mirageState } from '../src/lib/format'
import type { SessionView, TrafficRow } from '../src/lib/types'
import { firstAllowed, router } from '../src/router'
import { useAuthStore } from '../src/stores/auth'

function session(role: SessionView['user']['role'], permissions: SessionView['permissions'], mustChange = false): SessionView {
  return { user: { username: 'u', role, must_change: mustChange }, permissions, csrf_token: 't' }
}

beforeEach(async () => {
  setActivePinia(createPinia())
  await router.push('/login').catch(() => undefined)
})

describe('路由守卫', () => {
  it('未登录一律去登录页，并记住目标地址', async () => {
    await router.push('/honeypot')
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.next).toBe('/honeypot')
  })

  it('必须改密时只能进入改密页', async () => {
    useAuthStore().apply(session('admin', ['self:manage'], true))
    await router.push('/overview')
    expect(router.currentRoute.value.name).toBe('password')
  })

  it('无权限的标签页改道到该角色的首个可见页（蜜罐运维进不了欺骗层）', async () => {
    useAuthStore().apply(session('honeypot_operator', ['overview:read', 'honeypot:read', 'registry:read', 'self:manage']))
    await router.push('/deception')
    expect(router.currentRoute.value.name).toBe(firstAllowed())
    expect(router.currentRoute.value.name).toBe('overview')
    await router.push('/honeypot')
    expect(router.currentRoute.value.name).toBe('honeypot')
  })
})

describe('是否流入蜃楼的展示口径', () => {
  const base = { layer: 'origin', in_mirage: false, shadow_mirage: false } as TrafficRow
  it.each([
    [{ ...base, layer: 'mirage', in_mirage: true }, 'in'],
    [{ ...base, layer: 'decoy', delivery_result: 'backend_unavailable' }, 'failed'],
    [{ ...base, layer: 'fallback' }, 'failed'],
    [{ ...base, shadow_mirage: true }, 'shadow'],
    [base, 'none'],
  ] as [TrafficRow, string][])('%o → %s', (row, want) => {
    expect(mirageState(row)).toBe(want)
  })
})
