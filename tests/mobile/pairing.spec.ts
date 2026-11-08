import { exchange, userEntry } from './fake-kernel/history'
import { expect, openSessionList, pairPhone, pasteLink, test } from './fixtures'

test.use({
  scenario: {
    autonomy: 'ask',
    sessions: [
      { title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] },
      { title: 'Notes', cwd: 'acme-web', entries: exchange('todo', 'Noted.') },
      { title: 'Docs', cwd: 'handbook', entries: exchange('write', 'Written.') },
    ],
  },
})

test('pasting a pairing link connects and lists the Sessions grouped by Workspace', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  await expect(page.getByRole('button', { name: 'Switch computer, Test Mac' })).toBeVisible()
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  const web = list.getByRole('group', { name: 'acme-web' })
  await expect(web.getByRole('button', { name: /Deploy/ })).toBeVisible()
  await expect(web.getByRole('button', { name: /Notes/ })).toBeVisible()
  await expect(
    list.getByRole('group', { name: 'handbook' }).getByRole('button', { name: /Docs/ }),
  ).toBeVisible()
})

test('the Pairing Token goes to the keychain, not to app storage', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  const stored = await page.evaluate(() => ({
    app: JSON.stringify({ ...localStorage }),
    keychain: JSON.stringify({ ...sessionStorage }),
  }))
  expect(stored.keychain).toContain(fakeKernel.token)
  expect(stored.app).not.toContain(fakeKernel.token)
  expect(stored.app).toContain(fakeKernel.computerId)
})

test('a working Session and a Session waiting on a Prompt show different states', async ({
  page,
  fakeKernel,
}) => {
  fakeKernel.model.push(
    {
      match: 'deploy now',
      text: 'Deploying, step by step, slowly.',
      chunks: ['Deploying', ', step', ' by step', ', slowly.'],
      delayMs: 4_000,
    },
    { match: 'clean up', call: { name: 'bash', arguments: { command: 'rm -rf build' } } },
  )
  await pairPhone(page, fakeKernel)
  await fakeKernel.submit('Deploy', 'deploy now')
  await fakeKernel.submit('Notes', 'clean up')
  const list = await openSessionList(page)
  const deployRow = list.getByRole('button', { name: /Deploy/ })
  const notesRow = list.getByRole('button', { name: /Notes/ })
  await expect(deployRow.getByRole('status', { name: 'Working' })).toBeVisible()
  await expect(notesRow.getByRole('status', { name: 'Needs input' })).toBeVisible()
  await expect(deployRow.getByRole('status', { name: 'Needs input' })).toHaveCount(0)
})

test('a link without the address or an unreachable computer says so', async ({ page }) => {
  await page.goto('/')
  await pasteLink(page, 'abc-123')
  await expect(page.getByRole('alert')).toHaveText(/whole pairing link/)
  await pasteLink(page, 'http://127.0.0.1:9/#pair=abc')
  await expect(page.getByRole('alert')).toHaveText(/Cannot reach http:\/\/127\.0\.0\.1:9/)
  await expect(page.getByRole('heading', { name: 'Pair with a computer' })).toBeVisible()
})

test('typing in the search box asks the Kernel and lists the hits with the matching message', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  await list.getByRole('searchbox', { name: 'Search sessions' }).fill('ship')
  const results = list.getByRole('region', { name: 'Search results' })
  await expect(results.getByRole('listitem')).toHaveCount(1)
  await expect(results.getByRole('button', { name: /Deploy/ })).toContainText('ship it')
  await expect(list.getByRole('group', { name: 'acme-web' })).toHaveCount(0)
  await results.getByRole('button', { name: /Deploy/ }).click()
  await expect(page.getByRole('dialog', { name: 'Sessions' })).toBeHidden()
  await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
})

test.describe('with more Sessions than one page', () => {
  // The Kernel lists 200 a page.
  const many = Array.from({ length: 205 }, (_, i) => ({
    title: `Task ${String(i + 1).padStart(3, '0')}`,
    cwd: 'acme-web',
    entries: [userEntry('go')],
  }))
  test.use({ scenario: { sessions: many } })

  test('"Load older sessions" brings the rest', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    const list = await openSessionList(page)
    const web = list.getByRole('group', { name: 'acme-web' })
    await expect(web.getByRole('button', { name: /Task 205/ })).toBeVisible()
    await expect(web.getByRole('button', { name: /Task 001/ })).toHaveCount(0)
    await list.getByRole('button', { name: 'Load older sessions' }).click()
    await expect(web.getByRole('button', { name: /Task 001/ })).toBeAttached()
    await expect(list.getByRole('button', { name: 'Load older sessions' })).toHaveCount(0)
  })
})
