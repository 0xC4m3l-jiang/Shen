import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, onAuthProblem, setCsrfToken } from '../src/lib/api'

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn(async () => new Response(body === undefined ? null : JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => {
  vi.unstubAllGlobals()
  setCsrfToken('')
})

describe('api 客户端', () => {
  it('写请求自动携带 CSRF 头与同源凭证，读请求不带', async () => {
    setCsrfToken('csrf-123')
    const fn = mockFetch(200, { ok: true })
    await api.post('/api/v1/services', { name: 'x' })
    const [, init] = fn.mock.calls[0] as unknown as [string, RequestInit]
    const headers = init.headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBe('csrf-123')
    expect(headers['Content-Type']).toBe('application/json')
    expect(init.credentials).toBe('same-origin')

    await api.get('/api/v1/overview', { window: '15m', empty: '' })
    const [url, getInit] = fn.mock.calls[1] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/overview?window=15m')
    expect((getInit.headers as Record<string, string>)['X-CSRF-Token']).toBeUndefined()
  })

  it('401 触发全局认证事件并抛出带 code 的 ApiError', async () => {
    mockFetch(401, { error: '未登录或会话已过期', code: 'unauthenticated' })
    const seen: string[] = []
    const off = onAuthProblem((e) => seen.push(e.code))
    const err = await api.get('/api/v1/overview').catch((e: unknown) => e)
    off()
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(401)
    expect(seen).toEqual(['unauthenticated'])
  })

  it('登录接口的 401 不触发全局跳转（silentAuth）', async () => {
    mockFetch(401, { error: '用户名或口令错误', code: 'invalid_credentials' })
    const seen: string[] = []
    const off = onAuthProblem((e) => seen.push(e.code))
    await api.post('/api/v1/auth/login', { username: 'a', password: 'b' }, { silentAuth: true }).catch(() => undefined)
    off()
    expect(seen).toEqual([])
  })

  it('204 返回 undefined', async () => {
    mockFetch(204, undefined)
    await expect(api.del('/api/v1/services/x', { version: 2 })).resolves.toBeUndefined()
  })
})
