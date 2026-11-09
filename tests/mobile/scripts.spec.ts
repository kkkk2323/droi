// Script runs on the phone (the Kernel's codemode tool): the calls a run made hang under its Script and the row
// opens to its source and output, a failed run says so, the one permission a reading script asks for lists the
// tools it calls, and a reply's <json-render> output and TeX are drawn in place.
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { assistantEntry, userEntry } from './fake-kernel/history'
import type { Step } from './fake-kernel/model'
import { expect, pairPhone, pickSession, send, test, transcriptOf } from './fixtures'

/** A script the model sends, then what it says once the script has run. */
function scripted(prompt: string, code: string, reply: string): Step[] {
  return [
    { match: prompt, call: { name: 'codemode', arguments: { code } } },
    { match: prompt, text: reply },
  ]
}

const RELEASE_REPORT = {
  root: 'card',
  elements: {
    card: {
      type: 'Card',
      props: { title: 'Release 1.26' },
      children: ['status', 'table', 'progress', 'bars'],
    },
    status: {
      type: 'StatusLine',
      props: { text: 'All checks passed', status: 'success' },
      children: [],
    },
    table: {
      type: 'Table',
      props: {
        columns: [
          { header: 'Package', key: 'name' },
          { header: 'Version', key: 'version' },
        ],
        rows: [
          { name: 'droi', version: '1.26.0' },
          { name: '@droi/mobile', version: '0.4.0' },
        ],
      },
      children: [],
    },
    progress: { type: 'ProgressBar', props: { progress: 0.75, label: 'Rollout' }, children: [] },
    bars: {
      type: 'BarChart',
      props: {
        data: [
          { label: 'macOS', value: 60 },
          { label: 'iPhone', value: 40 },
        ],
        showPercentage: true,
      },
      children: [],
    },
  },
}

const REPORT = [
  'Here is where the release stands, drawn from a `<json-render>` tag.',
  '',
  `<json-render>${JSON.stringify(RELEASE_REPORT)}</json-render>`,
  '',
  'Ship once the last check is green.',
  '',
  'Rollout is $r = n / N$ of the devices:',
  '',
  '$$',
  'r = \\frac{n}{N}',
  '$$',
  '',
  '```html',
  '<json-render>{"root":"quoted"}</json-render>',
  '```',
].join('\n')

test.describe('Script runs', () => {
  test.use({
    scenario: {
      sessions: [
        { title: 'Audit the build', cwd: 'acme-web', entries: [userEntry('hi')] },
        {
          title: 'Release report',
          cwd: 'acme-web',
          entries: [userEntry('Summarize the release'), assistantEntry(REPORT)],
        },
      ],
    },
  })

  test('a Script lists its calls under it and opens to its source and output', async ({
    page,
    fakeKernel,
  }) => {
    writeFileSync(join(fakeKernel.folder('acme-web'), 'build.log'), 'build ok\n')
    fakeKernel.model.push(
      ...scripted(
        'Check the build',
        "const log = await tools.read({ path: 'build.log' })\nconst run = await tools.bash({ command: 'echo tests pass' })\ntext(log)\ntext(run)",
        'The build is fine.',
      ),
    )
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Audit the build/)
    await send(page, 'Check the build')
    const transcript = transcriptOf(page)
    await expect(transcript).toContainText('The build is fine.')

    const script = transcript.getByRole('button', { name: 'Script: read · bash' })
    await expect(script.getByRole('img', { name: 'Succeeded' })).toBeVisible()
    await expect(
      transcript.getByRole('group', { name: 'Calls made by Script' }).getByRole('button'),
    ).toHaveText([/read.*build\.log/, /bash.*echo tests pass/])
    await script.click()
    await expect(transcript.getByRole('region', { name: 'Source' })).toContainText(
      "tools.bash({ command: 'echo tests pass' })",
    )
    const output = transcript.getByRole('region', { name: 'Output' })
    await expect(output).toContainText('build ok')
    await expect(output).toContainText('tests pass')
    await expect(transcript).toContainText('2 calls')
  })

  test('a Script that fails says so', async ({ page, fakeKernel }) => {
    fakeKernel.model.push(
      ...scripted(
        'Read the notes',
        "text(await tools.read({ path: 'missing.md' }))",
        'There are no notes.',
      ),
    )
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Audit the build/)
    await send(page, 'Read the notes')
    const transcript = transcriptOf(page)
    await expect(transcript).toContainText('There are no notes.')
    const script = transcript.getByRole('button', { name: 'Script: read', exact: true })
    await expect(script.getByRole('img', { name: 'Failed' })).toBeVisible()
  })

  test('a reply draws its <json-render> output in place', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Release report/)
    const transcript = transcriptOf(page)
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

  test('a reply typesets its TeX', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Release report/)
    const transcript = transcriptOf(page)
    await expect(transcript.getByRole('img', { name: 'r = n / N' })).toBeVisible()
    await expect(transcript.getByRole('img', { name: 'r = \\frac{n}{N}' })).toBeVisible()
    await expect(transcript).not.toContainText('$$')
  })
})

test.describe('Script permission', () => {
  // Low autonomy: a script asks to run; the files it only reads do not ask again.
  test.use({
    scenario: {
      autonomy: 'edits',
      sessions: [{ title: 'Checks', cwd: 'acme-web', entries: [userEntry('hi')] }],
    },
  })

  test('lists the tools it calls, shows its source, and runs once allowed', async ({
    page,
    fakeKernel,
  }) => {
    writeFileSync(join(fakeKernel.folder('acme-web'), 'build.log'), 'build ok\n')
    fakeKernel.model.push(
      ...scripted(
        'Read the log',
        "text(await tools.read({ path: 'build.log' }))",
        'The log says the build is ok.',
      ),
    )
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Checks/)
    await send(page, 'Read the log')

    const card = page.getByRole('group', { name: 'Permission request: codemode' })
    await expect(
      card.getByRole('list', { name: 'Tools the script calls' }).getByRole('listitem'),
    ).toHaveText(['read'])
    await card.getByRole('button', { name: 'Show source' }).click()
    await expect(card.getByRole('group', { name: 'Script source' })).toContainText(
      "tools.read({ path: 'build.log' })",
    )

    await card.getByRole('button', { name: 'Allow once' }).click()
    await expect(card).toHaveCount(0)
    const transcript = transcriptOf(page)
    await expect(transcript).toContainText('The log says the build is ok.')
    await expect(
      transcript.getByRole('group', { name: 'Calls made by Script' }).getByRole('button'),
    ).toHaveText([/read/])
    expect(
      fakeKernel.model
        .turns()
        .flatMap((r) => r.messages.filter((m) => m.role === 'tool'))
        .map((m) => JSON.stringify(m.content))
        .join(),
    ).toContain('build ok')
  })
})
