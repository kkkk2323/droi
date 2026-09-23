import type { Page } from '@playwright/test'
import type { FakeDaemon } from '../e2e/fake-daemon/fake-daemon'
import { session, userMessage } from '../e2e/fake-daemon/scenario'
import { expect, pairPhone, pickSession, test } from './fixtures'

const first = session('First session', '/Users/dev/acme-web', [userMessage('hi')], {
  settings: { modelId: 'claude-opus-4-1', reasoningEffort: 'medium', autonomyLevel: 'low' },
})

test.use({ scenario: { sessions: [first] } })

function lastSettings(daemon: FakeDaemon) {
  return daemon.requests.filter((r) => r.method === 'daemon.update_session_settings').at(-1)?.params
}

async function choose(page: Page, control: string, option: string) {
  await page.getByRole('button', { name: control, exact: true }).click()
  await page.getByRole('dialog', { name: control }).getByRole('radio', { name: option }).click()
  await expect(page.getByRole('dialog', { name: control })).toHaveCount(0)
}

test('model, reasoning effort and autonomy change the Session on the Daemon', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /First session/)
  const model = page.getByRole('button', { name: 'Model', exact: true })
  const effort = page.getByRole('button', { name: 'Reasoning effort' })
  const autonomy = page.getByRole('button', { name: 'Autonomy' })
  await expect(model).toHaveText(/Claude Opus 4\.1/)
  await expect(effort).toHaveText(/Medium/)
  await expect(autonomy).toHaveText(/Low autonomy/)

  await model.click()
  const picker = page.getByRole('dialog', { name: 'Choose a model' })
  const models = picker.getByRole('list', { name: 'Models' })
  await expect(models.getByRole('radio')).toHaveCount(3)
  await expect(models.getByRole('listitem').filter({ hasText: 'Claude Opus 4.1' })).toContainText(
    '1.6×',
  )
  const rail = picker.getByRole('toolbar', { name: 'Filter models' })
  await rail.getByRole('button', { name: 'OpenAI' }).click()
  await expect(models.getByRole('radio')).toHaveText([/GPT-5/])
  await rail.getByRole('button', { name: 'OpenAI' }).click()
  await picker.getByRole('searchbox', { name: 'Search models' }).fill('gpt')
  await models.getByRole('radio', { name: 'GPT-5' }).click()
  await expect(picker).toHaveCount(0)
  await expect.poll(() => lastSettings(fakeDaemon)).toMatchObject({ modelId: 'gpt-5' })
  await expect(model).toHaveText(/GPT-5/)

  await choose(page, 'Reasoning effort', 'Extra high')
  await expect.poll(() => lastSettings(fakeDaemon)).toMatchObject({ reasoningEffort: 'xhigh' })
  await expect(effort).toHaveText(/Extra high/)

  await choose(page, 'Autonomy', 'High autonomy')
  await expect.poll(() => lastSettings(fakeDaemon)).toMatchObject({ autonomyLevel: 'high' })
  await expect(autonomy).toHaveText(/High autonomy/)
})

test('favourite models are kept on the phone', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /First session/)
  await page.getByRole('button', { name: 'Model', exact: true }).click()
  let picker = page.getByRole('dialog', { name: 'Choose a model' })
  await picker.getByRole('button', { name: 'Star GPT-5' }).click()
  await picker
    .getByRole('toolbar', { name: 'Filter models' })
    .getByRole('button', {
      name: 'Favorites',
    })
    .click()
  await expect(picker.getByRole('list', { name: 'Models' }).getByRole('radio')).toHaveText([
    /GPT-5/,
  ])

  await page.reload()
  await page.getByRole('button', { name: 'Model', exact: true }).click()
  picker = page.getByRole('dialog', { name: 'Choose a model' })
  await expect(picker.getByRole('button', { name: 'Unstar GPT-5' })).toBeVisible()
  expect(fakeDaemon.requests.map((r) => r.method)).not.toContain('daemon.update_session_settings')
})

test('models show their brand’s mark, and Auto Model says Router', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
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
  await expect(rows.filter({ hasText: 'Auto Model' })).toContainText('Router')
})
