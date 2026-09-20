import { expect, test } from './fixtures'
import { session, userMessage } from './fake-daemon/scenario'
import { streamedReply } from './fake-daemon/turns'

const chat = session('Chat', '/Users/dev/acme-web', [userMessage('hi')])

test.describe('slash commands', () => {
  test.use({
    scenario: {
      sessions: [chat],
      commands: [
        {
          name: 'opsx-propose',
          description: 'Propose a change',
          argumentHint: 'command arguments',
        },
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

  test('typing "/" offers commands and user-invocable skills; the pick is sent as text', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('/')
    const list = page.getByRole('listbox', { name: 'Commands and skills' })
    await expect(list.getByRole('option')).toHaveText([/opsx-propose.*Command/, /handoff.*Skill/])

    await input.pressSequentially('ha')
    await expect(list.getByRole('option')).toHaveText([/handoff/])
    await input.press('Enter')
    await expect(input).toHaveValue('/handoff ')
    await expect(list).toHaveCount(0)

    await input.pressSequentially('next session is for tests')
    await input.press('Enter')
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ text: '/handoff next session is for tests' })
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'Assistant' }).last()).toContainText(
      'activated',
    )
  })

  test('Escape hides the list; a plain message still sends on Enter', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
    await input.fill('/op')
    const list = page.getByRole('listbox', { name: 'Commands and skills' })
    await expect(list).toBeVisible()
    await input.press('Escape')
    await expect(list).toHaveCount(0)
    await input.press('Enter')
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ text: '/op' })
  })
})
