// Fixtures for the Phone App's web build: the web Client's Fake Daemon and
// Scenarios, plus access to the stand-ins for the phone's hardware.
import type { Locator, Page } from '@playwright/test'
import { test, expect } from '../e2e/fixtures'
import type { FakeDaemon } from '../e2e/fake-daemon/fake-daemon'
import type { JsonRpcRequest } from '../e2e/fake-daemon/protocol'
import { streamedReply } from '../e2e/fake-daemon/turns'

/** What the Phone App's stand-ins recorded (see apps/phone/src/platform/stand-ins.ts). */
export interface StandIns {
  haptics: string[]
  sounds: string[]
  signatureExpiry: string | null
  clipboard?: string | null
}

export function standIns(page: Page): Promise<StandIns> {
  return page.evaluate(
    () =>
      (window as unknown as { droiStandIns?: StandIns }).droiStandIns ?? {
        haptics: [],
        sounds: [],
        signatureExpiry: null,
      },
  )
}

/** Set stand-in values before the app starts. */
export async function presetStandIns(page: Page, values: Partial<StandIns>): Promise<void> {
  await page.addInitScript((preset) => {
    Object.assign(window, { droiStandIns: preset })
  }, values)
}

/** The Desktop Shell's pairing link for this Fake Daemon, as Settings shows it. */
export function pairingLink(daemon: FakeDaemon, token = daemon.token): string {
  return `${daemon.url}/#pair=${token}`
}

/** First launch: paste the pairing link and wait until the app is connected. */
export async function pairPhone(page: Page, daemon: FakeDaemon): Promise<void> {
  await page.goto('/')
  await pasteLink(page, pairingLink(daemon))
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
}

export async function pasteLink(page: Page, link: string): Promise<void> {
  await page.getByRole('textbox', { name: 'Pairing link' }).fill(link)
  await page.getByRole('button', { name: 'Pair' }).click()
}

/** The Session list in the drawer, opening the drawer first. */
export async function openDrawer(page: Page): Promise<Locator> {
  const drawer = page.getByRole('dialog', { name: 'Sessions' })
  if (!(await drawer.isVisible())) {
    await page.getByRole('button', { name: 'Open sessions' }).click()
  }
  await expect(drawer).toBeVisible()
  return drawer.getByRole('navigation', { name: 'Sessions' })
}

export async function pickSession(page: Page, title: RegExp | string): Promise<void> {
  const list = await openDrawer(page)
  await list.getByRole('button', { name: title }).click()
  await expect(page.getByRole('dialog', { name: 'Sessions' })).toBeHidden()
}

export { test, expect }

/**
 * Plays a turn in a Session as if a message had been sent from another
 * Client: the user message, then the reply streamed in these chunks.
 */
export function playTurn(
  daemon: FakeDaemon,
  sessionId: string,
  text: string,
  deltas: string[],
  delayMs = 150,
): void {
  void streamedReply({ deltas, delayMs })({ sessionId, text }, { daemon, connectionId: 0 }, {
    id: `turn-${Date.now()}`,
  } as unknown as JsonRpcRequest)
}
