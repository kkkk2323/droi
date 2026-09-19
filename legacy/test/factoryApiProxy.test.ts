import test from 'node:test'
import assert from 'node:assert/strict'
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import { brotliCompressSync } from 'node:zlib'
import { createFactoryApiProxy } from '../src/backend/keys/factoryApiProxy.ts'
import type { KeyStoreAPI } from '../src/backend/keys/keyStore.ts'

function getRequestBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = []
    req.on('data', (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)))
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    req.on('error', reject)
  })
}

async function startUpstreamServer(
  handler: (req: IncomingMessage, res: ServerResponse) => Promise<void> | void,
): Promise<{ url: string; close: () => Promise<void> }> {
  const server = createServer((req, res) => {
    void Promise.resolve(handler(req, res)).catch((err) => {
      res.statusCode = 500
      res.end(err instanceof Error ? err.message : String(err))
    })
  })

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()))
  const address = server.address() as AddressInfo

  return {
    url: `http://127.0.0.1:${address.port}`,
    close: async () => {
      await new Promise<void>((resolve, reject) => {
        server.close((err) => {
          if (err) reject(err)
          else resolve()
        })
      })
    },
  }
}

test('Factory API proxy rewrites only the documented LLM inference headers', async (t) => {
  const seen: Array<{ path: string; authorization: string; sessionId: string; body: string }> = []
  const upstream = await startUpstreamServer(async (req, res) => {
    seen.push({
      path: req.url || '',
      authorization: String(req.headers.authorization || ''),
      sessionId: String(req.headers['x-session-id'] || ''),
      body: await getRequestBody(req),
    })
    res.setHeader('Content-Type', 'application/json')
    res.end(JSON.stringify({ ok: true }))
  })

  let invalidations = 0
  const keyStore: KeyStoreAPI = {
    getKeys: async () => [],
    addKeys: async () => ({ added: 0, duplicates: 0 }),
    removeKey: async () => {},
    updateNote: async () => {},
    getUsages: async () => new Map(),
    refreshUsages: async () => new Map(),
    invalidateUsages: () => {
      invalidations += 1
    },
    getActiveKey: async () => 'fk-rotated',
    getBoundKey: async () => null,
    bindSessionKey: async () => {},
    moveSessionBinding: async () => {},
    deleteSessionBinding: async () => {},
    rebindSessionsUsingKey: async () => {},
    resolveKeyForRequest: async () => ({ key: 'fk-rotated' }),
  }

  const proxy = createFactoryApiProxy({ keyStore, upstreamBaseUrl: upstream.url })
  t.after(async () => {
    await proxy.close()
    await upstream.close()
  })
  const proxyBaseUrl = await proxy.getBaseUrl()

  const rotatedResponse = await fetch(`${proxyBaseUrl}/api/llm/o/v1/responses`, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer fk-original',
      'Content-Type': 'application/json',
      'x-session-id': 'session-1',
    },
    body: JSON.stringify({ model: 'gpt-5.4' }),
  })

  assert.equal(rotatedResponse.status, 200)

  const passthroughResponse = await fetch(`${proxyBaseUrl}/api/llm/failed-requests`, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer fk-original',
      'Content-Type': 'application/json',
      'x-session-id': 'session-2',
    },
    body: JSON.stringify({ reason: 'noop' }),
  })

  assert.equal(passthroughResponse.status, 200)
  assert.equal(seen.length, 2)
  assert.deepEqual(seen[0], {
    path: '/api/llm/o/v1/responses',
    authorization: 'Bearer fk-rotated',
    sessionId: 'session-1',
    body: JSON.stringify({ model: 'gpt-5.4' }),
  })
  assert.deepEqual(seen[1], {
    path: '/api/llm/failed-requests',
    authorization: 'Bearer fk-original',
    sessionId: 'session-2',
    body: JSON.stringify({ reason: 'noop' }),
  })
  assert.equal(invalidations, 1)
})

test('Factory API proxy preserves a sticky request key until keyStore asks to rebind', async (t) => {
  const seen: string[] = []
  const upstream = await startUpstreamServer(async (req, res) => {
    seen.push(String(req.headers.authorization || ''))
    res.setHeader('Content-Type', 'application/json')
    res.end(JSON.stringify({ ok: true }))
  })

  const presented: Array<string | null> = []
  const keyStore: KeyStoreAPI = {
    getKeys: async () => [],
    addKeys: async () => ({ added: 0, duplicates: 0 }),
    removeKey: async () => {},
    updateNote: async () => {},
    getUsages: async () => new Map(),
    refreshUsages: async () => new Map(),
    invalidateUsages: () => {},
    getActiveKey: async () => 'fk-new',
    getBoundKey: async () => null,
    bindSessionKey: async () => {},
    moveSessionBinding: async () => {},
    deleteSessionBinding: async () => {},
    rebindSessionsUsingKey: async () => {},
    resolveKeyForRequest: async (currentKey) => {
      presented.push(currentKey || null)
      if (currentKey === 'fk-sticky') return { key: 'fk-sticky' }
      return { key: 'fk-new', reboundFrom: currentKey || undefined }
    },
  }

  const proxy = createFactoryApiProxy({ keyStore, upstreamBaseUrl: upstream.url })
  t.after(async () => {
    await proxy.close().catch(() => {})
    await upstream.close().catch(() => {})
  })

  const proxyBaseUrl = await proxy.getBaseUrl()
  const stickyRes = await fetch(`${proxyBaseUrl}/api/llm/o/v1/responses`, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer fk-sticky',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ model: 'gpt-5.4' }),
  })
  assert.equal(stickyRes.status, 200)

  const reboundRes = await fetch(`${proxyBaseUrl}/api/llm/o/v1/responses`, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer fk-old',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ model: 'gpt-5.4' }),
  })
  assert.equal(reboundRes.status, 200)

  assert.deepEqual(presented, ['fk-sticky', 'fk-old'])
  assert.deepEqual(seen, ['Bearer fk-sticky', 'Bearer fk-new'])
})

test('Factory API proxy ignores FACTORY_API_BASE_URL when resolving the upstream target', async (t) => {
  const upstream = await startUpstreamServer((_req, res) => {
    res.statusCode = 204
    res.end()
  })

  const prevProxyBase = process.env['FACTORY_API_BASE_URL']
  const prevUpstreamBase = process.env['UPSTREAM_FACTORY_API_BASE_URL']
  process.env['FACTORY_API_BASE_URL'] = 'http://127.0.0.1:9'
  process.env['UPSTREAM_FACTORY_API_BASE_URL'] = upstream.url

  const keyStore: KeyStoreAPI = {
    getKeys: async () => [],
    addKeys: async () => ({ added: 0, duplicates: 0 }),
    removeKey: async () => {},
    updateNote: async () => {},
    getUsages: async () => new Map(),
    refreshUsages: async () => new Map(),
    invalidateUsages: () => {},
    getActiveKey: async () => null,
    getBoundKey: async () => null,
    bindSessionKey: async () => {},
    moveSessionBinding: async () => {},
    deleteSessionBinding: async () => {},
    rebindSessionsUsingKey: async () => {},
    resolveKeyForRequest: async () => ({ key: null }),
  }

  const proxy = createFactoryApiProxy({ keyStore })
  t.after(async () => {
    await proxy.close().catch(() => {})
    await upstream.close().catch(() => {})
    if (prevProxyBase == null) delete process.env['FACTORY_API_BASE_URL']
    else process.env['FACTORY_API_BASE_URL'] = prevProxyBase
    if (prevUpstreamBase == null) delete process.env['UPSTREAM_FACTORY_API_BASE_URL']
    else process.env['UPSTREAM_FACTORY_API_BASE_URL'] = prevUpstreamBase
  })

  const proxyBaseUrl = await proxy.getBaseUrl()
  const response = await fetch(`${proxyBaseUrl}/api/cli/whoami`)
  assert.equal(response.status, 204)
})

test('Factory API proxy strips compression headers to avoid double decompression', async (t) => {
  const seenRequestHeaders: Array<{ acceptEncoding: string }> = []
  const upstream = await startUpstreamServer((_req, res) => {
    seenRequestHeaders.push({
      acceptEncoding: String(_req.headers['accept-encoding'] || ''),
    })
    const body = brotliCompressSync(Buffer.from(JSON.stringify({ ok: true }), 'utf8'))
    res.setHeader('Content-Type', 'application/json')
    res.setHeader('Content-Encoding', 'br')
    res.end(body)
  })

  const keyStore: KeyStoreAPI = {
    getKeys: async () => [],
    addKeys: async () => ({ added: 0, duplicates: 0 }),
    removeKey: async () => {},
    updateNote: async () => {},
    getUsages: async () => new Map(),
    refreshUsages: async () => new Map(),
    invalidateUsages: () => {},
    getActiveKey: async () => null,
    getBoundKey: async () => null,
    bindSessionKey: async () => {},
    moveSessionBinding: async () => {},
    deleteSessionBinding: async () => {},
    rebindSessionsUsingKey: async () => {},
    resolveKeyForRequest: async () => ({ key: null }),
  }

  const proxy = createFactoryApiProxy({ keyStore, upstreamBaseUrl: upstream.url })
  t.after(async () => {
    await proxy.close().catch(() => {})
    await upstream.close().catch(() => {})
  })

  const proxyBaseUrl = await proxy.getBaseUrl()
  const response = await fetch(`${proxyBaseUrl}/api/cli/whoami`, {
    headers: { 'Accept-Encoding': 'gzip, deflate, br' },
  })

  assert.equal(response.status, 200)
  assert.equal(response.headers.get('content-encoding'), null)
  assert.equal(await response.text(), JSON.stringify({ ok: true }))
  assert.equal(seenRequestHeaders.length, 1)
  assert.equal(seenRequestHeaders[0].acceptEncoding, 'identity')
})
