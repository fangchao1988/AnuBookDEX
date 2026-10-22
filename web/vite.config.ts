import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    allowedHosts: ['localhost', '127.0.0.1', 'yilecai.top','stopped-yield-untruth.ngrok-free.dev'],
    port: 5173,
    proxy: {
      // 本地只起一个引擎（Anubis 在 9000）。双链并存的路径前缀分隔在
      // 生产环境由 nginx 完成（/ → Aleo, /anubis/ → Anubis），本地都打到 9000。
      //
      // Anubis 引擎（去掉 /anubis 前缀后转发，与 nginx 行为一致）
      '/anubis/ws': { target: 'http://localhost:9000', ws: true, rewrite: (p) => p.replace(/^\/anubis/, '') },
      '/anubis': { target: 'http://localhost:9000', rewrite: (p) => p.replace(/^\/anubis/, '') },
      // 后端引擎：WS 行情（internal/dex/ws，http.port 9000）
      '/ws': { target: 'http://localhost:9000', ws: true },
      // Aleo 链下订单通道（Phase 2b）
      '/order': { target: 'http://localhost:9000' },
      '/api': { target: 'http://localhost:9000' },
    },
  },
})
