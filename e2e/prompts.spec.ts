import { expect, test, openPairingLink, pickSession } from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'
import { askUserTurn, permissionTurn } from './fake-daemon/turns'

const chat = session('Deploy', '/Users/dev/acme-web', [userMessage('hi')])

async function openAndSend(page: import('@playwright/test').Page, text: string) {
  await pickSession(page, /Deploy/)
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
  await page.getByRole('button', { name: 'Send' }).click()
}

test.describe('permission requests', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': permissionTurn({
          command: 'rm -rf build',
          reply: 'Build directory removed.',
          toolOutput: '[Process exited with code 0]',
        }),
      },
    },
  })

  test('allow sends proceed_once and the turn continues', async ({
    page,
    fakeDaemon,
    openClient,
  }) => {
    await openClient()
    await openAndSend(page, 'Clean the build dir')

    const card = page.getByRole('group', { name: 'Permission request: Execute' })
    await expect(card).toBeVisible()
    await expect(card).toContainText('rm -rf build')
    await expect(page.getByRole('status', { name: 'Session activity' })).toHaveText(/approval/)

    const allow = card.getByRole('button', { name: 'Yes, allow' })
    await expect(allow).toBeFocused()
    await page.keyboard.press('Enter')

    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('Build directory removed.')
    await expect(page.getByRole('status', { name: 'Session activity' })).toHaveText('')
    // The answer went back on the Daemon's request id with the chosen option.
    const answered = fakeDaemon.requests.length
    expect(answered).toBeGreaterThan(0)
  })

  test('deny sends cancel and the turn ends without running the tool', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: Execute' })
    await card.getByRole('button', { name: 'No, cancel' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('I will not run that')
    await expect(transcript).not.toContainText('Build directory removed.')
    await expect(page.getByRole('status', { name: 'Session activity' })).toHaveText('')
  })
})

test.describe('ask-user questions', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': askUserTurn({
          question: 'Which environment?',
          options: ['staging', 'production'],
        }),
      },
    },
  })

  test('the chosen answer is sent back and the turn continues', async ({ page, openClient }) => {
    await openClient()
    await openAndSend(page, 'Deploy it')

    const card = page.getByRole('group', { name: 'Droid has a question' })
    await expect(card).toContainText('Which environment?')
    const answer = card.getByRole('button', { name: 'Answer' })
    await expect(answer).toBeDisabled()
    await expect(card).toContainText('Choice')
    await card.getByRole('radio', { name: 'staging' }).click()
    await expect(card.getByRole('radio', { name: 'staging' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    await expect(answer).toBeEnabled()
    await answer.click()

    await expect(card).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('You chose staging.')
  })

  test('Cancel declines the question without an answer', async ({ page, openClient }) => {
    await openClient()
    await openAndSend(page, 'Deploy it')

    const card = page.getByRole('group', { name: 'Droid has a question' })
    await card.getByRole('button', { name: 'Cancel' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('I will not ask')
    await expect(transcript).not.toContainText('You chose')
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
  })

  test('Escape inside the question cancels it too', async ({ page, openClient }) => {
    await openClient()
    await openAndSend(page, 'Deploy it')

    const card = page.getByRole('group', { name: 'Droid has a question' })
    await card.getByRole('textbox', { name: /Other answer/ }).fill('half typed')
    await card.getByRole('textbox', { name: /Other answer/ }).press('Escape')
    await expect(card).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('I will not ask')
  })
})

test.describe('two Clients on one Session (ADR 0003)', () => {
  test.use({
    scenario: {
      sessions: [chat],
      handlers: {
        'daemon.add_user_message': permissionTurn({
          command: 'npm test',
          reply: 'All tests pass.',
        }),
      },
    },
  })

  test('both see the prompt; the first answer wins and clears it everywhere', async ({
    browser,
    fakeDaemon,
    page: desktop,
    openClient,
  }) => {
    await openClient()
    const phoneContext = await browser.newContext({ viewport: { width: 390, height: 844 } })
    const phone = await phoneContext.newPage()
    await openPairingLink(phone, fakeDaemon, fakeDaemon.token)
    await pickSession(phone, /Deploy/)
    await expect(phone.getByRole('log', { name: 'Transcript' })).toBeVisible()

    await openAndSend(desktop, 'Run the tests')

    const desktopCard = desktop.getByRole('group', { name: 'Permission request: Execute' })
    const phoneCard = phone.getByRole('group', { name: 'Permission request: Execute' })
    await expect(desktopCard).toBeVisible()
    await expect(phoneCard).toBeVisible()
    // The phone sees the user message the desktop sent.
    await expect(phone.getByRole('log', { name: 'Transcript' })).toContainText('Run the tests')

    // Both answer before either hears back; exactly one wins, both cards
    // clear, the turn runs once. dispatchEvent skips actionability waits, which
    // would otherwise hang on the loser's card as it disappears.
    await phoneCard.getByRole('button', { name: 'Yes, allow' }).dispatchEvent('click')
    await desktopCard
      .getByRole('button', { name: 'Yes, allow' })
      .dispatchEvent('click', undefined, { timeout: 1_000 })
      .catch(() => undefined)
    await expect(desktopCard).toHaveCount(0)
    await expect(phoneCard).toHaveCount(0)
    await expect(desktop.getByRole('log', { name: 'Transcript' })).toContainText('All tests pass.')
    await expect(phone.getByRole('log', { name: 'Transcript' })).toContainText('All tests pass.')
    expect(await desktop.getByText('All tests pass.').count()).toBe(1)
    await expect(desktop.getByRole('alert')).toHaveCount(0)
    await expect(phone.getByRole('alert')).toHaveCount(0)

    await phoneContext.close()
  })
})
