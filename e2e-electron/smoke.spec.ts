// Layer 2 of ADR 0002: the real Desktop Shell, launched by Playwright, with a
// stand-in `droid` binary. At most five cases; product behaviour lives in e2e/.
import {
  expect,
  test,
  _electron as electron,
  type ElectronApplication,
  type Page,
} from '@playwright/test'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { networkInterfaces, tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

let app: ElectronApplication
let page: Page
let userData: string

test.beforeAll(async () => {
  userData = mkdtempSync(join(tmpdir(), 'droi-smoke-'))
  // The token is regenerated on first load; only the droid override matters.
  writeFileSync(
    join(userData, 'settings.json'),
    JSON.stringify({
      remoteAccess: false,
      droidPath: resolve(import.meta.dirname, 'fake-droid'),
      factoryApiBaseUrl: 'http://127.0.0.1:1/smoke-proxy',
    }),
  )
  app = await electron.launch({
    args: [resolve(import.meta.dirname, '../out/main/index.js')],
    env: {
      ...process.env,
      DROI_USER_DATA_DIR: userData,
      FACTORY_API_KEY: 'fk-smoke',
      FAKE_DROID_ENV_FILE: join(userData, 'daemon-env.json'),
      NODE_ENV: 'production',
    },
  })
  page = await app.firstWindow()
})

test.afterAll(async () => {
  await app?.close()
  rmSync(userData, { recursive: true, force: true })
})

test.describe.configure({ mode: 'serial' })

test('the window opens and shows the Client served by the Gateway', async () => {
  await expect(page.getByRole('navigation', { name: 'Sessions' })).toBeVisible()
  expect(page.url()).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/?/)
  const hasBridge = await page.evaluate(() =>
    Boolean((window as unknown as { droiShell?: { settings?: unknown } }).droiShell?.settings),
  )
  expect(hasBridge).toBe(true)
})

test('the Gateway answers /meta', async () => {
  const response = await fetch(new URL('/meta', page.url()))
  expect(response.status).toBe(200)
  expect(await response.json()).toEqual({
    app: 'Droi',
    version: expect.any(String),
    remoteAccess: false,
  })
})

test('the Local Client connects to the Daemon the Shell spawned with its settings', async () => {
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/, {
    timeout: 20_000,
  })
  expect(JSON.parse(readFileSync(join(userData, 'daemon-env.json'), 'utf8'))).toEqual({
    FACTORY_API_KEY: 'fk-smoke',
    FACTORY_API_BASE_URL: 'http://127.0.0.1:1/smoke-proxy',
  })
})

test('toggling Remote Access binds and unbinds the LAN addresses', async () => {
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: 'Remote Access' }).click()
  if (process.env['DROI_SCREENSHOT_DIR']) {
    await page.screenshot({ path: `${process.env['DROI_SCREENSHOT_DIR']}/settings.png` })
  }
  const toggle = page.getByRole('switch', { name: 'Remote Access' })
  await expect(toggle).not.toBeChecked()

  await toggle.check()
  await expect(toggle).toBeChecked()
  const port = Number(new URL(page.url()).port)
  const lan = Object.values(networkInterfaces())
    .flat()
    .filter((e) => e && e.family === 'IPv4' && !e.internal)
    .map((e) => e!.address)
  expect((await (await fetch(new URL('/meta', page.url()))).json()).remoteAccess).toBe(
    lan.length > 0,
  )
  if (lan[0]) {
    const viaLan = await fetch(`http://${lan[0]}:${port}/meta`)
    expect(viaLan.status).toBe(200)
  }

  await toggle.uncheck()
  await expect(toggle).not.toBeChecked()
  if (lan[0]) await expect(fetch(`http://${lan[0]}:${port}/meta`)).rejects.toThrow()
  // The Local Client's own bridge survived the rebinds.
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
})

test('the pairing QR renders and resetting changes the link', async () => {
  const toggle = page.getByRole('switch', { name: 'Remote Access' })
  await toggle.check()
  const lanCount = Object.values(networkInterfaces())
    .flat()
    .filter((e) => e && e.family === 'IPv4' && !e.internal).length
  test.skip(lanCount === 0, 'no LAN interface on this machine')

  const qr = page.getByRole('img', { name: 'Pairing QR code' })
  await expect(qr).toBeVisible()
  await expect(qr.locator('svg')).toHaveCount(1)
  const link = page.getByLabel('Pairing link')
  const before = await link.textContent()
  expect(before).toMatch(/^http:\/\/\d+\.\d+\.\d+\.\d+:\d+\/#pair=[A-Za-z0-9_-]{32}$/)

  await page.getByRole('button', { name: 'Reset pairing token' }).click()
  await expect(link).not.toHaveText(before!)
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
  await toggle.uncheck()
})
