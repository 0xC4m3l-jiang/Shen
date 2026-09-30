// 欺骗管控数据集（/api/v1/config/*）的类型 —— 与 Go 侧 internal/deception 一一对应（snake_case）。
// 改字段必须两侧同步。

export type Domain = 'honeypots' | 'decoys' | 'whitelist' | 'blacklist' | 'injects' | 'bindings'

export interface Honeypot {
  name: string
  type: string
  addr: string
  enabled: boolean
  description?: string
  template_id?: string
}

export interface DecoyAsset {
  id: string
  kind: string
  path: string
  hosts: string[]
  content: string
  backend: string
  enabled: boolean
  template_id?: string
  note?: string
}

export interface Whitelist {
  source_cidrs: string[]
  user_agents: string[]
  path_prefixes: string[]
}

export interface BlackRule {
  id: string
  path_prefix: string
  reason: string
}

export interface Inject {
  kind: string
  snippet: string
  marker: string
}

export interface Binding {
  service_id: string
  decoy_ids: string[]
}

export interface Dataset {
  version: number
  projection_rev: number
  projection_digest: string
  initialized: boolean
  seed_source?: string
  updated_at: string
  updated_by: string
  summary?: string
  honeypots: Honeypot[]
  decoys: DecoyAsset[]
  whitelist: Whitelist
  blacklist: BlackRule[]
  injects: Inject[]
  injects_provided: boolean
  bindings: Binding[]
}

export interface Issue {
  domain: Domain
  field: string
  reason: string
}

export interface Report {
  errors: Issue[]
  warnings: Issue[]
}

export interface ProjDecoy {
  id: string
  kind: string
  path: string
  hosts: string[]
  backend: string
  enabled: boolean
}

export interface EdgeAck {
  adapter_id: string
  version: number
  applied: boolean
  reason?: string
  received_at: string
}

export interface HoneypotProbe {
  name: string
  addr: string
  healthy: boolean
  latency_ms: number
  error?: string
  checked_at: string
}

export type SyncState = 'synced' | 'pending' | 'error' | 'offline' | 'unconfigured'

export interface SyncStatus {
  configured: boolean
  dataset_rev: number
  core_rev: number
  policy_version: number
  edge_min_version: number
  edge_in_sync: number
  edge_total: number
  applied: boolean
  reason?: string
  source?: string
  last_pull_at?: string
  last_report_at?: string
  edge_acks: EdgeAck[]
  honeypots: HoneypotProbe[]
  state: SyncState
}

export interface SeedOutcome {
  source: string
  seeded: boolean
  missing: boolean
  drifted: boolean
  error?: string
  initialized: boolean
}

export interface ServiceRef {
  id: string
  name: string
  hosts: string[]
  enabled: boolean
}

export interface DatasetView {
  dataset: Dataset
  report: Report
  projection: { rev: number; digest: string; decoys: ProjDecoy[]; scopes: Binding[] | null }
  sync: SyncStatus
  seed: SeedOutcome
  services: ServiceRef[]
}

export interface VersionMeta {
  version: number
  updated_at: string
  updated_by: string
  summary: string
}

export interface HoneypotTemplate {
  id: string
  type: string
  name: string
  default_port: number
  suggested_name: string
  description: string
  decoy_templates: string[]
}

export interface DecoyTemplate {
  id: string
  kind: string
  name: string
  path: string
  content: string
  honeypot_types: string[]
  description: string
}

export interface InjectTemplate {
  id: string
  kind: string
  name: string
  snippet: string
  marker: string
  description: string
}

export interface TemplatesView {
  templates: { honeypots: HoneypotTemplate[]; decoys: DecoyTemplate[]; injects: InjectTemplate[] }
  enums: { decoy_kinds: string[]; honeypot_types: string[]; inject_kinds: string[] }
}

/** 总览页「配置同步」卡片（Go 侧 api.configSyncCard）。 */
export interface ConfigSyncCard {
  state: SyncState
  dataset_version: number
  dataset_rev: number
  core_rev: number
  edge_in_sync: number
  edge_total: number
  honeypots_total: number
  honeypots_healthy: number
  initialized: boolean
}

/** 蜜罐层「已登记后端」一行（Go 侧 api.registeredView）。 */
export interface RegisteredBackend {
  name: string
  type: string
  addr: string
  enabled: boolean
  health: 'healthy' | 'unhealthy' | 'unknown' | 'disabled'
  latency_ms: number
  error?: string
  references: number
  requests: number
  idle: boolean
}

export interface SystemAlert {
  id: string
  severity: 'critical' | 'warning'
  title: string
  detail: string
  since: string
}

// ── 展示用的中文标签 ───────────────────────────────────────────────────────

export const decoyKindLabel: Record<string, string> = {
  developer_api: '开发者 API',
  instruction_file: '指令文件',
  mcp: 'MCP 工具',
  dataset: '消耗战数据集',
  bait: '蜜饵',
}

export const honeypotTypeLabel: Record<string, string> = {
  ssh: 'SSH',
  mysql: 'MySQL',
  redis: 'Redis',
  ftp: 'FTP',
  elasticsearch: 'Elasticsearch',
  'nginx-admin': 'Nginx 管理台',
  'web-clone': '站点克隆',
  'internal-wiki': '内部 Wiki',
  database: '数据库网关',
}

export const injectKindLabel: Record<string, string> = {
  developer_api: '开发者 API',
  instruction_file: '指令文件',
  hidden_link: '隐藏链接',
  dataset: '数据集入口',
  '': '未分类',
}

/** 每个域写入所需的权限（与 Go 侧 api.domainPerm 一致）。 */
export const domainPerm: Record<Domain, 'config:honeypot' | 'config:deception'> = {
  honeypots: 'config:honeypot',
  decoys: 'config:deception',
  whitelist: 'config:deception',
  blacklist: 'config:deception',
  injects: 'config:deception',
  bindings: 'config:deception',
}

export const domainLabel: Record<Domain, string> = {
  honeypots: '蜜罐池',
  decoys: '诱饵资产',
  whitelist: '白名单',
  blacklist: '禁止欺骗路径',
  injects: '注入规则',
  bindings: '服务绑定',
}

/** 深拷贝（草稿编辑用；数据都是纯 JSON）。 */
export function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}
