// Script runs on the phone: the calls a run made hang under its Script, a
// WaitForScript picks the run up where it sits in the turn, the one-time
// permission lists the calls by line, and a reply's <json-render> output is
// drawn in place.
import { session, userMessage } from '../fake-daemon/scenario'
import { scriptScenario } from '../fake-daemon/scripts'
import { scriptTurn } from '../fake-daemon/turns'
import { expect, pairPhone, pickSession, test } from './fixtures'

const { sessions } = scriptScenario()

test.describe('Script runs', () => {
  test.use({ scenario: { sessions } })

  test('a Script lists its calls under it and opens to its source and output', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Audit the build/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('button', { name: 'Used 2 tools' })).toBeVisible()
    const script = transcript.getByRole('button', { name: 'Script: Read · Execute' })
    await expect(script.getByRole('img', { name: 'Still running' })).toBeVisible()
    // The phone's list runs newest first in the DOM; find each group by what it holds.
    const calls = transcript.getByRole('group', { name: 'Calls made by Script' })
    await expect(calls.filter({ hasText: 'Run the tests' }).getByRole('button')).toHaveText([
      /Read/,
      /Execute.*Run the tests/,
    ])

    await script.click()
    await expect(transcript.getByRole('region', { name: 'Source' })).toContainText(
      "summary: 'Build the app'",
    )
    const wait = transcript.getByRole('button', { name: 'WaitForScript: continued' })
    await expect(calls.filter({ hasText: 'Build the app' }).getByRole('button')).toHaveText([
      /Execute.*Build the app/,
    ])
    await wait.click()
    const output = transcript.getByRole('region', { name: 'Output' })
    await expect(output).toContainText('build ok')
    await expect(output).not.toContainText('toolCallId')
    await expect(transcript).toContainText('3 calls · 120 B in sandbox')

    const broken = transcript.getByRole('button', { name: 'Script: Read', exact: true })
    await expect(broken.getByRole('img', { name: 'Failed' })).toBeVisible()
  })

  test('a reply draws its <json-render> output in place', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Release report/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const card = transcript.getByRole('region', { name: 'Release 1.26' })
    await expect(card).toContainText('All checks passed')
    await expect(card.getByRole('columnheader')).toHaveText(['Package', 'Version'])
    await expect(card.getByRole('progressbar', { name: 'Rollout' })).toHaveAttribute(
      'aria-valuenow',
      '75',
    )
    await expect(card.getByRole('listitem', { name: 'macOS: 60%' })).toBeVisible()
    await expect(transcript).toContainText('Ship once the last check is green.')
    await expect(transcript).not.toContainText('"elements"')
  })
})

test.describe('Script permission', () => {
  test.use({
    scenario: {
      sessions: [session('Checks', '/Users/dev/acme-web', [userMessage('hi')])],
      handlers: { 'daemon.add_user_message': scriptTurn({ reply: 'Tests and lint pass.' }) },
    },
  })

  test('lists the calls by line and runs once allowed', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Checks/)
    await page.getByRole('textbox', { name: 'Message' }).fill('Run the checks')
    await page.getByRole('button', { name: 'Send' }).click()

    const card = page.getByRole('group', { name: 'Permission request: Script' })
    const calls = card.getByRole('list', { name: 'Calls in the Script' }).getByRole('listitem')
    await expect(calls).toHaveCount(2)
    await expect(calls.first()).toHaveAccessibleName('Line 1, Execute: $ pnpm test, low')
    await card.getByRole('button', { name: 'Show source' }).click()
    await expect(card.getByRole('group', { name: 'Script source' })).toContainText("'pnpm lint'")

    await card.getByRole('button', { name: 'Yes, allow all' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(
      transcript.getByRole('group', { name: 'Calls made by Script' }).getByRole('button'),
    ).toHaveText([/Execute.*Run the tests/, /Execute.*Lint/])
    await expect(transcript).toContainText('Tests and lint pass.')
  })
})
