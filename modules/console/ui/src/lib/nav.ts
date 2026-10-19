import type { Component } from 'vue'
import {
  Activity,
  BellRing,
  BrainCircuit,
  Cable,
  Castle,
  Network,
  Radar,
  Settings2,
  SlidersHorizontal,
} from 'lucide-vue-next'
import type { Permission } from './types'

export interface NavItem {
  name: string
  /** 侧栏与页面标题共用同一个字符串 —— 两处各写一份必然漂移。 */
  label: string
  /** 侧栏副标题：说清「这一页能做什么」，并保留内部叫法以便对照文档。 */
  desc: string
  /** 分组名（见 `navGroups`）。分组是**展示顺序**的依据，不参与权限。 */
  group: NavGroup
  icon: Component
  perm: Permission
}

/**
 * 导航分组：按**使用顺序**排列 —— 先看全局，再接业务，然后看运行、做治理，最后是设置。
 *
 * 为什么要有分组：九个平铺标签里，「判定与流量」和「策略与数据集」是两件事（一个看、一个改），
 * 平铺时读起来像同一层的两个「欺骗」，用户要先点进去才知道区别。分组把「看 / 接 / 跑 / 控 / 设」
 * 这五件事直接摆在侧栏上。
 */
export type NavGroup = '概览' | '接入' | '运行观测' | '策略与治理' | '系统'

export const navGroups: NavGroup[] = ['概览', '接入', '运行观测', '策略与治理', '系统']

/**
 * 导航项（**顺序即展示顺序**，也是 `firstAllowed()` 的兜底依据）。
 *
 * 命名约定：
 * - 对外用**动作/对象**说话（服务登记、判定与流量、策略与数据集），而不是内部层次代号（L1/欺骗层）；
 * - 内部叫法保留在 `desc` 里（「反向链接器」「欺骗层」「蜜罐层」「欺骗管控」），文档与代码注释可以对照。
 */
export const navItems: NavItem[] = [
  {
    name: 'overview',
    label: '总览',
    desc: '新流量、来源与归属地',
    group: '概览',
    icon: Activity,
    perm: 'overview:read',
  },
  {
    name: 'services',
    label: '服务登记',
    desc: '登记被保护的 Web 服务（反向链接器）',
    group: '接入',
    icon: Network,
    perm: 'registry:read',
  },
  {
    name: 'connectors',
    label: '接入管理',
    desc: '反向隧道连接器与凭证',
    group: '接入',
    icon: Cable,
    perm: 'registry:read',
  },
  {
    name: 'deception',
    label: '判定与流量',
    desc: '核心判定、逐请求链路与策略快照（欺骗层）',
    group: '运行观测',
    icon: Radar,
    perm: 'deception:read',
  },
  {
    name: 'honeypot',
    label: '蜜罐与诱饵',
    desc: '投递结果与后端健康（蜜罐层）',
    group: '运行观测',
    icon: Castle,
    perm: 'honeypot:read',
  },
  {
    name: 'config',
    label: '策略与数据集',
    desc: '蜜罐池、诱饵、黑白名单、注入与版本（欺骗管控）',
    group: '策略与治理',
    icon: SlidersHorizontal,
    perm: 'config:read',
  },
  {
    name: 'alerts',
    label: '告警',
    desc: '需要关注的请求',
    group: '策略与治理',
    icon: BellRing,
    perm: 'alerts:read',
  },
  {
    name: 'analysis',
    label: '分析',
    desc: '模型对话、用量与近线结论',
    group: '策略与治理',
    icon: BrainCircuit,
    perm: 'analysis:read',
  },
  {
    name: 'system',
    label: '系统',
    desc: '主题、账号、审计与大模型接入',
    group: '系统',
    icon: Settings2,
    perm: 'self:manage',
  },
]

// 每项都绑定一个权限 —— 看不到的标签就是没权限（前端只是隐藏，后端同样拒绝）。

/** 页面标题的唯一来源：路由 meta 与页面头部都用它，避免同一页出现两个名字。 */
export function titleOf(name: string): string {
  return navItems.find((i) => i.name === name)?.label ?? '管控台'
}

/** 按分组切好的导航（空分组不返回）—— 侧栏只消费这个结构。 */
export function groupedNav(can: (perm: Permission) => boolean): { group: NavGroup; items: NavItem[] }[] {
  return navGroups
    .map((group) => ({ group, items: navItems.filter((i) => i.group === group && can(i.perm)) }))
    .filter((g) => g.items.length > 0)
}
