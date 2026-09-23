import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { streamedReply } from '../e2e/fake-daemon/turns'
import { expect, pairPhone, pickSession, test } from './fixtures'

const chat = session('Chat', '/Users/dev/acme-web', [userMessage('hi')])

test.use({
  scenario: {
    sessions: [chat],
    commands: [
      { name: 'opsx-propose', description: 'Propose a change', argumentHint: 'command arguments' },
    ],
    skills: [
      { name: 'handoff', description: 'Compact the conversation' },
      { name: 'hidden-helper', description: 'Not for users', userInvocable: false },
    ],
    handlers: {
      'daemon.add_user_message': streamedReply({ deltas: ['Skill "handoff" activated'] }),
    },
  },
})

test('"/" offers commands and skills, filters as you type, and inserts the pick', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Chat/)
  const input = page.getByRole('textbox', { name: 'Message' })
  await input.fill('/')
  const menu = page.getByRole('menu', { name: 'Commands and skills' })
  await expect(menu.getByRole('menuitem')).toHaveText([
    /\/compact.*Command/,
    /\/opsx-propose.*Command/,
    /\/handoff.*Skill/,
  ])

  await input.pressSequentially('ha')
  await expect(menu.getByRole('menuitem')).toHaveText([/handoff/])
  await menu.getByRole('menuitem', { name: /handoff/ }).click()
  await expect(input).toHaveValue('/handoff ')
  await expect(menu).toHaveCount(0)

  await input.pressSequentially('next session is for tests')
  await page.getByRole('button', { name: 'Send' }).click()
  const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(sent.params).toMatchObject({ text: '/handoff next session is for tests' })
})
