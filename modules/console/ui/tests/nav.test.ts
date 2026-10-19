// 导航信息架构的**结构约束**（不测渲染，只测「分类与顺序」这些会被悄悄改坏的东西）。
//
// 为什么需要它：侧栏顺序决定「用户先看到什么」，页面标题决定「他以为自己在哪」。
// 这两件事各写一份必然漂移（曾经 Overview 页头写「流量总览」而侧栏写「总览」）。

import { describe, expect, it } from 'vitest'
import { groupedNav, navGroups, navItems, titleOf } from '@/lib/nav'
import { router } from '@/router'

const ALL = () => true

describe('导航分组与顺序', () => {
  it('顺序即使用顺序：概览 → 接入 → 运行观测 → 策略与治理 → 系统', () => {
    expect(navGroups).toEqual(['概览', '接入', '运行观测', '策略与治理', '系统'])
  })

  it('每一组都非空，且分组顺序与 navGroups 一致', () => {
    const groups = groupedNav(ALL)
    expect(groups.map((g) => g.group)).toEqual(navGroups)
    for (const g of groups) {
      expect(g.items.length).toBeGreaterThan(0)
    }
  })

  it('「看」与「改」不在同一组：判定/蜜罐属于运行观测，策略属于策略与治理', () => {
    const byName = new Map(navItems.map((i) => [i.name, i]))
    expect(byName.get('deception')?.group).toBe('运行观测')
    expect(byName.get('honeypot')?.group).toBe('运行观测')
    expect(byName.get('config')?.group).toBe('策略与治理')
  })

  it('接入相关两项相邻（服务是父、连接器是子，不能隔着别的页）', () => {
    const names = navItems.map((i) => i.name)
    expect(names.indexOf('connectors')).toBe(names.indexOf('services') + 1)
  })

  it('第一项是总览（无权限时的兜底页也依赖它）', () => {
    expect(navItems[0]?.name).toBe('overview')
  })

  it('每个导航项都有对应的路由，且标题来自同一来源', () => {
    for (const item of navItems) {
      const route = router.getRoutes().find((r) => r.name === item.name)
      expect(route, `缺少路由：${item.name}`).toBeTruthy()
      expect(route?.meta.title).toBe(item.label)
      expect(titleOf(item.name)).toBe(item.label)
    }
  })

  it('名称与权限不重复（重复权限会让两个页面在同一角色下都出现，容易误导）', () => {
    const names = navItems.map((i) => i.name)
    expect(new Set(names).size).toBe(names.length)
    // registry:read 覆盖「服务登记」与「接入管理」是有意的（同一角色的两件事），
    // 其余权限各自唯一。
    const perms = navItems.map((i) => i.perm).filter((p) => p !== 'registry:read')
    expect(new Set(perms).size).toBe(perms.length)
  })

  it('描述里保留内部叫法，便于对照文档（服务登记=反向链接器 / 判定与流量=欺骗层 …）', () => {
    const byName = new Map(navItems.map((i) => [i.name, i]))
    expect(byName.get('services')?.desc).toContain('反向链接器')
    expect(byName.get('deception')?.desc).toContain('欺骗层')
    expect(byName.get('honeypot')?.desc).toContain('蜜罐层')
    expect(byName.get('config')?.desc).toContain('欺骗管控')
  })

  it('无权限时分组为空（不渲染空标题）', () => {
    expect(groupedNav(() => false)).toEqual([])
  })
})

// ── 渲染后再验一次顺序（数据模型对不代表 DOM 顺序对：模板里一个 v-for 嵌套改错就乱了）──
import { mount } from '@vue/test-utils'
import { vi } from 'vitest'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ can: () => true }),
}))
vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => ({ name: 'overview' }) }
})

describe('侧栏渲染顺序', () => {
  it('DOM 里的分组与链接顺序与 navItems 一致', async () => {
    const SideNav = (await import('@/layouts/SideNav.vue')).default
    const wrapper = mount(SideNav, {
      global: {
        stubs: {
          // 只为占位：真实 RouterLink 依赖 router 实例，这里只关心顺序与文案。
          RouterLink: { template: '<a><slot /></a>' },
        },
      },
    })

    const groups = wrapper.findAll('nav > div > p').map((n) => n.text())
    expect(groups).toEqual(navGroups)

    const labels = wrapper.findAll('nav a span.font-medium').map((n) => n.text())
    expect(labels).toEqual(navItems.map((i) => i.label))
  })
})
