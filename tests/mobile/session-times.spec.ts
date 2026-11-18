// The computer's Session Times (ADR 0032), which the Fake Daemon does not keep: the test
// plays the Gateway that does, and the window's list order, which here lists by time. The
// phone dates a Session by the last message the user sent it, not by the Daemon's date, and
// shares the computer's unread marks.
import type { Page, Route } from '@playwright/test'
import type { SessionTimes } from '@droi/daemon-layer/gateway'
import { session, userMessage } from '@droi/fake-daemon/scenario'
import { expect, inReadingOrder, openSessionList, pairPhone, pickSession, test } from './fixtures'

const now = Date.now()
const today = new Date(now)
const yesterdayNoon = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1, 12)
const day = 24 * 60 * 60 * 1000
const seconds = (ms: number) => Math.floor(ms / 1000)

// The Daemon dates Sessions by their files: a hook just wrote one, another was written days ago.
const sent = session('Sent today', '/Users/dev/acme-web', [userMessage('ship it')], {
  updatedAt: seconds(now - 2 * day),
})
const hooked = session('Touched by a hook', '/Users/dev/acme-api', [userMessage('lint')], {
  updatedAt: seconds(now),
})
const finished = session('Finished elsewhere', '/Users/dev/acme-web', [userMessage('build')], {
  updatedAt: seconds(now - 3 * day),
})
const old = session('Old notes', '/Users/dev/acme-web', [userMessage('todo')], {
  updatedAt: seconds(now - 10 * day),
})

test.use({ scenario: { sessions: [sent, hooked, finished, old] } })

const CORS = { 'access-control-allow-origin': '*' }

/** The Gateway's Session Times: every read is answered in full, a waiting one at a change. */
class TimesGateway {
  rev = 1
  times: Record<string, SessionTimes> = {
    [sent.sessionId]: { sentAt: now - 1_000 },
    [hooked.sessionId]: { sentAt: yesterdayNoon.getTime() },
    [finished.sessionId]: { sentAt: now - 2_000, endedAt: now - 1_500, viewedAt: now - 1_800 },
  }
  /** The Sessions the phone said it showed. */
  viewed: string[] = []
  #waiting: Array<() => void> = []

  set(sessionId: string, times: SessionTimes) {
    this.times[sessionId] = { ...this.times[sessionId], ...times }
    this.rev += 1
    for (const wake of this.#waiting.splice(0)) wake()
  }

  async serve(page: Page) {
    await page.route(
      (url) => url.pathname === '/session-list',
      (route) =>
        route.fulfill({
          headers: CORS,
          json: {
            pinnedSessions: [],
            pinnedWorkspaces: [],
            workspaceOrder: [],
            sessionSort: '',
            sessionView: 'time',
          },
        }),
    )
    await page.route(
      (url) => url.pathname.startsWith('/session-times'),
      (route) => this.#answer(route),
    )
  }

  async #answer(route: Route) {
    const url = new URL(route.request().url())
    try {
      if (url.pathname === '/session-times/viewed') {
        const id = url.searchParams.get('sessionId') ?? ''
        this.viewed.push(id)
        this.set(id, { viewedAt: Date.now() })
        await route.fulfill({ status: 204, headers: CORS })
        return
      }
      const after = Number(url.searchParams.get('after') ?? 0)
      if (after === this.rev) {
        // The long poll, cut short: a change or a second, whichever comes first.
        await new Promise<void>((resolve) => {
          this.#waiting.push(resolve)
          setTimeout(resolve, 1_000)
        })
      }
      await route.fulfill({
        headers: CORS,
        json: { rev: this.rev, full: true, sessions: this.times },
      })
    } catch {
      // The page closed while the read waited.
    }
  }
}

async function pairWithTimes(page: Page, fakeDaemon: Parameters<typeof pairPhone>[1]) {
  const gateway = new TimesGateway()
  await gateway.serve(page)
  await pairPhone(page, fakeDaemon)
  return gateway
}

const SECTIONS = /^(Pinned|Right now|Today|Yesterday|Earlier)$/

test('by time: right now, then by the day each was last sent a message', async ({
  page,
  fakeDaemon,
}) => {
  await pairWithTimes(page, fakeDaemon)
  const list = await openSessionList(page)
  await expect(list.getByRole('button', { name: 'Right now' })).toBeVisible()
  expect(await inReadingOrder(list.getByRole('button', { name: SECTIONS }))).toEqual([
    'Right now',
    'Today',
    'Yesterday',
    'Earlier',
  ])
  const inSection = (section: string, title: string) =>
    list.getByRole('group', { name: section }).getByRole('button', { name: new RegExp(title) })
  await expect(inSection('Right now', finished.title)).toBeVisible()
  await expect(inSection('Today', sent.title)).toBeVisible()
  await expect(inSection('Yesterday', hooked.title)).toBeVisible()
  await expect(inSection('Earlier', old.title)).toBeVisible()

  const row = inSection('Yesterday', hooked.title)
  await expect(row.getByRole('img', { name: 'In acme-api' })).toBeVisible()
  // Dated by its last message, not by the hook that just wrote its file.
  await expect(row).not.toContainText('now')
  await expect(
    inSection('Right now', finished.title).getByRole('img', { name: 'Unread' }),
  ).toBeVisible()
})

test('opening a Session reads it on the computer', async ({ page, fakeDaemon }) => {
  const gateway = await pairWithTimes(page, fakeDaemon)
  await pickSession(page, finished.title)
  await expect.poll(() => gateway.viewed).toContain(finished.sessionId)

  const list = await openSessionList(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)
  await expect(list.getByRole('button', { name: 'Right now' })).toHaveCount(0)
  await expect(
    list.getByRole('group', { name: 'Today' }).getByRole('button', { name: /Finished elsewhere/ }),
  ).toBeVisible()
})

test('a Session shown on the computer reads here too', async ({ page, fakeDaemon }) => {
  const gateway = await pairWithTimes(page, fakeDaemon)
  const list = await openSessionList(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(1)

  gateway.set(finished.sessionId, { viewedAt: Date.now() })
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)
  expect(gateway.viewed).toEqual([])

  // A turn ending on the computer marks it again.
  gateway.set(finished.sessionId, { endedAt: Date.now() + 1 })
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(1)
})
