import { describe, expect, test } from 'vitest'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { serveStaticFile } from './static-files'

async function request(dir: string, pathname: string): Promise<{ status: number; body: string }> {
  const server = createServer((req, res) => serveStaticFile(dir, req.url ?? '/', res))
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('no port')
  try {
    const response = await fetch(`http://127.0.0.1:${address.port}${pathname}`)
    return { status: response.status, body: await response.text() }
  } finally {
    server.close()
  }
}

describe('serveStaticFile', () => {
  const dir = mkdtempSync(join(tmpdir(), 'droi-static-'))
  writeFileSync(join(dir, 'index.html'), '<h1>Droi</h1>')

  test('unknown paths fall back to index.html', async () => {
    expect(await request(dir, '/sessions/abc')).toEqual({ status: 200, body: '<h1>Droi</h1>' })
  })

  test('a malformed percent-escape is a 400, not a crash', async () => {
    expect((await request(dir, '/%E0%A4%A')).status).toBe(400)
  })

  test('paths escaping the directory are refused', async () => {
    expect((await request(dir, '/..%2f..%2fetc/passwd')).status).toBe(403)
    rmSync(dir, { recursive: true, force: true })
  })
})
