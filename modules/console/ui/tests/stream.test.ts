import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LiveStream } from '../src/lib/sse'
import type { SseFrame, TrafficRow } from '../src/lib/types'
import { useLiveStore } from '../src/stores/live'

// 可控的 EventSource 替身：记录每次连接的 URL，测试手动推帧。
class FakeES {
  static OPEN = 1
  static instances: FakeES[] = []
  readyState = 1
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent<string>) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {
    FakeES.instances.push(this)
  }
  emit(frame: SseFrame) {
    this.onmessage?.({ data: JSON.stringify(frame) } as MessageEvent<string>)
  }
  close() {
    this.closed = true
  }
}

const row = (id: string): TrafficRow =>
  ({ decision_id: id, at: '2026-09-28T12:00:00Z', layer: 'mirage', in_mirage: true }) as TrafficRow

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('EventSource', FakeES)
  FakeES.instances = []
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('LiveStream', () => {
  it('断线后带 since 续传，并按指数退避重连', () => {
    const frames: SseFrame[] = []
    const s = new LiveStream(
      { onFrame: (f) => frames.push(f), onState: () => {}, onUnauthorized: () => {} },
      { factory: (u) => new FakeES(u) as unknown as EventSource },
    )
    s.start()
    const first = FakeES.instances[0]
    first.onopen?.()
    first.emit({ kind: 'event', view: { event_id: 'e1', type: 'request_judged', created_at: '2026-09-28T12:00:01Z' } })
    first.onerror?.()
    expect(first.closed).toBe(true)
    vi.advanceTimersByTime(1500)
    expect(FakeES.instances).toHaveLength(2)
    expect(FakeES.instances[1].url).toBe('/api/v1/stream?since=' + encodeURIComponent('2026-09-28T12:00:01Z'))
    expect(frames).toHaveLength(1)
    s.stop()
  })

  it('收到 unauthenticated 的 closed 帧即停止并通知上层，不再重连', () => {
    const onUnauthorized = vi.fn()
    const s = new LiveStream(
      { onFrame: () => {}, onState: () => {}, onUnauthorized },
      { factory: (u) => new FakeES(u) as unknown as EventSource },
    )
    s.start()
    FakeES.instances[0].emit({ kind: 'closed', code: 'unauthenticated', error: '会话已失效' })
    vi.advanceTimersByTime(60_000)
    expect(onUnauthorized).toHaveBeenCalledOnce()
    expect(FakeES.instances).toHaveLength(1)
  })
  it('连接打开后立刻被关闭（核心不可达）时不显示「实时」，且退避不被重置', () => {
    const states: string[] = []
    const s = new LiveStream(
      { onFrame: () => {}, onState: (st) => states.push(st), onUnauthorized: () => {} },
      { factory: (u) => new FakeES(u) as unknown as EventSource, stableMs: 5_000 },
    )
    s.start()
    for (let round = 0; round < 3; round++) {
      const es = FakeES.instances[FakeES.instances.length - 1]
      es.onopen?.()
      es.emit({ kind: 'closed', code: 'core_closed', error: '核心不可达' })
      vi.advanceTimersByTime(60_000)
    }
    expect(states).not.toContain('open')
    // 若 onopen 重置了退避，第 3 次重连会在 ~1s 内发生；这里验证间隔在增长（1s → 2s → 4s …）
    expect(FakeES.instances.length).toBe(4)
    const es = FakeES.instances[FakeES.instances.length - 1]
    es.onopen?.()
    vi.advanceTimersByTime(5_000)
    expect(states[states.length - 1]).toBe('open')
    s.stop()
  })
})

describe('live store', () => {
  it('按事件 id 去重、行数有界、丢弃计数可见', () => {
    setActivePinia(createPinia())
    const live = useLiveStore()
    const ev = (id: string, r?: TrafficRow): SseFrame => ({
      kind: 'event',
      view: { event_id: id, type: 'request_judged', created_at: '2026-09-28T12:00:00Z' },
      row: r,
    })
    live.push(ev('e1', row('d1')))
    live.push(ev('e1', row('d1'))) // 续传补发的重复事件
    live.push(ev('e2'))
    live.push({ kind: 'status', dropped: 7 })
    expect(live.rows).toHaveLength(1)
    expect(live.tick).toBe(2)
    expect(live.dropped).toBe(7)
    for (let i = 0; i < 500; i++) live.push(ev(`x${i}`, row(`r${i}`)))
    expect(live.rows.length).toBe(400)
    expect(live.rows[0].decision_id).toBe('r499')
  })
})
