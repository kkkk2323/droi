import { nextTime, userEntry } from './fake-kernel/history'
import { expect, openNewSession, openSessionList, pairPhone, test } from './fixtures'

test.use({
  scenario: {
    models: [{ id: 'gpt-5', name: 'GPT-5', thinking: ['low', 'medium', 'high'] }],
    sessions: [
      {
        title: 'Old work',
        cwd: 'billing-service',
        entries: [userEntry('a', nextTime() - 60 * 60_000)],
      },
      { title: 'Recent work', cwd: 'acme-web', entries: [userEntry('b')] },
    ],
    skills: [{ name: 'handoff', description: 'Compact the conversation' }],
  },
})

test('starts a Session in a recent Workspace with the chosen model', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  fakeKernel.model.push({ match: 'Refactor the invoices', text: 'On it.' })
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/acme-web\?/)

  await form.getByRole('button', { name: 'Workspace' }).click()
  await page
    .getByRole('dialog', { name: 'Workspace' })
    .getByRole('radio', { name: 'billing-service' })
    .click()
  await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/billing-service\?/)

  await form.getByRole('button', { name: 'Model', exact: true }).click()
  await page
    .getByRole('dialog', { name: 'Choose a model' })
    .getByRole('radio', { name: 'GPT-5' })
    .click()
  await expect(form.getByRole('button', { name: 'Model', exact: true })).toHaveText(/GPT-5/)

  await form.getByRole('textbox', { name: 'Message' }).fill('Refactor the invoices')
  await form.getByRole('button', { name: 'Start session' }).click()
  expect(await phoneWire.waitForCall('conversation.create')).toMatchObject({
    cwd: fakeKernel.folder('billing-service'),
    kind: 'chat',
    agent: { model: { provider: 'fake', modelId: 'gpt-5' } },
  })
  expect(await phoneWire.waitForCall('conversation.submit')).toMatchObject({
    input: 'Refactor the invoices',
  })
  const transcript = page.getByRole('log', { name: 'Transcript' })
  await expect(transcript.getByRole('article', { name: 'You' })).toContainText(
    'Refactor the invoices',
  )
  await expect(transcript).toContainText('On it.')
  expect(phoneWire.count('conversation.create')).toBe(1)
  // It works where it was asked to.
  const listed = await fakeKernel.kernel.call<{
    items: Array<{ title: string; workspace: string }>
  }>('conversation.list')
  expect(
    listed.items.find(
      (i) => i.workspace === fakeKernel.folder('billing-service') && i.title !== 'Old work',
    ),
  ).toBeTruthy()
})

test('a typed path is checked by the computer and starts a Session there', async ({
  page,
  fakeKernel,
}) => {
  fakeKernel.model.push({ match: 'Hello', text: 'Hi there.' })
  const fresh = fakeKernel.folder('fresh-project')
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  await form.getByRole('button', { name: 'Workspace' }).click()
  await page.getByRole('button', { name: 'Other folder…' }).click()
  await expect(form.getByRole('heading', { name: 'Where should Droid work?' })).toBeVisible()
  const path = form.getByRole('textbox', { name: 'Workspace path' })
  await path.fill('/nope/missing')
  await form.getByRole('button', { name: 'Start', exact: true }).click()
  await expect(form.getByRole('alert')).toContainText('/nope/missing')

  await path.fill(fresh)
  await form.getByRole('button', { name: 'Start', exact: true }).click()
  const message = page.getByRole('textbox', { name: 'Message' })
  await expect(message).toHaveAttribute('placeholder', 'Ask anything')
  await message.fill('Hello')
  await page.getByRole('button', { name: 'Send' }).click()
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Hi there.')
  const list = await openSessionList(page)
  await expect(list.getByRole('group', { name: 'fresh-project' })).toBeVisible()
})

test('"/" lists the user’s own skills before the first send', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  await form.getByRole('textbox', { name: 'Message' }).fill('/')
  const menu = page.getByRole('menu', { name: 'Commands and skills' })
  await expect(menu.getByRole('menuitem')).toHaveText([/\/handoff.*Skill/])
})

test('leaving the page makes no Session', async ({ page, fakeKernel, phoneWire }) => {
  await pairPhone(page, fakeKernel)
  await openNewSession(page)
  const list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Recent work/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Old work/ })).toBeVisible()
  await expect(list.getByRole('group')).toHaveCount(2)
  expect(phoneWire.count('conversation.create')).toBe(0)
})

test('Direct for tool calls starts a Session without scripts', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  const form = await openNewSession(page)
  const tools = form.getByRole('button', { name: 'Tool calls', exact: true })
  await tools.click()
  const sheet = page.getByRole('dialog', { name: 'Tool calls' })
  await expect(sheet.getByRole('radio')).toHaveText(['Direct', 'Script'])
  await sheet.getByRole('radio', { name: 'Direct' }).click()
  await expect(tools).toHaveText(/Direct/)

  await form.getByRole('button', { name: 'Start session' }).click()
  expect(await phoneWire.waitForCall('conversation.create')).toMatchObject({
    cwd: fakeKernel.folder('acme-web'),
    agent: { extensions: { remove: ['codemode'] } },
  })
  // Its model is offered no codemode, which a Session started as usual is.
  fakeKernel.model.push({ match: 'direct please', text: 'Direct it is.' })
  await page.getByRole('textbox', { name: 'Message' }).fill('direct please')
  await page.getByRole('button', { name: 'Send' }).click()
  const direct = await fakeKernel.model.waitForTurn('direct please')
  await fakeKernel.turn('Recent work', 'as usual', { text: 'Usual.' })
  const usual = await fakeKernel.model.waitForTurn('as usual')
  expect(usual.tools).toContain('codemode')
  expect(direct.tools).not.toContain('codemode')
  expect(direct.tools).toContain('bash')
})

test.describe('on a computer whose Session defaults name a model', () => {
  test.use({
    scenario: {
      models: [{ id: 'gpt-5', name: 'GPT-5', thinking: ['low', 'medium', 'high'] }],
      defaultModel: 'gpt-5',
      sessions: [{ title: 'Recent work', cwd: 'acme-web', entries: [userEntry('b')] }],
    },
  })

  test('a Session started without picking one has that model', async ({ page, fakeKernel }) => {
    fakeKernel.model.push({ match: 'Look around', text: 'Looked.' })
    await pairPhone(page, fakeKernel)
    const form = await openNewSession(page)
    await expect(form.getByRole('button', { name: 'Model', exact: true })).toHaveText(/GPT-5/)
    await form.getByRole('textbox', { name: 'Message' }).fill('Look around')
    await form.getByRole('button', { name: 'Start session' }).click()
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Looked.')
    await expect(page.getByRole('button', { name: 'Model', exact: true })).toHaveText(/GPT-5/)
  })
})
