import { expect, test } from './fixtures'
import { session, userMessage, assistantMessage } from '../fake-daemon/scenario'

const long = Array.from({ length: 30 }, (_, i) => `Paragraph ${i + 1} of the reply.`).join('\n\n')
const chat = session(
  'Long chat',
  '/Users/dev/acme-web',
  Array.from({ length: 6 }, (_, i) => [
    userMessage(`Question number ${i + 1}`),
    assistantMessage(`Answer ${i + 1} begins here.\n\n${long}`),
  ]).flat(),
)

test.describe('the rail of your messages', () => {
  test.use({ scenario: { sessions: [chat] } })
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
})
