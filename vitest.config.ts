import { resolve } from 'node:path'
import { defineConfig } from 'vitest/config'

const desktop = resolve(import.meta.dirname, 'apps/desktop')

export default defineConfig({
  resolve: {
    alias: {
      '@': resolve(desktop, 'src/renderer/src'),
      '@shared': resolve(desktop, 'src/shared'),
    },
  },
  test: {
    include: ['packages/*/src/**/*.test.{ts,tsx}', 'apps/*/src/**/*.test.{ts,tsx}'],
  },
})
