import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// 新前端整体挂在 /ui/ 下，构建产物直接落到 Node 服务的 public/ui，
// 这样老向导（/）和新界面（/ui/）可以并存，逐步切换，不用停机。
export default defineConfig({
  base: '/ui/',
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) }
  },
  build: {
    outDir: '../web/public/ui',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200
  },
  server: {
    port: 5273,
    proxy: { '/api': { target: 'http://127.0.0.1:5210', changeOrigin: true } }
  }
})
