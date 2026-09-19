import { resolve } from 'node:path'
import { defineConfig, type UserConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// One Client build serves both the Local Client (bundled by electron-vite) and
// the Remote Client (served to a browser by the Gateway), so the renderer
// config lives here and electron.vite.config.ts reuses it.
export const clientConfig: UserConfig = {
  root: resolve(import.meta.dirname, 'src/renderer'),
  resolve: {
    alias: {
      '@': resolve(import.meta.dirname, 'src/renderer/src'),
      '@shared': resolve(import.meta.dirname, 'src/shared'),
    },
  },
  plugins: [tailwindcss(), react()],
  server: {
    port: 5173,
    strictPort: true,
  },
}

export default defineConfig(clientConfig)
