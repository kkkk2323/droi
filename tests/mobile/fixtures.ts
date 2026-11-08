// Fixtures for the Phone App's web build: a Fake Kernel per test with its Scenario (fake-kernel/), plus
// access to the stand-ins for the phone's hardware.
import { test as base, expect, type Locator, type Page } from '@playwright/test'
import { FakeKernel, type Scenario } from './fake-kernel/fake-kernel'

export type { Scenario }

const test = base.extend<{ fakeKernel: FakeKernel; scenario: Scenario; phoneWire: PhoneWire }>({
  scenario: [{}, { option: true }],
  fakeKernel: async ({ scenario }, use, testInfo) => {
    const kernel = await FakeKernel.start(scenario)
    await use(kernel)
    if (testInfo.status !== testInfo.expectedStatus) {
      await testInfo.attach('model requests', {
        body: JSON.stringify(
          kernel.model.requests.map((r) => r.prompt),
          null,
          2,
        ),
        contentType: 'application/json',
      })
    }
    await kernel.stop()
  },
  phoneWire: async ({ page }, use) => {
    await use(watchWire(page))
  },
})

/** What the phone said to its Kernel through the Gateway: its calls, and its sockets. */
export interface PhoneWire {
  calls: Array<{ method: string; params: Record<string, unknown> }>
  /** The phone's Kernel sockets open now. */
  open(): number
  /** How many times the phone called this method. */
  count(method: string): number
  /** The params of the phone's nth call of this method (1 the first), once it was made. */
  waitForCall(method: string, nth?: number): Promise<Record<string, unknown>>
}

/** Watches a page's Kernel sockets from now on. */
export function watchWire(page: Page): PhoneWire {
  const calls: PhoneWire['calls'] = []
  let open = 0
  page.on('websocket', (ws) => {
    if (!new URL(ws.url()).pathname.endsWith('/kernel')) return
    open += 1
    ws.on('close', () => (open -= 1))
    ws.on('framesent', (frame) => {
      try {
        const sent = JSON.parse(String(frame.payload)) as {
          method?: string
          params?: Record<string, unknown>
        }
        if (sent.method) calls.push({ method: sent.method, params: sent.params ?? {} })
      } catch {
        // Not JSON-RPC.
      }
    })
  })
  const matching = (method: string) => calls.filter((c) => c.method === method)
  return {
    calls,
    open: () => open,
    count: (method) => matching(method).length,
    async waitForCall(method, nth = 1) {
      await expect
        .poll(() => matching(method).length, { message: `the phone's ${method} #${nth}` })
        .toBeGreaterThanOrEqual(nth)
      return matching(method)[nth - 1]!.params
    },
  }
}
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

/** The computer's pairing link for this Fake Kernel's Gateway, as Settings shows it. */
export function pairingLink(kernel: FakeKernel, token = kernel.token): string {
  return `${kernel.url}/#pair=${token}`
}

/** First launch: paste the pairing link and wait until the app is connected. */
export async function pairPhone(page: Page, kernel: FakeKernel): Promise<void> {
  await page.goto('/')
  await pasteLink(page, pairingLink(kernel))
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

/** The open Session's transcript. */
export function transcriptOf(page: Page): Locator {
  return page.getByRole('log', { name: 'Transcript' })
}

/** Sends a message from the open Session's composer. */
export async function send(page: Page, text: string): Promise<void> {
  const composer = page.getByRole('textbox', { name: 'Message' })
  await composer.fill(text)
  await page.getByRole('button', { name: 'Send' }).click()
}

export { test, expect }
