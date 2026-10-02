import { expect, test } from './fixtures'
import { session, userMessage } from '../fake-daemon/scenario'
import { scriptScenario } from '../fake-daemon/scripts'
import { scriptTurn } from '../fake-daemon/turns'

const { sessions } = scriptScenario()

test.describe('Script runs', () => {
  test.use({ scenario: { sessions } })

  test('a Script lists the calls it made under it, and a wait picks the run up again', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Audit the build/)
    const transcript = page.getByRole('log', { name: 'Transcript' })

    // The Script counts as the calls it made, not as a tool of its own.
    await expect(transcript.getByRole('button', { name: 'Used 2 tools' })).toBeVisible()
    const script = transcript.getByRole('button', { name: 'Script: Read · Execute' })
    await expect(script.getByRole('img', { name: 'Still running' })).toBeVisible()
    const calls = transcript.getByRole('group', { name: 'Calls made by Script' })
    await expect(calls.first().getByRole('button')).toHaveText([/Read/, /Execute.*Run the tests/])

    await script.click()
    await expect(transcript.getByRole('region', { name: 'Source' })).toContainText(
      "tools.Execute({ summary: 'Build the app'",
    )
    await expect(transcript.getByRole('region', { name: 'inputs.notes' })).toContainText(
      'Build with the release flags.',
    )

    // The wait shows where it sits in the turn, with the call it saw and the run's end.
    const wait = transcript.getByRole('button', { name: 'WaitForScript: continued' })
    await expect(wait.getByRole('img', { name: 'Succeeded' })).toBeVisible()
    await expect(calls.nth(1).getByRole('button')).toHaveText([/Execute.*Build the app/])
    await wait.click()
    const output = transcript.getByRole('region', { name: 'Output' })
    await expect(output).toContainText('42 passed')
    await expect(output).not.toContainText('toolCallId')
    await expect(transcript).toContainText('3 calls · 120 B in sandbox · 48 B emitted (40%)')
    await expect(transcript).not.toContainText('retained: r1')
    await expect(transcript.getByRole('button', { name: 'Copy log path' })).toBeVisible()

    const broken = transcript.getByRole('button', { name: 'Script: Read' }).last()
    await expect(broken.getByRole('img', { name: 'Failed' })).toBeVisible()
    await broken.click()
    await expect(transcript.getByRole('region', { name: 'Output' }).last()).toContainText(
      'ReferenceError: missing is not defined',
    )
  })

  test('a reply draws its <json-render> output in place', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Release report/)
    const reply = page
      .getByRole('log', { name: 'Transcript' })
      .getByRole('article', { name: 'Assistant' })
    const card = reply.getByRole('region', { name: 'Release 1.26' })
    await expect(card.getByRole('img', { name: 'success' })).toBeVisible()
    await expect(card).toContainText('All checks passed')
    await expect(card.getByRole('columnheader')).toHaveText(['Package', 'Version'])
    await expect(card.getByRole('row').nth(1)).toContainText('droi1.26.0')
    await expect(card.getByRole('progressbar', { name: 'Rollout' })).toHaveAttribute(
      'aria-valuenow',
      '75',
    )
    await expect(card.getByRole('listitem', { name: 'macOS: 60%' })).toBeVisible()
    await expect(reply).toContainText('Ship once the last check is green.')
    await expect(reply).not.toContainText('"elements"')
    // A tag quoted in a code block stays text.
    await expect(reply.locator('pre, code').filter({ hasText: '"root":"quoted"' })).not.toHaveCount(
      0,
    )
  })

  test('a reply typesets its TeX', async ({ page, openClient, pickSession }) => {
    await openClient()
    await pickSession(/Release report/)
    const reply = page
      .getByRole('log', { name: 'Transcript' })
      .getByRole('article', { name: 'Assistant' })
    await expect(reply.locator('.katex')).toHaveCount(2)
    await expect(reply.locator('.katex-display')).toHaveCount(1)
    await expect(reply).not.toContainText('$$')
  })
})

test.describe('Script permission', () => {
  test.use({
    scenario: {
      sessions: [session('Checks', '/Users/dev/acme-web', [userMessage('hi')])],
      handlers: { 'daemon.add_user_message': scriptTurn({ reply: 'Tests and lint pass.' }) },
    },
  })

  test('lists the calls by line, shows the source on request, and runs once allowed', async ({
    page,
    openClient,
    pickSession,
  }) => {
    await openClient()
    await pickSession(/Checks/)
    await page.getByRole('textbox', { name: 'Message' }).fill('Run the checks')
    await page.getByRole('button', { name: 'Send' }).click()

    const card = page.getByRole('group', { name: 'Permission request: Script' })
    const calls = card.getByRole('list', { name: 'Calls in the Script' }).getByRole('listitem')
    await expect(calls).toHaveText([
      /L1\s*Execute\s*\$ pnpm test\s*low/,
      /L3\s*Execute\s*\$ pnpm lint\s*medium/,
    ])
    await expect(card).not.toContainText('"script"')

    await card.getByRole('button', { name: 'Show source' }).click()
    const source = card.getByLabel('Script source')
    await expect(source).toContainText("command: 'pnpm lint'")
    await expect(source.locator('[data-marked]')).toHaveCount(2)

    await card.getByRole('button', { name: 'Yes, allow all' }).click()
    await expect(card).toHaveCount(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(
      transcript.getByRole('group', { name: 'Calls made by Script' }).getByRole('button'),
    ).toHaveText([/Execute.*Run the tests/, /Execute.*Lint/])
    await expect(
      transcript
        .getByRole('button', { name: 'Script: Execute' })
        .getByRole('img', { name: 'Succeeded' }),
    ).toBeVisible()
    await expect(transcript).toContainText('Tests and lint pass.')
  })
})
