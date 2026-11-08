import type { Page } from '@playwright/test'
import { userEntry } from './fake-kernel/history'
import { expect, openSessionList, pairPhone, relaunch, test } from './fixtures'

test.use({
  scenario: {
    sessions: [
      { title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] },
      { title: 'Notes', cwd: 'acme-web', entries: [userEntry('todo')] },
      { title: 'Budget', cwd: 'finance', entries: [userEntry('sum')] },
    ],
  },
})

/** A long press opens a row's actions, as on the phone. */
async function actionsFor(page: Page, name: RegExp | string) {
  const actions = page.getByRole('dialog', { name: /^Actions for / })
  const list = await openSessionList(page)
  await list.getByRole('button', { name, exact: typeof name === 'string' }).click({ delay: 800 })
  await expect(actions).toBeVisible()
  return actions
}

test('archive, unarchive and rename go to the Kernel', async ({ page, fakeKernel, phoneWire }) => {
  const deploy = fakeKernel.session('Deploy').conversationId
  const notes = fakeKernel.session('Notes').conversationId
  await pairPhone(page, fakeKernel)
  let actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Archive' }).click()
  expect(await phoneWire.waitForCall('index.update')).toEqual({ id: deploy, archived: true })
  let list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Deploy/ })).toHaveCount(0)

  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('switch', { name: 'Show archived sessions' }).click()
  await page.goBack()
  list = await openSessionList(page)
  const archived = list.getByRole('button', { name: /Deploy/ })
  await expect(archived).toContainText('Archived')
  actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Unarchive' }).click()
  expect(await phoneWire.waitForCall('index.update', 2)).toEqual({ id: deploy, archived: false })
  await expect(archived).not.toContainText('Archived')

  actions = await actionsFor(page, /Notes/)
  await actions.getByRole('button', { name: 'Rename' }).click()
  await actions.getByRole('textbox', { name: 'Session title' }).fill('Release notes')
  await actions.getByRole('button', { name: 'Save' }).click()
  expect(await phoneWire.waitForCall('index.update', 3)).toEqual({
    id: notes,
    title: 'Release notes',
  })
  list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Release notes/ })).toBeVisible()
  // The computer's window hears the new title too.
  await fakeKernel.kernel.waitFor(
    (n) => n.method === 'index.changed' && n.params['title'] === 'Release notes',
  )
})

// A Gateway without a window has no list order to take the pins from, so the phone keeps
// its own; they are laid out as the computer's sidebar.
test('pins and folds are kept on the phone across a relaunch', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  let actions = await actionsFor(page, /Deploy/)
  await actions.getByRole('button', { name: 'Pin', exact: true }).click()
  let list = await openSessionList(page)
  const pinned = list.getByRole('group', { name: 'Pinned' })
  await expect(list.getByRole('group').first()).toHaveAccessibleName('Pinned')
  await expect(
    pinned.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Pinned' }),
  ).toBeVisible()
  await expect(
    list.getByRole('group', { name: 'acme-web' }).getByRole('button', { name: /Deploy/ }),
  ).toHaveCount(0)

  actions = await actionsFor(page, 'finance')
  await expect(actions).toHaveAccessibleName('Actions for finance')
  await actions.getByRole('button', { name: 'Pin workspace' }).click()
  list = await openSessionList(page)
  await expect(pinned.getByRole('group', { name: 'finance' })).toBeVisible()
  await expect(
    list.getByRole('group', { name: 'Workspaces' }).getByRole('group', { name: 'acme-web' }),
  ).toBeVisible()

  await list.getByRole('button', { name: 'acme-web', exact: true }).click()
  await expect(list.getByRole('button', { name: 'acme-web', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await expect(list.getByRole('button', { name: /Notes/ })).toHaveCount(0)

  await relaunch(page)
  list = await openSessionList(page)
  await expect(pinned.getByRole('group', { name: 'finance' })).toBeVisible()
  await expect(
    pinned.getByRole('button', { name: /Deploy/ }).getByRole('img', { name: 'Pinned' }),
  ).toBeVisible()
  await expect(list.getByRole('button', { name: 'acme-web', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await list.getByRole('button', { name: 'acme-web', exact: true }).click()
  await expect(list.getByRole('button', { name: /Notes/ })).toBeVisible()
  expect(phoneWire.calls.map((c) => c.method)).not.toContain('index.update')
})

test('a Session row says when it changed', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  const row = list.getByRole('button', { name: /Budget/ })
  await expect(row).toContainText(/now|\d+[mhd]|[A-Z][a-z]{2} \d+/)
})

test('a Session row is a full-size touch target', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  const row = list.getByRole('button', { name: /Deploy/ })
  expect((await row.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  await expect(row.getByText('Deploy')).toHaveCSS('font-size', '15px')
})
