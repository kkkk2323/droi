import { defineConfig } from '@playwright/test'

// Layer 2 of ADR 0002: launches the built Desktop Shell. Run `pnpm build` first.
export default defineConfig({
  testDir: './e2e-electron',
  workers: 1,
  timeout: 60_000,
  reporter: process.env['CI'] ? 'github' : 'list',
  use: { trace: 'retain-on-failure' },
})
