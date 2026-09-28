import { onBeforeUnmount, ref, shallowRef, watch, type Ref, type WatchSource } from 'vue'
import { ApiError } from '@/lib/api'
import { useLiveStore } from '@/stores/live'

export interface LiveResourceOptions {
  /** 额外的依赖（如路由参数）：变化即立即重拉 */
  deps?: WatchSource[]
  /** 收到新事件后的防抖（毫秒） */
  debounceMs?: number
  /** 实时流不可用时的兜底轮询间隔（毫秒） */
  pollMs?: number
  /** 是否跟随实时事件刷新（配置快照这类静态数据可关） */
  live?: boolean
}

/**
 * 「后端为唯一数据源」的取数封装：
 *   首屏请求 → 时间窗 / 依赖变化立即重拉 → 收到 SSE 新事件后防抖重拉 → 流断开时兜底轮询。
 * 同一时刻只有一个在途请求：新请求会取消旧请求，避免慢响应覆盖新数据。
 */
export function useLiveResource<T>(fetcher: (signal: AbortSignal) => Promise<T>, opts: LiveResourceOptions = {}) {
  const live = useLiveStore()
  const data = shallowRef<T | null>(null) as Ref<T | null>
  const error = ref<ApiError | null>(null)
  const loading = ref(false)
  const updatedAt = ref(0)
  let controller: AbortController | null = null
  let debounce: ReturnType<typeof setTimeout> | null = null
  let poll: ReturnType<typeof setInterval> | null = null

  async function refresh(): Promise<void> {
    controller?.abort()
    const ctrl = new AbortController()
    controller = ctrl
    loading.value = true
    await fetcher(ctrl.signal)
      .then((v) => {
        if (ctrl.signal.aborted) return
        data.value = v
        error.value = null
        updatedAt.value = Date.now()
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        console.error('取数失败', err)
        error.value = err instanceof ApiError ? err : new ApiError(0, 'network', '网络错误：无法连接管控台 API')
      })
      .finally(() => {
        if (controller === ctrl) loading.value = false
      })
  }

  watch([() => live.span, ...(opts.deps ?? [])], () => void refresh(), { immediate: true })

  if (opts.live !== false) {
    watch(
      () => live.tick,
      () => {
        if (debounce) return
        debounce = setTimeout(() => {
          debounce = null
          void refresh()
        }, opts.debounceMs ?? 2500)
      },
    )
    poll = setInterval(() => {
      if (!live.healthy) void refresh()
    }, opts.pollMs ?? 20_000)
  }

  onBeforeUnmount(() => {
    controller?.abort()
    if (debounce) clearTimeout(debounce)
    if (poll) clearInterval(poll)
  })

  return { data, error, loading, updatedAt, refresh }
}
