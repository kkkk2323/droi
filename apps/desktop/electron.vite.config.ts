import { resolve } from 'node:path'
import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import { clientConfig } from './vite.config'

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    build: {
      rollupOptions: {
        // The Memory Server runs beside the Shell as its own process (ADR 0010).
        input: {
          index: resolve(import.meta.dirname, 'src/main/index.ts'),
          'memory-server': resolve(import.meta.dirname, 'src/memory-server/index.ts'),
        },
        // Electron's own module for the unpatched fs; not a package to bundle.
        external: ['original-fs'],
      },
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: clientConfig,
})
