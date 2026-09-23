import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import { clientConfig } from './vite.config'

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    // Electron's own module for the unpatched fs; not a package to bundle.
    build: { rollupOptions: { external: ['original-fs'] } },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: clientConfig,
})
