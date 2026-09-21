import { defineConfig, devices } from '@playwright/test'

const clientPort = 5173

// Layer 1 of ADR 0002: the Client runs in a real browser against a Fake Daemon.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: process.env['CI'] ? 1 : 0,
  reporter: process.env['CI'] ? 'github' : 'list',
  // Every worker is a browser (4-6 processes) plus a Fake Daemon. The default
  // of half the cores saturates a laptop and makes the timing-sensitive tests
  // flaky; four keeps the machine usable. Override with --workers.
  workers: process.env['CI'] ? undefined : 4,
  use: {
    baseURL: `http://127.0.0.1:${clientPort}`,
    // Tracing screenshots every step; locally it is opt-in (PW_TRACE=1).
    trace: process.env['CI'] || process.env['PW_TRACE'] ? 'retain-on-failure' : 'off',
  },
  webServer: {
    command: `pnpm vite --host 127.0.0.1 --port ${clientPort}`,
    url: `http://127.0.0.1:${clientPort}`,
    reuseExistingServer: !process.env['CI'],
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'phone', use: { ...devices['Pixel 7'] } },
  ],
})
