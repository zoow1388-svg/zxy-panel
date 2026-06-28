import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: process.env.VITE_BASE_PATH || '/',
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://zxy-panel-api:8088',
        changeOrigin: true
      }
    },
    hmr: {
      clientPort: 5173
    }
  }
})
