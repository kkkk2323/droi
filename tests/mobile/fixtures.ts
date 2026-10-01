// Fixtures for the Phone App's web build: the web Client's Fake Daemon and
// Scenarios, plus access to the stand-ins for the phone's hardware.
import type { Locator, Page } from '@playwright/test'
import { test, expect } from '../web/fixtures'
import type { FakeDaemon } from '../fake-daemon/fake-daemon'
import type { JsonRpcRequest } from '../fake-daemon/protocol'
import { streamedReply } from '../fake-daemon/turns'

/** What the Phone App's stand-ins recorded (see apps/mobile/src/platform/stand-ins.ts). */
export interface StandIns {
  haptics: string[]
  sounds: string[]
  signatureExpiry: string | null
  clipboard?: string | null
  nextImages?: Array<{ name: string; mediaType: 'image/png' | 'image/jpeg'; data: string }>
  picked?: string[]
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

/**
 * Scrolls the transcript to its oldest message. The list is inverted: the
 * oldest message is at its far end, and the newest comes first in the page.
 * The far end grows as rows on the way render, so it takes a few scrolls, as
 * it would a reader.
 */
export async function scrollToOldest(transcript: Locator): Promise<void> {
  await expect
    .poll(
      () =>
        transcript.evaluate((el) => {
          const end = el.scrollHeight - el.clientHeight
          const there = Math.abs(el.scrollTop - end) < 1
          // Not el.scrollTo: React Native Web replaces it with its own, which takes { x, y }.
          el.scrollTop = el.scrollHeight
          return there
        }),
      { intervals: [50], timeout: 20_000 },
    )
    .toBe(true)
}

/** The texts of these elements in reading order, top of the screen first. */
export async function inReadingOrder(elements: Locator): Promise<string[]> {
  const boxes = await elements.evaluateAll((els) =>
    els.map((el) => ({ y: el.getBoundingClientRect().top, text: el.textContent ?? '' })),
  )
  return boxes.sort((a, b) => a.y - b.y).map((box) => box.text)
}

/** The Desktop Shell's pairing link for this Fake Daemon, as Settings shows it. */
export function pairingLink(daemon: FakeDaemon, token = daemon.token): string {
  return `${daemon.url}/#pair=${token}`
}

/** First launch: paste the pairing link and wait until the app is connected. */
export async function pairPhone(page: Page, daemon: FakeDaemon): Promise<void> {
  await page.goto('/')
  await pasteLink(page, pairingLink(daemon))
  await expect(sessionList(page)).toBeVisible()
}

export async function pasteLink(page: Page, link: string): Promise<void> {
  await page.getByRole('textbox', { name: 'Pairing link' }).fill(link)
  await page.getByRole('button', { name: 'Pair' }).click()
}

/**
 * Waits for sheets to finish sliding away. The web build's sheet (vaul) hides
 * the rest of the page from the accessibility tree until its exit ends.
 */
export async function sheetsClosed(page: Page): Promise<void> {
  await expect(page.locator('[data-vaul-drawer]')).toHaveCount(0)
}

/**
 * Quits and launches the app again. A phone always launches at the app's
 * root, whatever page it showed; the app reopens that page itself. (A reload
 * would ask the static test server for a Session's address, which it lacks.)
 */
export async function relaunch(page: Page): Promise<void> {
  await page.goto('/')
}

/** The Session list, the connected computer's first page. */
export function sessionList(page: Page): Locator {
  return page.getByRole('navigation', { name: 'Sessions' })
}

/** Goes back to the Session list from whatever was pushed over it. */
export async function openSessionList(page: Page): Promise<Locator> {
  await sheetsClosed(page)
  const list = sessionList(page)
  // react-navigation's web header draws its back button as a link.
  const back = page
    .getByRole('link', { name: /back$/i })
    .or(page.getByRole('button', { name: /back$/i }))
    .first()
  await expect(async () => {
    // Right after a launch neither the list nor a back button is there yet.
    if (!(await list.isVisible()) && (await back.isVisible())) await back.click()
    await expect(list).toBeVisible({ timeout: 1_000 })
  }).toPass({ timeout: 10_000 })
  return list
}

export async function pickSession(page: Page, title: RegExp | string): Promise<void> {
  const list = await openSessionList(page)
  await list.getByRole('button', { name: title }).click()
  await expect(list).toBeHidden()
}

/** Opens the New session page from the Session list. */
export async function openNewSession(page: Page): Promise<Locator> {
  await openSessionList(page)
  await page.getByRole('button', { name: 'New session' }).click()
  const form = page.getByRole('region', { name: 'New session' })
  await expect(form).toBeVisible()
  return form
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
