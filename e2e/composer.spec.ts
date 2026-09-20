// The composer around a running turn: queued and injected messages, pasted
// images, the task list and the context meter.
import type { Page } from '@playwright/test'
import { expect, test } from './fixtures'
import { CONTEXT_BUDGET, session, userMessage, assistantMessage } from './fake-daemon/scenario'
import {
  interruptHandler,
  resolveQueuedHandler,
  streamedReply,
  todoTurn,
  withQueue,
} from './fake-daemon/turns'

const chat = session('Chat', '/Users/dev/acme-web', [
  userMessage('hello'),
  assistantMessage('Hi! How can I help?'),
])

// A 1x1 PNG.
const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

async function pasteImage(page: Page, name = 'shot.png'): Promise<void> {
  await page.getByRole('textbox', { name: 'Message' }).evaluate(
    (element, { base64, fileName }) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0))
      const file = new File([bytes], fileName, { type: 'image/png' })
      const data = new DataTransfer()
      data.items.add(file)
      element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true }))
    },
    { base64: PNG_BASE64, fileName: name },
  )
}

test.describe('sending while a turn runs', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': withQueue(
          streamedReply({ deltas: ['One. ', 'Two. ', 'Three.'], delayMs: 500 }),
        ),
        'daemon.interrupt_session': interruptHandler,
        'daemon.resolve_queued_user_message': resolveQueuedHandler,
      },
    },
  })

  test('Enter queues for after the turn, ⌘Enter hands it to the running turn', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('first')
    await input.press('Enter')
    await fakeDaemon.waitForRequest('daemon.add_user_message')
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()

    // The composer stays usable; Send becomes Queue (⌘↩ still inserts).
    await expect(input).toHaveAttribute('placeholder', /Queue a message/)
    await input.fill('later')
    await input.press('Enter')
    const queued = page.getByRole('list', { name: 'Queued messages' })
    await expect(queued.getByRole('listitem')).toHaveCount(1)
    await expect(queued).toContainText('later')
    await expect(queued).toContainText('Queued')
    const second = await fakeDaemon.waitForRequest('daemon.add_user_message', 2)
    expect(second.params).toMatchObject({ text: 'later', queuePlacement: 'end_of_loop' })

    await input.fill('now')
    await input.press('Meta+Enter')
    await expect(queued.getByRole('listitem')).toHaveCount(2)
    await expect(queued.getByRole('listitem').nth(1)).toContainText('Next')
    const third = await fakeDaemon.waitForRequest('daemon.add_user_message', 3)
    expect(third.params).toMatchObject({ text: 'now', queuePlacement: 'end_of_turn' })

    // The injected message runs first once the turn ends, then the queued one.
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const you = transcript.getByRole('article', { name: 'You' })
    await expect(you).toHaveCount(4, { timeout: 15_000 })
    await expect(you.nth(2)).toContainText('now')
    await expect(you.nth(3)).toContainText('later')
    await expect(queued).toHaveCount(0)
  })

  test('a queued message can be removed before it goes out', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('first')
    await input.press('Enter')
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
    await input.fill('never mind')
    await page.getByRole('button', { name: 'Queue' }).click()
    const queued = page.getByRole('list', { name: 'Queued messages' })
    await expect(queued.getByRole('listitem')).toHaveCount(1)
    await queued.getByRole('button', { name: 'Remove queued message' }).click()
    const removed = await fakeDaemon.waitForRequest('daemon.resolve_queued_user_message')
    expect(removed.params).toMatchObject({ action: 'delete' })
    await expect(queued).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('log', { name: 'Transcript' })).not.toContainText('never mind')
  })
})

test.describe('images in the composer', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': streamedReply({ deltas: ['Nice screenshot.'], delayMs: 50 }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('a pasted image shows as a chip, can be removed, and goes out as an image block', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const attachments = page.getByRole('list', { name: 'Attachments' })
    await pasteImage(page, 'first.png')
    await pasteImage(page, 'second.png')
    await expect(attachments.getByRole('img')).toHaveCount(2)
    await page.getByRole('button', { name: 'Remove first.png' }).click()
    await expect(attachments.getByRole('img')).toHaveCount(1)

    // An image alone is enough to send.
    const send = page.getByRole('button', { name: 'Send' })
    await expect(send).toBeEnabled()
    await page.getByRole('textbox', { name: 'Message' }).fill('What is this?')
    await send.click()
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    // The Daemon reads images from `images`, not from `content`.
    expect(sent.params).toMatchObject({
      text: 'What is this?',
      images: [{ type: 'base64', mediaType: 'image/png', data: PNG_BASE64 }],
    })
    expect(sent.params).not.toHaveProperty('content')
    await expect(attachments).toHaveCount(0)

    const you = page.getByRole('log', { name: 'Transcript' }).getByRole('article', { name: 'You' })
    await expect(you.last().getByRole('img', { name: 'Attached image' })).toBeVisible()
    await expect(you.last()).toContainText('What is this?')
  })

  test('Enter right after a paste waits for the image instead of dropping it', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('quick')
    // Paste and submit in the same tick, before the image has been read.
    await input.evaluate((element, base64) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0))
      const data = new DataTransfer()
      data.items.add(new File([bytes], 'fast.png', { type: 'image/png' }))
      element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true }))
      element.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
      )
    }, PNG_BASE64)
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ text: 'quick', images: [{ mediaType: 'image/png' }] })
    await expect(input).toHaveValue('')
    await expect(page.getByRole('list', { name: 'Attachments' })).toHaveCount(0)
  })

  test('the plus button opens a picker; chosen images attach like a paste', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const chooser = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: 'Add image' }).click()
    await (
      await chooser
    ).setFiles({
      name: 'photo.png',
      mimeType: 'image/png',
      buffer: Buffer.from(PNG_BASE64, 'base64'),
    })
    await expect(page.getByRole('list', { name: 'Attachments' }).getByRole('img')).toHaveCount(1)
    await page.getByRole('button', { name: 'Send' }).click()
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ images: [{ mediaType: 'image/png', data: PNG_BASE64 }] })
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

  test('todos from TodoWrite sit above the composer and unfold', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    await page.getByRole('textbox', { name: 'Message' }).fill('go')
    await page.getByRole('button', { name: 'Send' }).click()
    const summary = page.getByRole('button', { name: 'Tasks, 1 of 3 done' })
    await expect(summary).toBeVisible()
    await expect(summary).toContainText('Write the migration')
    await summary.click()
    const tasks = page.getByRole('list', { name: 'Tasks' })
    await expect(tasks.getByRole('listitem')).toHaveCount(3)
    await expect(tasks.getByRole('listitem').first()).toHaveAttribute('data-status', 'completed')
    await expect(tasks.getByRole('listitem').nth(2)).toContainText('Run the tests')
  })

  test('the meter shows used tokens against the budget', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const meter = page.getByRole('meter', { name: 'Context used' })
    await expect(meter).toBeVisible()
    // Before any call the Daemon's estimate stands in.
    await expect(meter).toHaveAttribute('aria-valuenow', '25')
    await expect(meter).toContainText(`50k / ${CONTEXT_BUDGET / 1000}k · 25%`)

    // After a call the meter shows what that call actually sent.
    await page.getByRole('textbox', { name: 'Message' }).fill('go')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(meter).toContainText(`10k / ${CONTEXT_BUDGET / 1000}k · 5%`)
  })
})

test.describe('finished task list', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': todoTurn({
          todos: [
            { id: '1', content: 'Read the config', status: 'completed' },
            { id: '2', content: 'Run the tests', status: 'completed' },
          ],
          reply: 'All done.',
        }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('a list with everything done is not shown above the composer', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    await page.getByRole('textbox', { name: 'Message' }).fill('go')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('All done.')
    await expect(page.getByRole('button', { name: /^Tasks,/ })).toHaveCount(0)
  })
})

test.describe('drafts', () => {
  test.use({
    scenario: {
      sessions: [chat, session('Other', '/Users/dev/acme-web', [userMessage('yo')])],
    },
  })

  test('what was typed is still there after visiting another Session', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('half a thought')
    await pickSession(/Other/)
    await expect(input).toHaveValue('')
    await pickSession(/Chat/)
    await expect(input).toHaveValue('half a thought')

    // The text outlives a reload; emptying the box drops it.
    await page.reload()
    await pickSession(/Chat/)
    await expect(input).toHaveValue('half a thought')
    await input.fill('')
    await pickSession(/Other/)
    await pickSession(/Chat/)
    await expect(input).toHaveValue('')
  })
})
