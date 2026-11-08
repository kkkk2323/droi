// The transcript on the phone, from the Kernel's Pi history: where a Session opens, older entries a page at a
// time, a streamed reply's Markdown and how the view follows it, and tool calls, reasoning and their folds.
import {
  assistantEntry,
  toolResultEntry,
  userEntry,
  type AssistantBlock,
  type EntryDraft,
} from './fake-kernel/history'
import {
  expect,
  inReadingOrder,
  pairPhone,
  pickSession,
  scrollToOldest,
  standIns,
  test,
  transcriptOf,
} from './fixtures'

/** Questions and answers, `count` entries in all: fewer than any window, or more, as a test needs. */
function chat(count: number, start = Date.now() - 2 * 60 * 60_000): EntryDraft[] {
  return Array.from({ length: count }, (_, i) =>
    i % 2 === 0
      ? userEntry(`Question ${i}`, start + i * 1000)
      : assistantEntry(`Answer ${i}`, start + i * 1000),
  )
}

test.describe('opening a Session', () => {
  test.use({ scenario: { sessions: [{ title: 'Long chat', cwd: 'acme-web', entries: chat(40) }] } })

  test('lands on the latest message; a new reply follows; scrolling up reaches the first', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Long chat/)
    const transcript = transcriptOf(page)
    await expect(transcript.getByText('Answer 39', { exact: true })).toBeInViewport()
    await expect(page.getByRole('button', { name: 'Scroll to latest' })).toHaveCount(0)

    await fakeKernel.turn('Long chat', 'carry on', { text: 'Carrying on.' })
    await expect(transcript.getByText('Carrying on.')).toBeInViewport()
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })

  test('opens on the latest messages, not scrolling down past all the others', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
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
    const transcript = transcriptOf(page)
    await expect(transcript.getByText('Answer 39', { exact: true })).toBeInViewport()
    const firstRows = await page.evaluate(
      () => (window as unknown as { firstRows: string[] }).firstRows,
    )
    expect(firstRows.some((row) => row.startsWith('Answer 39'))).toBe(true)
    expect(firstRows).not.toContain('Question 0')
    // The earlier messages come in above.
    await scrollToOldest(transcript)
    await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible()
  })
})

test.describe('a Session longer than one window', () => {
  test.use({
    scenario: { sessions: [{ title: 'Longer chat', cwd: 'acme-web', entries: chat(300) }] },
  })

  test('offers the previous messages at the top and brings them in, a page at a time', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Longer chat/)
    const transcript = transcriptOf(page)
    await expect(transcript.getByText('Answer 299', { exact: true })).toBeInViewport()
    await scrollToOldest(transcript)
    const more = transcript.getByRole('button', { name: 'Load previous messages' })
    await expect(more).toBeVisible()
    await expect(transcript.getByText('Question 0', { exact: true })).toHaveCount(0)

    // The oldest row on screen now: what the reader is looking at.
    const oldest = (await inReadingOrder(transcript.getByRole('article', { name: 'You' })))[0]!
    const reading = transcript.getByText(oldest, { exact: true })
    const before = (await reading.boundingBox())!.y
    await more.click()
    await phoneWire.waitForCall('conversation.entries')
    // They come in above, out of sight; what was being read stays where it was.
    const n = Number(oldest.replace('Question ', ''))
    await expect(transcript.getByText(`Question ${n - 2}`, { exact: true })).toHaveCount(1)
    await page.waitForTimeout(300)
    expect(Math.abs((await reading.boundingBox())!.y - before)).toBeLessThan(2)

    // Page after page, back to the first.
    await expect(async () => {
      await scrollToOldest(transcript)
      if (await more.isVisible()) await more.click()
      await expect(transcript.getByText('Question 0', { exact: true })).toBeVisible({
        timeout: 1_000,
      })
    }).toPass({ timeout: 30_000 })
    await expect(more).toHaveCount(0)
  })
})

test.describe('a streamed reply', () => {
  test.use({
    scenario: {
      sessions: [
        {
          title: 'Chat',
          cwd: 'acme-web',
          entries: [userEntry('hello'), assistantEntry('Hi. What should we build?')],
        },
      ],
    },
  })

  test('renders Markdown as it arrives and follows it; scrolling up stops following', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    const transcript = transcriptOf(page)
    const filler = Array.from({ length: 30 }, (_, i) => `- point ${i}\n`).join('')
    await fakeKernel.turn('Chat', 'plan it', {
      chunks: [
        '## The plan\n\n',
        filler,
        '\n**Bold** move with `npm test` and [docs](https://example.com)\n\n```ts\nconst answer',
        ' = 42\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\nThe end.',
      ],
      delayMs: 400,
    })
    const reply = transcript
      .getByRole('article', { name: 'Assistant' })
      .filter({ hasText: 'The plan' })
    await expect(reply.getByRole('heading', { name: 'The plan' })).toBeVisible()
    // A fence still open while streaming already shows as code.
    await expect(reply.getByRole('group', { name: 'ts code' })).toContainText('const answer')
    await expect(reply.getByText('The end.')).toBeInViewport()
    await expect(reply.getByRole('group', { name: 'ts code' })).toContainText('const answer = 42')
    await expect(reply.getByRole('link', { name: 'docs' })).toBeVisible()
    await expect(reply.getByRole('table')).toContainText('2')
    await fakeKernel.settled('Chat')

    // Reading further up while the next turn streams: the view stays put.
    await fakeKernel.turn('Chat', 'more', {
      chunks: Array.from({ length: 6 }, (_, i) => `Paragraph ${i} `.repeat(20) + '\n\n'),
      delayMs: 300,
    })
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

  test('a code block copies its text', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    await fakeKernel.turn('Chat', 'code', { text: '```sh\npnpm install:phone\n```\n' })
    const code = page.getByRole('group', { name: 'sh code' })
    await code.getByRole('button', { name: 'Copy code' }).click()
    await expect(code.getByRole('button', { name: 'Copied' })).toBeVisible()
    expect((await standIns(page)).clipboard).toBe('pnpm install:phone')
  })

  test('a table lines its columns up across rows', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Chat/)
    await fakeKernel.turn('Chat', 'table', {
      text:
        '| 状态 | 管理员邀请码 | Other codes |\n| - | - | - |\n' +
        '| 平时 | 可用 | see the current setting |\n| 收紧模式（新增） | 可用 | 不可用 |\n',
    })
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
  test.use({
    scenario: {
      sessions: [
        {
          title: 'Work',
          cwd: 'acme-web',
          entries: [
            userEntry('go'),
            assistantEntry(Array.from({ length: 40 }, (_, i) => `Block A line ${i}`).join('\n\n')),
          ],
        },
      ],
    },
  })

  test('an earlier part stays put while a later part streams below it', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Work/)
    const transcript = transcriptOf(page)
    await expect(transcript.getByText('Block A line 39')).toBeVisible()
    // A tool call first, then a reply that streams slowly: one turn that goes on below what is read.
    fakeKernel.model.push(
      { match: 'keep going', call: { name: 'bash', arguments: { command: 'echo step' } } },
      {
        match: 'keep going',
        chunks: Array.from(
          { length: 20 },
          (_, i) => `Streamed paragraph ${i}. `.repeat(8) + '\n\n',
        ),
        delayMs: 150,
      },
    )
    await fakeKernel.submit('Work', 'keep going')
    await expect(transcript.getByRole('button', { name: /bash: echo step/ })).toBeVisible()
    // Up in the earlier answer; the list is inverted, so up is further from 0.
    await transcript.evaluate((el) => (el.scrollTop = 600))
    const line = transcript.getByText('Block A line 20', { exact: true })
    await expect(line).toBeInViewport()
    const before = (await line.boundingBox())!.y

    await expect(transcript.getByText(/Streamed paragraph 19/)).toHaveCount(1, { timeout: 15_000 })
    await page.waitForTimeout(300)
    expect(Math.abs((await line.boundingBox())!.y - before)).toBeLessThan(2)
  })
})

const PIXEL =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

/** One turn of work, a second apart from prompt to reply: eleven seconds in all. */
function toolTurn(start: number): EntryDraft[] {
  let at = start
  const next = () => (at += 1000)
  const call = (
    id: string,
    name: string,
    args: Record<string, unknown>,
    before: AssistantBlock[] = [],
  ) => assistantEntry([...before, { type: 'toolCall', id, name, arguments: args }], next())
  return [
    userEntry('Fix login and add a test', at),
    call('read', 'read', { path: 'src/auth.ts' }, [
      { type: 'thinking', thinking: 'I should read the file before changing it.' },
    ]),
    toolResultEntry({ id: 'read', name: 'read' }, 'export function login() {}', { at: next() }),
    call('shot', 'read', { path: '/tmp/shot.png' }),
    {
      kind: 'pi.tool-result',
      model: [
        {
          role: 'toolResult',
          toolCallId: 'shot',
          toolName: 'read',
          content: [
            { type: 'text', text: 'Read image file [image/png]' },
            { type: 'image', data: PIXEL, mimeType: 'image/png' },
          ],
          isError: false,
          timestamp: next(),
        },
      ],
    },
    call('test', 'bash', { command: 'npm test' }),
    toolResultEntry({ id: 'test', name: 'bash' }, 'Error: 1 test failed', {
      isError: true,
      at: next(),
    }),
    call('edit', 'edit', {
      path: 'src/auth.ts',
      edits: [{ oldText: 'login()', newText: 'await login()' }],
    }),
    toolResultEntry({ id: 'edit', name: 'edit' }, 'Successfully replaced 1 block in src/auth.ts.', {
      details: { diff: ' 1 export async function run() {\n-2   login()\n+2   await login()\n 3 }' },
      at: next(),
    }),
    call('create', 'write', {
      path: 'src/auth.test.ts',
      content: "import { login } from './auth'\ntest('logs in', () => login())\n",
    }),
    toolResultEntry(
      { id: 'create', name: 'write' },
      'Successfully wrote 63 bytes to src/auth.test.ts',
      { at: next() },
    ),
    assistantEntry('Fixed: `login` is awaited now.', next()),
  ]
}

test.describe('tool calls and reasoning', () => {
  const start = Date.now() - 2 * 60 * 60_000
  test.use({
    scenario: {
      sessions: [
        { title: 'Login fix', cwd: 'acme-web', entries: toolTurn(start) },
        {
          title: 'Links',
          cwd: 'acme-web',
          entries: [
            userEntry(
              `see https://example.com/${'a-very-long-path-segment/'.repeat(12)}index.html`,
            ),
          ],
        },
        // The same work after enough conversation to fill the screen.
        {
          title: 'A long chat, then the fix',
          cwd: 'acme-web',
          entries: [
            ...Array.from({ length: 6 }, (_, i) => [
              userEntry(`Question ${i}`, start + i * 2000),
              assistantEntry(`Answer ${i}. `.repeat(30), start + i * 2000 + 1000),
            ]).flat(),
            ...toolTurn(start + 60_000),
          ],
        },
        // An MCP server's tool, with no file or command to show.
        {
          title: 'Remember how we test',
          cwd: 'acme-web',
          entries: [
            userEntry('Remember how we test'),
            assistantEntry([
              {
                type: 'toolCall',
                id: 'list',
                name: 'memory___memory_list',
                arguments: { scope: 'project', category: 'insight' },
              },
            ]),
            toolResultEntry(
              { id: 'list', name: 'memory___memory_list' },
              'No entries in project Memory.',
            ),
            assistantEntry('Noted.'),
          ],
        },
      ],
    },
  })

  test('an MCP tool shows its server apart from its name, and its inputs as key: value', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Remember how we test/)
    const transcript = transcriptOf(page)
    const list = transcript.getByRole('button', {
      name: 'memory___memory_list: scope: project · category: insight',
    })
    await expect(list).toBeVisible()
    await expect(list).not.toContainText('___')
    await expect(list.getByText('memory', { exact: true })).toBeVisible()
    await expect(list.getByText('memory_list', { exact: true })).toBeVisible()
    await expect(list).toContainText('scope: project · category: insight')
  })

  test('rows show success and failure and open to their details', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Login fix/)
    const transcript = transcriptOf(page)
    const read = transcript.getByRole('button', { name: 'read: src/auth.ts' })
    const tests = transcript.getByRole('button', { name: 'bash: npm test' })
    await expect(read.getByRole('img', { name: 'Succeeded' })).toBeVisible()
    await expect(tests.getByRole('img', { name: 'Failed' })).toBeVisible()

    await tests.click()
    const details = transcript.getByRole('region', { name: 'bash details' })
    await expect(details).toContainText('$ npm test')
    await expect(details).toContainText('Error: 1 test failed')

    // A picture a tool handed back shows in its detail, from the result itself.
    await expect(transcript.getByRole('img', { name: 'Picture from read' })).toHaveCount(0)
    await transcript.getByRole('button', { name: 'read: /tmp/shot.png' }).click()
    await expect(transcript.getByRole('img', { name: 'Picture from read' })).toBeVisible()
    await expect(transcript.getByText('Read image file [image/png]')).toBeVisible()

    // The reply closes the turn; its time says how long the turn took.
    await expect(transcript.getByText(/· took 11s$/)).toBeVisible()
    await expect(transcript.getByText(/· took/)).toHaveCount(1)
  })

  test('an edit shows a line diff and a write the new file', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Login fix/)
    const transcript = transcriptOf(page)
    const edit = transcript.getByRole('button', { name: 'edit: src/auth.ts' })
    await expect(edit).toContainText('+1')
    await expect(edit).toContainText('−1')
    await edit.click()
    const diff = transcript
      .getByRole('region', { name: 'edit details' })
      .getByRole('group', { name: 'Diff' })
    await expect(diff.getByLabel('Removed:   login()')).toBeVisible()
    await expect(diff.getByLabel('Added:   await login()')).toBeVisible()

    const write = transcript.getByRole('button', { name: 'write: src/auth.test.ts' })
    await expect(write).toContainText('+2')
    await write.click()
    const written = transcript.getByRole('region', { name: 'write details' })
    await expect(written.getByLabel("Added: import { login } from './auth'")).toBeVisible()
  })

  test('reasoning is folded and opens on tap', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Login fix/)
    const transcript = transcriptOf(page)
    const reasoning = transcript.getByRole('button', { name: 'Reasoning', exact: true })
    await expect(reasoning).toHaveAttribute('aria-expanded', 'false')
    await expect(transcript.getByText('I should read the file')).toHaveCount(0)
    await reasoning.click()
    await expect(transcript.getByText('I should read the file before changing it.')).toBeVisible()
  })

  test('opening and closing a fold keeps its header where it was tapped', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /A long chat, then the fix/)
    const transcript = transcriptOf(page)
    const write = transcript.getByRole('button', { name: 'write: src/auth.test.ts' })
    const details = transcript.getByRole('region', { name: 'write details' })
    await expect(write).toBeInViewport()
    await page.waitForTimeout(300)
    const before = (await write.boundingBox())!.y

    await write.click()
    await expect(details).toBeVisible()
    expect(Math.abs((await write.boundingBox())!.y - before)).toBeLessThan(2)
    // It stayed by scrolling, not because the row happened to grow the other way.
    expect(await transcript.evaluate((el) => el.scrollTop)).toBeGreaterThan(0)
    await write.click()
    await expect(details).toHaveCount(0)
    expect(Math.abs((await write.boundingBox())!.y - before)).toBeLessThan(2)

    // The whole cluster folds under its header the same way, given room to
    // scroll (at the very bottom nothing can move down to make up for it).
    await transcript.evaluate((el) => (el.scrollTop = 250))
    await page.waitForTimeout(300)
    const cluster = transcript.getByRole('button', { name: /Used 5 tools/ })
    await expect(cluster).toBeInViewport()
    const clusterBefore = (await cluster.boundingBox())!.y
    await cluster.click()
    await expect(write).toHaveCount(0)
    expect(Math.abs((await cluster.boundingBox())!.y - clusterBefore)).toBeLessThan(2)
  })

  test('a long link in a sent message wraps inside the bubble', async ({ page, fakeKernel }) => {
    await pairPhone(page, fakeKernel)
    await pickSession(page, /Links/)
    const bubble = transcriptOf(page)
      .getByRole('article', { name: 'You' })
      .getByText(/example\.com/)
    const box = (await bubble.boundingBox())!
    const width = page.viewportSize()!.width
    expect(box.x + box.width).toBeLessThanOrEqual(width)
  })
})
