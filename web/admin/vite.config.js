import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 产物会被 Go 用 embed.FS 挂在 /admin 下，所以 base 必须是 /admin/
export default defineConfig({
  base: '/admin/',
  plugins: [react()],
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          antd: ['antd'],
          echarts: ['echarts']
        }
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      // 本地开发时把接口打到后端
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true }
    }
  }
})
