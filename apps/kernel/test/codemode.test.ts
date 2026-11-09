import { afterEach, describe, expect, test } from 'bun:test'
import { existsSync, mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelConfig } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { context } from '../src/session/service.ts'
import { NESTED_OUTPUT_BYTES } from '../src/extensions/codemode.ts'
import { DEFAULT_SCRIPT_TIMEOUT_MS, codemodeSandbox } from '../src/codemode/sandbox.ts'
import {
  CodemodeSandbox,
  type CodemodeRun,
  type CodemodeRunOptions,
} from '@earendil-works/pi-codemode'

/** Codemode (design/m3-kernel.md §6): on by default, scripts call the conversation's tools, and the card's calls. */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; client: Client; faux: Faux; dir: string }

async function boot(overrides: Partial<KernelConfig> = {}): Promise<Booted> {
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({
    dir: data.dir,
    faux,
    overrides: { policy: { autonomy: 'all', rules: [] }, ...overrides },
  })
  const client = new Client(kernel.port, undefined, 'window')
  let stopped = false
  cleanups.push(() => data.cleanup())
  cleanups.push(async () => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  })
  return { kernel, client, faux, dir: realpathSync(data.dir) }
}

async function conversation(b: Booted): Promise<number> {
  const created = await b.client.call('conversation.create', {
    agent: { model: b.faux.model, cwd: b.dir },
  })
  return created['conversationId'] as number
}

type Call = {
  id: number
  name: string
  args: string
  arguments?: unknown
  status: string
  output?: string
  truncated?: boolean
  error?: string
}
type Entry = {
  kind: string
  model?: {
    content: { type: string; text?: string }[]
    isError?: boolean
    details?: { calls?: Call[] }
  }[]
}

/** The calls a codemode result lists: Pi keeps `details` on the result message. */
const callsOf = (entry: Entry): Call[] => entry.model?.[0]?.details?.calls ?? []

async function until<T>(read: () => Promise<T | undefined>, timeoutMs = 30_000): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await read()
    if (value !== undefined) return value
    if (Date.now() > deadline) throw new Error('timed out')
    await Bun.sleep(20)
  }
}

async function result(client: Client, id: number): Promise<Entry> {
  return until(async () => {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    return (page['items'] as Entry[]).find((entry) => entry.kind === 'pi.tool-result')
  })
}

function script(b: Booted, code: string): void {
  b.faux.faux.setResponses([
    fauxAssistantMessage(fauxToolCall('codemode', { code }), { stopReason: 'toolUse' }),
    fauxAssistantMessage('done'),
  ])
}

async function offered(b: Booted, id: number): Promise<string[]> {
  const conv = await b.kernel.harness.conversation(id as never, context)
  return (await conv!.agent(context)).tools.map((tool) => tool.name)
}

describe('codemode', () => {
  test("is on for a new conversation, and a script's calls carry what they answered", async () => {
    const b = await boot()
    writeFileSync(join(b.dir, 'a.txt'), 'hello from a file')
    const id = await conversation(b)
    expect(await offered(b, id)).toContain('codemode')
    script(b, 'const t = await tools.read({ path: "a.txt" }); text(t); return "ok"')
    await b.client.call('conversation.submit', { id, input: 'read it in a script' })
    const entry = await result(b.client, id)
    expect(entry.model?.[0]?.isError ?? false).toBe(false)
    const calls = callsOf(entry)
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ name: 'read', status: 'ok' })
    expect(calls[0]?.output).toContain('hello from a file')
  }, 60_000)

  test('a call that fails keeps the calls before it, each with its own answer', async () => {
    const b = await boot()
    writeFileSync(join(b.dir, 'a.txt'), 'first')
    const id = await conversation(b)
    script(
      b,
      'await tools.read({ path: "a.txt" }); await tools.read({ path: "missing.txt" }); return 1',
    )
    await b.client.call('conversation.submit', { id, input: 'two reads' })
    const calls = callsOf(await result(b.client, id))
    expect(calls.map((call) => [call.name, call.status])).toEqual([
      ['read', 'ok'],
      ['read', 'error'],
    ])
    expect(calls[0]?.output).toContain('first')
    expect(calls[1]?.output ?? '').not.toBe('')
  }, 60_000)

  test('a long answer is cut on the card, and says so', async () => {
    const b = await boot()
    writeFileSync(join(b.dir, 'big.txt'), `${'x'.repeat(200)}\n`.repeat(100))
    const id = await conversation(b)
    script(b, 'await tools.read({ path: "big.txt" }); return 1')
    await b.client.call('conversation.submit', { id, input: 'read a big file' })
    const [call] = callsOf(await result(b.client, id))
    expect(call?.truncated).toBe(true)
    expect(Buffer.byteLength(call?.output ?? '')).toBeLessThanOrEqual(NESTED_OUTPUT_BYTES)
  }, 60_000)

  test('a conversation can turn it off', async () => {
    const b = await boot()
    const id = await conversation(b)
    await b.client.call('conversation.configure', {
      id,
      change: { extensions: { remove: ['codemode'] } },
    })
    expect(await offered(b, id)).not.toContain('codemode')
  }, 60_000)

  test('in only mode the model is offered codemode and what a script cannot call, and scripts still call the rest', async () => {
    const b = await boot({ codemodeMode: 'only' })
    writeFileSync(join(b.dir, 'a.txt'), 'only through a script')
    const id = await conversation(b)
    const tools = await offered(b, id)
    expect(tools).toContain('codemode')
    expect(tools).toContain('ask_user')
    expect(tools).not.toContain('read')
    expect(tools).not.toContain('bash')
    const info = await b.client.call('harness.info', {})
    expect(info['codemode']).toEqual({ mode: 'only' })
    script(b, 'const t = await tools.read({ path: "a.txt" }); text(t); return "ok"')
    await b.client.call('conversation.submit', { id, input: 'read it in a script' })
    const [call] = callsOf(await result(b.client, id))
    expect(call).toMatchObject({ name: 'read', status: 'ok' })
    expect(call?.output).toContain('only through a script')
  }, 60_000)

  test('harness.configure switches the mode for every conversation, and one without codemode keeps its tools', async () => {
    const b = await boot()
    const id = await conversation(b)
    const direct = await conversation(b)
    await b.client.call('conversation.configure', {
      id: direct,
      change: { extensions: { remove: ['codemode'] } },
    })
    expect(await offered(b, id)).toContain('read')
    await b.client.call('harness.configure', { codemode: { mode: 'only' } })
    expect(await offered(b, id)).not.toContain('read')
    expect(await offered(b, direct)).toContain('read')
    await b.client.call('harness.configure', { codemode: { mode: 'on' } })
    expect(await offered(b, id)).toContain('read')
    await expect(
      b.client.call('harness.configure', { codemode: { mode: 'off' } }),
    ).rejects.toThrow()
  }, 60_000)

  test("a script's own calls wait for the approval policy too", async () => {
    const b = await boot({ policy: { autonomy: 'ask', rules: [] } })
    const marker = join(b.dir, 'ran.txt')
    const id = await conversation(b)
    // The script itself asks first; then its bash call does.
    script(b, `await tools.bash({ command: "echo ok > ${marker}" }); return 1`)
    await b.client.call('conversation.submit', { id, input: 'run a command in a script' })
    const first = await b.client.waitForMethod('approval.requested')
    expect(first.params['tool']).toBe('codemode')
    await b.client.call('approval.respond', {
      requestId: first.params['requestId'],
      outcome: 'proceed_once',
    })
    const nested = await b.client.waitFor(
      (n) => n.method === 'approval.requested' && n.params['tool'] === 'bash',
    )
    await b.client.call('approval.respond', {
      requestId: nested.params['requestId'],
      outcome: 'proceed_once',
    })
    const [call] = callsOf(await result(b.client, id))
    expect(call).toMatchObject({ name: 'bash', status: 'ok' })
  }, 60_000)
  for (const mode of ['on', 'only'] as const)
    test(`an MCP server's tools are a script's too, under the server's namespace (${mode})`, async () => {
      const data = tempData()
      cleanups.push(() => data.cleanup())
      mkdirSync(join(data.dir, 'user'), { recursive: true })
      writeFileSync(
        join(data.dir, 'user', 'mcp.json'),
        JSON.stringify({
          mcpServers: {
            fake: {
              command: process.execPath,
              args: ['run', new URL('./mcp-server.ts', import.meta.url).pathname],
            },
          },
        }),
      )
      const faux = fauxCatalog()
      const kernel = await start({
        dir: data.dir,
        faux,
        overrides: { policy: { autonomy: 'all', rules: [] }, codemodeMode: mode },
      })
      const client = new Client(kernel.port, undefined, 'window')
      cleanups.push(async () => {
        client.close()
        await kernel.stop()
      })
      await until(async () =>
        ((await client.call('mcp.list', {}))['servers'] as { status: string }[])[0]?.status ===
        'connected'
          ? true
          : undefined,
      )
      const id = (
        await client.call('conversation.create', {
          agent: { model: faux.model, cwd: realpathSync(data.dir) },
        })
      )['conversationId'] as number
      faux.faux.setResponses([
        fauxAssistantMessage(
          fauxToolCall('codemode', { code: 'return await tools.fake___echo({ text: "hi" })' }),
          { stopReason: 'toolUse' },
        ),
        fauxAssistantMessage('done'),
      ])
      await client.call('conversation.submit', { id, input: 'echo from a script' })
      const [call] = callsOf(await result(client, id))
      expect(call).toMatchObject({ name: 'fake___echo', status: 'ok' })
      expect(call?.output).toContain('echo: hi')
      // The server's tools were installed after the kernel started; `only` hides them from the model all the same.
      const conv = await kernel.harness.conversation(id as never, context)
      const names = (await conv!.agent(context)).tools.map((tool) => tool.name)
      expect(names.includes('fake___echo')).toBe(mode === 'on')
    }, 60_000)
  test("a call's card has the whole arguments it ran with, not Pi's 200-character preview", async () => {
    const b = await boot()
    const id = await conversation(b)
    const command = `echo ${'z'.repeat(300)}`
    script(b, `await tools.bash({ command: ${JSON.stringify(command)} }); return 1`)
    await b.client.call('conversation.submit', { id, input: 'a long command in a script' })
    const [call] = callsOf(await result(b.client, id))
    expect(call?.args.endsWith('...')).toBe(true)
    expect(call?.arguments).toEqual({ command })
  }, 60_000)

  test('a script the user stops keeps the calls it made', async () => {
    const b = await boot()
    writeFileSync(join(b.dir, 'a.txt'), 'first')
    const marker = join(b.dir, 'started')
    const id = await conversation(b)
    script(
      b,
      `await tools.read({ path: "a.txt" }); await tools.bash({ command: "touch ${marker}; sleep 30" }); return 1`,
    )
    await b.client.call('conversation.submit', { id, input: 'read, then wait' })
    await until(async () => (existsSync(marker) ? true : undefined))
    await b.client.call('conversation.abort', { id })
    const calls = callsOf(await result(b.client, id))
    expect(calls.map((call) => [call.name, call.status])).toEqual([
      ['read', 'ok'],
      ['bash', 'cancelled'],
    ])
    expect(calls[0]?.output).toContain('first')
    expect(calls[0]?.arguments).toEqual({ path: 'a.txt' })
  }, 60_000)

  test('a script that names no timeout gets one', () => {
    const seen: (number | undefined)[] = []
    const original = Object.getOwnPropertyDescriptor(CodemodeSandbox.prototype, 'start')
    CodemodeSandbox.prototype.start = function (_code: string, options: CodemodeRunOptions) {
      seen.push(options.timeoutMs)
      return {} as CodemodeRun
    }
    try {
      const sandbox = codemodeSandbox()
      const options = { startedAt: 0, seed: 1, tools: [], globals: [], onCall: () => {} }
      sandbox.start('return 1', options)
      sandbox.start('return 1', { ...options, timeoutMs: 5 })
    } finally {
      if (original === undefined) delete (CodemodeSandbox.prototype as { start?: unknown }).start
      else Object.defineProperty(CodemodeSandbox.prototype, 'start', original)
    }
    expect(seen).toEqual([DEFAULT_SCRIPT_TIMEOUT_MS, 5])
  })
})
