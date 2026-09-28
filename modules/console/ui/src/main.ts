import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { onAuthProblem } from './lib/api'
import { router } from './router'
import { useAuthStore } from './stores/auth'
import { usePrefsStore } from './stores/prefs'
import './lib/charts'
import './index.css'

async function boot() {
  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  const auth = useAuthStore()
  const prefs = usePrefsStore()
  prefs.applyToDocument()
  // 首屏前先问后端「我是谁」（Cookie 是 HttpOnly，前端无法自行判断登录态）。
  await auth.bootstrap()
  prefs.load(auth.session?.preferences)

  // 任何接口返回 401 ⇒ 会话已失效；返回 password_change_required ⇒ 去改密。
  onAuthProblem((err) => {
    if (err.code === 'password_change_required') {
      void router.replace({ name: 'password' })
      return
    }
    auth.expire()
    if (router.currentRoute.value.name !== 'login') void router.replace({ name: 'login', query: { reason: 'expired' } })
  })

  app.use(router)
  await router.isReady()
  app.mount('#app')
}

boot().catch((err: unknown) => console.error('管控台启动失败', err))
