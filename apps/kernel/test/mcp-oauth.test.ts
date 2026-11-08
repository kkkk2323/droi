import { afterEach, describe, expect, test } from 'bun:test'
import { mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import type { KernelHandle } from '../src/kernel.ts'

/** An HTTP MCP server that asks for OAuth: mcp.authorize, the redirect, the tokens, and a restart. */

const cleanups: (() => Promise<void> | void)[] = []
afterEach(async () => {
  for (const run of cleanups.reverse()) await run()
  cleanups.length = 0
})

const TOKEN = 'good-token'

/** Discovery, dynamic registration, an authorize page that redirects at once, a token endpoint, and /mcp. */
function oauthServer(): { base: string; tokenRequests: () => number } {
  let tokens = 0
  const server: ReturnType<typeof Bun.serve> = Bun.serve({
    port: 0,
    hostname: '127.0.0.1',
    async fetch(request: Request): Promise<Response> {
      const url = new URL(request.url)
      const base: string = `http://127.0.0.1:${server.port}`
      const json = (value: unknown, status = 200) =>
        new Response(JSON.stringify(value), {
          status,
          headers: { 'content-type': 'application/json' },
        })
      switch (url.pathname) {
        case '/.well-known/oauth-protected-resource':
        case '/.well-known/oauth-protected-resource/mcp':
          return json({ resource: `${base}/mcp`, authorization_servers: [base] })
        case '/.well-known/oauth-authorization-server':
          return json({
            issuer: base,
            authorization_endpoint: `${base}/authorize`,
            token_endpoint: `${base}/token`,
            registration_endpoint: `${base}/register`,
            response_types_supported: ['code'],
            grant_types_supported: ['authorization_code', 'refresh_token'],
            code_challenge_methods_supported: ['S256'],
            token_endpoint_auth_methods_supported: ['none'],
          })
        case '/register': {
          const body = (await request.json()) as Record<string, unknown>
          return json({ ...body, client_id: 'droi-client' }, 201)
        }
        case '/authorize': {
          const redirect = new URL(url.searchParams.get('redirect_uri')!)
          redirect.searchParams.set('code', 'the-code')
          redirect.searchParams.set('state', url.searchParams.get('state') ?? '')
          return Response.redirect(redirect.toString(), 302)
        }
        case '/token': {
          tokens++
          const form = new URLSearchParams(await request.text())
          if (form.get('grant_type') === 'authorization_code' && form.get('code') !== 'the-code') {
            return json({ error: 'invalid_grant' }, 400)
          }
          return json({
            access_token: TOKEN,
            token_type: 'Bearer',
            expires_in: 3600,
            refresh_token: 'r1',
          })
        }
        case '/mcp': {
          if (request.headers.get('authorization') !== `Bearer ${TOKEN}`) {
            return new Response('unauthorized', {
              status: 401,
              headers: {
                'www-authenticate': `Bearer resource_metadata="${base}/.well-known/oauth-protected-resource/mcp"`,
              },
            })
          }
          if (request.method === 'GET') return new Response(null, { status: 405 })
          if (request.method === 'DELETE') return new Response(null, { status: 200 })
          const message = (await request.json()) as {
            id?: unknown
            method: string
            params?: Record<string, unknown>
          }
          if (message.id === undefined) return new Response(null, { status: 202 })
          const answer = (result: unknown) => json({ jsonrpc: '2.0', id: message.id, result })
          if (message.method === 'initialize') {
            return answer({
              protocolVersion: message.params?.['protocolVersion'],
              capabilities: { tools: {} },
              serverInfo: { name: 'secure', version: '1' },
            })
          }
          if (message.method === 'tools/list') {
            return answer({ tools: [{ name: 'whoami', inputSchema: { type: 'object' } }] })
          }
          return answer({ content: [{ type: 'text', text: 'you' }] })
        }
      }
      return new Response('not found', { status: 404 })
    },
  })
  cleanups.push(() => server.stop(true))
  return { base: `http://127.0.0.1:${server.port}`, tokenRequests: () => tokens }
}

type Server = { name: string; status: string; tools: string[]; error: string | null }

async function boot(dir: string): Promise<{ kernel: KernelHandle; client: Client }> {
  const kernel = await start({ dir, faux: fauxCatalog() })
  const client = new Client(kernel.port)
  let stopped = false
  const stop = async () => {
    if (stopped) return
    stopped = true
    client.close()
    await kernel.stop()
  }
  cleanups.push(stop)
  return { kernel, client }
}

async function status(client: Client, name: string, wanted: string): Promise<Server> {
  const deadline = Date.now() + 20_000
  for (;;) {
    const found = ((await client.call('mcp.list', {}))['servers'] as Server[]).find(
      (s) => s.name === name,
    )
    if (found?.status === wanted) return found
    if (Date.now() > deadline) throw new Error(`${name} is ${JSON.stringify(found)}, not ${wanted}`)
    await Bun.sleep(25)
  }
}

describe('a server that asks for OAuth', () => {
  test('waits for a sign-in, connects after the redirect, and keeps its tokens across a restart', async () => {
    const made = tempData()
    cleanups.push(() => made.cleanup())
    const { base, tokenRequests } = oauthServer()
    mkdirSync(join(made.dir, 'user'), { recursive: true })
    writeFileSync(
      join(made.dir, 'user', 'mcp.json'),
      JSON.stringify({ mcpServers: { secure: { type: 'http', url: `${base}/mcp` } } }),
    )

    const first = await boot(made.dir)
    const waiting = await status(first.client, 'secure', 'failed')
    expect(waiting.error).toContain('authorization required')

    const answer = await first.client.call('mcp.authorize', { name: 'secure' })
    const page = String(answer['authorizationUrl'])
    expect(page.startsWith(`${base}/authorize?`)).toBe(true)
    // The browser: the authorize page redirects to the kernel's callback.
    expect((await fetch(page)).status).toBe(200)
    const connected = await status(first.client, 'secure', 'connected')
    expect(connected.tools).toEqual(['secure___whoami'])

    const stored = join(made.dir, 'mcp-auth.json')
    expect(readFileSync(stored, 'utf8')).toContain(TOKEN)
    if (process.platform !== 'win32') expect(statSync(stored).mode & 0o777).toBe(0o600)

    first.client.close()
    await first.kernel.stop()
    const before = tokenRequests()
    const second = await boot(made.dir)
    await status(second.client, 'secure', 'connected')
    expect(tokenRequests()).toBe(before)
  }, 60_000)

  test('a cancelled sign-in stops listening for the redirect', async () => {
    const made = tempData()
    cleanups.push(() => made.cleanup())
    const { base } = oauthServer()
    mkdirSync(join(made.dir, 'user'), { recursive: true })
    writeFileSync(
      join(made.dir, 'user', 'mcp.json'),
      JSON.stringify({ mcpServers: { secure: { type: 'http', url: `${base}/mcp` } } }),
    )
    const b = await boot(made.dir)
    await status(b.client, 'secure', 'failed')
    const page = new URL(
      String((await b.client.call('mcp.authorize', { name: 'secure' }))['authorizationUrl']),
    )
    const callback = page.searchParams.get('redirect_uri')!
    expect(await b.client.call('mcp.authorize.cancel', { name: 'secure' })).toEqual({})
    await Bun.sleep(100)
    await expect(fetch(`${callback}?code=x&state=y`)).rejects.toThrow()
    expect((await b.client.fails('mcp.authorize', { name: 'nope' })).code).toBe(-32602)
  }, 60_000)
})
