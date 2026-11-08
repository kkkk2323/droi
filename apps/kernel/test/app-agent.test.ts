import { afterEach, describe, expect, test } from 'bun:test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { appAgentFromEnv, type AppAgent } from '../src/config.ts'
import { Client, fauxCatalog, start, tempData, waitIdle } from './helpers.ts'

/**
 * The brand's app (ADR 0041): its MCP servers beside the user's, the tools it lets run without asking, and its
 * prompt section, as the Host gives them in `DROI_KERNEL_AGENT`.
 */

const serverPath = new URL('./mcp-server.ts', import.meta.url).pathname
const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

function fakeServer(env: Record<string, string> = {}): Record<string, unknown> {
  return { type: 'stdio', command: process.execPath, args: ['run', serverPath], env }
}

type Seen = { tools: string[]; system: string }

/** A kernel with the app, policy at `ask` unless said, and the model's view of each turn. */
async function boot(app: AppAgent, script: unknown[], autonomy = 'ask') {
  const data = tempData()
  cleanups.push(() => data.cleanup())
  const faux = fauxCatalog()
  const seen: Seen[] = []
  const watch = (reply: unknown) => (context: { messages: Record<string, unknown>[] }) => {
    seen.push(promptOf(context.messages))
    return reply
  }
  faux.faux.setResponses(script.map((reply) => watch(reply)) as never)
  const kernel = await start({
    dir: data.dir,
    faux,
    overrides: { app, policy: { autonomy: autonomy as never, rules: [] } },
  })
  const client = new Client(kernel.port)
  cleanups.push(async () => {
    client.close()
    await kernel.stop()
  })
  const id = (
    await client.call('conversation.create', { agent: { model: faux.model, cwd: data.dir } })
  )['conversationId'] as number
  const ask = async (input: string) => {
    await client.call('conversation.submit', { id, input })
    await waitIdle(client, id)
  }
  return { client, ask, seen, dir: data.dir, id }
}

/**
 * The prompt as the model has it at a turn: each system message adds tools, takes some away, and sets sections; a
 * later value of a section replaces the earlier one.
 */
function promptOf(messages: Record<string, unknown>[]): Seen {
  const tools = new Set<string>()
  const sections = new Map<string, string>()
  for (const message of messages) {
    if (message['role'] !== 'system') continue
    for (const tool of (message['toolsRemoved'] ?? []) as { name: string }[])
      tools.delete(tool.name)
    for (const tool of (message['toolsAdded'] ?? []) as { name: string }[]) tools.add(tool.name)
    for (const [name, text] of Object.entries(
      (message['sections'] ?? {}) as Record<string, string>,
    )) {
      sections.set(name, text)
    }
  }
  return { tools: [...tools], system: [...sections.values()].join('\n') }
}

const app = (overrides: Partial<AppAgent>): AppAgent => ({
  mcpServers: {},
  blockOnLoad: [],
  alwaysAllow: [],
  ...overrides,
})

describe('DROI_KERNEL_AGENT', () => {
  test('reads servers with blockOnLoad apart, the tools that run unasked, and the section', () => {
    const read = appAgentFromEnv({
      DROI_KERNEL_AGENT: JSON.stringify({
        mcpServers: {
          anlan: { command: '/bin/anlan', args: ['mcp'], blockOnLoad: true },
          web: { url: 'http://x' },
        },
        alwaysAllow: ['anlan___db_query'],
        section: 'Use the anlan tools.',
      }),
    })
    expect(read).toEqual({
      mcpServers: { anlan: { command: '/bin/anlan', args: ['mcp'] }, web: { url: 'http://x' } },
      blockOnLoad: ['anlan'],
      alwaysAllow: ['anlan___db_query'],
      section: 'Use the anlan tools.',
    })
    expect(appAgentFromEnv({})).toBeUndefined()
    expect(() => appAgentFromEnv({ DROI_KERNEL_AGENT: '[]' })).toThrow()
    expect(() => appAgentFromEnv({ DROI_KERNEL_AGENT: '{"alwaysAllow":[""]}' })).toThrow()
  })
})

describe("the app's servers", () => {
  test('a slow blockOnLoad server is in the very first turn, and runs a tool the app allows unasked', async () => {
    const dir = tempData()
    cleanups.push(() => dir.cleanup())
    const touched = join(dir.dir, 'touched')
    const { ask, seen, client } = await boot(
      app({
        mcpServers: { fake: fakeServer({ DELAY: '1500' }) },
        blockOnLoad: ['fake'],
        alwaysAllow: ['fake___touch'],
      }),
      [
        fauxAssistantMessage(fauxToolCall('fake___touch', { path: touched }), {
          stopReason: 'toolUse',
        }),
        fauxAssistantMessage('done'),
      ],
    )
    await ask('touch it')
    expect(seen[0]!.tools).toContain('fake___touch')
    expect(existsSync(touched)).toBe(true)
    const servers = (await client.call('mcp.list', {}))['servers'] as {
      name: string
      app?: boolean
    }[]
    expect(servers).toEqual([
      expect.objectContaining({ name: 'fake', app: true, status: 'connected' }),
    ])
    // The app's rule is the app's: no snapshot the Host stores holds it.
    const policy = (await client.call('policy.set', { policy: { autonomy: 'ask' } }))['policy'] as {
      rules: unknown[]
    }
    expect(policy.rules).toEqual([])
  }, 60_000)

  test('a tool the app does not allow still asks', async () => {
    const dir = tempData()
    cleanups.push(() => dir.cleanup())
    const touched = join(dir.dir, 'touched')
    const { client, id, seen } = await boot(
      app({
        mcpServers: { fake: fakeServer() },
        blockOnLoad: ['fake'],
        alwaysAllow: ['fake___echo'],
      }),
      [
        fauxAssistantMessage(fauxToolCall('fake___touch', { path: touched }), {
          stopReason: 'toolUse',
        }),
        fauxAssistantMessage('done'),
      ],
    )
    await client.call('conversation.submit', { id, input: 'touch it' })
    await client.waitForMethod('approval.requested')
    expect(seen[0]!.tools).toContain('fake___touch')
    expect(existsSync(touched)).toBe(false)
  }, 60_000)

  test("one takes the place of the user's server of the same name", async () => {
    const data = tempData()
    cleanups.push(() => data.cleanup())
    const { mkdirSync, writeFileSync } = await import('node:fs')
    mkdirSync(join(data.dir, 'user'), { recursive: true })
    writeFileSync(
      join(data.dir, 'user', 'mcp.json'),
      JSON.stringify({ mcpServers: { fake: fakeServer({ INSTRUCTIONS: 'the user one' }) } }),
    )
    const faux = fauxCatalog()
    const kernel = await start({
      dir: data.dir,
      faux,
      overrides: { app: app({ mcpServers: { fake: fakeServer() }, blockOnLoad: ['fake'] }) },
    })
    const client = new Client(kernel.port)
    cleanups.push(async () => {
      client.close()
      await kernel.stop()
    })
    const id = (
      await client.call('conversation.create', { agent: { model: faux.model, cwd: data.dir } })
    )['conversationId'] as number
    // Both connected: the user's would be offered now, were it not replaced.
    for (let i = 0; i < 400; i++) {
      const listed = (await client.call('mcp.list', {}))['servers'] as { status: string }[]
      if (listed.length === 2 && listed.every((s) => s.status === 'connected')) break
      await Bun.sleep(25)
    }
    let prompt: Seen = { tools: [], system: '' }
    faux.faux.setResponses([
      ((context: { messages: Record<string, unknown>[] }) => {
        prompt = promptOf(context.messages)
        return fauxAssistantMessage('ok')
      }) as never,
    ])
    await client.call('conversation.submit', { id, input: 'hi' })
    await waitIdle(client, id)
    expect(prompt.tools.filter((tool) => tool.startsWith('fake'))).toEqual([
      'fake___echo',
      'fake___touch',
    ])
    expect(prompt.system).not.toContain('the user one')
    const servers = (await client.call('mcp.list', {}))['servers'] as {
      name: string
      app?: boolean
    }[]
    expect(servers.map((s) => [s.name, s.app === true])).toEqual([
      ['fake', false],
      ['fake', true],
    ])
  }, 60_000)
})

describe("the app's section", () => {
  test('is in every turn, and harness.configure changes it or takes it away', async () => {
    const { client, ask, seen } = await boot(
      app({ section: 'Brand section one.' }),
      [fauxAssistantMessage('a'), fauxAssistantMessage('b'), fauxAssistantMessage('c')],
      'all',
    )
    await ask('hi')
    expect(seen.at(-1)!.system).toContain('Brand section one.')
    await client.call('harness.configure', { section: 'Brand section two.' })
    await ask('again')
    expect(seen.at(-1)!.system).toContain('Brand section two.')
    expect(seen.at(-1)!.system).not.toContain('Brand section one.')
    await client.call('harness.configure', { section: null })
    await ask('and again')
    expect(seen.at(-1)!.system).not.toContain('Brand section')
  }, 60_000)
})
