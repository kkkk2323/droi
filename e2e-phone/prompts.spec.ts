import type { Page } from '@playwright/test'
import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { askUserTurn, permissionTurn } from '../e2e/fake-daemon/turns'
import { expect, pairPhone, pickSession, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('hi')])

async function openAndSend(page: Page, text: string) {
  await pickSession(page, /Deploy/)
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
  await page.getByRole('button', { name: 'Send' }).click()
}

test.describe('permission requests', () => {
  test.use({
    scenario: {
      sessions: [deploy],
      handlers: {
        'daemon.add_user_message': permissionTurn({
          command: 'rm -rf build',
          reply: 'Build directory removed.',
          toolOutput: '[Process exited with code 0]',
        }),
      },
    },
  })

  test('allow lets the turn continue; the card takes the composer’s place', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: Execute' })
    await expect(card).toContainText('rm -rf build')
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveCount(0)
    await card.getByRole('button', { name: 'Yes, allow' }).click()
    await expect(card).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText(
      'Build directory removed.',
    )
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
  })

  test('deny ends the turn without running the tool', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: Execute' })
    await card.getByRole('button', { name: 'No, cancel' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('I will not run that')
    await expect(transcript).not.toContainText('Build directory removed.')
  })

  test('a Prompt answered by another Client disappears', async ({ page, fakeDaemon, browser }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    const other = await browser.newContext({ viewport: { width: 390, height: 844 } })
    const second = await other.newPage()
    await pairPhone(second, fakeDaemon)
    await openAndSend(second, 'Clean the build dir')

    const mine = page.getByRole('group', { name: 'Permission request: Execute' })
    const theirs = second.getByRole('group', { name: 'Permission request: Execute' })
    await expect(mine).toBeVisible()
    await expect(theirs).toBeVisible()
    await theirs.getByRole('button', { name: 'Yes, allow' }).click()
    await expect(theirs).toHaveCount(0)
    await expect(mine).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText(
      'Build directory removed.',
    )
    await expect(page.getByRole('alert')).toHaveCount(0)
    await other.close()
  })
})

test.describe('ask-user questions', () => {
  test.use({
    scenario: {
      sessions: [deploy],
      handlers: {
        'daemon.add_user_message': askUserTurn({
          question: 'Which environment?',
          options: ['staging', 'production'],
        }),
      },
    },
  })

  test('the chosen answer goes back and the turn continues', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await openAndSend(page, 'Deploy it')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await expect(card).toContainText('Which environment?')
    const answer = card.getByRole('button', { name: 'Answer' })
    await expect(answer).toBeDisabled()
    await card.getByRole('radio', { name: 'staging' }).click()
    await expect(card.getByRole('radio', { name: 'staging' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    await answer.click()
    await expect(card).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('You chose staging.')
  })

  test('Cancel declines the question', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await openAndSend(page, 'Deploy it')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await card.getByRole('button', { name: 'Cancel' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('I will not ask')
    await expect(transcript).not.toContainText('You chose')
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
  })
})
