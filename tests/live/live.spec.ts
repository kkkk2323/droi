// Layer 3 of ADR 0002: one case against a real `droid daemon`, run only when
// FACTORY_API_KEY is set. It exists to catch protocol drift, not to cover
// features. The Gateway and Daemon supervisor are the real ones from src/main;
// only Electron is missing, the Client runs in Chromium.
//
//   FACTORY_API_KEY=fk-... pnpm test:live
//
// Optional:
//   FACTORY_API_BASE_URL   route the Daemon's Factory traffic through a local
//                          proxy, e.g. droid-proxy: $(dp status | awk '/baseURL/ {print $2}')
//   DROI_LIVE_MODEL        model id to select before sending (default glm-5.3-flash)
//   DROI_LIVE_SHARED_HOME  set to 1 to use the real ~/.factory instead of a
//                          throwaway HOME (then the key must belong to the
//                          user who owns this computer's registration)
import { expect, test } from '@playwright/test'
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DaemonSupervisor } from '../../apps/desktop/src/main/daemon/daemon-supervisor'
import { locateDroid } from '../../apps/desktop/src/main/daemon/locate-droid'
import { startGateway, type Gateway } from '../../apps/desktop/src/main/gateway/gateway'

const apiKey = process.env['FACTORY_API_KEY']
const baseUrl = process.env['FACTORY_API_BASE_URL']
const model = process.env['DROI_LIVE_MODEL'] ?? 'glm-5.3-flash'
const PAIRING_TOKEN = 'live-test-pairing-token'

test.skip(!apiKey, 'FACTORY_API_KEY is not set; the live test only runs with a real key')

let daemon: DaemonSupervisor
let gateway: Gateway
let home: string | null = null
let workspace: string

test.beforeAll(async () => {
  const droidPath = locateDroid()
  if (!droidPath) throw new Error('droid executable not found')
  if (process.env['DROI_LIVE_SHARED_HOME'] !== '1') {
    home = mkdtempSync(join(tmpdir(), 'droi-live-home-'))
  }
  workspace = mkdtempSync(join(tmpdir(), 'droi-live-ws-'))
  writeFileSync(join(workspace, 'README.md'), '# live test workspace\n')

  daemon = new DaemonSupervisor({
    spawn: (port) =>
      spawn(
        droidPath,
        [
          'daemon',
          '--host',
          '127.0.0.1',
          '--port',
          String(port),
          '--parent-pid',
          String(process.pid),
        ],
        {
          stdio: 'ignore',
          env: {
            ...process.env,
            ...(home ? { HOME: home } : {}),
            FACTORY_API_KEY: apiKey!,
            ...(baseUrl ? { FACTORY_API_BASE_URL: baseUrl } : {}),
          },
        },
      ),
  })
  daemon.start()
  gateway = await startGateway({
    port: 0,
    remoteAccess: false,
    getDaemonUrl: () => daemon.daemonUrl,
    getPairingToken: () => PAIRING_TOKEN,
    getCredential: async () => ({ apiKey: apiKey! }),
    getMeta: () => ({
      app: 'Droi',
      version: 'live',
      remoteAccess: false,
      name: 'Live',
      computerId: 'live',
    }),
    client: { kind: 'none' },
  })
})

test.afterAll(async () => {
  await gateway?.close()
  await daemon?.stop()
  rmSync(workspace, { recursive: true, force: true })
  // The Daemon flushes its log for a moment after exiting; sweep until quiet.
  if (home) {
    for (let attempt = 0; attempt < 5; attempt++) {
      await new Promise((r) => setTimeout(r, 300))
      rmSync(home, { recursive: true, force: true })
    }
  }
})

test('pair, create a Session, send a prompt, get a reply', async ({ page }) => {
  test.setTimeout(180_000)
  const fragment = new URLSearchParams({ pair: PAIRING_TOKEN, gateway: gateway.url })
  await page.goto(`/#${fragment.toString()}`)
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/, {
    timeout: 60_000,
  })

  const sidebar = page.getByRole('navigation', { name: 'Sessions' })
  await sidebar.getByRole('button', { name: 'New session' }).click()
  await page.getByRole('textbox', { name: 'Workspace path' }).fill(workspace)
  await page
    .getByRole('region', { name: 'New session' })
    .getByRole('button', { name: 'Start' })
    .click()
  const input = page.getByRole('textbox', { name: 'Message' })
  await expect(input).toBeEnabled({ timeout: 60_000 })

  const modelSelect = page.getByRole('button', { name: 'Model' })
  await modelSelect.click()
  const option = page
    .getByRole('listbox', { name: 'Models' })
    .locator(`[role="option"][data-value="${model}"]`)
  if ((await option.count()) > 0) {
    const label = await option.textContent()
    await option.click()
    await expect(modelSelect).toHaveText(label ?? model)
  } else {
    await page.keyboard.press('Escape')
    console.warn(
      `live: model ${model} not offered by the Daemon; using ${await modelSelect.textContent()}`,
    )
  }

  await input.fill('Reply with exactly the single word: pong')
  await input.press('Enter')
  const transcript = page.getByRole('log', { name: 'Transcript' })
  await expect(transcript.getByRole('article', { name: 'You' }).last()).toContainText('pong')
  await expect(transcript.getByRole('article', { name: 'Assistant' }).last()).toContainText(
    /pong/i,
    {
      timeout: 120_000,
    },
  )
  await expect(page.getByRole('status', { name: 'Session activity' })).toHaveText('', {
    timeout: 60_000,
  })
})
