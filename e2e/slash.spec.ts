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
    await expect(list.getByRole('option')).toHaveText([
      /compact.*Command/,
      /opsx-propose.*Command/,
      /handoff.*Skill/,
    ])

    await input.pressSequentially('ha')
    await expect(list.getByRole('option')).toHaveText([/handoff/])
    await input.press('Enter')
    // The pick shows as a tag before the text, so it reads as a skill to run.
    await expect(page.getByRole('group', { name: 'Skill handoff' })).toBeVisible()
    await expect(input).toHaveValue('')
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

  test('the New session page offers the Workspace commands and skills before the Session exists', async ({
    page,
    fakeDaemon,
    openClient,
    openSidebar,
  }) => {
    await openClient()
    await (await openSidebar()).getByRole('button', { name: 'New session', exact: true }).click()
    const form = page.getByRole('region', { name: 'New session' })
    const input = form.getByRole('textbox', { name: 'Message' })
    await input.fill('/')
    const list = page.getByRole('listbox', { name: 'Commands and skills' })
    // `/compact` is left out: there is no conversation to summarise yet.
    await expect(list.getByRole('option')).toHaveText([/opsx-propose.*Command/, /handoff.*Skill/])
    const draft = await fakeDaemon.waitForRequest('daemon.initialize_session')
    const listed = await fakeDaemon.waitForRequest('daemon.list_skills')
    const draftId = (draft.params as Record<string, unknown>)['sessionId']
    expect(listed.params).toMatchObject({ sessionId: draftId })

    await input.pressSequentially('ha')
    await input.press('Enter')
    await expect(form.getByRole('group', { name: 'Skill handoff' })).toBeVisible()
    await input.pressSequentially('carry on')
    await input.press('Enter')
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ sessionId: draftId, text: '/handoff carry on' })
  })

  test('a typed name becomes a tag; Backspace or its remove button drops it', async ({
    page,
    fakeDaemon,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Chat/)
    const input = page.getByRole('textbox', { name: 'Message' })
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
    await input.press('Enter')
    const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
    expect(sent.params).toMatchObject({ text: 'tidy up' })
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
