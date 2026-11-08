import { afterEach, describe, expect, test } from 'bun:test'
import { execFileSync } from 'node:child_process'
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { context } from '../src/session/service.ts'
import { setToolClass, toolClass } from '../src/policy/decide.ts'

/** MCP servers (design/m3-kernel.md §4): the global file's, a repository's after trust, and their tools. */

const serverPath = new URL('./mcp-server.ts', import.meta.url).pathname
const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

type Booted = { kernel: KernelHandle; client: Client; faux: Faux; dir: string }

function data(): string {
  const made = tempData()
  cleanups.push(() => made.cleanup())
  return made.dir
}

async function boot(dir: string): Promise<Booted> {
  const faux = fauxCatalog()
  const kernel = await start({ dir, faux })
  const client = new Client(kernel.port)
  let stopped = false
  const stop = async () => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  }
  cleanups.push(stop)
  return { kernel, client, faux, dir }
}

function fakeServer(marker?: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    type: 'stdio',
    command: process.execPath,
    args: ['run', serverPath],
    ...(marker === undefined ? {} : { env: { MARKER: marker } }),
    ...extra,
  }
}

function writeGlobal(dir: string, servers: Record<string, unknown>): void {
  mkdirSync(join(dir, 'user'), { recursive: true })
  writeFileSync(join(dir, 'user', 'mcp.json'), JSON.stringify({ mcpServers: servers }))
}

/** A git repository with a `.factory/mcp.json`, at its real path (the kernel compares real paths). */
function repository(servers: Record<string, unknown>): string {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'mcp-repo-')))
  cleanups.push(() => rmSync(root, { recursive: true, force: true }))
  execFileSync('git', ['init', '-q'], { cwd: root })
  mkdirSync(join(root, '.factory'), { recursive: true })
  writeFileSync(join(root, '.factory', 'mcp.json'), JSON.stringify({ mcpServers: servers }))
  return root
}

type Server = {
  name: string
  status: string
  tools: string[]
  repoRoot?: string
  trusted?: boolean
  fingerprint?: string
  command?: string
  args?: string[]
  error: string | null
}

async function servers(client: Client, repoRoot?: string): Promise<Server[]> {
  return (await client.call('mcp.list', repoRoot === undefined ? {} : { repoRoot }))[
    'servers'
  ] as Server[]
}

/** What the Allow button does: trust the config as `mcp.list` showed it. */
async function trust(client: Client, name: string, repoRoot: string): Promise<void> {
  const shown = (await servers(client, repoRoot)).find(
    (s) => s.name === name && s.repoRoot === repoRoot,
  )
  await client.call('mcp.set', { name, repoRoot, trust: true, fingerprint: shown?.fingerprint })
}

async function until<T>(read: () => Promise<T | undefined>, timeoutMs = 20_000): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await read()
    if (value !== undefined) return value
    if (Date.now() > deadline) throw new Error('timed out')
    await Bun.sleep(25)
  }
}

async function status(
  client: Client,
  name: string,
  wanted: string,
  repoRoot?: string,
): Promise<Server> {
  return until(async () => {
    const found = (await servers(client, repoRoot)).find(
      (s) =>
        s.name === name &&
        (repoRoot === undefined || s.repoRoot === repoRoot || s.repoRoot === undefined),
    )
    return found?.status === wanted ? found : undefined
  })
}

async function conversation(b: Booted, cwd: string): Promise<number> {
  return (await b.client.call('conversation.create', { agent: { model: b.faux.model, cwd } }))[
    'conversationId'
  ] as number
}

async function offered(b: Booted, id: number): Promise<string[]> {
  const conv = await b.kernel.harness.conversation(id as never, context)
  return (await conv!.agent(context)).tools.map((tool) => tool.name)
}

async function toolText(client: Client, id: number): Promise<string> {
  return until(async () => {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const result = (page['items'] as { kind: string; model?: unknown }[]).find(
      (e) => e.kind === 'pi.tool-result',
    )
    return result === undefined ? undefined : JSON.stringify(result.model)
  })
}

function pids(marker: string): number[] {
  if (!existsSync(marker)) return []
  return readFileSync(marker, 'utf8').split('\n').filter(Boolean).map(Number)
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

describe('a global server', () => {
  test('connects at start, its tools reach the model, and it ends with the kernel', async () => {
    const dir = data()
    const marker = join(dir, 'marker')
    writeGlobal(dir, { fake: fakeServer(marker) })
    const b = await boot(dir)
    const connected = await status(b.client, 'fake', 'connected')
    expect(connected.tools).toEqual(['fake___echo', 'fake___touch'])
    // The approval policy judges a tool its server marks read-only as a read, any other as a command.
    expect(toolClass('fake___echo')).toBe('read')
    expect(toolClass('fake___touch')).toBe('exec')

    const id = await conversation(b, process.cwd())
    b.faux.faux.setResponses([
      fauxAssistantMessage(fauxToolCall('fake___echo', { text: 'hi' }), { stopReason: 'toolUse' }),
      fauxAssistantMessage('done'),
    ])
    await b.client.call('conversation.submit', { id, input: 'say hi' })
    expect(await toolText(b.client, id)).toContain('echo: hi')
    expect(await offered(b, id)).toContain('fake___echo')

    const [pid] = pids(marker)
    expect(alive(pid!)).toBe(true)
    b.client.close()
    await b.kernel.stop()
    await until(async () => (alive(pid!) ? undefined : true), 10_000)
  }, 60_000)

  test('mcp.set adds, switches off and removes a server at once', async () => {
    const dir = data()
    const b = await boot(dir)
    expect(await servers(b.client)).toEqual([])
    await b.client.call('mcp.set', {
      name: 'fake',
      server: { transport: 'stdio', command: process.execPath, args: ['run', serverPath] },
    })
    await status(b.client, 'fake', 'connected')
    const changed = await b.client.waitFor(
      (n) =>
        n.method === 'mcp.changed' &&
        (n.params['servers'] as Server[]).some((s) => s.status === 'connected'),
    )
    expect(changed.params['servers']).toBeDefined()
    const file = JSON.parse(readFileSync(join(dir, 'user', 'mcp.json'), 'utf8')) as {
      mcpServers: Record<string, unknown>
    }
    expect(file.mcpServers['fake']).toMatchObject({ type: 'stdio', command: process.execPath })

    await b.client.call('mcp.set', { name: 'fake', enabled: false })
    await status(b.client, 'fake', 'disabled')
    expect(b.kernel.harness).toBeDefined()

    await b.client.call('mcp.set', { name: 'fake', remove: true })
    expect(await servers(b.client)).toEqual([])
    expect((await b.client.fails('mcp.set', { name: 'fake', enabled: true })).code).toBe(-32602)
  }, 60_000)

  test('a file that cannot be read is said, not silently empty', async () => {
    const dir = data()
    mkdirSync(join(dir, 'user'), { recursive: true })
    writeFileSync(join(dir, 'user', 'mcp.json'), '{ not json')
    const b = await boot(dir)
    const listed = await b.client.call('mcp.list', {})
    expect(listed['servers']).toEqual([])
    expect((listed['diagnostics'] as { path: string }[])[0]?.path).toBe(
      join(dir, 'user', 'mcp.json'),
    )
  }, 60_000)
})

describe("a repository's server", () => {
  test("does not run until trusted, and then only in that repository's conversations", async () => {
    const dir = data()
    const marker = join(dir, 'marker')
    const root = repository({ repo: fakeServer(marker) })
    const b = await boot(dir)
    const inRepo = await conversation(b, root)
    const elsewhere = await conversation(b, dir)
    b.faux.faux.setResponses([fauxAssistantMessage('ok')])
    await b.client.call('conversation.submit', { id: inRepo, input: 'hello' })

    const untrusted = await status(b.client, 'repo', 'untrusted', root)
    expect(untrusted).toMatchObject({
      repoRoot: root,
      trusted: false,
      command: process.execPath,
      args: ['run', serverPath],
    })
    await Bun.sleep(300)
    expect(pids(marker)).toEqual([])

    await trust(b.client, 'repo', root)
    await status(b.client, 'repo', 'connected', root)
    expect(pids(marker).length).toBe(1)
    expect(await offered(b, inRepo)).toContain('repo___echo')

    b.faux.faux.setResponses([fauxAssistantMessage('ok')])
    await b.client.call('conversation.submit', { id: elsewhere, input: 'hello' })
    expect(await offered(b, elsewhere)).not.toContain('repo___echo')
  }, 60_000)

  test('a changed config asks again, and trust: false disconnects', async () => {
    const dir = data()
    const marker = join(dir, 'marker')
    const root = repository({ repo: fakeServer(marker) })
    const first = await boot(dir)
    await servers(first.client, root)
    await trust(first.client, 'repo', root)
    await status(first.client, 'repo', 'connected', root)
    const [pid] = pids(marker)
    first.client.close()
    await first.kernel.stop()

    // The trust is kept across a restart.
    const second = await boot(dir)
    await status(second.client, 'repo', 'connected', root)

    await second.client.call('mcp.set', { name: 'repo', repoRoot: root, trust: false })
    await status(second.client, 'repo', 'untrusted', root)
    const [, next] = pids(marker)
    await until(async () => (alive(next!) ? undefined : true), 10_000)
    expect(alive(pid!)).toBe(false)

    await trust(second.client, 'repo', root)
    await status(second.client, 'repo', 'connected', root)
    writeFileSync(
      join(root, '.factory', 'mcp.json'),
      JSON.stringify({
        mcpServers: { repo: fakeServer(marker, { args: ['run', serverPath, '--changed'] }) },
      }),
    )
    const asked = await status(second.client, 'repo', 'untrusted', root)
    expect(asked.args).toEqual(['run', serverPath, '--changed'])
  }, 60_000)

  test('trust names the repository root, nothing under it', async () => {
    const dir = data()
    const root = repository({ repo: fakeServer() })
    mkdirSync(join(root, 'sub'))
    const b = await boot(dir)
    expect(
      (await b.client.fails('mcp.set', { name: 'repo', repoRoot: join(root, 'sub'), trust: true }))
        .code,
    ).toBe(-32602)
    expect(
      (
        await b.client.fails('mcp.set', {
          name: 'nope',
          repoRoot: root,
          trust: true,
          fingerprint: 'x',
        })
      ).code,
    ).toBe(-32602)
  }, 60_000)

  test('a repository can switch a global server off for itself', async () => {
    const dir = data()
    writeGlobal(dir, { fake: fakeServer() })
    const root = repository({ fake: { disabled: true } })
    const b = await boot(dir)
    await status(b.client, 'fake', 'connected')
    const inRepo = await conversation(b, root)
    const elsewhere = await conversation(b, dir)
    for (const id of [inRepo, elsewhere]) {
      b.faux.faux.setResponses([fauxAssistantMessage('ok')])
      await b.client.call('conversation.submit', { id, input: 'hello' })
    }
    expect(await offered(b, elsewhere)).toContain('fake___echo')
    expect(await offered(b, inRepo)).not.toContain('fake___echo')
  }, 60_000)
})

describe('trust and change', () => {
  test('an Allow names the config it was shown: a file changed since is asked again, and nothing runs', async () => {
    const dir = data()
    const shownMarker = join(dir, 'shown')
    const newMarker = join(dir, 'new')
    const root = repository({ repo: fakeServer(shownMarker) })
    const b = await boot(dir)
    const shown = await status(b.client, 'repo', 'untrusted', root)
    expect(shown.fingerprint).toMatch(/^[0-9a-f]{64}$/)
    writeFileSync(
      join(root, '.factory', 'mcp.json'),
      JSON.stringify({ mcpServers: { repo: fakeServer(newMarker) } }),
    )
    const refused = await b.client.fails('mcp.set', {
      name: 'repo',
      repoRoot: root,
      trust: true,
      fingerprint: shown.fingerprint,
    })
    expect(refused.code).toBe(-32602)
    expect(
      (await b.client.fails('mcp.set', { name: 'repo', repoRoot: root, trust: true })).code,
    ).toBe(-32602)
    await Bun.sleep(300)
    expect(pids(shownMarker)).toEqual([])
    expect(pids(newMarker)).toEqual([])
    expect((await status(b.client, 'repo', 'untrusted', root)).fingerprint).not.toBe(
      shown.fingerprint,
    )
  }, 60_000)

  test('two refreshes while the file changes never start a config nobody trusted', async () => {
    const dir = data()
    const trustedMarker = join(dir, 'trusted')
    const otherMarker = join(dir, 'other')
    const root = repository({ repo: { ...fakeServer(trustedMarker), disabled: true } })
    const b = await boot(dir)
    await trust(b.client, 'repo', root)
    const file = join(root, '.factory', 'mcp.json')
    writeFileSync(file, JSON.stringify({ mcpServers: { repo: fakeServer(trustedMarker) } }))
    const first = servers(b.client, root)
    writeFileSync(file, JSON.stringify({ mcpServers: { repo: fakeServer(otherMarker) } }))
    const second = servers(b.client, root)
    await Promise.all([first, second])
    await status(b.client, 'repo', 'untrusted', root)
    await Bun.sleep(300)
    expect(pids(otherMarker)).toEqual([])
  }, 60_000)

  test("a repository's entry takes the fields it does not set from the global one, and is asked about", async () => {
    const dir = data()
    const marker = join(dir, 'marker')
    writeGlobal(dir, { fake: fakeServer(marker) })
    const root = repository({ fake: { args: ['run', serverPath, '--repo'] } })
    const b = await boot(dir)
    await status(b.client, 'fake', 'connected')
    const asked = (await servers(b.client, root)).find((s) => s.repoRoot === root)!
    expect(asked).toMatchObject({
      status: 'untrusted',
      command: process.execPath,
      args: ['run', serverPath, '--repo'],
    })
    await trust(b.client, 'fake', root)
    await until(async () =>
      (await servers(b.client, root)).find((s) => s.repoRoot === root)?.status === 'connected'
        ? true
        : undefined,
    )
    expect(pids(marker).length).toBe(2)
  }, 60_000)
})

test('a tool name two servers register is a read only while both say it is', () => {
  setToolClass('same___tool', 'mcp-aaaa-same', 'read')
  expect(toolClass('same___tool')).toBe('read')
  setToolClass('same___tool', 'mcp-bbbb-same', 'exec')
  expect(toolClass('same___tool')).toBe('exec')
  setToolClass('same___tool', 'mcp-bbbb-same', undefined)
  expect(toolClass('same___tool')).toBe('read')
  setToolClass('same___tool', 'mcp-aaaa-same', undefined)
  expect(toolClass('same___tool')).toBe('exec')
})
