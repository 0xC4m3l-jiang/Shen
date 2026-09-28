// 与 Go 侧 internal/api 的响应一一对应（snake_case）。改字段必须两侧同步。

export type Role = 'admin' | 'deception_operator' | 'honeypot_operator' | 'viewer'
export type Permission =
  | 'overview:read'
  | 'deception:read'
  | 'honeypot:read'
  | 'registry:read'
  | 'registry:write'
  | 'analysis:read'
  | 'alerts:read'
  | 'stream:read'
  | 'self:manage'
  | 'users:admin'
  | 'audit:read'
  | 'llm:use'
  | 'llm:admin'

export type ThemeId = 'mirage' | 'haze'
export interface Preferences {
  theme: ThemeId
  motion: boolean
}

export interface SessionView {
  user: { username: string; role: Role; must_change: boolean }
  permissions: Permission[]
  csrf_token: string
  preferences?: Preferences
}

export type Scope = 'public' | 'private' | 'loopback' | 'reserved' | 'unknown'
export interface GeoLocation {
  country: string
  province: string
  city: string
  isp: string
  iso: string
  scope: Scope
  label: string
}

export type Layer = 'origin' | 'fallback' | 'mirage' | 'decoy' | 'block'

export interface TrafficRow {
  decision_id: string
  at: string
  host: string
  source_ip: string
  geo: GeoLocation
  method: string
  path: string
  user_agent: string
  action: string
  executed: string
  dispatched?: string
  layer: Layer
  delivery_result?: string
  backend?: string
  status?: number
  duration_ms?: number
  score?: number
  signals?: string[]
  severity?: string
  shadow: boolean
  in_mirage: boolean
  shadow_mirage: boolean
  alert: boolean
  service_id?: string
  service_name?: string
  source: 'adapter' | 'core'
}

export interface Window {
  start: string
  end: string
  seconds: number
  truncated: boolean
  coverage_start: string
  note?: string
  freshest?: string
}

export interface Stats {
  total: number
  by_layer: Partial<Record<Layer, number>>
  by_action: Record<string, number>
  in_mirage: number
  shadow_mirage: number
  mirage_ratio: number
  alerts: number
  unique_ips: number
  decoy_failed: number
}

export interface SourceStat {
  ip: string
  geo: GeoLocation
  count: number
  in_mirage: number
  last_seen: string
}
export interface GeoStat {
  label: string
  scope: Scope
  count: number
}
export interface Bucket {
  at: string
  by_layer: Partial<Record<Layer, number>>
}

export interface Overview {
  window: Window
  stats: Stats
  trend: Bucket[]
  top_sources: SourceStat[]
  geo: GeoStat[]
  recent: TrafficRow[]
  services_registered: number
  geo_db_built_at: number
}

export interface Service {
  id: string
  name: string
  upstream: string
  hosts: string[]
  owner: string
  description: string
  enabled: boolean
  version: number
  created_at: string
  updated_at: string
  updated_by: string
}
export type ServiceInput = Pick<Service, 'name' | 'upstream' | 'hosts' | 'owner' | 'description' | 'enabled'>

export interface ServiceSummary extends Service {
  stats: Stats
  last_seen?: string
  spark: number[]
}

export interface ServiceTraffic {
  service: Service
  window: Window
  stats: Stats
  funnel: { key: string; label: string; count: number }[]
  trend: Bucket[]
  top_sources: SourceStat[]
  geo: GeoStat[]
  rows: TrafficRow[]
}

export interface FlowRecord {
  decision_id: string
  source_ip: string
  method: string
  path: string
  query?: string
  user_agent: string
  action: string
  severity: string
  backend: string
  score: number
  signals: string[]
  at: string
  geo: GeoLocation
}

export interface ChainNode {
  id: string
  label: string
  kind: string
  value?: string
  alert?: boolean
  warn?: boolean
  request?: string
  response?: string
  why?: string
}

export interface RequestGraph {
  decision_id: string
  at: string
  method: string
  path: string
  ua: string
  source_ip: string
  action: string
  score: number
  signals: string[] | null
  severity: string
  executed: string
  backend: string
  status: number
  bytes: number
  duration_ms: number
  inject: string
  content_id: string
  real_alert: boolean
  high_risk: boolean
  l4_conclusions: number
  unjudged: boolean
  chain: ChainNode[]
}

export interface CoreConfig {
  policy: Record<string, unknown>
  ai: Record<string, unknown>
}

export interface Deliveries {
  window: Window
  total: number
  by_layer: Partial<Record<Layer, number>>
  by_result: Record<string, number>
  labels: Record<string, string>
  failures: TrafficRow[]
  rows: TrafficRow[]
  interaction_events: { connected: boolean; note: string }
}

export interface BackendView {
  name: string
  layer: Layer
  requests: number
  ok: number
  failed: number
  unconfirmed: number
  last_seen: string
}

export interface AnalysisItem {
  event_id: string
  at: string
  kind: string
  accepted: boolean
  data?: unknown
  rejected_reason?: string
  analyzed: number
  evidence_ids?: string[]
}

export interface AlertItem extends TrafficRow {
  reasons: string[]
  level: 'critical' | 'warning' | 'info'
}

export interface UserView {
  username: string
  role: Role
  must_change: boolean
  disabled: boolean
  created_at: string
  updated_at: string
  last_login_at?: string
}

export interface AuditEntry {
  at: string
  actor: string
  role: string
  action: string
  target: string
  result: string
  source: string
  detail?: string
}

export interface SystemStatus {
  core_reachable: boolean
  core_error: string
  streams_active: number
  streams_max: number
  geo_db_built_at: number
  alert_score: number
  server_time: string
  interaction_events: { connected: boolean; note: string }
}

export interface EventView {
  event_id: string
  type: string
  created_at: string
  flow?: FlowRecord
  raw?: unknown
}

export interface SseFrame {
  kind: 'event' | 'status' | 'closed'
  view?: EventView
  row?: TrafficRow
  dropped?: number
  buffered?: number
  capacity?: number
  error?: string
  code?: string
}

// ── 大模型分析（/api/v1/llm/*）────────────────────────────────────────────
export interface LLMTestResult {
  at: string
  ok: boolean
  model: string
  latency_ms: number
  error?: string
  reply?: string
}
export interface LLMProvider {
  id: string
  name: string
  base_url: string
  models: string[]
  default_model: string
  /** 脱敏提示（sk-****末4位）；完整密钥永远不会下发到前端 */
  key_hint: string
  enabled: boolean
  note?: string
  version: number
  created_by: string
  created_at: string
  updated_at: string
  last_test?: LLMTestResult
}
export interface LLMProviderInput {
  name: string
  base_url: string
  /** 为空表示不修改密钥（仅编辑时） */
  api_key: string
  models: string[]
  default_model: string
  enabled: boolean
  note: string
  version?: number
}
/** 「获取模型列表」探测：按已填的地址与密钥（或既有提供方保存的密钥）拉取上游 /models。 */
export interface LLMModelProbeInput {
  base_url: string
  api_key?: string
  provider_id?: string
}
export interface LLMModelProbeResult {
  models: string[]
}
export interface LLMUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated?: boolean
}
export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  at: string
  model?: string
  usage?: LLMUsage
  latency_ms?: number
  truncated?: boolean
  error?: string
}
export interface Conversation {
  id: string
  title: string
  provider_id: string
  provider_name: string
  model: string
  decision_ids: string[]
  redact_ip: boolean
  context: string
  messages: ChatMessage[]
  total_tokens: number
  created_at: string
  updated_at: string
}
export interface ConversationSummary {
  id: string
  title: string
  provider_name: string
  model: string
  traffic: number
  messages: number
  total_tokens: number
  updated_at: string
}
export interface UsageTotals {
  requests: number
  failed: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated_requests: number
}
export interface UsageRecord extends LLMUsage {
  at: string
  user: string
  provider_id: string
  provider_name: string
  model: string
  kind: 'test' | 'chat'
  conversation_id?: string
  latency_ms: number
  ok: boolean
  error?: string
}
export interface UsageSummary {
  window_start: string
  records: number
  totals: UsageTotals
  by_model: (UsageTotals & { provider_id: string; provider_name: string; model: string; last_at: string })[]
  by_day: (UsageTotals & { date: string })[]
  by_user?: (UsageTotals & { user: string })[]
  recent: UsageRecord[]
}
export interface UsageResponse {
  summary: UsageSummary
  scope: 'all' | 'self'
  in_flight: number
}
