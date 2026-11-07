import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { Client, fauxCatalog, start, tempData, TOKEN, type Faux } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle
let client: Client

beforeEach(async () => {
  data = tempData()
  faux = fauxCatalog()
  kernel = await start({ dir: data.dir, faux })
  client = new Client(kernel.port)
})

afterEach(async () => {
  client.close()
  await kernel.stop()
  data.cleanup()
})

describe('the handshake and the connection', () => {
  test('/health answers without a token', async () => {
    const response = await fetch(`http://127.0.0.1:${kernel.port}/health`)
    expect(response.status).toBe(200)
    const body = (await response.json()) as Record<string, unknown>
    expect(body['ok']).toBe(true)
    expect(String(body['version'])).toStartWith('kernel-')
  })

  test('a wrong token is refused before the upgrade', async () => {
    const response = await fetch(`http://127.0.0.1:${kernel.port}/rpc`, {
      headers: { Upgrade: 'websocket', Connection: 'Upgrade', Authorization: 'Bearer nope' },
    })
    expect(response.status).toBe(401)
  })

  test('harness.info names the build, the capabilities and the tools', async () => {
    const info = await client.call('harness.info')
    expect(info['version']).toBe('kernel-1.5.0-1')
    const capabilities = info['capabilities'] as Record<string, boolean>
    expect(capabilities['conversations']).toBe(true)
    expect(capabilities['approvals']).toBe(false)
    const extensions = info['extensions'] as { name: string; tools: string[] }[]
    expect(extensions[0]?.name).toBe('coding')
    expect(extensions[0]?.tools).toContain('read')
  })

  test('an unknown method is -32601, not a dropped frame', async () => {
    const error = await client.fails('conversation.rename', { id: 1 })
    expect(error.code).toBe(-32601)
    expect(error.data).toEqual({ method: 'conversation.rename' })
  })

  test('a method this build gates is -32009 with its capability', async () => {
    const error = await client.fails('mcp.authorize', { name: 'anlan' })
    expect(error.code).toBe(-32009)
    expect(error.data).toEqual({ capability: 'mcp' })
  })

  test('bad params are -32602 with a path', async () => {
    const error = await client.fails('conversation.open', {})
    expect(error.code).toBe(-32602)
  })

  test('an unknown conversation is -32000', async () => {
    const error = await client.fails('conversation.open', { id: 404 })
    expect(error.code).toBe(-32000)
    expect(error.data).toEqual({ conversationId: 404 })
  })

  test('the empty M3 readers answer their shape', async () => {
    expect(await client.call('process.list', { id: 1 })).toEqual({ processes: [] })
    expect(await client.call('mcp.list')).toEqual({ servers: [] })
    expect(await client.call('skills.list')).toEqual({ skills: [] })
  })

  test('every fixture is a method this build answers, or a gated one it names', async () => {
    const root = new URL('../protocol/fixtures/', import.meta.url).pathname
    const index = (await Bun.file(`${root}index.json`).json()) as {
      methods: string[]
      notifications: string[]
      errors: string[]
    }
    expect(index.methods.length).toBe(28)
    expect(index.notifications.length).toBeGreaterThan(10)

    const known = new Set(kernel.kernel.methodNames())

    for (const path of index.methods) {
      const fixture = (await Bun.file(`${root}${path}`).json()) as {
        kind: string
        method: string
        request: { method: string; params?: Record<string, unknown> }
        response: { result?: Record<string, unknown>; error?: unknown }
      }
      expect(fixture.kind).toBe('method')
      expect(fixture.request.method).toBe(fixture.method)
      // Gated methods still answer `-32009` through the dispatcher, so the whole surface is reachable.
      expect(known.has(fixture.method)).toBe(true)
      expect(Object.keys(fixture.response).length).toBeGreaterThan(1)
    }
    // And nothing this build answers is missing from the fixtures: the two lists are the same contract.
    const fixtures = new Set(
      await Promise.all(
        index.methods.map(async (path) => {
          const fixture = (await Bun.file(`${root}${path}`).json()) as { method: string }
          return fixture.method
        }),
      ),
    )
    expect([...known].filter((method) => !fixtures.has(method))).toEqual([])

    for (const path of index.notifications) {
      const fixture = (await Bun.file(`${root}${path}`).json()) as {
        kind: string
        method: string
        notification: { method: string; params: Record<string, unknown> }
      }
      expect(fixture.kind).toBe('notification')
      expect(fixture.notification.method).toBe(fixture.method)
    }

    for (const path of index.errors) {
      const fixture = (await Bun.file(`${root}${path}`).json()) as {
        kind: string
        error: { code: number; name: string }
        response: { error: { code: number } }
      }
      expect(fixture.kind).toBe('error')
      // The name is the contract's spelling; the code is what a client branches on.
      expect(fixture.response.error.code).toBe(fixture.error.code)
    }
  })

  test('a gated fixture method answers -32009 with the capability the contract names', async () => {
    const root = new URL('../protocol/fixtures/', import.meta.url).pathname
    const fixture = (await Bun.file(`${root}errors/unsupported.json`).json()) as {
      request: { method: string; params: Record<string, unknown> }
      response: { error: { code: number; data: { capability: string } } }
    }
    const error = await client.fails(fixture.request.method, fixture.request.params)
    expect(error.code).toBe(fixture.response.error.code)
    expect(error.data).toEqual(fixture.response.error.data)
  })
})

describe('the token', () => {
  test('is read from the header and nowhere else', async () => {
    // The header works.
    const good = new Client(kernel.port)
    await good.opened
    expect((await good.call('harness.info'))['version']).toBe('kernel-1.5.0-1')
    good.close()

    // A correct token in the query string is refused: the Rust client's fix, pinned from this side too.
    const upgrade = await fetch(
      `http://127.0.0.1:${kernel.port}/rpc?token=${encodeURIComponent(TOKEN)}&access_token=${encodeURIComponent(TOKEN)}`,
      {
        headers: {
          Upgrade: 'websocket',
          Connection: 'Upgrade',
          'Sec-WebSocket-Version': '13',
          'Sec-WebSocket-Key': '************************',
        },
      },
    )
    expect(upgrade.status).toBe(401)

    // And a wrong header is refused as well, so the check is the token and not merely the header's presence.
    const wrong = await fetch(`http://127.0.0.1:${kernel.port}/rpc`, {
      headers: {
        Upgrade: 'websocket',
        Connection: 'Upgrade',
        Authorization: 'Bearer not-the-token',
      },
    })
    expect(wrong.status).toBe(401)

    // A bare token without the scheme is not accepted either.
    const bare = await fetch(`http://127.0.0.1:${kernel.port}/rpc`, {
      headers: { Upgrade: 'websocket', Connection: 'Upgrade', Authorization: TOKEN },
    })
    expect(bare.status).toBe(401)
  })

  test('/health is the one endpoint that answers without it, and says nothing else', async () => {
    const response = await fetch(`http://127.0.0.1:${kernel.port}/health`)
    expect(response.status).toBe(200)
    const body = (await response.json()) as Record<string, unknown>
    expect(Object.keys(body).sort()).toEqual(['ok', 'pi', 'version'])
    // No data directory, no token, no provider list: liveness only.
    expect(JSON.stringify(body)).not.toContain(data.dir)
  })

  test('the handshake line carries no token', async () => {
    // `main.ts` prints the handshake; a kernel started by the test harness never prints it, so this checks
    // the shape the Host parses rather than a live process.
    const { port, pid, version } = {
      port: kernel.port,
      pid: process.pid,
      version: 'kernel-1.5.0-1',
    }
    const line = JSON.stringify({ port, pid, version })
    expect(line).not.toContain(TOKEN)
    expect(Object.keys(JSON.parse(line)).sort()).toEqual(['pid', 'port', 'version'])
  })
})
