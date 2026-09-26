// Sessions without a Workspace: Workspace None starts one in a fresh Scratch
// Workspace the Gateway makes, listed under Recents (ADR 0008).
import { expect, test } from './fixtures'
import { SCRATCH_FOLDER, type FakeDaemon, type RecordedRequest } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'

const SCRATCH = { name: 'droi.scratch' }

function paramsOf(request: RecordedRequest): Record<string, unknown> {
  return request.params as Record<string, unknown>
}

async function createdFolder(fakeDaemon: FakeDaemon, nth = 1): Promise<string> {
  await expect
    .poll(() => fakeDaemon.scratchRequests.filter((r) => r.action === 'create').length)
    .toBeGreaterThanOrEqual(nth)
  return fakeDaemon.scratchRequests.filter((r) => r.action === 'create')[nth - 1]!.path
}

test.describe('with no Workspace yet', () => {
  test.use({
    scenario: { handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['Sure.'] }) } },
  })

  test('None is preselected and the Session lands under Recents', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { level: 2 })).toContainText('What’s on your mind?')
    await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/Workspace: None/)

    const folder = await createdFolder(fakeDaemon)
    expect(folder.startsWith(`${SCRATCH_FOLDER}/`)).toBe(true)
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
    expect(paramsOf(draft)).toMatchObject({ cwd: folder, tags: [{ name: 'droi.draft' }, SCRATCH] })

    await form.getByRole('textbox', { name: 'Message' }).fill('A pasta recipe for two')
    await form.getByRole('button', { name: 'Start session' }).click()
    const takeover = await fakeDaemon.waitForRequest('daemon.update_session_settings')
    expect(paramsOf(takeover)).toMatchObject({
      sessionId: paramsOf(draft)['sessionId'],
      tags: [SCRATCH],
    })
    await expect(page).toHaveURL(new RegExp(`#/s/${String(paramsOf(draft)['sessionId'])}`))

    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Sure.')
    const recents = (await openSidebar()).getByRole('region', { name: 'Recents' })
    await expect(recents.getByRole('listitem').first().getByRole('button')).toHaveAttribute(
      'aria-current',
      'page',
    )
  })
})

test.describe('next to Workspaces', () => {
  const project = session('Fix the build', '/Users/dev/acme-web', [userMessage('a')])
  const chat = session('Pasta recipe', `${SCRATCH_FOLDER}/2026-09-20-aaaaaa`, [userMessage('b')], {
    tags: [SCRATCH],
  })
  test.use({ scenario: { sessions: [chat, project] } })

  test('Recents sits last, and Scratch folders never show up as recent Workspaces', async ({
    page,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { level: 2 })).toContainText('acme-web')
    await form.getByRole('button', { name: 'Workspace' }).click()
    await expect(page.getByRole('menu').getByRole('menuitemradio')).toHaveText(['None', 'acme-web'])
    await page.keyboard.press('Escape')

    const sidebar = await openSidebar()
    await expect(sidebar.getByRole('region')).toHaveCount(2)
    await expect(sidebar.getByRole('region').last()).toHaveAccessibleName('Recents')
    await expect(
      sidebar
        .getByRole('region', { name: 'Recents' })
        .getByRole('button', { name: /Pasta recipe/ }),
    ).toBeVisible()
  })

  test('picking None makes a folder; going back to a Workspace throws the empty one away', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()
    const form = page.getByRole('region', { name: 'New session' })
    await fakeDaemon.waitForRequest('daemon.initialize_session')
    await form.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitemradio', { name: 'None' }).click()
    await expect(form.getByRole('heading', { level: 2 })).toContainText('What’s on your mind?')
    const folder = await createdFolder(fakeDaemon)
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session', 2)
    expect(paramsOf(draft)).toMatchObject({ cwd: folder })

    await form.getByRole('button', { name: 'Workspace' }).click()
    await page.getByRole('menuitemradio', { name: 'acme-web' }).click()
    await expect(form.getByRole('heading', { level: 2 })).toContainText('acme-web')
    await expect
      .poll(() => fakeDaemon.scratchRequests)
      .toContainEqual({ action: 'trash', path: folder })
  })

  test('the + on Recents starts a Session with None', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    const recents = (await openSidebar()).getByRole('region', { name: 'Recents' })
    await recents.getByRole('heading').hover()
    await recents.getByRole('button', { name: 'New session in Recents' }).click()
    expect(new URL(page.url()).hash).toBe('#/new?scratch')
    const form = page.getByRole('region', { name: 'New session' })
    await expect(form.getByRole('heading', { level: 2 })).toContainText('What’s on your mind?')
    await createdFolder(fakeDaemon)
  })
})

test.describe('archiving', () => {
  const folder = `${SCRATCH_FOLDER}/2026-09-20-bbbbbb`
  const earlier = session('Trip plan', folder, [userMessage('a')], { tags: [SCRATCH] })
  const later = session('Trip plan', folder, [userMessage('b')], {
    tags: [SCRATCH, { name: 'droi.continues', metadata: { parent: earlier.sessionId } }],
  })
  test.use({ scenario: { sessions: [later, earlier] } })

  test('takes the conversation and its folder away; unarchiving brings the folder back first', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    let sidebar = await openSidebar()
    await sidebar.getByRole('button', { name: /Trip plan/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Archive' }).click()

    await expect
      .poll(() => fakeDaemon.scratchRequests)
      .toContainEqual({ action: 'trash', path: folder })
    const archived = fakeDaemon.requests
      .filter((r) => r.method === 'daemon.archive_session')
      .map((r) => paramsOf(r)['sessionId'])
    expect(archived).toEqual([later.sessionId, earlier.sessionId])
    await expect(sidebar.getByRole('button', { name: /Trip plan/ })).toHaveCount(0)

    await sidebar.getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('switch', { name: 'Show archived sessions' }).check()
    await page.getByRole('button', { name: 'Back' }).click()
    sidebar = await openSidebar()
    await sidebar.getByRole('button', { name: /Trip plan/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Unarchive' }).click()
    await fakeDaemon.waitForRequest('daemon.unarchive_session', 2)
    expect(fakeDaemon.scratchRequests.at(-1)).toEqual({ action: 'restore', path: folder })
  })
})
