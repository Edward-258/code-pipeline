import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 本地开发：/api 与 /healthz 代理到集群引擎（生产由 nginx 反代，见 nginx.conf）
const ENGINE = process.env.MP_BASE ?? 'https://pipeline.47.113.177.198.nip.io'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: ENGINE, changeOrigin: true, secure: false },
      '/healthz': { target: ENGINE, changeOrigin: true, secure: false },
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1500, // antd 单包偏大，学习版不做拆包优化
  },
})
