import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// 后端（Gin）默认监听 :8080，且没有开启 CORS，
// 因此开发环境统一走 Vite 代理，把 /api 转发到后端，前端代码里只写相对路径 /api。
// 代理目标兼容两个变量名：VITE_API_PROXY_TARGET（沿用既有约定）/ VITE_API_TARGET。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.VITE_API_PROXY_TARGET || env.VITE_API_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      host: '127.0.0.1',
      port: Number(env.VITE_PORT || 5173),
      strictPort: false,
      proxy: {
        '/api': {
          target,
          changeOrigin: true,
        },
      },
    },
    build: {
      outDir: 'dist',
      sourcemap: false,
      chunkSizeWarningLimit: 900,
    },
  }
})
