import { test as base, type Page } from '@playwright/test'
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
})

export async function openPairingLink(
  page: Page,
  daemon: FakeDaemon,
  token: string,
): Promise<void> {
  const fragment = new URLSearchParams({ pair: token, gateway: daemon.url })
  await page.goto(`/#${fragment.toString()}`)
}

export { expect } from '@playwright/test'
