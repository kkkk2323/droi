import { userEntry } from './fake-kernel/history'
import { expect, pairPhone, pickSession, test } from './fixtures'

test.use({
  scenario: {
    sessions: [{ title: 'Chat', cwd: 'acme-web', entries: [userEntry('hi')] }],
    prompts: [
      {
        name: 'opsx-propose',
        description: 'Propose a change',
        body: 'Propose this change: $ARGUMENTS',
      },
    ],
    skills: [
      { name: 'handoff', description: 'Compact the conversation', body: 'Write a handoff note.' },
      { name: 'switched-off', description: 'Turned off on the computer' },
    ],
  },
})

test.beforeEach(async ({ fakeKernel }) => {
  await fakeKernel.kernel.call('skills.setDisabled', { name: 'switched-off', disabled: true })
})

test('"/" offers commands and skills, filters as you type, and the Kernel runs the pick', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  fakeKernel.model.push({ match: 'next session is for tests', text: 'Handoff written.' })
  await pairPhone(page, fakeKernel)
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
  expect(await phoneWire.waitForCall('conversation.submit')).toMatchObject({
    input: '/handoff next session is for tests',
  })
  // The Kernel put the skill in the prompt.
  const turn = await fakeKernel.model.waitForTurn('next session is for tests')
  expect(JSON.stringify(turn.messages)).toContain('Write a handoff note.')
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Handoff written.')
})

test('a typed name becomes a tag; Backspace or its remove button drops it', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  const input = page.getByRole('textbox', { name: 'Message' })
  await expect(input).toBeEditable()
  await input.pressSequentially('/opsx-propose ')
  const tag = page.getByRole('group', { name: 'Command opsx-propose' })
  await expect(tag).toBeVisible()
  await input.press('Backspace')
  await expect(tag).toHaveCount(0)

  await input.pressSequentially('/handoff tidy up')
  await page.getByRole('button', { name: 'Remove skill handoff' }).click()
  await expect(page.getByRole('group', { name: 'Skill handoff' })).toHaveCount(0)
  await expect(input).toHaveValue('tidy up')
  await page.getByRole('button', { name: 'Send' }).click()
  expect(await phoneWire.waitForCall('conversation.submit')).toMatchObject({ input: 'tidy up' })
})
