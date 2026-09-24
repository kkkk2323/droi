import type { Page } from '@playwright/test'
import type { FakeDaemon } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'
import { expect, openDrawer, pairPhone, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])

function saved(daemon: FakeDaemon) {
  return daemon.requests.filter((r) => r.method === 'daemon.update_session_defaults').at(-1)?.params
}

async function openDefaults(page: Page) {
  const list = await openDrawer(page)
  await list.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: 'Session defaults: Test Mac' }).click()
}

function row(page: Page, label: string) {
  return page.getByRole('button', { name: new RegExp(`^${label}: `) })
}

async function choose(page: Page, label: string, option: string) {
  await row(page, label).click()
  await page.getByRole('dialog', { name: label }).getByRole('radio', { name: option }).click()
  await expect(page.getByRole('dialog', { name: label })).toHaveCount(0)
}

test.describe('session defaults', () => {
  test.use({ scenario: { sessions: [deploy] } })

  test('the selected computer’s defaults change on its Daemon', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await openDefaults(page)
    await expect(row(page, 'Default model')).toHaveAccessibleName('Default model: Auto Model')

    await choose(page, 'Default model', 'GPT-5')
    await expect.poll(() => saved(fakeDaemon)).toEqual({ modelId: 'gpt-5', reasoningEffort: 'low' })
    await expect(row(page, 'Reasoning level')).toHaveAccessibleName('Reasoning level: Low')

    await choose(page, 'Interaction mode', 'Spec')
    await expect.poll(() => saved(fakeDaemon)).toEqual({ interactionMode: 'spec' })

    await choose(page, 'Spec folder', 'Project')
    await expect.poll(() => saved(fakeDaemon)).toEqual({ specSaveDir: '.factory/docs' })

    await choose(page, 'Token limit', '400K')
    await expect.poll(() => saved(fakeDaemon)).toEqual({ compactionTokenLimit: 400_000 })
    await page.getByRole('switch', { name: 'Compact automatically' }).click()
    await expect.poll(() => saved(fakeDaemon)).toEqual({ compactionThresholdCheckEnabled: false })

    await choose(page, 'Heavy task model', 'Claude Opus 4.1')
    await expect
      .poll(() => saved(fakeDaemon))
      .toEqual({ subagentModelSettings: { heavyModel: 'claude-opus-4-1' } })
    await choose(page, 'Heavy task reasoning', 'High')
    await expect
      .poll(() => saved(fakeDaemon))
      .toEqual({
        subagentModelSettings: { heavyModel: 'claude-opus-4-1', heavyReasoningEffort: 'high' },
      })

    // Back and in again reads them from the Daemon.
    await page.goBack()
    await page.getByRole('button', { name: 'Session defaults: Test Mac' }).click()
    await expect(row(page, 'Default model')).toHaveAccessibleName('Default model: GPT-5')
    await expect(row(page, 'Heavy task reasoning')).toHaveAccessibleName(
      'Heavy task reasoning: High',
    )
  })
})

test.describe('session defaults the organization manages', () => {
  test.use({
    scenario: {
      sessions: [deploy],
      defaults: { management: { modelId: { disabled: true, source: 'org' } } },
    },
  })

  test('show their value but do not open a picker', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await openDefaults(page)
    await expect(page.getByLabel('Default model: Auto Model')).toBeVisible()
    await expect(row(page, 'Default model')).toHaveCount(0)
    await expect(row(page, 'Reasoning level')).toBeVisible()
  })
})
