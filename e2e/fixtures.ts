import { test as base, expect, type Locator, type Page } from '@playwright/test'
import { FakeDaemon } from './fake-daemon/fake-daemon'
import type { ScenarioInput } from './fake-daemon/scenario'

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
    await use(async (title) => {
      const sidebar = await openSidebar(page)
      await sidebar.getByRole('button', { name: title }).click()
      await drawerGone(page)
    })
  },
})

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

export async function openPairingLink(
  page: Page,
  daemon: FakeDaemon,
  token: string,
): Promise<void> {
  const fragment = new URLSearchParams({ pair: token, gateway: daemon.url })
  await page.goto(`/#${fragment.toString()}`)
}

export { expect }
