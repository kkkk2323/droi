import { test as base, expect, type Locator, type Page } from '@playwright/test'
import { FakeDaemon } from '../fake-daemon/fake-daemon'
import type { ScenarioInput } from '../fake-daemon/scenario'

export interface OpenClientOptions {
  /** Override the Pairing Token in the link; defaults to the Fake Daemon's. */
  token?: string
}

interface Fixtures {
  fakeDaemon: FakeDaemon
  scenario: ScenarioInput
  /** Open the Client as a Remote Client following a pairing link. */
  openClient: (options?: OpenClientOptions) => Promise<void>
  /** The Sessions sidebar, opening the drawer first on narrow screens. */
  openSidebar: () => Promise<Locator>
  /** Pick a Session from the sidebar (or drawer) by its title. */
  pickSession: (title: RegExp | string) => Promise<void>
}

export const test = base.extend<Fixtures>({
  scenario: [{}, { option: true }],
  fakeDaemon: async ({ scenario }, use) => {
    const daemon = await FakeDaemon.start(scenario)
    await use(daemon)
    await daemon.stop()
  },
  openClient: async ({ page, fakeDaemon }, use) => {
    await use(async (options = {}) => {
      await openPairingLink(page, fakeDaemon, options.token ?? fakeDaemon.token)
    })
  },
  openSidebar: async ({ page }, use) => {
    await use(() => openSidebar(page))
  },
  pickSession: async ({ page }, use) => {
    await use((title) => pickSession(page, title))
  },
})

export async function pickSession(page: Page, title: RegExp | string): Promise<void> {
  const sidebar = await openSidebar(page)
  await sidebar.getByRole('button', { name: title }).click()
  await drawerGone(page)
  // Home is the New session page, whose composer is also named "Message";
  // typing before the Session replaces it would land there.
  await expect(page.getByRole('button', { name: 'Start session' })).toHaveCount(0)
}

export async function openSidebar(page: Page): Promise<Locator> {
  const inline = page.getByRole('navigation', { name: 'Sessions' })
  if (await inline.isVisible()) return inline
  const drawer = page.getByRole('dialog', { name: 'Sessions' })
  // A drawer on its way out still counts as visible; let it go rather than
  // hand back a panel about to leave the DOM.
  await expect(page.locator('[role="dialog"][data-closed]')).toHaveCount(0)
  if (!(await drawer.isVisible())) {
    await page.getByRole('button', { name: 'Open sessions' }).click()
  }
  await expect(drawer).toHaveAttribute('data-open')
  await expect(drawer).not.toHaveAttribute('data-starting-style')
  return drawer.getByRole('navigation', { name: 'Sessions' })
}

/**
 * On the phone a pick closes the drawer, and its closed state lands a frame
 * after the click. Call this after picking so a following openSidebar does
 * not catch the drawer on its way out.
 */
export async function drawerGone(page: Page): Promise<void> {
  await expect(page.getByRole('dialog', { name: 'Sessions' })).toBeHidden()
}

/** What the stand-in Desktop Shell was asked to do, read with shellRecord(page). */
export interface ShellRecord {
  opened: Array<[string, string]>
  notifications: Array<{ title: string; body: string; sessionId: string }>
  /** The src of every sound played, in order. */
  sounds: string[]
  daemonRestarts: number
}

export interface LocalClientOptions {
  /** The Shell reports that `droid` changed on disk since the Daemon started. */
  droidUpdated?: boolean
}

/**
 * Open the Client as the Local Client: a stand-in for the Desktop Shell
 * preload that points at the Fake Daemon and records what it is asked to do.
 * Built-in sounds come back as `data:audio/wav,<name>`, custom ones as
 * `data:audio/wav,<path>`. The window counts as focused until
 * setWindowFocused(page, false).
 */
export async function openLocalClient(
  page: Page,
  daemon: FakeDaemon,
  options: LocalClientOptions = {},
): Promise<void> {
  await page.addInitScript(
    ({ gatewayUrl, pairingToken, droidUpdated }) => {
      const record: ShellRecord = { opened: [], notifications: [], sounds: [], daemonRestarts: 0 }
      let updated = droidUpdated
      const snapshot = () => ({
        hasCredential: true,
        update: { status: 'idle' },
        droidUpdated: updated,
      })
      let focused = true
      let onClick: ((sessionId: string) => void) | null = null
      HTMLMediaElement.prototype.play = function () {
        record.sounds.push(this.src)
        return Promise.resolve()
      }
      document.hasFocus = () => focused
      Object.assign(window, {
        shellRecord: record,
        setWindowFocused: (value: boolean) => void (focused = value),
        clickNotification: (sessionId: string) => onClick?.(sessionId),
        droiShell: {
          gatewayUrl,
          pairingToken,
          platform: 'darwin',
          settings: {
            get: async () => snapshot(),
            restartDaemon: async () => {
              record.daemonRestarts += 1
              updated = false
              return snapshot()
            },
            onChange: () => () => {},
          },
          openIn: {
            list: async () => [
              { id: 'vscode', label: 'VS Code', icon: null },
              { id: 'finder', label: 'Finder', icon: null },
            ],
            open: async (path: string, appId: string) => void record.opened.push([path, appId]),
          },
          alerts: {
            builtinSound: async (name: string) => `data:audio/wav,${name}`,
            pickSoundFile: async () => '/Users/dev/sounds/ding.wav',
            readSoundFile: async (path: string) => `data:audio/wav,${path}`,
            notify: async (notification: ShellRecord['notifications'][number]) =>
              void record.notifications.push(notification),
            onNotificationClick: (listener: (sessionId: string) => void) => {
              onClick = listener
              return () => void (onClick = null)
            },
          },
        },
      })
    },
    { gatewayUrl: daemon.url, pairingToken: daemon.token, droidUpdated: !!options.droidUpdated },
  )
  await page.goto('/')
}

export function shellRecord(page: Page): Promise<ShellRecord> {
  return page.evaluate(() => (window as unknown as { shellRecord: ShellRecord }).shellRecord)
}

export async function setWindowFocused(page: Page, focused: boolean): Promise<void> {
  await page.evaluate(
    (value) =>
      (window as unknown as { setWindowFocused: (v: boolean) => void }).setWindowFocused(value),
    focused,
  )
}

/** As if the Desktop Shell's notification for this Session were clicked. */
export async function clickNotification(page: Page, sessionId: string): Promise<void> {
  await page.evaluate(
    (id) =>
      (window as unknown as { clickNotification: (id: string) => void }).clickNotification(id),
    sessionId,
  )
}

export async function openPairingLink(
  page: Page,
  daemon: FakeDaemon,
  token: string,
): Promise<void> {
  const fragment = new URLSearchParams({ pair: token, gateway: daemon.url })
  await page.goto(`/#${fragment.toString()}`)
}

export { expect }
