import type { Page } from '@playwright/test'
import { assistantMessage, CONTEXT_BUDGET, session, userMessage } from '../fake-daemon/scenario'
import {
  interruptHandler,
  resolveQueuedHandler,
  streamedReply,
  todoTurn,
  withQueue,
} from '../fake-daemon/turns'
import { expect, pairPhone, pickSession, test } from './fixtures'

const chat = session('Chat', '/Users/dev/acme-web', [
  userMessage('hello'),
  assistantMessage('Hi! How can I help?'),
])
const notes = session('Notes', '/Users/dev/acme-web', [userMessage('todo')])

async function type(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
}

test.describe('a turn from the phone', () => {
  test.use({
    scenario: {
      sessions: [chat, notes],
      handlers: {
        'daemon.add_user_message': withQueue(
          streamedReply({ deltas: ['Part one. ', 'Part two. ', 'Part three.'], delayMs: 600 }),
        ),
        'daemon.interrupt_session': interruptHandler,
        'daemon.resolve_queued_user_message': resolveQueuedHandler,
      },
    },
  })

  test('sends, streams and cancels', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    await expect(page.getByRole('button', { name: 'Send' })).toBeDisabled()
    await type(page, 'Tell me a story')
    await page.getByRole('button', { name: 'Send' }).click()
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ text: 'Tell me a story' })
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('')

    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' }).last()).toContainText(
      'Tell me a story',
    )
    const reply = transcript.getByRole('article', { name: 'Assistant' }).last()
    await expect(reply).toContainText('Part one.')
    await page.getByRole('button', { name: 'Cancel' }).click()
    await fakeDaemon.waitForRequest('daemon.interrupt_session')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(reply).toContainText('Part one.')
    await expect(reply).not.toContainText('Part three.')
  })

  test('three queued messages fold into one row that opens; one can be removed', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    await type(page, 'first')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveAttribute(
      'placeholder',
      'Queue a message',
    )
    for (const text of ['one', 'two', 'three']) {
      await type(page, text)
      await page.getByRole('button', { name: 'Queue', exact: true }).click()
    }
    const fourth = await fakeDaemon.waitForRequest('daemon.add_user_message', 4)
    expect(fourth.params).toMatchObject({ text: 'three', queuePlacement: 'end_of_loop' })

    const fold = page.getByRole('button', { name: 'Queued messages, 3' })
    await expect(fold).toHaveAttribute('aria-expanded', 'false')
    await expect(fold).toContainText('3 queued')
    const queued = page.getByRole('list', { name: 'Queued messages' })
    await expect(queued).toHaveCount(0)
    await fold.click()
    await expect(queued.getByRole('listitem')).toHaveText([/one/, /two/, /three/])
    await queued.getByRole('button', { name: 'Remove queued message' }).nth(1).click()
    const removed = await fakeDaemon.waitForRequest('daemon.resolve_queued_user_message')
    expect(removed.params).toMatchObject({ action: 'delete' })
    await expect(queued.getByRole('listitem')).toHaveText([/one/, /three/])
    await expect(fold).toHaveCount(0)
  })

  test('a draft survives switching Sessions', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    await type(page, 'half a thought')
    await pickSession(page, /Notes/)
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('')
    await pickSession(page, /Chat/)
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('half a thought')
    await page.reload()
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('half a thought')
  })
})

test.describe('task list and context meter', () => {
  test.use({
    scenario: {
      sessions: [chat],
      contextUsedTokens: 50_000,
      handlers: {
        'daemon.add_user_message': todoTurn({
          todos: [
            { id: '1', content: 'Read the config', status: 'completed' },
            { id: '2', content: 'Write the migration', status: 'in_progress' },
            { id: '3', content: 'Run the tests', status: 'pending' },
          ],
          reply: 'Working through it.',
        }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('follow the Daemon’s updates', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    const meter = page.getByRole('meter', { name: 'Context used' })
    await expect(meter).toHaveAttribute('aria-valuenow', '25')
    await expect(meter).toContainText(`50k / ${CONTEXT_BUDGET / 1000}k · 25%`)

    await type(page, 'go')
    await page.getByRole('button', { name: 'Send' }).click()
    const summary = page.getByRole('button', { name: 'Tasks, 1 of 3 done' })
    await expect(summary).toContainText('Write the migration')
    await summary.click()
    const tasks = page.getByRole('list', { name: 'Tasks' }).getByRole('listitem')
    await expect(tasks).toHaveCount(3)
    await expect(tasks.first()).toHaveAccessibleName('Done: Read the config')
    await expect(tasks.nth(2)).toHaveAccessibleName('Pending: Run the tests')
    await expect(meter).toContainText(`10k / ${CONTEXT_BUDGET / 1000}k · 5%`)
  })
})
