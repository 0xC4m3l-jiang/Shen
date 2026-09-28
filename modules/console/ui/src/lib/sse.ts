// 实时事件流客户端（EventSource + 断点续传 + 指数退避 + 心跳超时检测）。
//
// 为什么自己管重连而不完全依赖 EventSource 的自动重连：
//   ① 需要带 `since`（最后收到事件的时间）续传，EventSource 自动重连不会改 URL；
//   ② 服务端发 `closed` 帧（核心断开 / 会话失效）时要按原因决定是重连还是回登录页；
//   ③ 心跳超时（中间层静默断开）时浏览器可能长时间不报错 —— 需要主动检测。

import type { SseFrame } from './types'

export type StreamState = 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface StreamHandlers {
  onFrame: (frame: SseFrame) => void
  onState: (state: StreamState, detail?: string) => void
  onUnauthorized: () => void
}

export interface StreamOptions {
  url?: string
  heartbeatTimeoutMs?: number
  maxBackoffMs?: number
  factory?: (url: string) => EventSource
  /** 连接保持多久才算「稳定」：稳定前不显示「实时」、也不重置退避（防止 打开→立刻关闭 的抖动循环）。 */
  stableMs?: number
}

export class LiveStream {
  private es: EventSource | null = null
  private since = ''
  private attempt = 0
  private stopped = true
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private watchdog: ReturnType<typeof setInterval> | null = null
  private lastActivity = 0
  private stableTimer: ReturnType<typeof setTimeout> | null = null
  private readonly url: string
  private readonly heartbeatTimeoutMs: number
  private readonly maxBackoffMs: number
  private readonly factory: (url: string) => EventSource
  private readonly stableMs: number

  constructor(
    private handlers: StreamHandlers,
    opts: StreamOptions = {},
  ) {
    this.url = opts.url ?? '/api/v1/stream'
    this.heartbeatTimeoutMs = opts.heartbeatTimeoutMs ?? 45_000
    this.maxBackoffMs = opts.maxBackoffMs ?? 30_000
    this.factory = opts.factory ?? ((u) => new EventSource(u, { withCredentials: true }))
    this.stableMs = opts.stableMs ?? 5_000
  }

  start(): void {
    if (!this.stopped) return
    this.stopped = false
    this.connect()
    this.watchdog = setInterval(() => this.checkHeartbeat(), 5_000)
  }

  stop(): void {
    this.stopped = true
    this.clearStable()
    if (this.retryTimer) clearTimeout(this.retryTimer)
    if (this.watchdog) clearInterval(this.watchdog)
    this.retryTimer = null
    this.watchdog = null
    this.es?.close()
    this.es = null
    this.handlers.onState('closed')
  }

  /** 续传位置：最后一条事件的时间（RFC3339）。 */
  get resumeFrom(): string {
    return this.since
  }

  private connect(): void {
    const url = this.since ? `${this.url}?since=${encodeURIComponent(this.since)}` : this.url
    this.handlers.onState(this.attempt === 0 ? 'connecting' : 'reconnecting')
    const es = this.factory(url)
    this.es = es
    this.lastActivity = Date.now()
    es.onopen = () => {
      this.lastActivity = Date.now()
      // HTTP 200 只说明管控台 API 在；核心流可能下一刻就报错关闭。连接稳定一段时间后才算「实时」。
      this.clearStable()
      this.stableTimer = setTimeout(() => {
        this.stableTimer = null
        if (this.es !== es) return
        this.attempt = 0
        this.handlers.onState('open')
      }, this.stableMs)
    }
    es.onmessage = (ev: MessageEvent<string>) => {
      this.lastActivity = Date.now()
      const frame = JSON.parse(ev.data) as SseFrame
      if (frame.kind === 'event' && frame.view?.created_at) this.since = frame.view.created_at
      if (frame.kind === 'event' && this.stableTimer) {
        // 已经有真实事件到达：不必再等，直接视为健康。
        this.clearStable()
        this.attempt = 0
        this.handlers.onState('open')
      }
      if (frame.kind === 'closed') {
        if (frame.code === 'unauthenticated') {
          this.stop()
          this.handlers.onUnauthorized()
          return
        }
        this.scheduleReconnect(frame.error)
        return
      }
      this.handlers.onFrame(frame)
    }
    es.onerror = () => {
      // EventSource 在 401/403 等非 200 响应时也只给一个 error 事件：交给重连逻辑，
      // 连续失败后由上层的会话检查（REST 401）负责把人送回登录页。
      this.scheduleReconnect('连接中断')
    }
  }

  private clearStable(): void {
    if (this.stableTimer) clearTimeout(this.stableTimer)
    this.stableTimer = null
  }

  private scheduleReconnect(reason?: string): void {
    this.clearStable()
    this.es?.close()
    this.es = null
    if (this.stopped || this.retryTimer) return
    const delay = Math.min(this.maxBackoffMs, 1000 * 2 ** this.attempt) * (0.8 + Math.random() * 0.4)
    this.attempt++
    this.handlers.onState('reconnecting', reason)
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null
      if (!this.stopped) this.connect()
    }, delay)
  }

  private checkHeartbeat(): void {
    if (this.stopped || !this.es) return
    // 服务端每 15 秒至少发一次心跳注释；注释行不触发 onmessage，因此这里用 readyState 兜底：
    // 连接处于 OPEN 时浏览器仍在接收（包括注释）—— 只有长时间非 OPEN 或完全无数据才重连。
    const idle = Date.now() - this.lastActivity
    if (this.es.readyState !== EventSource.OPEN && idle > this.heartbeatTimeoutMs) {
      this.scheduleReconnect('心跳超时')
    }
  }
}
