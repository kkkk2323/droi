import { expect, test } from './fixtures'
import { session, userMessage, assistantMessage } from './fake-daemon/scenario'
import { interruptHandler, streamedReply } from './fake-daemon/turns'

const chat = session('Chat', '/Users/dev/acme-web', [
  userMessage('hello'),
  assistantMessage('Hi! How can I help?'),
])

test.describe('sending a prompt', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': streamedReply({
          deltas: ['The answer ', 'is forty-', 'two.'],
          delayMs: 200,
        }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('shows the user message at once, streams the reply, then settles idle', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('How can I help?')

    const input = page.getByRole('textbox', { name: 'Message' })
    const send = page.getByRole('button', { name: 'Send' })
    await expect(send).toBeDisabled()
    await input.fill('What is the answer?')
    await expect(send).toBeEnabled()
    await input.press('Enter')

    await expect(transcript.getByRole('article', { name: 'You' }).last()).toContainText(
      'What is the answer?',
    )
    await expect(input).toHaveValue('')

    const activity = page.getByRole('status', { name: 'Session activity' })
    const cancel = page.getByRole('button', { name: 'Cancel' })
    await expect(cancel).toBeVisible()
    await expect(activity).not.toHaveText('')
    // The activity row sits in the transcript, after the last message.
    await expect(transcript.getByRole('status', { name: 'Session activity' })).toBeVisible()

    const assistant = transcript.getByRole('article', { name: 'Assistant' }).last()
    await expect(assistant).toContainText('The answer')
    await expect(assistant).toContainText('The answer is forty-')
    await expect(assistant).toContainText('The answer is forty-two.')

    await expect(activity).toHaveText('')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(cancel).toHaveCount(0)

    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ sessionId: chat.sessionId, text: 'What is the answer?' })
    expect(await transcript.getByRole('article', { name: 'You' }).count()).toBe(2)
  })

  test('send is disabled for empty input; a running turn offers Cancel and Queue', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    const send = page.getByRole('button', { name: 'Send' })
    await expect(send).toBeDisabled()
    await input.fill('   ')
    await expect(send).toBeDisabled()
    await input.fill('go')
    await send.click()
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
    await expect(send).toHaveCount(0)
    // Nothing typed: only Stop shows. Queue appears with text.
    await expect(page.getByRole('button', { name: 'Queue' })).toHaveCount(0)
    await input.fill('later')
    await expect(page.getByRole('button', { name: 'Queue' })).toBeVisible()
    await input.fill('')
    await expect(send).toBeVisible({ timeout: 10_000 })
  })
})

test.describe('sending from further up the transcript', () => {
  const long = session(
    'Long',
    '/Users/dev/acme-web',
    Array.from({ length: 30 }, (_, i) =>
      i % 2 === 0 ? userMessage(`question ${i}`) : assistantMessage(`answer ${i}\n`.repeat(6)),
    ),
  )
  test.use({
    scenario: {
      sessions: [long],
      handlers: {
        'daemon.add_user_message': streamedReply({ deltas: ['Right away.'], delayMs: 100 }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('the new message scrolls into view even when the list was scrolled up', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Long/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('question 28')
    // The list re-pins itself to the end shortly after opening; scroll up after that.
    await page.waitForTimeout(300)
    await transcript.evaluate((el) => el.scrollTo({ top: 0 }))
    await expect(transcript.getByText('question 28')).not.toBeInViewport()

    await page.getByRole('textbox', { name: 'Message' }).fill('one more')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(transcript.getByRole('article', { name: 'You' }).last()).toContainText('one more')
    await expect(transcript.getByRole('article', { name: 'You' }).last()).toBeInViewport()
    await expect(transcript.getByRole('article', { name: 'Assistant' }).last()).toContainText(
      'Right away.',
    )
    await expect(transcript.getByRole('article', { name: 'Assistant' }).last()).toBeInViewport()
  })

  test('opening lands on the latest message; a button offers the way back down', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Long/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('question 28')).toBeInViewport()
    const down = page.getByRole('button', { name: 'Scroll to latest' })
    await expect(down).toHaveCount(0)

    // The list re-pins itself to the end shortly after opening; scroll up after that.
    await page.waitForTimeout(300)
    await transcript.evaluate((el) => el.scrollTo({ top: 0 }))
    await expect(transcript.getByText('question 28')).not.toBeInViewport()
    await down.click()
    await expect(transcript.getByText('question 28')).toBeInViewport()
    await expect(down).toHaveCount(0)
  })
})

test.describe('cancelling a turn', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': streamedReply({
          deltas: ['Part one. ', 'Part two. ', 'Part three. ', 'Part four.'],
          delayMs: 700,
        }),
        'daemon.interrupt_session': interruptHandler,
      },
    },
  })

  test('interrupts the Daemon and keeps the partial output', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
    openSidebar,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await page.getByRole('textbox', { name: 'Message' }).fill('Tell me a long story')
    await page.getByRole('button', { name: 'Send' }).click()

    const assistant = transcript.getByRole('article', { name: 'Assistant' }).last()
    await expect(assistant).toContainText('Part one.')
    // The sidebar marks the busy Session while the turn runs.
    const row = (await openSidebar()).getByRole('button', { name: /Chat/ })
    await expect(row.getByRole('status', { name: 'Working' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Cancel' }).click()

    await fakeDaemon.waitForRequest('daemon.interrupt_session')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(page.getByRole('status', { name: 'Session activity' })).toHaveText('')
    await expect(assistant).toContainText('Part one.')
    await expect(assistant).not.toContainText('Part four.')
  })
})
