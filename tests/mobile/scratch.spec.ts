// Sessions without a Workspace on the phone: Workspace None starts one in a
// fresh Scratch Workspace the Gateway makes, listed under Recents (ADR 0008).
import { SCRATCH_FOLDER, type FakeDaemon, type RecordedRequest } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'
import { expect, openDrawer, pairPhone, pickSession, test } from './fixtures'

const SCRATCH = { name: 'droi.scratch' }

function paramsOf(request: RecordedRequest): Record<string, unknown> {
  return request.params as Record<string, unknown>
}

async function createdFolder(fakeDaemon: FakeDaemon): Promise<string> {
  await expect.poll(() => fakeDaemon.scratchRequests.length).toBeGreaterThan(0)
  return fakeDaemon.scratchRequests[0]!.path
}

test.describe('with no Workspace yet', () => {
  test.use({
    scenario: { handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['Sure.'] }) } },
  })

  test('None is preselected and the Session lands under Recents', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { name: 'What’s on your mind?' })).toBeVisible()
    await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/Workspace: None/)
    const folder = await createdFolder(fakeDaemon)
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(paramsOf(draft)).toMatchObject({ cwd: folder, tags: [{ name: 'droi.draft' }, SCRATCH] })

    await form.getByRole('textbox', { name: 'Message' }).fill('A pasta recipe for two')
    await form.getByRole('button', { name: 'Start session' }).click()
    const takeover = await fakeDaemon.waitForRequest('daemon.update_session_settings')
    expect(paramsOf(takeover)).toMatchObject({ tags: [SCRATCH] })
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Sure.')

    const list = await openDrawer(page)
    await expect(list.getByRole('group', { name: 'Recents' })).toBeVisible()
  })
})

test.describe('next to Workspaces', () => {
  const project = session('Fix the build', '/Users/dev/acme-web', [userMessage('a')])
  const chat = session('Pasta recipe', `${SCRATCH_FOLDER}/2026-09-20-aaaaaa`, [userMessage('b')], {
    tags: [SCRATCH],
  })
  test.use({ scenario: { sessions: [chat, project] } })

  test('Recents sits last; None makes a folder and leaving throws the empty one away', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/acme-web\?/)
    await form.getByRole('button', { name: 'Workspace' }).click()
    const sheet = page.getByRole('dialog', { name: 'Workspace' })
    await expect(sheet.getByRole('radio')).toHaveText(['None', 'acme-web'])
    await sheet.getByRole('radio', { name: 'None' }).click()
    await expect(form.getByRole('heading', { name: 'What’s on your mind?' })).toBeVisible()
    const folder = await createdFolder(fakeDaemon)
    await fakeDaemon.waitForRequest('daemon.initialize_session', 2)

    const list = await openDrawer(page)
    await expect(list.getByRole('group')).toHaveCount(2)
    await expect(list.getByRole('group').last()).toHaveAccessibleName('Recents')
    await pickSession(page, /Fix the build/)
    await expect
      .poll(() => fakeDaemon.scratchRequests)
      .toContainEqual({ action: 'trash', path: folder })
  })

  test('archiving a Scratch Session moves its folder to the Trash', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    const list = await openDrawer(page)
    await list.getByRole('button', { name: /Pasta recipe/ }).click({ delay: 800 })
    await page
      .getByRole('dialog', { name: /^Actions for / })
      .getByRole('button', { name: 'Archive' })
      .click()
    await fakeDaemon.waitForRequest('daemon.archive_session')
    await expect
      .poll(() => fakeDaemon.scratchRequests)
      .toContainEqual({ action: 'trash', path: chat.cwd })
  })
})
