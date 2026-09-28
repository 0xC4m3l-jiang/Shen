import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'
import { LiveStream, type StreamState } from '@/lib/sse'
import type { SseFrame, TrafficRow } from '@/lib/types'
import type { WindowValue } from '@/lib/format'

const MAX_ROWS = 400
const MAX_SEEN = 2000

/**
 * 实时数据：SSE 推来的请求行（有界缓冲 + 按事件 id 去重）与连接状态。
 *
 * 聚合统计（总览数字、趋势图）**不在前端算**：页面在收到新事件后防抖重拉对应接口，
 * 保证数字与后端口径完全一致；这里只负责「新流量滑入」与「连接是否健康」。
 */
export const useLiveStore = defineStore('live', () => {
  const state = ref<StreamState>('closed')
  const detail = ref('')
  const dropped = ref(0)
  const rows = shallowRef<TrafficRow[]>([])
  const tick = ref(0) // 每收到一条事件 +1：页面 watch 它做防抖刷新
  const lastEventAt = ref('')
  const span = ref<WindowValue>('15m') // 全局时间窗（顶栏选择）
  const seen = new Set<string>()
  const seenOrder: string[] = []
  let stream: LiveStream | null = null
  let onUnauthorized: () => void = () => {}

  const healthy = computed(() => state.value === 'open')

  function remember(id: string): boolean {
    if (seen.has(id)) return false
    seen.add(id)
    seenOrder.push(id)
    if (seenOrder.length > MAX_SEEN) seen.delete(seenOrder.shift() as string)
    return true
  }

  function push(frame: SseFrame) {
    if (frame.kind === 'status') {
      dropped.value = frame.dropped ?? dropped.value
      return
    }
    const id = frame.view?.event_id
    if (!id || !remember(id)) return // 断线续传会补发一段历史：按事件 id 去重
    lastEventAt.value = frame.view?.created_at ?? lastEventAt.value
    if (frame.row) rows.value = [frame.row, ...rows.value].slice(0, MAX_ROWS)
    tick.value++
  }

  function start(unauthorized: () => void) {
    onUnauthorized = unauthorized
    if (stream) return
    stream = new LiveStream({
      onFrame: push,
      onState: (s, d) => {
        state.value = s
        detail.value = d ?? ''
      },
      onUnauthorized: () => onUnauthorized(),
    })
    stream.start()
  }

  function stop() {
    stream?.stop()
    stream = null
    rows.value = []
    seen.clear()
    seenOrder.length = 0
  }

  return { state, detail, dropped, rows, tick, lastEventAt, span, healthy, start, stop, push }
})
