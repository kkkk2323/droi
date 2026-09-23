import type { Page } from '@playwright/test'
import { session, userMessage } from '../fake-daemon/scenario'
import { expect, openDrawer, pairPhone, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])
const notes = session('Notes', '/Users/dev/acme-web', [userMessage('todo')])
const budget = session('Budget', '/Users/dev/finance', [userMessage('sum')])

test.use({ scenario: { sessions: [deploy, notes, budget] } })

/** A long press opens a row's actions, as on the phone. */
async function actionsFor(page: Page, name: RegExp | string) {
  const actions = page.getByRole('dialog', { name: /^Actions for / })
  // A sheet still sliding away (its backdrop still there) holds focus and
  // would cancel the press.
  await expect(page.getByRole('button', { name: 'Close', exact: true })).toHaveCount(0)
  const list = await openDrawer(page)
  await list.getByRole('button', { name, exact: typeof name === 'string' }).click({ delay: 800 })
  await expect(actions).toBeVisible()
  return actions
}

test('archive, unarchive and rename go to the Daemon', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  let actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Archive' }).click()
  await fakeDaemon.waitForRequest('daemon.archive_session')
  let list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Deploy/ })).toHaveCount(0)

  await list.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('switch', { name: 'Show archived sessions' }).click()
  await page.goBack()
  list = await openDrawer(page)
  const archived = list.getByRole('button', { name: /Deploy/ })
  await expect(archived).toContainText('Archived')
  actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Unarchive' }).click()
  await fakeDaemon.waitForRequest('daemon.unarchive_session')
  await expect(archived).not.toContainText('Archived')

  actions = await actionsFor(page, /Notes/)
  await actions.getByRole('button', { name: 'Rename' }).click()
  await actions.getByRole('textbox', { name: 'Session title' }).fill('Release notes')
  await actions.getByRole('button', { name: 'Save' }).click()
  const renamed = await fakeDaemon.waitForRequest('daemon.rename_session')
  expect(renamed.params).toMatchObject({ sessionId: notes.sessionId, title: 'Release notes' })
  list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Release notes/ })).toBeVisible()
})

test('pins and folds are kept on the phone across a relaunch', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  let actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Pin', exact: true }).click()
  let list = await openDrawer(page)
  const web = list.getByRole('group', { name: 'acme-web' })
  await expect(
    web.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Pinned' }),
  ).toBeVisible()

  actions = await actionsFor(page, 'finance')
  await expect(actions).toHaveAccessibleName('Actions for finance')
  await actions.getByRole('button', { name: 'Pin workspace' }).click()
  list = await openDrawer(page)
  await expect(list.getByRole('group').first()).toHaveAccessibleName('finance')

  await list.getByRole('button', { name: 'acme-web', exact: true }).click()
  await expect(list.getByRole('button', { name: 'acme-web', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await expect(list.getByRole('button', { name: /Deploy/ })).toHaveCount(0)

  await page.reload()
  list = await openDrawer(page)
  await expect(list.getByRole('group').first()).toHaveAccessibleName('finance')
  await expect(list.getByRole('button', { name: 'acme-web', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await list.getByRole('button', { name: 'acme-web', exact: true }).click()
  await expect(
    list.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Pinned' }),
  ).toBeVisible()
  expect(fakeDaemon.requests.map((r) => r.method)).not.toContain('daemon.update_session_settings')
})
