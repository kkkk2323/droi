import { resolve } from 'path'
import { defineConfig } from 'electron-vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const apiPort = Number(process.env['DROID_APP_API_PORT'] || 3001)
const apiTarget = `http://127.0.0.1:${Number.isFinite(apiPort) && apiPort > 0 ? apiPort : 3001}`

export default defineConfig({
  main: {
    build: {
      externalizeDeps: true,
      rollupOptions: {
        external: ['original-fs']
      }
    }
  },
  preload: {
    build: {
      externalizeDeps: true,
    }
  },
  renderer: {
    root: resolve('src/renderer'),
    server: {
      host: true,
      port: 5173,
      fs: {
        allow: [resolve('.')]
      },
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
          xfwd: true,
        },
        '/mobile': {
          target: apiTarget,
          changeOrigin: true,
          xfwd: true,
        },
      },
    },
    build: {
      rollupOptions: {
        input: resolve('src/renderer/index.html')
      }
    },
    resolve: {
      alias: {
        '@/': resolve('src/renderer/src') + '/'
      }
    },
    plugins: [
      tailwindcss(),
      react({
        babel: {
          plugins: [
            ['babel-plugin-react-compiler', { target: '19' }]
          ]
        }
      })
    ]
  }
})
