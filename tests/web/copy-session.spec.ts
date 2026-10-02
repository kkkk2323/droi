import type { Page } from '@playwright/test'
import { expect, test } from './fixtures'
import { SESSIONS_FOLDER } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'

const loginBug = session('Fix the login bug', '/Users/dev/acme-web', [
  userMessage('Why does login fail?'),
])

test.use({ scenario: { sessions: [loginBug] } })

test.beforeEach(async ({ context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
})

const clipboard = (page: Page) => page.evaluate(() => navigator.clipboard.readText())

const details = `Title: Fix the login bug
Session ID: ${loginBug.sessionId}
Workspace: /Users/dev/acme-web
Transcript: ${SESSIONS_FOLDER}/-Users-dev-acme-web/${loginBug.sessionId}.jsonl`

test("a Session row's menu copies its id and its details", async ({
  page,
  openClient,
  openSidebar,
}) => {
  await openClient()
  const row = (await openSidebar()).getByRole('button', { name: /Fix the login bug/ })

  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Copy session ID' }).click()
  await expect.poll(() => clipboard(page)).toBe(loginBug.sessionId)

  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Copy session details' }).click()
  await expect.poll(() => clipboard(page)).toBe(details)
})

test("the Session header copies its details, the transcript's path included", async ({
  page,
  openClient,
  pickSession,
}) => {
  await openClient()
  await pickSession(/Fix the login bug/)
  await page.getByRole('button', { name: 'Copy session info' }).click()
  await page.getByRole('menuitem', { name: 'Copy session details' }).click()
  await expect.poll(() => clipboard(page)).toBe(details)
  await expect(page.getByRole('button', { name: 'Copied' })).toBeVisible()
})
