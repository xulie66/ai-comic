import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// 管理后台是独立前端（前后端分离）：自己一份入口、自己一份产物，落在 public/admin，
// 由 Node 服务以 /admin/ 静态托管，接口全走 /api/admin/*。
export default defineConfig({
  base: '/admin/',
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) }
  },
  build: {
    outDir: '../web/public/admin',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      input: fileURLToPath(new URL('./admin.html', import.meta.url))
    }
  }
})
