import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { navItems } from '@/lib/nav'
import type { Permission } from '@/lib/types'
import { useAuthStore } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    perm?: Permission
    title?: string
  }
}

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: '登录' } },
  {
    path: '/',
    component: () => import('@/layouts/AppShell.vue'),
    children: [
      { path: '', redirect: { name: 'overview' } },
      { path: 'password', name: 'password', component: () => import('@/views/PasswordView.vue'), meta: { title: '修改口令' } },
      { path: 'overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { perm: 'overview:read', title: '总览' } },
      { path: 'services', name: 'services', component: () => import('@/views/ServicesView.vue'), meta: { perm: 'registry:read', title: '反向链接器' } },
      { path: 'connectors', name: 'connectors', component: () => import('@/views/ConnectorsView.vue'), meta: { perm: 'registry:read', title: '接入管理' } },
      {
        path: 'services/:id',
        name: 'service-detail',
        component: () => import('@/views/ServiceDetailView.vue'),
        meta: { perm: 'registry:read', title: '服务详情' },
      },
      { path: 'deception', name: 'deception', component: () => import('@/views/DeceptionView.vue'), meta: { perm: 'deception:read', title: '欺骗层' } },
      { path: 'honeypot', name: 'honeypot', component: () => import('@/views/HoneypotView.vue'), meta: { perm: 'honeypot:read', title: '蜜罐层' } },
      { path: 'alerts', name: 'alerts', component: () => import('@/views/AlertsView.vue'), meta: { perm: 'alerts:read', title: '告警' } },
      { path: 'analysis', name: 'analysis', component: () => import('@/views/AnalysisView.vue'), meta: { perm: 'analysis:read', title: '分析' } },
      { path: 'system', name: 'system', component: () => import('@/views/SystemView.vue'), meta: { perm: 'self:manage', title: '系统' } },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({ history: createWebHistory(), routes })

/** 当前角色可进入的第一个标签页（没有权限的页面一律改道到这里）。 */
export function firstAllowed(): string {
  const auth = useAuthStore()
  return navItems.find((i) => auth.can(i.perm))?.name ?? 'password'
}

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.meta.public) {
    return auth.session && to.name === 'login' ? { name: firstAllowed() } : true
  }
  if (!auth.session) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : undefined }
  if (auth.mustChange && to.name !== 'password') return { name: 'password' }
  if (to.meta.perm && !auth.can(to.meta.perm)) return { name: firstAllowed() }
  return true
})

router.afterEach((to) => {
  document.title = `${to.meta.title ?? '管控台'} · 蜃楼 Shen`
})
