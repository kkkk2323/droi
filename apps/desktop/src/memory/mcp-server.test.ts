import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createInterface } from 'node:readline'
import type { ChildProcessWithoutNullStreams } from 'node:child_process'
import { openMemoryStore } from './store'
import { startTs } from './test-support/processes'

const ENTRY = fileURLToPath(new URL('../memory-server/index.ts', import.meta.url))

let root: string
let memoryDir: string
let workspace: string
let factoryHome: string
const SESSION = '622a994c-cfbe-483c-848b-fbcd93ee8055'
let server: ChildProcessWithoutNullStreams
let nextId = 1
const waiting = new Map<number, (message: Record<string, unknown>) => void>()

function request(method: string, params?: Record<string, unknown>) {
  const id = nextId++
  const answer = new Promise<Record<string, unknown>>((resolve) => waiting.set(id, resolve))
  server.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`)
  return answer
}

async function call(
  name: string,
  args: Record<string, unknown>,
  sessionId: string | null = SESSION,
) {
  const response = await request('tools/call', {
    name,
    arguments: args,
    ...(sessionId ? { _meta: { assemblySessionId: sessionId, caller: 'AGENT' } } : {}),
  })
  const result = response['result'] as { content: Array<{ text: string }>; isError?: boolean }
  return { text: result.content[0]!.text, isError: result.isError === true }
}

beforeEach(() => {
  root = realpathSync(mkdtempSync(join(tmpdir(), 'droi-memory-server-')))
  memoryDir = join(root, 'memory')
  workspace = join(root, 'workspace')
  mkdirSync(workspace)
  // The Daemon keeps a file per Session that says where it runs; the server's
  // own cwd is the Daemon's, not the Session's.
  factoryHome = join(root, 'factory')
  const folder = join(factoryHome, 'sessions', workspace.replaceAll('/', '-'))
  mkdirSync(folder, { recursive: true })
  writeFileSync(
    join(folder, `${SESSION}.jsonl`),
    `${JSON.stringify({ type: 'session_start', id: SESSION, title: 't', cwd: workspace })}\n`,
  )
  server = startTs(ENTRY, {
    cwd: root,
    env: { DROI_MEMORY_DIR: memoryDir, FACTORY_HOME_OVERRIDE: factoryHome },
  })
  createInterface({ input: server.stdout }).on('line', (line) => {
    const message = JSON.parse(line) as Record<string, unknown>
    waiting.get(message['id'] as number)?.(message)
  })
})

afterEach(async () => {
  server.stdin.end()
  await new Promise((resolve) => server.once('exit', resolve))
  rmSync(root, { recursive: true, force: true })
})

describe('Memory Server over stdio', () => {
  it('initializes and lists its five tools, the readers marked read-only', async () => {
    const init = await request('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'test', version: '1' },
    })
    expect(init['result']).toMatchObject({
      protocolVersion: '2025-06-18',
      capabilities: { tools: {} },
      serverInfo: { name: 'droi-memory' },
    })
    server.stdin.write(
      `${JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' })}\n`,
    )

    const listed = (await request('tools/list'))['result'] as {
      tools: Array<{
        name: string
        inputSchema: { required: string[] }
        annotations: { readOnlyHint: boolean }
      }>
    }
    expect(listed.tools.map((t) => t.name)).toEqual([
      'memory_search',
      'memory_list',
      'memory_add',
      'memory_replace',
      'memory_remove',
    ])
    const readOnly = Object.fromEntries(
      listed.tools.map((t) => [t.name, t.annotations.readOnlyHint]),
    )
    expect(readOnly).toEqual({
      memory_search: true,
      memory_list: true,
      memory_add: false,
      memory_replace: false,
      memory_remove: false,
    })
    expect(listed.tools.find((t) => t.name === 'memory_add')?.inputSchema.required).toEqual([
      'scope',
      'category',
      'text',
    ])
  })

  it('adds to the Workspace it runs in, then searches, replaces and removes', async () => {
    const added = await call('memory_add', {
      scope: 'project',
      category: 'convention',
      text: 'Run pnpm check before committing',
    })
    expect(added.isError).toBe(false)
    const id = /Saved ([0-9a-f]{8}) in Project Memory/.exec(added.text)?.[1]
    expect(id).toBeDefined()

    const store = openMemoryStore(memoryDir)
    expect(store.list({ scope: 'project', workspace }).map((e) => e.text)).toEqual([
      'Run pnpm check before committing',
    ])
    expect(store.hasWrite(SESSION)).toBe(true)
    expect(store.hasWrite('another-session')).toBe(false)
    store.close()

    const found = await call('memory_search', { query: 'pnpm' })
    expect(found.text).toContain(`${id} [project/convention`)
    expect((await call('memory_search', { query: 'pnpm', scope: 'global' })).text).toMatch(
      /^No entries/,
    )

    expect((await call('memory_replace', { id, text: 'Run pnpm check && pnpm test' })).text).toBe(
      `Replaced ${id} in Project Memory.`,
    )
    expect((await call('memory_list', { scope: 'project' })).text).toContain(
      'pnpm check && pnpm test',
    )
    expect((await call('memory_remove', { id })).text).toBe(`Removed ${id}.`)
    expect(await call('memory_remove', { id })).toMatchObject({ isError: true })
  })

  it('keeps Global Memory apart', async () => {
    await call('memory_add', { scope: 'global', category: 'preference', text: 'Answer in Chinese' })
    expect((await call('memory_list', { scope: 'global' })).text).toContain('[global/preference')
    expect((await call('memory_list', { scope: 'project' })).text).toMatch(/^No entries/)
  })

  it('reaches only Global Memory for a Session it cannot place', async () => {
    expect(
      await call(
        'memory_add',
        { scope: 'project', category: 'insight', text: 'x' },
        'unknown-session',
      ),
    ).toMatchObject({ isError: true, text: expect.stringMatching(/scope "global"/) })
    expect(
      await call(
        'memory_add',
        { scope: 'global', category: 'preference', text: 'terse answers' },
        null,
      ),
    ).toMatchObject({ isError: false })
  })

  it('gives a Memory Session no Memory of its own', async () => {
    const memorySession = '9b1f2c3d-0000-4000-8000-000000000001'
    const folder = join(factoryHome, 'sessions', workspace.replaceAll('/', '-'))
    writeFileSync(
      join(folder, `${memorySession}.jsonl`),
      `${JSON.stringify({ type: 'session_start', id: memorySession, cwd: workspace })}\n`,
    )
    writeFileSync(
      join(folder, `${memorySession}.settings.json`),
      JSON.stringify({ tags: [{ name: 'droi.memory' }] }),
    )
    for (const [name, args] of [
      ['memory_add', { scope: 'global', category: 'insight', text: 'x' }],
      ['memory_search', { query: 'anything' }],
    ] as const) {
      expect(await call(name, { ...args }, memorySession)).toMatchObject({
        isError: true,
        text: expect.stringMatching(/Memory Session/),
      })
    }
    expect((await call('memory_list', { scope: 'global' })).text).toMatch(/^No entries/)
  })

  it('gives a Scratch Session no Project Memory, only Global', async () => {
    const scratchSession = '9b1f2c3d-0000-4000-8000-000000000002'
    const folder = join(factoryHome, 'sessions', workspace.replaceAll('/', '-'))
    writeFileSync(
      join(folder, `${scratchSession}.jsonl`),
      `${JSON.stringify({ type: 'session_start', id: scratchSession, cwd: workspace })}\n`,
    )
    writeFileSync(
      join(folder, `${scratchSession}.settings.json`),
      JSON.stringify({ tags: [{ name: 'droi.scratch' }] }),
    )
    expect(
      await call(
        'memory_add',
        { scope: 'project', category: 'convention', text: 'x' },
        scratchSession,
      ),
    ).toMatchObject({ isError: true, text: expect.stringMatching(/no Project Memory.*"global"/) })
    expect(await call('memory_search', { query: 'x' }, scratchSession)).toMatchObject({
      isError: false,
      text: expect.stringMatching(/no Project Memory/),
    })
    expect(await call('memory_list', { scope: 'project' }, scratchSession)).toMatchObject({
      isError: false,
      text: expect.stringMatching(/no Project Memory/),
    })
    expect(
      await call(
        'memory_add',
        { scope: 'global', category: 'insight', text: 'win.myhome runs WSL2' },
        scratchSession,
      ),
    ).toMatchObject({ isError: false, text: expect.stringMatching(/in Global Memory/) })
    const store = openMemoryStore(memoryDir)
    expect(store.list({ scope: 'project', workspace })).toEqual([])
    store.close()
  })

  it('reports refusals as tool errors', async () => {
    expect(
      await call('memory_add', { scope: 'project', category: 'insight', text: 'password=hunter2' }),
    ).toMatchObject({ isError: true, text: expect.stringMatching(/password/) })
    expect(
      await call('memory_add', { scope: 'project', category: 'gossip', text: 'x' }),
    ).toMatchObject({
      isError: true,
    })
  })

  it('answers unknown methods with an error and ignores notifications', async () => {
    server.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', method: 'notifications/cancelled' })}\n`)
    expect(await request('resources/list')).toMatchObject({ error: { code: -32601 } })
    expect(await request('ping')).toMatchObject({ result: {} })
  })
})
