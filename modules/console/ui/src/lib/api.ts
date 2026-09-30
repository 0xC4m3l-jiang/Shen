// 类型化的 /api/v1 客户端。
//
// 约定：
//   - 只走同源相对路径（nginx / Vite 代理到后端）；会话在 HttpOnly Cookie 里，JS 读不到；
//   - 写请求自动带 X-CSRF-Token（令牌来自登录 / 会话接口的响应，只存在内存里）；
//   - 401 ⇒ 通知上层回到登录页；403 password_change_required ⇒ 引导改密。
//   - 不使用 try/catch 吞错：失败统一抛 ApiError，由调用方决定展示方式。

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    /** 原始响应体（例如 validation_failed 时附带的逐字段校验报告）。 */
    public body?: unknown,
  ) {
    super(message)
  }
}

type Listener = (err: ApiError) => void

let csrfToken = ''
const unauthorizedListeners = new Set<Listener>()

export function setCsrfToken(token: string): void {
  csrfToken = token
}

/** 订阅「会话失效」与「必须先改密」事件（路由层用它跳转）。 */
export function onAuthProblem(fn: Listener): () => void {
  unauthorizedListeners.add(fn)
  return () => unauthorizedListeners.delete(fn)
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  query?: Record<string, string | number | undefined>
  signal?: AbortSignal
  /** 登录接口自身的 401 不应触发全局跳转 */
  silentAuth?: boolean
}

function buildURL(path: string, query?: RequestOptions['query']): string {
  if (!query) return path
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== '') qs.set(k, String(v))
  }
  const s = qs.toString()
  return s ? `${path}?${s}` : path
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken

  const resp = await fetch(buildURL(path, opts.query), {
    method,
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    credentials: 'same-origin',
    cache: 'no-store',
    signal: opts.signal,
  })
  if (resp.status === 204) return undefined as T
  const text = await resp.text()
  const data = text ? safeParse(text) : undefined
  if (!resp.ok) {
    const body = (data ?? {}) as { error?: string; code?: string }
    const err = new ApiError(resp.status, body.code ?? 'http_' + resp.status, body.error ?? `请求失败（HTTP ${resp.status}）`, data)
    const authProblem = resp.status === 401 || err.code === 'password_change_required'
    if (authProblem && !opts.silentAuth) unauthorizedListeners.forEach((fn) => fn(err))
    throw err
  }
  return data as T
}

function safeParse(text: string): unknown {
  if (!text.startsWith('{') && !text.startsWith('[')) return { error: text }
  return JSON.parse(text)
}

export const api = {
  get: <T>(path: string, query?: RequestOptions['query'], signal?: AbortSignal) =>
    request<T>(path, { query, signal }),
  post: <T>(path: string, body?: unknown, opts: Partial<RequestOptions> = {}) =>
    request<T>(path, { ...opts, method: 'POST', body: body ?? {} }),
  put: <T>(path: string, body: unknown) => request<T>(path, { method: 'PUT', body }),
  patch: <T>(path: string, body: unknown) => request<T>(path, { method: 'PATCH', body }),
  del: <T>(path: string, query?: RequestOptions['query']) => request<T>(path, { method: 'DELETE', query }),
}
