import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { api } from '@/lib/api'
import type { Preferences, ThemeId } from '@/lib/types'

const themes: ThemeId[] = ['mirage', 'haze']

/**
 * 界面偏好：只有主题（深色 mirage / 亮色 haze）。动效恒开、不提供开关；
 * 唯一例外是操作系统开启了「减少动态效果」—— 那是无障碍设置，页面照样尊重。
 *
 * 数据源是**服务端**（/api/v1/me/preferences）—— 换浏览器、换电脑都是同一套偏好；
 * 本地只负责把它应用到 <html> 上。未登录时（登录页）使用默认主题。
 */
export const usePrefsStore = defineStore('prefs', () => {
  const theme = ref<ThemeId>('mirage')
  const saving = ref(false)
  const systemReduced = typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

  function applyToDocument() {
    const root = document.documentElement
    root.dataset.theme = theme.value
    root.dataset.motion = systemReduced ? 'off' : 'on'
    root.classList.toggle('dark', theme.value === 'mirage')
  }

  function load(p?: Partial<Preferences>) {
    // 历史数据里可能是已下线的主题（aurora）：一律回落到深色
    theme.value = p?.theme && themes.includes(p.theme) ? p.theme : 'mirage'
    applyToDocument()
  }

  /** 切换主题：只在用户**点击**时调用（不做悬停预览，避免鼠标划过就闪屏）。 */
  async function save(next: { theme: ThemeId }): Promise<void> {
    if (next.theme === theme.value) return
    const prev = theme.value
    load({ theme: next.theme }) // 乐观更新：失败再回滚
    saving.value = true
    await api
      .put<Preferences>('/api/v1/me/preferences', { theme: next.theme, motion: true })
      .then((saved) => load(saved))
      .catch((err: unknown) => {
        console.error('保存主题失败，已回滚', err)
        load({ theme: prev })
      })
      .finally(() => (saving.value = false))
  }

  watch(theme, applyToDocument)

  return { theme, saving, systemReduced, load, save, applyToDocument }
})
