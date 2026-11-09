import { afterEach, describe, expect, test } from 'bun:test'
import { existsSync, mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelConfig } from '../src/config.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { context } from '../src/session/service.ts'
import { NESTED_OUTPUT_BYTES } from '../src/extensions/codemode.ts'
import { backgroundOption } from '../src/extensions/codemode-jobs.ts'
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
  diff?: string
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

  test("a script's edit carries its diff, so the card shows what changed", async () => {
    const b = await boot()
    writeFileSync(join(b.dir, 'a.txt'), 'one\ntwo\nthree\n')
    const id = await conversation(b)
    script(
      b,
      'await tools.edit({ path: "a.txt", edits: [{ oldText: "two", newText: "zwei" }] }); return 1',
    )
    await b.client.call('conversation.submit', { id, input: 'edit in a script' })
    const [call] = callsOf(await result(b.client, id))
    expect(call).toMatchObject({ name: 'edit', status: 'ok' })
    expect(call?.diff).toContain('-2 two')
    expect(call?.diff).toContain('+2 zwei')
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

  test('in only mode the model is offered codemode, read and what a script cannot call, and scripts still call the rest', async () => {
    const b = await boot({ codemodeMode: 'only' })
    writeFileSync(join(b.dir, 'a.txt'), 'only through a script')
    const id = await conversation(b)
    const tools = await offered(b, id)
    expect(tools).toContain('codemode')
    expect(tools).toContain('ask_user')
    expect(tools).toContain('read')
    for (const name of ['bash', 'edit', 'write', 'grep', 'find', 'ls'])
      expect(tools).not.toContain(name)
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
    expect(await offered(b, id)).toContain('bash')
    await b.client.call('harness.configure', { codemode: { mode: 'only' } })
    expect(await offered(b, id)).not.toContain('bash')
    expect(await offered(b, direct)).toContain('bash')
    await b.client.call('harness.configure', { codemode: { mode: 'on' } })
    expect(await offered(b, id)).toContain('bash')
    await expect(
      b.client.call('harness.configure', { codemode: { mode: 'off' } }),
    ).rejects.toThrow()
  }, 60_000)

  test('in only mode a direct call of a script-only tool says how to make it from a script', async () => {
    const b = await boot({ codemodeMode: 'only' })
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'echo hi' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('done'),
    ])
    await b.client.call('conversation.submit', { id, input: 'run it' })
    const entry = await result(b.client, id)
    const said = entry.model?.[0]?.content.map((part) => part.text ?? '').join('\n') ?? ''
    expect(said).toContain('is not available')
    expect(said).toContain('await tools.bash(args)')
  }, 60_000)

  test('a script gets the whole output of a long command, the card still a cut of it', async () => {
    const b = await boot()
    const id = await conversation(b)
    // 30,000 lines, about 170 KB: past the 2000 lines and 50 KB the model sees, which keep only the tail.
    script(
      b,
      'const r = await tools.bash({ command: "seq 1 30000" }); const lines = r.output.trim().split("\\n"); return JSON.stringify({ first: lines[0], count: lines.length, truncated: r.truncated })',
    )
    await b.client.call('conversation.submit', { id, input: 'count' })
    const entry = await result(b.client, id)
    const said = entry.model?.[0]?.content.map((part) => part.text ?? '').join('') ?? ''
    expect(said).toContain('"first":"1"')
    expect(said).toContain('"count":30000')
    expect(said).toContain('"truncated":false')
    const [call] = callsOf(entry)
    expect(Buffer.byteLength(call?.output ?? '', 'utf8')).toBeLessThanOrEqual(NESTED_OUTPUT_BYTES)
  }, 60_000)

  test('a direct bash call still gives the model the tail it always had', async () => {
    const b = await boot()
    const id = await conversation(b)
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('bash', { command: 'seq 1 30000' }), {
        stopReason: 'toolUse',
      }),
      fauxAssistantMessage('done'),
    ])
    await b.client.call('conversation.submit', { id, input: 'run it' })
    const said =
      (await result(b.client, id)).model?.[0]?.content.map((part) => part.text ?? '').join('') ?? ''
    expect(said).toContain('30000')
    expect(said).not.toMatch(/^1\n2\n/)
  }, 60_000)

  test('grep, find and ls answer a script arrays, and git decides which files there are', async () => {
    const b = await boot()
    const repo = join(b.dir, 'repo')
    mkdirSync(join(repo, 'src', 'deep'), { recursive: true })
    writeFileSync(join(repo, '.gitignore'), 'built/\n')
    writeFileSync(join(repo, 'src', 'a.ts'), 'export const a = 1\n// TODO: one\n')
    writeFileSync(join(repo, 'src', 'deep', 'b.ts'), 'const b = 2 // todo: two\n')
    writeFileSync(join(repo, 'src', 'c.go'), 'package c // TODO: three\n')
    mkdirSync(join(repo, 'built'))
    writeFileSync(join(repo, 'built', 'x.ts'), '// TODO: ignored\n')
    Bun.spawnSync(['git', 'init', '-q'], { cwd: repo })
    const id = await conversation(b)
    script(
      b,
      [
        'const g = await tools.grep({ pattern: "todo", ignoreCase: true, path: "repo", glob: "*.ts" })',
        'const f = await tools.find({ pattern: "*.ts", path: "repo" })',
        'const l = await tools.ls({ path: "repo/src" })',
        'return JSON.stringify({ g: g.matches.map((m) => m.path + ":" + m.line), f: f.paths, l: l.entries.map((e) => e.name + "/" + e.type) })',
      ].join('\n'),
    )
    await b.client.call('conversation.submit', { id, input: 'search' })
    const entry = await result(b.client, id)
    const said = entry.model?.[0]?.content.map((part) => part.text ?? '').join('') ?? ''
    const answer = JSON.parse(said.slice(said.indexOf('{'), said.lastIndexOf('}') + 1)) as {
      g: string[]
      f: string[]
      l: string[]
    }
    expect(answer.g.sort()).toEqual(['repo/src/a.ts:2', 'repo/src/deep/b.ts:1'])
    expect(answer.f.sort()).toEqual(['repo/src/a.ts', 'repo/src/deep/b.ts'])
    expect(answer.l).toEqual(['a.ts/file', 'c.go/file', 'deep/directory'])
    expect(callsOf(entry).map((call) => call.name)).toEqual(['grep', 'find', 'ls'])
  }, 60_000)

  test('a conversation with codemode is told how to write a script; one without is not', async () => {
    const b = await boot()
    const id = await conversation(b)
    const direct = await conversation(b)
    await b.client.call('conversation.configure', {
      id: direct,
      change: { extensions: { remove: ['codemode'] } },
    })
    const prompts: string[] = []
    // The system prompt is the request's system message.
    const answer = (request: { messages: { role: string; content: unknown }[] }) => {
      prompts.push(JSON.stringify(request.messages.filter((message) => message.role === 'system')))
      return fauxAssistantMessage('ok')
    }
    b.faux.faux.setResponses([answer, answer])
    await b.client.call('conversation.submit', { id, input: 'hi' })
    await until(async () => (prompts.length >= 1 ? true : undefined))
    await b.client.call('conversation.submit', { id: direct, input: 'hi' })
    await until(async () => (prompts.length >= 2 ? true : undefined))
    expect(prompts[0]).toContain('How to write codemode scripts')
    expect(prompts[1]).not.toContain('How to write codemode scripts')
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

describe('background scripts', () => {
  /** The job id a codemode call that started one answered, from the conversation's context. */
  const jobIdIn = (messages: unknown): number => {
    const found = /as job (\d+)/.exec(JSON.stringify(messages))
    if (found === null) throw new Error('no job was started')
    return Number(found[1])
  }

  type Done = {
    kind: string
    data?: { jobId: number; status: string; output: string; details: { calls: Call[] } }
    model?: unknown
  }

  async function done(client: Client, id: number): Promise<Done> {
    return until(async () => {
      const page = await client.call('conversation.entries', {
        id,
        query: { order: 'ascending' },
        limit: 200,
      })
      return (page['items'] as Done[]).find((entry) => entry.kind === 'droi.codemode.done')
    })
  }

  test('a background script answers at once, runs on, and its result comes back as an entry the model reads', async () => {
    const b = await boot()
    const id = await conversation(b)
    script(
      b,
      '// @options: {"background": true, "timeout_ms": 60000}\nconst r = await tools.bash({ command: "sleep 1; echo from-the-job" }); text(r.output.trim())',
    )
    const started = Date.now()
    await b.client.call('conversation.submit', { id, input: 'run it in the background' })
    const entry = await result(b.client, id)
    const text = entry.model?.[0]?.content.map((c) => c.text).join('') ?? ''
    expect(text).toContain('Started in the background as job')
    const ended = await done(b.client, id)
    expect(Date.now() - started).toBeGreaterThan(900)
    expect(ended.data?.status).toBe('completed')
    expect(ended.data?.output).toContain('from-the-job')
    expect(ended.data?.details.calls[0]).toMatchObject({ name: 'bash', status: 'ok' })
    expect(ended.data?.details.calls[0]?.output).toContain('from-the-job')
    expect(ended.data?.details.calls[0]?.arguments).toEqual({
      command: 'sleep 1; echo from-the-job',
    })
    // The conversation was idle: the result is in its context for the next turn.
    expect(JSON.stringify(ended.model)).toContain('from-the-job')
    const live = await b.client.call('conversation.open', { id }).catch(() => undefined)
    void live
  }, 60_000)

  test('codemode_job waits for a job and reports its result', async () => {
    const b = await boot()
    const id = await conversation(b)
    let seen = ''
    b.faux.faux.setResponses([
      fauxAssistantMessage(
        fauxToolCall('codemode', {
          code: '// @options: {"background": true}\nawait tools.bash({ command: "sleep 1" }); text("waited for")',
        }),
        { stopReason: 'toolUse' },
      ),
      (context) =>
        fauxAssistantMessage(
          fauxToolCall('codemode_job', { id: jobIdIn(context.messages), wait_seconds: 20 }),
          {
            stopReason: 'toolUse',
          },
        ),
      (context) => {
        seen = JSON.stringify(context.messages.at(-1))
        return fauxAssistantMessage('done')
      },
    ])
    await b.client.call('conversation.submit', { id, input: 'start it and wait' })
    await until(async () => (seen === '' ? undefined : seen))
    expect(seen).toContain('ended')
    expect(seen).toContain('waited for')
  }, 60_000)

  test('codemode_job stops a job: the call it was making is cancelled', async () => {
    const b = await boot()
    const id = await conversation(b)
    let seen = ''
    b.faux.faux.setResponses([
      fauxAssistantMessage(
        fauxToolCall('codemode', {
          code: '// @options: {"background": true}\nawait tools.bash({ command: "sleep 30" }); text("never")',
        }),
        { stopReason: 'toolUse' },
      ),
      // A second in, it is still running, and it has made its call.
      (context) =>
        fauxAssistantMessage(
          fauxToolCall('codemode_job', { id: jobIdIn(context.messages), wait_seconds: 1 }),
          {
            stopReason: 'toolUse',
          },
        ),
      (context) => {
        running = JSON.stringify(context.messages.at(-1))
        return fauxAssistantMessage(
          fauxToolCall('codemode_job', { id: jobIdIn(context.messages), stop: true }),
          {
            stopReason: 'toolUse',
          },
        )
      },
      (context) => {
        seen = JSON.stringify(context.messages.at(-1))
        return fauxAssistantMessage('done')
      },
    ])
    let running = ''
    await b.client.call('conversation.submit', { id, input: 'start it and stop it' })
    const ended = await done(b.client, id)
    expect(ended.data?.status).toBe('stopped')
    expect(ended.data?.details.calls[0]).toMatchObject({ name: 'bash', status: 'cancelled' })
    await until(async () => (seen === '' ? undefined : seen))
    expect(running).toContain('still running')
    expect(running).toContain('bash running')
    expect(seen).toContain('was stopped')
  }, 60_000)

  test('the background option leaves the rest of the options line, and the line, in place', () => {
    expect(
      backgroundOption('// @options: {"background": true, "timeout_ms": 5}\nreturn 1'),
    ).toEqual({
      code: '// @options: {"timeout_ms":5}\nreturn 1',
      background: true,
    })
    expect(backgroundOption('// @options: {"background": true}\nreturn 1')).toEqual({
      code: '\nreturn 1',
      background: true,
    })
    expect(backgroundOption('return 1')).toEqual({ code: 'return 1', background: false })
  })
})
