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
      { path: 'config', name: 'config', component: () => import('@/views/ConfigView.vue'), meta: { perm: 'config:read', title: '欺骗管控' } },
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

// 路由组件都是懒加载（`() => import(...)`）。动态 import 失败时 vue-router 会**静默取消**这次导航：
// URL 与页面都停在原处 —— 用户看到的就是「点了没反应」，而且控制台常常什么也没有。
// 常见触发：① 开发时 Vite 重新预打包依赖（504 Outdated Optimize Dep）；② 部署后旧标签页请求已不存在的 chunk。
// 处置：把失败**说清楚**（控制台 + 提示），并自动整页重载一次去拿新的 chunk 图；
// 只重载一次（sessionStorage 闸门，导航成功即解除），避免服务器真的坏掉时无限刷新。
const CHUNK_RELOAD_FLAG = 'shen:chunk-load-retry'

export function isChunkLoadError(err: unknown): boolean {
  const msg = String((err as Error)?.message ?? err)
  return /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Outdated Optimize Dep/i.test(msg)
}

router.afterEach((to) => {
  document.title = `${to.meta.title ?? '管控台'} · 蜃楼 Shen`
  sessionStorage.removeItem(CHUNK_RELOAD_FLAG) // 导航成功 ⇒ 解除「只自动重载一次」的闸
})

// 自动重载救不了时的兜底提示：直接插一个横幅（本项目没有全局 toast，且 alert 会**阻塞**页面 —— 
// 自动化巡检遇到它会挂住）。只插一次，刷新后自然消失。
function showStaleHint(): void {
  if (document.getElementById('shen-stale-hint')) return
  const el = document.createElement('div')
  el.id = 'shen-stale-hint'
  el.setAttribute('role', 'alert')
  el.style.cssText =
    'position:fixed;z-index:9999;left:50%;top:16px;transform:translateX(-50%);max-width:36rem;' +
    'padding:10px 16px;border-radius:12px;background:#1f2937;color:#f9fafb;font-size:13px;' +
    'box-shadow:0 10px 30px rgb(0 0 0 / 25%)'
  el.textContent = '页面资源已更新：请刷新浏览器（⌘/Ctrl + R）后重试。'
  document.body.append(el)
}

router.onError((err, to) => {
  if (!isChunkLoadError(err)) return
  console.error(`[路由] 加载「${to.meta.title ?? to.fullPath}」失败：${String((err as Error)?.message ?? err)}`, err)
  if (sessionStorage.getItem(CHUNK_RELOAD_FLAG)) {
    showStaleHint()
    return
  }
  sessionStorage.setItem(CHUNK_RELOAD_FLAG, '1')
  window.location.reload()
})
