import { afterEach, describe, expect, test } from 'bun:test'
import { existsSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { realpathSync } from 'node:fs'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelConfig } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'

/** A call the policy holds: the card on every connection, the first answer, and what the transcript keeps. */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; window: Client; phone: Client; faux: Faux; dir: string }

async function boot(overrides: Partial<KernelConfig> = {}): Promise<Booted> {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({
    dir: data.dir,
    faux,
    overrides: { policy: { autonomy: 'ask', rules: [] }, ...overrides },
  })
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
  cleanups.push(() => data.cleanup())
  cleanups.push(stop)
  return { kernel, window, phone, faux, dir: realpathSync(data.dir) }
}

async function conversation(b: Booted): Promise<number> {
  const created = await b.window.call('conversation.create', {
    agent: { model: b.faux.model, cwd: b.dir },
  })
  return created['conversationId'] as number
}

type Entry = {
  kind: string
  data?: Record<string, unknown>
  model?: { content: { text?: string }[]; isError?: boolean }[]
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

const toolResult = (client: Client, id: number) =>
  until(async () => (await entries(client, id)).find((entry) => entry.kind === 'pi.tool-result'))

const approvals = (list: Entry[]) =>
  list.filter((entry) => entry.kind === 'droi.approval').map((entry) => entry.data?.['status'])

describe('a call the policy holds', () => {
  test('waits for a person, on every connection, and runs once allowed', async () => {
    const b = await boot()
    const id = await conversation(b)
    const marker = join(b.dir, 'ran.txt')
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: `echo ok > ${marker}` }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('done'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.phone.waitForMethod('approval.requested')
    expect(card.params).toMatchObject({ id, tool: 'bash', scopeHint: 'ask' })
    // A command names no file: once, always this tool, and cancel.
    expect((card.params['options'] as { id: string }[]).map((option) => option.id)).toEqual([
      'proceed_once',
      'proceed_always_tool',
      'cancel',
    ])
    await b.window.waitForMethod('approval.requested')
    await Bun.sleep(200)
    expect(existsSync(marker)).toBe(false)

    const requestId = card.params['requestId'] as string
    expect(await b.phone.call('approval.respond', { requestId, outcome: 'proceed_once' })).toEqual({
      status: 'accepted',
    })
    const late = await b.window.fails('approval.respond', { requestId, outcome: 'cancel' })
    expect(late.code).toBe(-32007)
    const resolved = await b.window.waitForMethod('approval.resolved')
    expect(resolved.params).toEqual({ requestId, id, outcome: 'proceed_once', by: 'phone' })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError ?? false).toBe(false)
    expect(existsSync(marker)).toBe(true)
    expect(approvals(await entries(b.window, id))).toEqual(['pending', 'allowed'])
  }, 60_000)

  test('a connection that opens the conversation later sees the card, and can answer it', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'true' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('done'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.window.waitForMethod('approval.requested')
    const late = new Client(b.kernel.port, undefined, 'host')
    cleanups.push(() => late.close())
    const opened = await late.call('conversation.open', { id })
    const pending = opened['pending'] as Record<string, unknown>[]
    expect(pending).toEqual([{ kind: 'approval', ...card.params }])
    expect(opened['policy']).toEqual({ autonomy: 'ask', rules: [] })
    await late.call('approval.respond', {
      requestId: card.params['requestId'],
      outcome: 'proceed_once',
    })
    expect((await b.window.waitForMethod('approval.resolved')).params['by']).toBe('host')
    await toolResult(b.window, id)
    expect((await late.call('conversation.open', { id }))['pending']).toBeUndefined()
  }, 60_000)

  test('a cancel blocks the call: it never runs, and the model is told', async () => {
    const b = await boot()
    const id = await conversation(b)
    const marker = join(b.dir, 'never.txt')
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('write', { path: 'never.txt', content: 'x' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('understood'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.window.waitForMethod('approval.requested')
    // A file tool's card offers that file.
    expect(card.params['options']).toContainEqual({
      id: 'proceed_always_file',
      label: `Always allow ${marker}`,
      scope: { path: marker },
    })
    await b.window.call('approval.respond', {
      requestId: card.params['requestId'],
      outcome: 'cancel',
    })
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError).toBe(true)
    expect(JSON.stringify(result.model)).toContain('The user denied this call.')
    expect(existsSync(marker)).toBe(false)
    expect(approvals(await entries(b.window, id))).toEqual(['pending', 'denied'])
  }, 60_000)

  test('"always allow this file" becomes a rule of the conversation, announced whole, and is not asked again', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('write', { path: 'notes.md', content: 'one' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage(fauxToolCall('write', { path: './notes.md', content: 'two' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('written twice'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.window.waitForMethod('approval.requested')
    const file = join(b.dir, 'notes.md')
    await b.window.call('approval.respond', {
      requestId: card.params['requestId'],
      outcome: 'proceed_always_file',
    })
    const changed = await b.phone.waitForMethod('policy.changed')
    expect(changed.params).toEqual({
      id,
      policy: { autonomy: 'ask', rules: [{ kind: 'file', path: file }] },
    })
    await until(async () =>
      (await entries(b.window, id)).filter((entry) => entry.kind === 'pi.tool-result').length === 2
        ? true
        : undefined,
    )
    expect(
      b.window.notifications.filter((note) => note.method === 'approval.requested').length,
    ).toBe(1)
    expect(await Bun.file(file).text()).toBe('two')
    const recorded = (await entries(b.window, id)).filter((entry) => entry.kind === 'droi.approval')
    expect(recorded[1]?.data).toMatchObject({
      status: 'allowed',
      outcome: 'proceed_always_file',
      scope: { path: file },
    })
  }, 60_000)

  test('an abort ends the card: abandoned in the transcript, cancel by the kernel on the wire', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'true' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('never'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.window.waitForMethod('approval.requested')
    await b.window.call('conversation.abort', { id })
    const resolved = await b.phone.waitForMethod('approval.resolved')
    expect(resolved.params).toEqual({
      requestId: card.params['requestId'],
      id,
      outcome: 'cancel',
      by: 'kernel',
    })
    await until(async () =>
      approvals(await entries(b.window, id)).includes('abandoned') ? true : undefined,
    )
    expect(
      (
        await b.window.fails('approval.respond', {
          requestId: card.params['requestId'],
          outcome: 'proceed_once',
        })
      ).code,
    ).toBe(-32007)
  }, 60_000)

  test('a card nobody answers is cancelled by the kernel when its time runs out', async () => {
    const b = await boot({ approvalTimeoutMs: 150 })
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'true' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('ok'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    await b.window.waitForMethod('approval.requested')
    expect((await b.window.waitForMethod('approval.resolved')).params['by']).toBe('kernel')
    const result = await toolResult(b.window, id)
    expect(result.model?.[0]?.isError).toBe(true)
    expect(JSON.stringify(result.model)).toContain('No answer came')
    expect(approvals(await entries(b.window, id))).toEqual(['pending', 'abandoned'])
  }, 60_000)
})

describe('the policy', () => {
  test('nothing runs above ask before the Host pushes a policy, and the push lets it through', async () => {
    const b = await boot({ policy: undefined, policyWaitMs: 60_000 })
    const id = await conversation(b)
    const marker = join(b.dir, 'later.txt')
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: `echo ok > ${marker}` }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('done'),
    ])
    expect((await b.window.call('harness.info'))['policy']).toBeUndefined()
    await b.window.call('conversation.submit', { id, input: 'go' })
    await Bun.sleep(400)
    expect(existsSync(marker)).toBe(false)
    expect(b.window.notifications.some((note) => note.method === 'approval.requested')).toBe(false)
    const answer = await b.window.call('policy.set', { policy: { autonomy: 'all' } })
    expect(answer).toEqual({ policy: { autonomy: 'all', rules: [] } })
    expect((await b.phone.waitForMethod('policy.changed')).params).toEqual({
      id: null,
      policy: { autonomy: 'all', rules: [] },
    })
    await toolResult(b.window, id)
    expect(existsSync(marker)).toBe(true)
    expect((await b.window.call('harness.info'))['policy']).toEqual({ autonomy: 'all', rules: [] })
  }, 60_000)

  test('a conversation of its own: its level wins, an unknown one is -32000, a bad policy -32602', async () => {
    const b = await boot()
    const id = await conversation(b)
    expect(await b.window.call('policy.set', { id, policy: { autonomy: 'all' } })).toEqual({
      policy: { autonomy: 'all', rules: [] },
    })
    expect((await b.window.call('conversation.open', { id }))['policy']).toEqual({
      autonomy: 'all',
      rules: [],
    })
    expect((await b.window.fails('policy.set', { id: 424242, policy: {} })).code).toBe(-32000)
    expect((await b.window.fails('policy.set', { policy: { autonomy: 'often' } })).code).toBe(
      -32602,
    )
    const marker = join(b.dir, 'own.txt')
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: `echo ok > ${resolve(marker)}` }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('done'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    await toolResult(b.window, id)
    expect(existsSync(marker)).toBe(true)
  }, 60_000)

  test('a stop ends every card it leaves waiting', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'true' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('never'),
    ])
    await b.window.call('conversation.submit', { id, input: 'go' })
    const card = await b.window.waitForMethod('approval.requested')
    const resolved = b.phone.waitFor((note) => note.method === 'approval.resolved')
    await b.kernel.stop()
    expect((await resolved).params).toMatchObject({
      requestId: card.params['requestId'],
      by: 'kernel',
    })
  }, 60_000)
})
