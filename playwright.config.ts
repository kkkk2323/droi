import { defineConfig, devices } from '@playwright/test'

const clientPort = 5173

// Layer 1 of ADR 0002: the Client runs in a real browser against a Fake Daemon.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: process.env['CI'] ? 1 : 0,
  reporter: process.env['CI'] ? 'github' : 'list',
  use: {
    baseURL: `http://127.0.0.1:${clientPort}`,
    trace: 'retain-on-failure',
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
