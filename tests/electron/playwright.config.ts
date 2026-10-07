import { defineConfig } from '@playwright/test'

// Deprecated, with the Electron app and web Client it tests (apps/desktop/README.md).
// Layer 2 of ADR 0002: launches the built Desktop Shell. Run `pnpm build` first.
export default defineConfig({
  testDir: '.',
  workers: 1,
  timeout: 60_000,
  reporter: process.env['CI'] ? 'github' : 'list',
  use: { trace: 'retain-on-failure' },
})
