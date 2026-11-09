// When a Session was last active and whether it is read are the Kernel's (`lastActivityAt` and
// `viewedAt` in its list), shared by every Client; the window's list order, here by time, is the
// Gateway's. A Session is dated by its last message, and reads once anyone shows it.
import { userEntry } from './fake-kernel/history'
import { expect, inReadingOrder, openSessionList, pairPhone, pickSession, test } from './fixtures'

const now = Date.now()
const today = new Date(now)
const startOfToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
const yesterdayNoon = new Date(
  today.getFullYear(),
  today.getMonth(),
  today.getDate() - 1,
  12,
).getTime()
const day = 24 * 60 * 60 * 1000
// Today, even a minute after midnight.
const earlierToday = Math.max(startOfToday + 1_000, now - 60_000)

test.use({
  scenario: {
    window: { sessionView: 'time' },
    sessions: [
      { title: 'Sent today', cwd: 'acme-web', entries: [userEntry('ship it', earlierToday)] },
      { title: 'Sent yesterday', cwd: 'acme-api', entries: [userEntry('lint', yesterdayNoon)] },
      {
        title: 'Finished elsewhere',
        cwd: 'acme-web',
        entries: [userEntry('build', now - 3 * day)],
      },
      { title: 'Old notes', cwd: 'acme-web', entries: [userEntry('todo', now - 10 * day)] },
    ],
  },
})

const SECTIONS = /^(Pinned|Right now|Today|Yesterday|Earlier)$/

test('by time: right now, then by the day each was last active', async ({ page, fakeKernel }) => {
  // A turn the computer's window ran, which nobody has read yet.
  await fakeKernel.turn('Finished elsewhere', 'and the tests?', { text: 'All green.' })
  await fakeKernel.settled('Finished elsewhere')
  await pairPhone(page, fakeKernel)
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
  await expect(inSection('Right now', 'Finished elsewhere')).toBeVisible()
  await expect(inSection('Today', 'Sent today')).toBeVisible()
  await expect(inSection('Yesterday', 'Sent yesterday')).toBeVisible()
  await expect(inSection('Earlier', 'Old notes')).toBeVisible()

  await expect(
    inSection('Yesterday', 'Sent yesterday').getByRole('img', { name: 'In acme-api' }),
  ).toBeVisible()
  await expect(
    inSection('Right now', 'Finished elsewhere').getByRole('img', { name: 'Unread' }),
  ).toBeVisible()
})

test('opening a Session reads it on the computer', async ({ page, fakeKernel }) => {
  const finished = fakeKernel.session('Finished elsewhere').conversationId
  await fakeKernel.turn('Finished elsewhere', 'and the tests?', { text: 'All green.' })
  await fakeKernel.settled('Finished elsewhere')
  await pairPhone(page, fakeKernel)
  await pickSession(page, 'Finished elsewhere')
  // The computer's window hears it read.
  await fakeKernel.kernel.waitFor(
    (n) =>
      n.method === 'index.changed' &&
      n.params['id'] === finished &&
      Number(n.params['viewedAt']) >= Number(n.params['lastActivityAt']),
  )

  const list = await openSessionList(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)
  await expect(list.getByRole('button', { name: 'Right now' })).toHaveCount(0)
  await expect(
    list.getByRole('group', { name: 'Today' }).getByRole('button', { name: /Finished elsewhere/ }),
  ).toBeVisible()
})

test('a Session shown on the computer reads here too, and a turn there marks it again', async ({
  page,
  fakeKernel,
}) => {
  const finished = fakeKernel.session('Finished elsewhere').conversationId
  await fakeKernel.turn('Finished elsewhere', 'and the tests?', { text: 'All green.' })
  await fakeKernel.settled('Finished elsewhere')
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(1)

  await fakeKernel.kernel.call('index.update', { id: finished, viewedAt: Date.now() })
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(0)

  await fakeKernel.turn('Finished elsewhere', 'and lint?', { text: 'Clean.' })
  await expect(list.getByRole('img', { name: 'Unread' })).toHaveCount(1)
})
