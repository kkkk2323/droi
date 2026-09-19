import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import { clientConfig } from './vite.config'

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: clientConfig,
})
