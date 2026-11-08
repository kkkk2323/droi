import type { Page } from '@playwright/test'
import { CONTEXT_WINDOW } from './fake-kernel/fake-kernel'
import { assistantEntry, exchange, userEntry } from './fake-kernel/history'
import { expect, pairPhone, pickSession, relaunch, test } from './fixtures'

async function type(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
}

const transcript = (page: Page) => page.getByRole('log', { name: 'Transcript' })

test.describe('a turn from the phone', () => {
  test.use({
    scenario: {
      sessions: [
        { title: 'Chat', cwd: 'acme-web', entries: exchange('hello', 'Hi! How can I help?') },
        { title: 'Notes', cwd: 'acme-web', entries: [userEntry('todo')] },
      ],
    },
  })

  test('sends, streams and cancels', async ({ page, fakeKernel, phoneWire }) => {
    fakeKernel.model.push({
      match: 'Tell me a story',
      chunks: ['Part one. ', 'Part two. ', 'Part three.'],
      delayMs: 800,
    })
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    await expect(page.getByRole('button', { name: 'Send' })).toBeDisabled()
    await type(page, 'Tell me a story')
    await page.getByRole('button', { name: 'Send' }).click()
    expect(await phoneWire.waitForCall('conversation.submit')).toMatchObject({
      input: 'Tell me a story',
    })
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('')

    await expect(
      transcript(page).getByRole('article', { name: 'You' }).filter({ hasText: 'Tell me a story' }),
    ).toBeVisible()
    const reply = transcript(page)
      .getByRole('article', { name: 'Assistant' })
      .filter({ hasText: 'Part one.' })
    await expect(reply).toContainText('Part one.')
    await page.getByRole('button', { name: 'Cancel' }).click()
    await phoneWire.waitForCall('conversation.abort')
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    await expect(reply).toContainText('Part one.')
    await page.waitForTimeout(2_000)
    await expect(reply).not.toContainText('Part three.')
  })

  test('three queued messages fold into one row that opens; one can be removed', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    fakeKernel.model.push({
      match: 'first',
      chunks: Array.from({ length: 30 }, (_, i) => `Step ${i}. `),
      delayMs: 500,
    })
    await pairPhone(page, fakeKernel)
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
    expect(await phoneWire.waitForCall('conversation.submit', 4)).toMatchObject({ input: 'three' })

    const fold = page.getByRole('button', { name: 'Queued messages, 3' })
    await expect(fold).toHaveAttribute('aria-expanded', 'false')
    await expect(fold).toContainText('3 queued')
    const queued = page.getByRole('list', { name: 'Queued messages' })
    await expect(queued).toHaveCount(0)
    await fold.click()
    await expect(queued.getByRole('listitem')).toHaveText([/one/, /two/, /three/])
    await queued.getByRole('button', { name: 'Remove queued message' }).nth(1).click()
    await phoneWire.waitForCall('submission.abort')
    await expect(queued.getByRole('listitem')).toHaveText([/one/, /three/])
    await expect(fold).toHaveCount(0)
  })

  test('cancelling keeps a queued message here, paused, to put back in the composer', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    fakeKernel.model.push({
      match: 'first',
      chunks: Array.from({ length: 30 }, (_, i) => `Step ${i}. `),
      delayMs: 500,
    })
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    await type(page, 'first')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
    await type(page, 'keep me')
    await page.getByRole('button', { name: 'Queue', exact: true }).click()
    await phoneWire.waitForCall('conversation.submit', 2)
    const queued = page.getByRole('list', { name: 'Queued messages' })
    await expect(queued.getByRole('listitem')).toHaveCount(1)

    await page.getByRole('button', { name: 'Cancel' }).click()
    await phoneWire.waitForCall('conversation.abort')
    await expect(queued.getByRole('listitem')).toHaveText([/keep me.*Paused/])
    // Nothing reached the model for it.
    expect(fakeKernel.model.turns().some((r) => r.prompt.includes('keep me'))).toBe(false)
    await queued.getByRole('button', { name: 'Edit queued message' }).click()
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('keep me')
    await expect(queued).toHaveCount(0)
  })

  test('a draft survives switching Sessions', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    await type(page, 'half a thought')
    await pickSession(page, /Notes/)
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('')
    await pickSession(page, /Chat/)
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('half a thought')
    await relaunch(page)
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('half a thought')
  })
})

test.describe('the context meter', () => {
  // The Kernel counts four characters of the context to a token: about 50k here.
  test.use({
    scenario: {
      sessions: [
        {
          title: 'Chat',
          cwd: 'acme-web',
          entries: [userEntry('read it all'), assistantEntry('x'.repeat(200_000))],
        },
      ],
    },
  })

  test('follows the Kernel’s count as the conversation grows', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    const meter = page.getByRole('meter', { name: 'Context used' })
    await expect(meter).toHaveAttribute('aria-valuenow', '25')
    await expect(meter).toContainText(`50k / ${CONTEXT_WINDOW / 1000}k · 25%`)

    fakeKernel.model.push({ match: 'go on', text: 'y'.repeat(40_000) })
    await type(page, 'go on')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(transcript(page)).toContainText('yyyy')
    await fakeKernel.settled('Chat')
    const usage = await fakeKernel.kernel.call<{ context: { used: number; limit: number } }>(
      'conversation.usage',
      {
        id: fakeKernel.session('Chat').conversationId,
      },
    )
    const percent = Math.round((usage.context.used / usage.context.limit) * 100)
    expect(percent).toBeGreaterThan(25)
    await expect(meter).toHaveAttribute('aria-valuenow', String(percent))
    await expect(meter).toContainText(
      `${Math.round(usage.context.used / 1000)}k / ${CONTEXT_WINDOW / 1000}k · ${percent}%`,
    )
  })
})
