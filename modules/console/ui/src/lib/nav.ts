import type { Component } from 'vue'
import { Activity, BellRing, BrainCircuit, Castle, Network, Radar, Settings2 } from 'lucide-vue-next'
import type { Permission } from './types'

export interface NavItem {
  name: string
  label: string
  desc: string
  icon: Component
  perm: Permission
}

// 按功能分类的标签栏：每项都绑定一个权限 —— 看不到的标签就是没权限（前端只是隐藏，后端同样拒绝）。
export const navItems: NavItem[] = [
  { name: 'overview', label: '总览', desc: '新流量 · 来源 · 归属地', icon: Activity, perm: 'overview:read' },
  { name: 'services', label: '反向链接器', desc: '登记被保护的 Web 服务', icon: Network, perm: 'registry:read' },
  { name: 'deception', label: '欺骗层', desc: '判定流 · 链路 · 策略快照', icon: Radar, perm: 'deception:read' },
  { name: 'honeypot', label: '蜜罐层', desc: '幻境 / 诱饵投递', icon: Castle, perm: 'honeypot:read' },
  { name: 'alerts', label: '告警', desc: '需要关注的请求', icon: BellRing, perm: 'alerts:read' },
  { name: 'analysis', label: '分析', desc: '大模型对话 · 用量 · 近线结论', icon: BrainCircuit, perm: 'analysis:read' },
  { name: 'system', label: '系统', desc: '大模型接入 · 账号 · 审计 · 主题', icon: Settings2, perm: 'self:manage' },
]
