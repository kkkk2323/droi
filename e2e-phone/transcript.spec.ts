import {
  assistantMessage,
  session,
  thinkingBlock,
  userMessage,
  type MessageFixture,
} from '../e2e/fake-daemon/scenario'
import { expect, pairPhone, pickSession, playTurn, standIns, test } from './fixtures'

const long = session(
  'Long chat',
  '/Users/dev/acme-web',
  Array.from({ length: 120 }, (_, i) =>
    i % 2 === 0 ? userMessage(`Question ${i}`) : assistantMessage(`Answer ${i}`),
  ),
)
const chat = session('Chat', '/Users/dev/acme-web', [
  userMessage('hello'),
  assistantMessage('Hi. What should we build?'),
])

test.describe('opening a Session', () => {
  test.use({ scenario: { sessions: [long] } })

  test('lands on the latest message and keeps what came before a compaction', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Long chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Answer 119')).toBeInViewport()
    await expect(page.getByRole('button', { name: 'Scroll to latest' })).toHaveCount(0)

    // The Daemon summarises the context in place; the earlier messages stay.
    const now = Date.now()
    fakeDaemon.notify(long.sessionId, {
      type: 'session_compacted',
      summaryId: 'summary_1',
      removedCount: 100,
      visibleBoundaryMessageId: long.messages[100]!.id,
    })
    fakeDaemon.notify(long.sessionId, {
      type: 'create_message',
      message: {
        id: 'after_compaction',
        role: 'assistant',
        content: [{ type: 'text', text: 'Carrying on from the summary.' }],
        createdAt: now,
        updatedAt: now,
      },
    })
    await expect(transcript.getByText('Carrying on from the summary.')).toBeInViewport()
    await transcript.evaluate((el) => el.scrollTo({ top: 0 }))
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })
})

test.describe('a streamed reply', () => {
  test.use({ scenario: { sessions: [chat] } })

  test('renders Markdown as it arrives and follows it; scrolling up stops following', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const filler = Array.from({ length: 30 }, (_, i) => `- point ${i}\n`).join('')
    playTurn(
      fakeDaemon,
      chat.sessionId,
      'plan it',
      [
        '## The plan\n\n',
        filler,
        '\n**Bold** move with `npm test` and [docs](https://example.com)\n\n```ts\nconst answer',
        ' = 42\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\nThe end.',
      ],
      400,
    )
    const reply = transcript.getByRole('article', { name: 'Assistant' }).last()
    await expect(reply.getByRole('heading', { name: 'The plan' })).toBeVisible()
    // A fence still open while streaming already shows as code.
    await expect(reply.getByRole('group', { name: 'ts code' })).toContainText('const answer')
    await expect(reply.getByText('The end.')).toBeInViewport()
    await expect(reply.getByRole('group', { name: 'ts code' })).toContainText('const answer = 42')
    await expect(reply.getByRole('link', { name: 'docs' })).toBeVisible()
    await expect(reply.getByRole('table')).toContainText('2')

    // Reading further up while the next turn streams: the view stays put.
    playTurn(
      fakeDaemon,
      chat.sessionId,
      'more',
      Array.from({ length: 6 }, (_, i) => `Paragraph ${i} `.repeat(20) + '\n\n'),
      300,
    )
    await expect(transcript.getByText(/Paragraph 0/)).toBeVisible()
    await transcript.evaluate((el) => el.scrollTo({ top: 0 }))
    await expect(transcript.getByText('hello', { exact: true })).toBeInViewport()
    await expect(transcript.getByText(/Paragraph 5/)).toBeVisible({ timeout: 5_000 })
    await expect(transcript.getByText('hello', { exact: true })).toBeInViewport()

    const down = page.getByRole('button', { name: 'Scroll to latest' })
    await down.click()
    await expect(transcript.getByText(/Paragraph 5/)).toBeInViewport()
    await expect(down).toHaveCount(0)
  })

  test('a code block copies its text', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    playTurn(fakeDaemon, chat.sessionId, 'code', ['```sh\npnpm install:phone\n```\n'])
    const code = page.getByRole('group', { name: 'sh code' })
    await code.getByRole('button', { name: 'Copy code' }).click()
    await expect(code.getByRole('button', { name: 'Copied' })).toBeVisible()
    expect((await standIns(page)).clipboard).toBe('pnpm install:phone')
  })
})

function toolTurn(): MessageFixture[] {
  const call = (id: string, name: string, input: Record<string, unknown>): MessageFixture => ({
    ...assistantMessage(''),
    content: [{ type: 'tool_use', id, name, input }],
  })
  const result = (toolUseId: string, content: string, isError = false): MessageFixture => ({
    ...assistantMessage(''),
    role: 'tool',
    content: [{ type: 'tool_result', toolUseId, content, ...(isError ? { isError } : {}) }],
  })
  return [
    userMessage('Fix login and add a test'),
    {
      ...assistantMessage(''),
      content: [
        thinkingBlock('I should read the file before changing it.', 1200),
        { type: 'tool_use', id: 'read', name: 'Read', input: { file_path: 'src/auth.ts' } },
      ],
    },
    result('read', 'export function login() {}'),
    call('test', 'Execute', { command: 'npm test', summary: 'Run the tests' }),
    result('test', 'Error: 1 test failed', true),
    call('edit', 'Edit', {
      file_path: 'src/auth.ts',
      old_str: 'login()',
      new_str: 'await login()',
    }),
    result(
      'edit',
      JSON.stringify({
        success: true,
        file_path: 'src/auth.ts',
        diffLines: [
          {
            type: 'unchanged',
            content: 'export async function run() {',
            lineNumber: { old: 1, new: 1 },
          },
          { type: 'removed', content: '  login()', lineNumber: { old: 2 } },
          { type: 'added', content: '  await login()', lineNumber: { new: 2 } },
          { type: 'unchanged', content: '}', lineNumber: { old: 3, new: 3 } },
        ],
      }),
    ),
    call('create', 'Create', {
      file_path: 'src/auth.test.ts',
      content: "import { login } from './auth'\ntest('logs in', () => login())\n",
    }),
    result('create', JSON.stringify({ success: true, file_path: 'src/auth.test.ts' })),
    assistantMessage('Fixed: `login` is awaited now.'),
  ]
}

test.describe('tool calls and reasoning', () => {
  const work = session('Login fix', '/Users/dev/acme-web', toolTurn())
  const link = session('Links', '/Users/dev/acme-web', [
    userMessage(`see https://example.com/${'a-very-long-path-segment/'.repeat(12)}index.html`),
  ])
  test.use({ scenario: { sessions: [work, link] } })

  test('rows show success and failure and open to their details', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Login fix/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const read = transcript.getByRole('button', { name: 'Read: src/auth.ts' })
    const tests = transcript.getByRole('button', { name: 'Execute: Run the tests' })
    await expect(read.getByRole('img', { name: 'Succeeded' })).toBeVisible()
    await expect(tests.getByRole('img', { name: 'Failed' })).toBeVisible()

    await tests.click()
    const details = transcript.getByRole('region', { name: 'Execute details' })
    await expect(details).toContainText('$ npm test')
    await expect(details).toContainText('Error: 1 test failed')
  })

  test('an Edit shows a line diff and a Create the new file', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Login fix/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const edit = transcript.getByRole('button', { name: 'Edit: src/auth.ts' })
    await expect(edit).toContainText('+1')
    await expect(edit).toContainText('−1')
    await edit.click()
    const diff = transcript.getByRole('region', { name: 'Edit details' }).getByRole('group', {
      name: 'Diff',
    })
    await expect(diff.getByLabel('Removed:   login()')).toBeVisible()
    await expect(diff.getByLabel('Added:   await login()')).toBeVisible()

    const create = transcript.getByRole('button', { name: 'Create: src/auth.test.ts' })
    await expect(create).toContainText('+2')
    await create.click()
    const created = transcript.getByRole('region', { name: 'Create details' })
    await expect(created.getByLabel("Added: import { login } from './auth'")).toBeVisible()
  })

  test('reasoning is folded and opens on tap', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Login fix/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const reasoning = transcript.getByRole('button', { name: /Reasoned for 1s/ })
    await expect(reasoning).toHaveAttribute('aria-expanded', 'false')
    await expect(transcript.getByText('I should read the file')).toHaveCount(0)
    await reasoning.click()
    await expect(transcript.getByText('I should read the file before changing it.')).toBeVisible()
  })

  test('opening a tool row near the bottom keeps it where it was tapped', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Login fix/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const create = transcript.getByRole('button', { name: 'Create: src/auth.test.ts' })
    await expect(create).toBeInViewport()
    await page.waitForTimeout(300)
    const before = (await create.boundingBox())!.y
    await create.click()
    await expect(transcript.getByRole('region', { name: 'Create details' })).toBeVisible()
    await page.waitForTimeout(300)
    expect(Math.abs((await create.boundingBox())!.y - before)).toBeLessThan(2)
  })

  test('a long link in a sent message wraps inside the bubble', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Links/)
    const bubble = page
      .getByRole('log', { name: 'Transcript' })
      .getByRole('article', { name: 'You' })
      .getByText(/example\.com/)
    const box = (await bubble.boundingBox())!
    const width = page.viewportSize()!.width
    expect(box.x + box.width).toBeLessThanOrEqual(width)
  })
})
