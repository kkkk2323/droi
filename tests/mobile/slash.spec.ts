import { session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'
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
  // The pick shows as a tag before the text, so it reads as a skill to run.
  await expect(page.getByRole('group', { name: 'Skill handoff' })).toBeVisible()
  await expect(input).toHaveValue('')
  await expect(menu).toHaveCount(0)

  await input.pressSequentially('next session is for tests')
  await page.getByRole('button', { name: 'Send' }).click()
  const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(sent.params).toMatchObject({ text: '/handoff next session is for tests' })
})

test('a typed name becomes a tag; Backspace or its remove button drops it', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Chat/)
  const input = page.getByRole('textbox', { name: 'Message' })
  await expect(input).toBeEditable()
  await input.pressSequentially('/opsx-propose ')
  const tag = page.getByRole('group', { name: 'Command opsx-propose' })
  await expect(tag).toBeVisible()
  await expect(input).toHaveAttribute('placeholder', 'command arguments')
  await input.press('Backspace')
  await expect(tag).toHaveCount(0)

  await input.pressSequentially('/handoff tidy up')
  await page.getByRole('button', { name: 'Remove skill handoff' }).click()
  await expect(page.getByRole('group', { name: 'Skill handoff' })).toHaveCount(0)
  await expect(input).toHaveValue('tidy up')
  await page.getByRole('button', { name: 'Send' }).click()
  const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(sent.params).toMatchObject({ text: 'tidy up' })
})
