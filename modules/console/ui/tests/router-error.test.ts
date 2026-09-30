import { describe, expect, it } from 'vitest'
import { isChunkLoadError } from '../src/router'

// 「点了没反应」的一大根因：路由组件是懒加载的，chunk 拉取失败时 vue-router 会**静默取消**导航
// （URL 与页面都停在原处）。这里锁住判定函数：哪些错误算「资源变了，值得自动重载一次」。
describe('chunk 加载失败的识别', () => {
  it('认得各浏览器对动态 import 失败的措辞', () => {
    expect(isChunkLoadError(new TypeError('Failed to fetch dynamically imported module: http://x/a.js'))).toBe(true)
    expect(isChunkLoadError(new Error('error loading dynamically imported module'))).toBe(true)
    expect(isChunkLoadError(new Error('Importing a module script failed.'))).toBe(true)
  })

  it('认得 Vite 重新预打包依赖时的 504', () => {
    expect(isChunkLoadError(new Error('Outdated Optimize Dep'))).toBe(true)
  })

  it('普通错误不触发重载（别把用户正在填的表单刷掉）', () => {
    expect(isChunkLoadError(new Error('Network Error'))).toBe(false)
    expect(isChunkLoadError(undefined)).toBe(false)
  })
})
