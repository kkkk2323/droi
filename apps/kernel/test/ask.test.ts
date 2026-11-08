import { afterEach, describe, expect, test } from 'bun:test'
import { Database } from 'bun:sqlite'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelConfig } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'

/** The ask_user tool: a question on every connection, the first answer, and where the answer goes. */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; window: Client; phone: Client; faux: Faux; dir: string }

async function boot(overrides: Partial<KernelConfig> = {}, dir = tempData()): Promise<Booted> {
  const faux = fauxCatalog()
  const kernel = await start({ dir: dir.dir, faux, overrides })
  const window = new Client(kernel.port, undefined, 'window')
  const phone = new Client(kernel.port, undefined, 'phone')
  let stopped = false
  const stop = async () => {
    if (stopped) return
    stopped = true
    window.close()
    phone.close()
    await kernel.stop()
  }
  cleanups.push(() => dir.cleanup())
  cleanups.push(stop)
  return { kernel, window, phone, faux, dir: dir.dir }
}

async function conversation(b: Booted): Promise<number> {
  const created = await b.window.call('conversation.create', { agent: { model: b.faux.model } })
  return created['conversationId'] as number
}

type Entry = {
  kind: string
  data?: Record<string, unknown>
  model?: {
    role?: string
    content: string | { type: string; text?: string }[]
    isError?: boolean
  }[]
}

async function entries(client: Client, id: number): Promise<Entry[]> {
  const page = await client.call('conversation.entries', {
    id,
    query: { order: 'ascending' },
    limit: 200,
  })
  return page['items'] as Entry[]
}

async function until<T>(read: () => Promise<T | undefined>, timeoutMs = 20_000): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await read()
    if (value !== undefined) return value
    if (Date.now() > deadline) throw new Error('timed out')
    await Bun.sleep(20)
  }
}

const textOf = (entry: Entry | undefined) =>
  (entry?.model ?? [])
    .flatMap((m) =>
      typeof m.content === 'string' ? [m.content] : m.content.map((c) => c.text ?? ''),
    )
    .join('')

const toolResult = (client: Client, id: number) =>
  until(async () => (await entries(client, id)).find((entry) => entry.kind === 'pi.tool-result'))

const asks = (list: Entry[]) =>
  list.filter((entry) => entry.kind === 'droi.ask').map((entry) => entry.data?.['status'])

const question = {
  context: 'Asked while running the test command.',
  questions: [
    {
      question: 'The suite cannot reach the network. What should the new test do?',
      options: [
        { id: 'skip', label: 'Skip when offline' },
        { id: 'fail', label: 'Fail' },
      ],
    },
  ],
}

describe('a blocking question', () => {
  test('reaches every connection, and the first answer is the tool result', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', question), { stopReason: 'toolUse' }),
      fauxAssistantMessage('noted'),
    ])
    await b.window.call('conversation.submit', { id, input: 'write the test' })
    const asked = await b.phone.waitForMethod('ask.requested')
    expect(asked.params).toMatchObject({
      id,
      context: question.context,
      blocking: true,
      questions: [{ questionId: 'q1', question: question.questions[0]!.question }],
    })
    await b.window.waitForMethod('ask.requested')
    const requestId = asked.params['requestId'] as string
    expect(
      await b.phone.call('ask.respond', {
        requestId,
        answers: [{ questionId: 'q1', selected: ['fail'] }],
      }),
    ).toEqual({ status: 'accepted' })
    expect((await b.window.fails('ask.respond', { requestId, cancelled: true })).code).toBe(-32007)
    expect((await b.window.waitForMethod('ask.resolved')).params).toEqual({
      requestId,
      id,
      answers: [{ questionId: 'q1', selected: ['fail'] }],
      cancelled: false,
      by: 'phone',
    })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError ?? false).toBe(false)
    expect(textOf(result)).toContain('A: Fail')
    expect(asks(await entries(b.window, id))).toEqual(['pending', 'answered'])
  }, 60_000)

  test('a typed answer, a dismissal, and answers that do not fit', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', question), { stopReason: 'toolUse' }),
      fauxAssistantMessage('ok'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const requestId = (await b.window.waitForMethod('ask.requested')).params['requestId'] as string
    for (const answers of [
      [{ questionId: 'q1', selected: ['maybe'] }],
      [{ questionId: 'q1', selected: ['skip', 'fail'] }],
      [{ questionId: 'q9', text: 'x' }],
      [],
      [{ questionId: 'q1' }],
    ]) {
      expect((await b.window.fails('ask.respond', { requestId, answers })).code).toBe(-32602)
    }
    await b.window.call('ask.respond', { requestId, cancelled: true })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError).toBe(true)
    expect(textOf(result)).toContain('dismissed')
  }, 60_000)

  test('a connection that opens later sees the question and can answer it in its own words', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', question), { stopReason: 'toolUse' }),
      fauxAssistantMessage('ok'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const asked = await b.window.waitForMethod('ask.requested')
    const late = new Client(b.kernel.port, undefined, 'host')
    cleanups.push(() => late.close())
    const opened = await late.call('conversation.open', { id })
    expect(opened['pending']).toEqual([{ kind: 'ask', ...asked.params }])
    await late.call('ask.respond', {
      requestId: asked.params['requestId'],
      answers: [{ questionId: 'q1', text: 'Retry three times' }],
    })
    expect(textOf(await toolResult(b.window, id))).toContain('A: Retry three times')
    expect((await late.call('conversation.open', { id }))['pending']).toBeUndefined()
  }, 60_000)

  test('nobody answers: the kernel ends it, and the model is told', async () => {
    const b = await boot({ approvalTimeoutMs: 150 })
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', question), { stopReason: 'toolUse' }),
      fauxAssistantMessage('ok'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    await b.window.waitForMethod('ask.requested')
    expect((await b.window.waitForMethod('ask.resolved')).params).toMatchObject({
      cancelled: true,
      by: 'kernel',
    })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError).toBe(true)
    expect(asks(await entries(b.window, id))).toEqual(['pending', 'abandoned'])
  }, 60_000)

  test('without a context the question is not asked', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', { ...question, context: '' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('ok'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError).toBe(true)
    expect(b.window.notifications.some((note) => note.method === 'ask.requested')).toBe(false)
  }, 60_000)

  for (const [name, questions] of [
    ['no questions', []],
    ['a blank question', [{ question: '   ' }]],
  ] as const) {
    test(`with ${name} nothing waits`, async () => {
      const b = await boot()
      const id = await conversation(b)
      b.faux.faux.setResponses([
        fauxAssistantMessage(fauxToolCall('ask_user', { ...question, questions }), {
          stopReason: 'toolUse',
        }),
        fauxAssistantMessage('ok'),
      ])
      await b.window.call('conversation.submit', { id, input: 'go' })
      const result = await toolResult(b.window, id)
      expect(result.model?.[0]?.isError).toBe(true)
      expect(b.window.notifications.some((note) => note.method === 'ask.requested')).toBe(false)
    }, 60_000)
  }
})

describe('a question that does not block', () => {
  test('lets the run go on, and its answer comes back as the next message', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', { ...question, blocking: false }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('carrying on'),
      fauxAssistantMessage('thanks, skipping when offline'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const asked = await b.window.waitForMethod('ask.requested')
    expect(asked.params['blocking']).toBe(false)
    // The run ends without the answer.
    await until(async () =>
      (await entries(b.window, id)).some((entry) => textOf(entry).includes('carrying on'))
        ? true
        : undefined,
    )
    await b.phone.call('ask.respond', {
      requestId: asked.params['requestId'],
      answers: [{ questionId: 'q1', selected: ['skip'] }],
    })
    await until(async () =>
      (await entries(b.window, id)).some((entry) => textOf(entry).includes('thanks, skipping'))
        ? true
        : undefined,
    )
    const list = await entries(b.window, id)
    const users = list.filter((entry) => entry.kind === 'pi.user').map(textOf)
    expect(users.at(-1)).toContain('A: Skip when offline')
    expect(asks(list)).toEqual(['pending', 'answered'])
  }, 60_000)

  test('stays open across a restart, and is still answered there', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const first = await boot({}, { dir: data.dir, cleanup: () => undefined })
    const id = await conversation(first)
    first.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', { ...question, blocking: false }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('carrying on'),
    ])
    await first.window.call('conversation.submit', { id, input: 'go' })
    const asked = await first.window.waitForMethod('ask.requested')
    await until(async () =>
      (await entries(first.window, id)).some((entry) => textOf(entry).includes('carrying on'))
        ? true
        : undefined,
    )
    first.window.close()
    first.phone.close()
    await first.kernel.stop()

    const second = await boot({}, { dir: data.dir, cleanup: () => undefined })
    second.faux.faux.setResponses([fauxAssistantMessage('got it')])
    const opened = await second.window.call('conversation.open', { id })
    expect(opened['pending']).toEqual([{ kind: 'ask', ...asked.params }])
    await second.window.call('ask.respond', {
      requestId: asked.params['requestId'],
      answers: [{ questionId: 'q1', selected: ['fail'] }],
    })
    await until(async () =>
      (await entries(second.window, id)).some((entry) => textOf(entry).includes('got it'))
        ? true
        : undefined,
    )
    expect(asks(await entries(second.window, id))).toEqual(['pending', 'answered'])
  }, 60_000)

  test('a row left by a process that died after the answer does not bring the question back', async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const first = await boot({}, { dir: data.dir, cleanup: () => undefined })
    const id = await conversation(first)
    first.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('ask_user', { ...question, blocking: false }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('carrying on'),
      fauxAssistantMessage('got it'),
    ])
    await first.window.call('conversation.submit', { id, input: 'go' })
    const asked = await first.window.waitForMethod('ask.requested')
    await first.window.call('ask.respond', {
      requestId: asked.params['requestId'],
      answers: [{ questionId: 'q1', selected: ['fail'] }],
    })
    await until(async () =>
      (await entries(first.window, id)).some((entry) => textOf(entry).includes('got it'))
        ? true
        : undefined,
    )
    first.window.close()
    first.phone.close()
    await first.kernel.stop()

    // What a crash between the answer's commit and the row's removal leaves.
    const db = new Database(join(data.dir, 'kernel.sqlite'))
    db.run(
      'INSERT INTO droi_open_questions (request_id, conversation_id, request, at) VALUES (?, ?, ?, ?)',
      [asked.params['requestId'] as string, id, JSON.stringify(asked.params), Date.now()],
    )
    db.close()

    const second = await boot({}, { dir: data.dir, cleanup: () => undefined })
    expect((await second.window.call('conversation.open', { id }))['pending']).toBeUndefined()
    expect(
      (
        await second.window.fails('ask.respond', {
          requestId: asked.params['requestId'],
          cancelled: true,
        })
      ).code,
    ).toBe(-32007)
    second.window.close()
    second.phone.close()
    await second.kernel.stop()
    const after = new Database(join(data.dir, 'kernel.sqlite'))
    expect(after.query('SELECT COUNT(*) AS n FROM droi_open_questions').get()).toEqual({ n: 0 })
    after.close()
  }, 60_000)
})
