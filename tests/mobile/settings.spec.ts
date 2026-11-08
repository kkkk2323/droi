import type { Page } from '@playwright/test'
import type { ScenarioModel } from './fake-kernel/fake-kernel'
import { userEntry } from './fake-kernel/history'
import { expect, pairPhone, pickSession, relaunch, test, type PhoneWire } from './fixtures'

const models: ScenarioModel[] = [
  { id: 'claude-opus-4-1', name: 'Claude Opus 4.1', thinking: ['off', 'low', 'medium', 'high'] },
  { id: 'gpt-5', name: 'GPT-5', thinking: ['low', 'medium', 'high', 'xhigh'] },
]

const first = {
  title: 'First session',
  cwd: 'acme-web',
  entries: [userEntry('hi')],
  model: 'claude-opus-4-1',
  thinkingLevel: 'medium',
  autonomy: 'edits' as const,
}

test.use({ scenario: { models, sessions: [first] } })

function lastCall(wire: PhoneWire, method: string) {
  return wire.calls.filter((c) => c.method === method).at(-1)?.params
}

async function choose(page: Page, control: string, option: string) {
  await page.getByRole('button', { name: control, exact: true }).click()
  await page.getByRole('dialog', { name: control }).getByRole('radio', { name: option }).click()
  await expect(page.getByRole('dialog', { name: control })).toHaveCount(0)
}

test('model, reasoning effort and autonomy change the Session on the Kernel', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /First session/)
  const model = page.getByRole('button', { name: 'Model', exact: true })
  const effort = page.getByRole('button', { name: 'Reasoning effort' })
  const autonomy = page.getByRole('button', { name: 'Autonomy' })
  await expect(model).toHaveText(/Claude Opus 4\.1/)
  await expect(effort).toHaveText(/Medium/)
  await expect(autonomy).toHaveText(/Low autonomy/)

  await model.click()
  const picker = page.getByRole('dialog', { name: 'Choose a model' })
  const list = picker.getByRole('list', { name: 'Models' })
  await expect(list.getByRole('radio')).toHaveCount(3)
  const rail = picker.getByRole('toolbar', { name: 'Filter models' })
  await rail.getByRole('button', { name: 'OpenAI' }).click()
  await expect(list.getByRole('radio')).toHaveText([/GPT-5/])
  await rail.getByRole('button', { name: 'OpenAI' }).click()
  await picker.getByRole('searchbox', { name: 'Search models' }).fill('gpt')
  await list.getByRole('radio', { name: 'GPT-5' }).click()
  await expect(picker).toHaveCount(0)
  await expect
    .poll(() => lastCall(phoneWire, 'conversation.configure'))
    .toMatchObject({ change: { model: { provider: 'fake', modelId: 'gpt-5' } } })
  // The button follows the Kernel's view of the Session.
  await expect(model).toHaveText(/GPT-5/)

  await choose(page, 'Reasoning effort', 'Extra high')
  await expect
    .poll(() => lastCall(phoneWire, 'conversation.configure'))
    .toMatchObject({ change: { thinkingLevel: 'xhigh' } })
  await expect(effort).toHaveText(/Extra high/)

  await choose(page, 'Autonomy', 'High autonomy')
  await expect
    .poll(() => lastCall(phoneWire, 'policy.set'))
    .toMatchObject({ policy: { autonomy: 'all' } })
  await expect(autonomy).toHaveText(/High autonomy/)
})

test('favourite models are kept on the phone', async ({ page, fakeKernel, phoneWire }) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /First session/)
  await page.getByRole('button', { name: 'Model', exact: true }).click()
  let picker = page.getByRole('dialog', { name: 'Choose a model' })
  await picker.getByRole('button', { name: 'Star GPT-5' }).click()
  await picker
    .getByRole('toolbar', { name: 'Filter models' })
    .getByRole('button', { name: 'Favorites' })
    .click()
  await expect(picker.getByRole('list', { name: 'Models' }).getByRole('radio')).toHaveText([
    /GPT-5/,
  ])

  await relaunch(page)
  await page.getByRole('button', { name: 'Model', exact: true }).click()
  picker = page.getByRole('dialog', { name: 'Choose a model' })
  await expect(picker.getByRole('button', { name: 'Unstar GPT-5' })).toBeVisible()
  expect(phoneWire.count('conversation.configure')).toBe(0)
})

test('models show their brand’s mark', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await pickSession(page, /First session/)
  const model = page.getByRole('button', { name: 'Model', exact: true })
  await expect(model.getByTestId('brand-anthropic')).toBeVisible()

  await model.click()
  const picker = page.getByRole('dialog', { name: 'Choose a model' })
  const rail = picker.getByRole('toolbar', { name: 'Filter models' })
  await expect(
    rail.getByRole('button', { name: 'OpenAI' }).getByTestId('brand-openai'),
  ).toBeVisible()
  const rows = picker.getByRole('list', { name: 'Models' }).getByRole('listitem')
  await expect(
    rows.filter({ hasText: 'Claude Opus 4.1' }).getByTestId('brand-anthropic'),
  ).toBeVisible()
  await expect(rows.filter({ hasText: 'GPT-5' }).getByTestId('brand-openai')).toBeVisible()
  await expect(rows.filter({ hasText: 'Fake one' })).toContainText('Other')
})

// Magpie offers models under the names of the computer's own; the picker keeps them in a tab of
// their own rather than beside those copies.
test.describe('Magpie’s models', () => {
  test.use({
    scenario: {
      models: [
        { id: 'gpt-6-1-sol', name: 'GPT-6.1-Sol', thinking: ['low', 'high'] },
        {
          id: 'custom:magpie-codex-gpt-6-1-sol-0a1b2c',
          name: 'Magpie · GPT-6.1-Sol',
          thinking: ['low', 'high'],
        },
      ],
      sessions: [
        {
          title: 'First session',
          cwd: 'acme-web',
          entries: [userEntry('hi')],
          model: 'gpt-6-1-sol',
        },
      ],
    },
  })

  test('have a tab of their own, out of the brands and the full list', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /First session/)
    await page.getByRole('button', { name: 'Model', exact: true }).click()
    const picker = page.getByRole('dialog', { name: 'Choose a model' })
    const list = picker.getByRole('list', { name: 'Models' })
    const rail = picker.getByRole('toolbar', { name: 'Filter models' })

    await expect(rail.getByRole('button', { name: 'Magpie' })).toBeVisible()
    await expect(list.getByRole('radio')).toHaveText([/GPT-6\.1-Sol/, /Fake one/])

    await rail.getByRole('button', { name: 'Magpie' }).click()
    await expect(list.getByRole('radio')).toHaveText([/Magpie · GPT-6\.1-Sol/])

    // A brand's tab holds the computer's own model only.
    await rail.getByRole('button', { name: 'OpenAI' }).click()
    await expect(list.getByRole('radio')).toHaveText([/GPT-6\.1-Sol/])

    // The search still reaches the gateway's list.
    await picker.getByRole('searchbox', { name: 'Search models' }).fill('gpt')
    await expect(list.getByRole('radio')).toHaveText([/GPT-6\.1-Sol/, /Magpie · GPT-6\.1-Sol/])
    await list.getByRole('radio', { name: /Magpie · GPT-6\.1-Sol/ }).click()
    await expect
      .poll(() => lastCall(phoneWire, 'conversation.configure'))
      .toMatchObject({ change: { model: { modelId: 'custom:magpie-codex-gpt-6-1-sol-0a1b2c' } } })
  })
})
