import { defineConfig, devices } from '@playwright/test'

const phonePort = 5174

// ADR 0006: the Phone App's web build (react-native-web) in a phone-sized
// browser against the same Fake Daemon as the web Client's suite. The build
// exists only for these tests; native hardware is replaced by web stand-ins.
export default defineConfig({
  testDir: '.',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: process.env['CI'] ? 1 : 0,
  reporter: process.env['CI'] ? 'github' : 'list',
  workers: process.env['CI'] ? undefined : 4,
  use: {
    baseURL: `http://localhost:${phonePort}`,
    trace: process.env['CI'] || process.env['PW_TRACE'] ? 'retain-on-failure' : 'off',
  },
  webServer: {
    command: `pnpm --filter @droi/mobile exec expo export --platform web && pnpm --filter @droi/mobile exec expo serve --port ${phonePort}`,
    url: `http://localhost:${phonePort}`,
    reuseExistingServer: !process.env['CI'],
    timeout: 240_000,
  },
  projects: [
    {
      name: 'phone-app',
      // The iPhone's size and touch, in the Chromium the CI installs.
      use: { ...devices['iPhone 15'], defaultBrowserType: 'chromium' },
    },
  ],
})
