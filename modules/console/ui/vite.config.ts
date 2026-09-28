/// <reference types="vitest" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from 'tailwindcss'
import autoprefixer from 'autoprefixer'

// 开发期：浏览器 → Vite(5173) → /api 代理到管控台 API。
// 容器内（compose.dev）API 在共享命名空间的 127.0.0.1:9445；宿主机直跑时同样默认 127.0.0.1:9445。
const apiTarget = process.env.SHEN_CONSOLE_API_URL ?? 'http://127.0.0.1:9445'
// Windows / macOS 的绑定挂载下文件事件不可靠：compose.dev 里设 CHOKIDAR_USEPOLLING=true 改为轮询。
const usePolling = process.env.CHOKIDAR_USEPOLLING === 'true'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  // PostCSS 配置写在这里而不是 postcss.config.js：仓库的语言层数门禁（archcheck TB-21）不接受 .js 源文件。
  css: {
    postcss: { plugins: [tailwindcss(), autoprefixer()] },
  },
  server: {
    host: '0.0.0.0',
    port: Number(process.env.SHEN_VITE_PORT ?? 5173),
    strictPort: true,
    allowedHosts: true,
    watch: usePolling ? { usePolling: true, interval: 300 } : undefined,
    proxy: {
      // changeOrigin=false：保持浏览器的 Host，使 API 的同源校验与生产（nginx）一致。
      '/api': { target: apiTarget, changeOrigin: false, ws: false },
      '/healthz': { target: apiTarget, changeOrigin: false },
    },
  },
  build: {
    target: 'es2022',
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks: {
          echarts: ['echarts', 'vue-echarts'],
          vendor: ['vue', 'vue-router', 'pinia'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['tests/**/*.test.ts'],
  },
})
