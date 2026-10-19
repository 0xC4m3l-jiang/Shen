// 导航预览页（**本地截图用**，不参与构建产物，也不入库）：真实组件 + 真实样式，只把会话换成假的。
//
// 为什么需要它：侧栏的视觉核对必须看真实渲染 —— 单测只能保证 DOM 顺序，保证不了「分组标题是否挤、
// 收起时是否还有分组感」。这个页面用真 `SideNav` + 真 `index.css`，只把 `auth.session` 换成一个
// 全权限的假会话，于是不需要后端也能看到完整的九个入口。
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory, RouterLink } from 'vue-router'
import '@/index.css'
import SideNav from '@/layouts/SideNav.vue'
import AuroraBackground from '@/components/fx/AuroraBackground.vue'
import { navItems } from '@/lib/nav'
import { useAuthStore } from '@/stores/auth'

// 假会话：权限取全部导航项所需的集合（只影响「显示哪些标签」）。
const permissions = Array.from(new Set(navItems.map((i) => i.perm)))

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'overview', component: { render: () => h('div') } },
    ...navItems.map((i) => ({ path: `/${i.name}`, name: i.name, component: { render: () => h('div') } })),
  ],
})

const app = createApp({
  setup() {
    const auth = useAuthStore()
    auth.apply({
      user: { username: 'preview', role: 'admin', must_change: false },
      permissions,
      csrf_token: 'preview',
    } as never)
    return () => h('div', { class: 'relative min-h-screen' }, [
      h('div', { class: 'fixed inset-0 -z-10 overflow-hidden' }, [
        h('div', { class: 'grid-floor absolute inset-0' }),
        h(AuroraBackground, { intensity: 1 }),
      ]),
      h('div', { class: 'flex pt-16' }, [
        h(SideNav),
        h('main', { class: 'mx-auto w-full max-w-5xl px-6 py-8' }, [
          h('h1', { class: 'text-xl font-semibold' }, '导航预览'),
          h(
            'p',
            { class: 'mt-2 text-sm text-muted-foreground' },
            '这一页只用于本地截图核对侧栏分组与顺序（真实组件 + 真实样式，会话是假的）。',
          ),
        ]),
      ]),
    ])
  },
})

app.use(createPinia())
app.use(router)
app.component('RouterLink', RouterLink)
app.mount('#nav-preview')
