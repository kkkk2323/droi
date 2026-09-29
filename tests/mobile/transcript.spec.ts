import {
  assistantMessage,
  session,
  thinkingBlock,
  userMessage,
  type MessageFixture,
} from '../fake-daemon/scenario'
import {
  expect,
  inReadingOrder,
  pairPhone,
  pickSession,
  playTurn,
  scrollToOldest,
  standIns,
  test,
} from './fixtures'

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
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })

  test('opens on the latest messages, not scrolling down past all the others', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    // The rows of the first frame the transcript draws any.
    await page.evaluate(() => {
      const tick = () => {
        const rows = [...document.querySelectorAll('[role="log"] [role="article"]')]
        if (rows.length === 0) {
          requestAnimationFrame(tick)
          return
        }
        ;(window as unknown as { firstRows: string[] }).firstRows = rows.map(
          (row) => row.textContent ?? '',
        )
      }
      requestAnimationFrame(tick)
    })
    await pickSession(page, /Long chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Answer 119')).toBeInViewport()
    const firstRows = await page.evaluate(
      () => (window as unknown as { firstRows: string[] }).firstRows,
    )
    expect(firstRows.some((row) => row.startsWith('Answer 119'))).toBe(true)
    expect(firstRows).not.toContain('Question 0')
    // The earlier messages come in above.
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })
})

test.describe('a Session longer than one load', () => {
  // 400 come with the load, the 50 before them on request.
  const many = Array.from({ length: 450 }, (_, i) =>
    i % 2 === 0 ? userMessage(`Question ${i}`) : assistantMessage(`Answer ${i}`),
  )
  const longer = session('Longer chat', '/Users/dev/acme-web', many)
  test.use({ scenario: { sessions: [longer] } })

  test('offers the previous messages at the top and brings them in', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Longer chat/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Answer 449')).toBeInViewport()
    expect((await fakeDaemon.waitForRequest('daemon.load_session')).params).toMatchObject({
      messageLimit: 400,
    })
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 50', { exact: true })).toBeVisible()
    await expect(transcript.getByText('Answer 49', { exact: true })).toHaveCount(0)

    const reading = transcript.getByText('Question 50', { exact: true })
    const before = (await reading.boundingBox())!.y
    await transcript.getByRole('button', { name: 'Load previous messages' }).click()
    const older = await fakeDaemon.waitForRequest('daemon.get_session_messages')
    expect(older.params).toMatchObject({ cursor: many[50]!.id, limit: 100 })
    await expect(transcript.getByRole('button', { name: 'Load previous messages' })).toHaveCount(0)
    // They come in above, out of sight; what was being read stays where it was.
    await page.waitForTimeout(300)
    expect(Math.abs((await reading.boundingBox())!.y - before)).toBeLessThan(2)
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })
})

test.describe('a Session continued after two compactions', () => {
  const first = session('Plan v1', '/Users/dev/acme-web', [
    userMessage('first question'),
    assistantMessage('first answer'),
  ])
  const continues = (parent: { sessionId: string }) => ({
    tags: [{ name: 'droi.continues', metadata: { parent: parent.sessionId } }],
  })
  const second = session(
    'Plan v2',
    '/Users/dev/acme-web',
    [userMessage('middle question'), assistantMessage('middle answer')],
    continues(first),
  )
  const third = session(
    'Plan v3',
    '/Users/dev/acme-web',
    [userMessage('latest question')],
    continues(second),
  )
  test.use({ scenario: { sessions: [first, second, third] } })

  test('offers each earlier Session in turn, back to the first', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Plan v3/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript).toContainText('latest question')
    await expect(transcript).not.toContainText('middle question')

    await transcript.getByRole('button', { name: /Continued from “Plan v2”/ }).click()
    await expect(transcript).toContainText('middle answer')
    await expect(transcript).not.toContainText('first question')
    await expect(transcript.getByRole('separator', { name: 'Context compacted here' })).toHaveCount(
      1,
    )

    await transcript.getByRole('button', { name: /Continued from “Plan v1”/ }).click()
    await expect(transcript).toContainText('first question')
    await expect(transcript.getByRole('separator', { name: 'Context compacted here' })).toHaveCount(
      2,
    )
    await expect(transcript.getByRole('button', { name: /Continued from/ })).toHaveCount(0)
    expect(await inReadingOrder(transcript.getByRole('article', { name: 'You' }))).toEqual([
      'first question',
      'middle question',
      'latest question',
    ])
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
    const reply = transcript.getByRole('article', { name: 'Assistant' }).filter({
      hasText: 'The plan',
    })
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
    await scrollToOldest(transcript)
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

  test('a table lines its columns up across rows', async ({ page, fakeDaemon }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Chat/)
    playTurn(fakeDaemon, chat.sessionId, 'table', [
      '| 状态 | 管理员邀请码 | Other codes |\n| - | - | - |\n' +
        '| 平时 | 可用 | see the current setting |\n| 收紧模式（新增） | 可用 | 不可用 |\n',
    ])
    const table = page.getByRole('table').last()
    await expect(table.getByRole('row')).toHaveCount(3)
    const columns = await table.evaluate((el) =>
      [...el.querySelectorAll('[role="row"]')].map((row) =>
        [...row.children].map((cell) => {
          const box = cell.getBoundingClientRect()
          return [Math.round(box.left), Math.round(box.width)]
        }),
      ),
    )
    expect(columns[0]).toHaveLength(3)
    for (const row of columns) expect(row).toEqual(columns[0])
  })
})

test.describe('a turn that goes on', () => {
  const work = session('Work', '/Users/dev/acme-web', [
    userMessage('go'),
    assistantMessage(Array.from({ length: 40 }, (_, i) => `Block A line ${i}`).join('\n\n')),
  ])
  test.use({ scenario: { sessions: [work] } })

  test('an earlier part of the turn stays put while a later part streams below it', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Work/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByText('Block A line 39')).toBeVisible()
    // Up in the turn's first block; the list is inverted, so up is further from 0.
    await transcript.evaluate((el) => (el.scrollTop = 600))
    const line = transcript.getByText('Block A line 20', { exact: true })
    await expect(line).toBeInViewport()
    const before = (await line.boundingBox())!.y

    // The same turn goes on with a second message.
    fakeDaemon.notify(work.sessionId, {
      type: 'droid_working_state_changed',
      newState: 'streaming_assistant_message',
    })
    for (let i = 0; i < 20; i++) {
      fakeDaemon.notify(work.sessionId, {
        type: 'assistant_text_delta',
        messageId: 'second_message',
        blockIndex: 0,
        textDelta: `Streamed paragraph ${i}. `.repeat(8) + '\n\n',
      })
      await page.waitForTimeout(50)
    }
    await expect(transcript.getByText(/Streamed paragraph 19/)).toHaveCount(1)
    await page.waitForTimeout(300)
    expect(Math.abs((await line.boundingBox())!.y - before)).toBeLessThan(2)
  })
})

const PIXEL =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

function toolTurn(): MessageFixture[] {
  const call = (id: string, name: string, input: Record<string, unknown>): MessageFixture => ({
    ...assistantMessage(''),
    content: [{ type: 'tool_use', id, name, input }],
  })
  const result = (
    toolUseId: string,
    content: string | Array<Record<string, unknown>>,
    isError = false,
  ): MessageFixture => ({
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
    call('shot', 'Read', { file_path: '/tmp/shot.png' }),
    result('shot', [
      { type: 'text', text: 'Image file: shot.png (original size: 1.2 KB)' },
      { type: 'image', source: { type: 'base64', mediaType: 'image/png', data: PIXEL } },
    ]),
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
  // The same work after enough conversation to fill the screen.
  const longWork = session('A long chat, then the fix', '/Users/dev/acme-web', [
    ...Array.from({ length: 6 }, (_, i) => [
      userMessage(`Question ${i}`),
      assistantMessage(`Answer ${i}. `.repeat(30)),
    ]).flat(),
    ...toolTurn(),
  ])
  // Tools with no file or command to show: an MCP Server's and a Skill load.
  const mcpWork = session('Remember how we test', '/Users/dev/acme-web', [
    userMessage('Remember how we test'),
    {
      ...assistantMessage(''),
      content: [
        { type: 'tool_use', id: 'skill', name: 'Skill', input: { skill: 'grilling' } },
        {
          type: 'tool_use',
          id: 'list',
          name: 'droi-memory___memory_list',
          input: { scope: 'project', category: 'insight' },
        },
      ],
    },
    {
      ...assistantMessage(''),
      role: 'tool',
      content: [
        { type: 'tool_result', toolUseId: 'skill', content: 'Loaded skill grilling' },
        { type: 'tool_result', toolUseId: 'list', content: 'No entries in project Memory.' },
      ],
    },
    assistantMessage('Noted.'),
  ])
  test.use({ scenario: { sessions: [work, link, longWork, mcpWork] } })

  test('an MCP tool shows its server apart from its name, and its inputs as key: value', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /Remember how we test/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const list = transcript.getByRole('button', {
      name: 'droi-memory___memory_list: scope: project · category: insight',
    })
    await expect(list).toBeVisible()
    await expect(list).not.toContainText('___')
    await expect(list.getByText('droi-memory', { exact: true })).toBeVisible()
    await expect(list.getByText('memory_list', { exact: true })).toBeVisible()
    await expect(list).toContainText('scope: project · category: insight')
    await expect(transcript.getByRole('button', { name: 'Skill: grilling' })).toBeVisible()
  })

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

    // A picture a tool handed back shows in its detail, from the result itself.
    await expect(transcript.getByRole('img', { name: 'Picture from Read' })).toHaveCount(0)
    await transcript.getByRole('button', { name: 'Read: /tmp/shot.png' }).click()
    const picture = transcript.getByRole('img', { name: 'Picture from Read' })
    await expect(picture).toBeVisible()
    await expect(transcript.getByText('Image file: shot.png (original size: 1.2 KB)')).toBeVisible()
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

  test('opening and closing a fold keeps its header where it was tapped', async ({
    page,
    fakeDaemon,
  }) => {
    await pairPhone(page, fakeDaemon)
    await pickSession(page, /A long chat, then the fix/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    const create = transcript.getByRole('button', { name: 'Create: src/auth.test.ts' })
    const details = transcript.getByRole('region', { name: 'Create details' })
    await expect(create).toBeInViewport()
    await page.waitForTimeout(300)
    const before = (await create.boundingBox())!.y

    await create.click()
    await expect(details).toBeVisible()
    expect(Math.abs((await create.boundingBox())!.y - before)).toBeLessThan(2)
    // It stayed by scrolling, not because the row happened to grow the other way.
    expect(await transcript.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
    await create.click()
    await expect(details).toHaveCount(0)
    expect(Math.abs((await create.boundingBox())!.y - before)).toBeLessThan(2)

    // The whole cluster folds under its header the same way, given room to
    // scroll (at the very bottom nothing can move down to make up for it).
    await transcript.evaluate((el) => (el.scrollTop = 250))
    await page.waitForTimeout(300)
    const cluster = transcript.getByRole('button', { name: /Used 5 tools/ })
    await expect(cluster).toBeInViewport()
    const clusterBefore = (await cluster.boundingBox())!.y
    await cluster.click()
    await expect(create).toHaveCount(0)
    expect(Math.abs((await cluster.boundingBox())!.y - clusterBefore)).toBeLessThan(2)
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
