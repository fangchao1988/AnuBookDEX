import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    allowedHosts: ['localhost', '127.0.0.1', 'fangchao1988.xicp.net','stopped-yield-untruth.ngrok-free.dev'],
    port: 5173,
    proxy: {
      // 后端引擎：WS 行情（internal/dex/ws，http.port 9000）
      '/ws': { target: 'http://localhost:9000', ws: true },
      // Aleo 链下订单通道（Phase 2b）
      '/order': { target: 'http://localhost:9000' },
      '/api': { target: 'http://localhost:9000' },
    },
  },
})
