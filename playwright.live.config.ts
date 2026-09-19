import { defineConfig, devices } from '@playwright/test'

const clientPort = 5175

// Layer 3 of ADR 0002: the Client in Chromium against a real Daemon through the
// real Gateway. Skips unless FACTORY_API_KEY is set.
export default defineConfig({
  testDir: './e2e-live',
  workers: 1,
  timeout: 180_000,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: `http://127.0.0.1:${clientPort}`,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `pnpm vite --host 127.0.0.1 --port ${clientPort}`,
    url: `http://127.0.0.1:${clientPort}`,
    reuseExistingServer: true,
  },
})
