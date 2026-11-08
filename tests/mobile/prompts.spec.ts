import type { Page } from '@playwright/test'
import type { Step } from './fake-kernel/model'
import { userEntry } from './fake-kernel/history'
import { expect, pairPhone, pickSession, test } from './fixtures'

test.use({
  scenario: {
    autonomy: 'ask',
    sessions: [{ title: 'Deploy', cwd: 'acme-web', entries: [userEntry('hi')] }],
  },
})

async function openAndSend(page: Page, text: string) {
  await pickSession(page, /Deploy/)
  await page.getByRole('textbox', { name: 'Message' }).fill(text)
  await page.getByRole('button', { name: 'Send' }).click()
}

const transcript = (page: Page) => page.getByRole('log', { name: 'Transcript' })

/** What the model was told about its last tool call in the conversation request for `prompt`. */
function toolResults(
  requests: Array<{ prompt: string; messages: Array<{ role: string; content: unknown }> }>,
  prompt: string,
) {
  return requests
    .filter((r) => r.prompt.includes(prompt))
    .flatMap((r) =>
      r.messages.filter((m) => m.role === 'tool').map((m) => JSON.stringify(m.content)),
    )
}

test.describe('permission requests', () => {
  const clean = (after: string): Step[] => [
    {
      match: 'Clean the build dir',
      call: { name: 'bash', arguments: { command: 'echo removed build' } },
    },
    { match: 'Clean the build dir', text: after },
  ]

  test('allow lets the turn continue; the card takes the composer’s place', async ({
    page,
    fakeKernel,
  }) => {
    fakeKernel.model.push(...clean('Build directory removed.'))
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: bash' })
    await expect(card).toContainText('echo removed build')
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveCount(0)
    await card.getByRole('button', { name: 'Allow once' }).click()
    await expect(card).toHaveCount(0)
    await expect(transcript(page)).toContainText('Build directory removed.')
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
    // The command ran on the computer.
    expect(toolResults(fakeKernel.model.requests, 'Clean the build dir').join()).toContain(
      'removed build',
    )
  })

  test('cancel does not run the tool', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(...clean('I will not run that.'))
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: bash' })
    await card.getByRole('button', { name: 'Cancel' }).click()
    await expect(card).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Send' })).toBeVisible()
    expect(toolResults(fakeKernel.model.requests, 'Clean the build dir').join()).not.toContain(
      'removed build',
    )
  })

  test('a Prompt answered by another Client disappears', async ({ page, fakeKernel, browser }) => {
    fakeKernel.model.push(...clean('Build directory removed.'))
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Deploy/)
    const other = await browser.newContext({ viewport: { width: 390, height: 844 } })
    const second = await other.newPage()
    await pairPhone(second, fakeKernel)
    await openAndSend(second, 'Clean the build dir')

    const mine = page.getByRole('group', { name: 'Permission request: bash' })
    const theirs = second.getByRole('group', { name: 'Permission request: bash' })
    await expect(mine).toBeVisible()
    await expect(theirs).toBeVisible()
    await theirs.getByRole('button', { name: 'Allow once' }).click()
    await expect(theirs).toHaveCount(0)
    await expect(mine).toHaveCount(0)
    await expect(transcript(page)).toContainText('Build directory removed.')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await other.close()
  })

  test('the computer’s window answering takes the card away', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(...clean('Build directory removed.'))
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Clean the build dir')
    const card = page.getByRole('group', { name: 'Permission request: bash' })
    await expect(card).toBeVisible()
    await fakeKernel.approve('Deploy')
    await expect(card).toHaveCount(0)
    await expect(transcript(page)).toContainText('Build directory removed.')
  })
})

test.describe('ask-user questions', () => {
  const ask = (blocking = true): Step => ({
    match: 'Deploy it',
    call: {
      name: 'ask_user',
      arguments: {
        context: 'Before I deploy.',
        ...(blocking ? {} : { blocking: false }),
        questions: [
          {
            question: 'Which environment?',
            options: [
              { id: 'staging', label: 'staging' },
              { id: 'production', label: 'production' },
            ],
          },
        ],
      },
    },
  })

  test('the chosen answer goes back and the turn continues', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(ask(), { match: 'Deploy it', text: 'Deploying to the one you chose.' })
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Deploy it')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await expect(card).toContainText('Which environment?')
    await expect(page.getByRole('textbox', { name: 'Message' })).toHaveCount(0)
    const answer = card.getByRole('button', { name: 'Answer' })
    await expect(answer).toBeDisabled()
    await card.getByRole('radio', { name: 'staging' }).click()
    await expect(card.getByRole('radio', { name: 'staging' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    await answer.click()
    await expect(card).toHaveCount(0)
    await expect(transcript(page)).toContainText('Deploying to the one you chose.')
    expect(toolResults(fakeKernel.model.requests, 'Deploy it').join()).toContain('staging')
  })

  test('an answer of one’s own goes back as typed', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(ask(), { match: 'Deploy it', text: 'Deploying there.' })
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Deploy it')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await card
      .getByRole('textbox', { name: 'Other answer for: Which environment?' })
      .fill('the preview one')
    await card.getByRole('button', { name: 'Answer' }).click()
    await expect(transcript(page)).toContainText('Deploying there.')
    expect(toolResults(fakeKernel.model.requests, 'Deploy it').join()).toContain('the preview one')
  })

  test('Cancel declines the question', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(ask(), { match: 'Deploy it', text: 'I will not ask again.' })
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Deploy it')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await card.getByRole('button', { name: 'Cancel' }).click()
    await expect(card).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
    expect(toolResults(fakeKernel.model.requests, 'Deploy it').join()).not.toContain('staging')
  })

  test('a question that does not block sits above the composer while the turn goes on', async ({
    page,
    fakeKernel,
  }) => {
    fakeKernel.model.push(
      ask(false),
      { match: 'Deploy it', text: 'Carrying on meanwhile.' },
      { text: 'Noted your choice.' },
    )
    await pairPhone(page, fakeKernel)
    await openAndSend(page, 'Deploy it')
    await expect(transcript(page)).toContainText('Carrying on meanwhile.')
    const card = page.getByRole('group', { name: 'Droid has a question' })
    await expect(card).toContainText('Which environment?')
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible()
    await card.getByRole('radio', { name: 'production' }).click()
    await card.getByRole('button', { name: 'Answer' }).click()
    await expect(card).toHaveCount(0)
    await expect(transcript(page)).toContainText('Noted your choice.')
  })
})
