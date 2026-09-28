import type { GeoLocation, Layer, Role, ThemeId, TrafficRow } from './types'

export const layerMeta: Record<Layer, { label: string; tone: string; hint: string }> = {
  origin: { label: '真实业务', tone: 'origin', hint: '原样透传到被保护的业务' },
  fallback: { label: '回落业务', tone: 'warn', hint: '判定改道但幻境不可用，回落到业务' },
  mirage: { label: '幻境', tone: 'mirage', hint: '评分改道，进入合成幻境' },
  decoy: { label: '诱饵', tone: 'decoy', hint: '命中专属诱饵路由（故障固定 502，不回源）' },
  block: { label: '拦截', tone: 'danger', hint: '返回 403' },
}

export const roleLabels: Record<Role, string> = {
  admin: '管理员',
  deception_operator: '欺骗运维',
  honeypot_operator: '蜜罐运维',
  viewer: '只读',
}

export const themeMeta: Record<ThemeId, { name: string; desc: string; swatch: string[] }> = {
  mirage: { name: '蜃海', desc: '深色：深海蓝底，青色幻境之门', swatch: ['#0A1628', '#22D3EE', '#34D399', '#F0FAFF'] },
  haze: { name: '晨雾', desc: '亮色：白与冰蓝的晨雾海面', swatch: ['#F0F9FF', '#2563EB', '#0891B2', '#059669'] },
}

export const actionLabels: Record<string, string> = {
  route_origin: '放行',
  route_mirage: '改道',
  block: '拦截',
  unknown: '未识别',
}

const nf = new Intl.NumberFormat('zh-CN')
export const fmtNum = (n: number | undefined): string => nf.format(n ?? 0)
export const fmtPct = (r: number | undefined, digits = 1): string => `${((r ?? 0) * 100).toFixed(digits)}%`

export function fmtTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '—'
  return d.toLocaleTimeString('zh-CN', { hour12: false })
}

export function fmtDateTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '—'
  return d.toLocaleString('zh-CN', { hour12: false })
}

/** 相对时间：「12 秒前」「3 分钟前」。 */
export function fmtAgo(iso?: string, now = Date.now()): string {
  if (!iso) return '无数据'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t) || t < 946684800000) return '无数据'
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return `${s} 秒前`
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  return `${Math.floor(s / 86400)} 天前`
}

export function geoText(g?: GeoLocation): string {
  if (!g) return '未知来源'
  const isp = g.isp ? ` · ${g.isp}` : ''
  return (g.label || '未知来源') + (g.scope === 'public' ? isp : '')
}

/** 「是否流入蜃楼」的展示口径（与后端 in_mirage / shadow_mirage 一致）。 */
export function mirageState(row: TrafficRow): 'in' | 'shadow' | 'failed' | 'none' {
  if (row.in_mirage) return 'in'
  if (row.layer === 'decoy' || row.layer === 'fallback') return 'failed'
  if (row.shadow_mirage) return 'shadow'
  return 'none'
}

export const windowOptions = [
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时' },
] as const
export type WindowValue = (typeof windowOptions)[number]['value']

export const deliveryLabels: Record<string, string> = {
  delivered: '已投递',
  backend_unavailable: '后端不可用（502）',
  delivery_failed: '投递中途失败',
  tombstoned: '已撤销（502）',
}
