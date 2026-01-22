import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'VITE_')
  
  return {
    plugins: [react()],
    server: {
      allowedHosts: env.VITE_ALLOWED_HOSTS?.split(',').map(h => h.trim()).filter(Boolean) || [],
      port: 3000,
      proxy: {
        '/api': {
          target: 'http://localhost:8888',
          changeOrigin: true,
        },
      },
    },
  }
})
