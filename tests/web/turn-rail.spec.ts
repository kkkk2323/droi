import { expect, test } from './fixtures'
import {
  assistantMessage,
  session,
  toolCallMessage,
  toolResultMessage,
  userMessage,
} from '../fake-daemon/scenario'

const long = Array.from({ length: 30 }, (_, i) => `Paragraph ${i + 1} of the reply.`).join('\n\n')
const chat = session(
  'Long chat',
  '/Users/dev/acme-web',
  Array.from({ length: 6 }, (_, i) => [
    userMessage(`Question number ${i + 1}`),
    assistantMessage(`Answer ${i + 1} begins here.\n\n${long}`),
  ]).flat(),
)

// Long runs of tool calls: the 400 messages a Session opens with hold one of the user's.
const toolRun = (name: string, count: number) =>
  Array.from({ length: count }, (_, i) => [
    toolCallMessage(`${name}-${i}`, 'Read', { file_path: `/Users/dev/acme-web/f${i}.ts` }),
    toolResultMessage(`${name}-${i}`, 'ok'),
  ]).flat()
const agentRun = session('Long agent run', '/Users/dev/acme-web', [
  userMessage('First task'),
  ...toolRun('first', 250),
  assistantMessage('First task done.'),
  userMessage('Second task'),
  ...toolRun('second', 250),
  assistantMessage('Second task done.'),
  userMessage('Third task'),
  assistantMessage('Third task done.'),
])

test.describe('the rail of your messages', () => {
  test.use({ scenario: { sessions: [chat, agentRun] } })
  test.skip(() => test.info().project.name === 'phone', 'the rail needs room beside the column')

  test('previews a message on hover and jumps to it on click', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Long chat/)
    const rail = page.getByRole('navigation', { name: 'Your messages' })
    const marks = rail.getByRole('button')
    await expect(marks).toHaveCount(6)
    await expect(marks.last()).toHaveAttribute('aria-current', 'true')

    const first = rail.getByRole('button', { name: 'Jump to message 1' })
    await first.hover()
    const preview = page.getByRole('tooltip')
    await expect(preview).toContainText('Question number 1')
    await expect(preview).toContainText('Answer 1 begins here.')

    await first.click()
    const question = page
      .getByRole('log', { name: 'Transcript' })
      .getByRole('article', { name: 'You' })
      .filter({ hasText: 'Question number 1' })
    await expect(question).toBeInViewport()
    await expect(first).toHaveAttribute('aria-current', 'true')
    await expect(page.getByRole('button', { name: 'Scroll to latest' })).toBeVisible()

    // The arrow keys walk the marks; the focused one previews.
    await first.press('ArrowDown')
    await expect(rail.getByRole('button', { name: 'Jump to message 2' })).toBeFocused()
    await expect(preview).toContainText('Question number 2')
    await page.keyboard.press('Enter')
    await expect(
      page.getByRole('article', { name: 'You' }).filter({ hasText: 'Question number 2' }),
    ).toBeInViewport()
  })

  test('marks the messages not loaded yet and loads the way to one', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Long agent run/)
    const rail = page.getByRole('navigation', { name: 'Your messages' })
    await expect(rail.getByRole('button')).toHaveCount(3)
    const userOnly = fakeDaemon.requests.filter(
      (r) =>
        r.method === 'daemon.get_session_messages' &&
        (r.params as Record<string, unknown>)['role'] === 'user',
    )
    expect(userOnly).toHaveLength(1)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const first = transcript.getByRole('article', { name: 'You' }).filter({ hasText: 'First task' })
    await expect(first).toHaveCount(0)

    const mark = rail.getByRole('button', { name: 'Jump to message 1' })
    await mark.hover()
    await expect(page.getByRole('tooltip')).toContainText('First task')
    await mark.click()
    await expect(first).toBeInViewport()
    await expect(mark).toHaveAttribute('aria-current', 'true')
    // Every page up to the message landed; nothing older is left.
    await expect(page.getByRole('button', { name: 'Load previous messages' })).toHaveCount(0)
  })
})
