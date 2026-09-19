import { createServer, type IncomingHttpHeaders, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { Readable } from 'node:stream'
import type { KeyStoreAPI } from './keyStore.ts'

const DEFAULT_UPSTREAM_BASE_URL = 'https://api.factory.ai'

const ROTATING_FACTORY_API_ENDPOINTS = new Set([
  'POST /api/llm/a/v1/messages',
  'POST /api/llm/o/v1/responses',
  'POST /api/llm/o/v1/chat/completions',
  'POST /api/llm/g/v1/generate',
])

const HOP_BY_HOP_HEADERS = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
])

function isHopByHopHeader(name: string): boolean {
  return HOP_BY_HOP_HEADERS.has(name.toLowerCase())
}

function normalizeMethod(method: string | undefined): string {
  return (
    String(method || 'GET')
      .trim()
      .toUpperCase() || 'GET'
  )
}

function copyRequestHeaders(headers: IncomingHttpHeaders): Headers {
  const outgoing = new Headers()

  for (const [name, value] of Object.entries(headers)) {
    if (!value) continue
    if (isHopByHopHeader(name)) continue
    if (name.toLowerCase() === 'host') continue
    if (name.toLowerCase() === 'content-length') continue
    if (name.toLowerCase() === 'accept-encoding') continue

    if (Array.isArray(value)) {
      for (const item of value) outgoing.append(name, item)
      continue
    }

    outgoing.set(name, value)
  }

  outgoing.set('accept-encoding', 'identity')
  return outgoing
}

function readBearerToken(headers: Headers): string | null {
  const auth = String(headers.get('Authorization') || '').trim()
  if (!auth) return null
  const match = /^Bearer\s+(.+)$/i.exec(auth)
  return match?.[1]?.trim() || null
}

async function readRequestBody(req: NodeJS.ReadableStream): Promise<Buffer> {
  const chunks: Buffer[] = []
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk))
  }
  return Buffer.concat(chunks)
}

export function shouldRotateFactoryApiRequest(
  method: string | undefined,
  pathname: string,
): boolean {
  return ROTATING_FACTORY_API_ENDPOINTS.has(`${normalizeMethod(method)} ${pathname}`)
}

export interface FactoryApiProxyController {
  getBaseUrl: () => Promise<string>
  close: () => Promise<void>
}

export function createFactoryApiProxy(opts: {
  keyStore: KeyStoreAPI
  host?: string
  upstreamBaseUrl?: string
}): FactoryApiProxyController {
  const host = opts.host || '127.0.0.1'
  const upstreamBaseUrl = new URL(
    (
      opts.upstreamBaseUrl ||
      process.env['UPSTREAM_FACTORY_API_BASE_URL'] ||
      DEFAULT_UPSTREAM_BASE_URL
    ).trim(),
  )

  let server: Server | null = null
  let baseUrl = ''
  let startPromise: Promise<string> | null = null

  const ensureStarted = async (): Promise<string> => {
    if (baseUrl) return baseUrl
    if (startPromise) return startPromise

    startPromise = new Promise<string>((resolve, reject) => {
      const nextServer = createServer(async (req, res) => {
        let targetUrl: URL | null = null
        try {
          const incomingUrl = new URL(req.url || '/', 'http://local')
          const method = normalizeMethod(req.method)
          targetUrl = new URL(incomingUrl.pathname + incomingUrl.search, upstreamBaseUrl)
          const headers = copyRequestHeaders(req.headers)
          const shouldRotate = shouldRotateFactoryApiRequest(method, incomingUrl.pathname)

          if (shouldRotate) {
            try {
              const selectedKey = await opts.keyStore.resolveKeyForRequest(readBearerToken(headers))
              if (selectedKey.key) {
                headers.set('Authorization', `Bearer ${selectedKey.key}`)
                opts.keyStore.invalidateUsages()
              }
            } catch {
              // Preserve the original Authorization header on selection failures.
            }
          }

          const hasBody = method !== 'GET' && method !== 'HEAD'
          const body = hasBody ? await readRequestBody(req) : undefined
          const upstreamResponse = await fetch(targetUrl, {
            method,
            headers,
            body,
          })

          res.statusCode = upstreamResponse.status
          res.statusMessage = upstreamResponse.statusText || res.statusMessage

          upstreamResponse.headers.forEach((value, name) => {
            if (isHopByHopHeader(name)) return
            if (name.toLowerCase() === 'content-length') return
            if (name.toLowerCase() === 'content-encoding') return
            res.setHeader(name, value)
          })

          if (!upstreamResponse.body) {
            res.end()
            return
          }

          const responseBody = Readable.fromWeb(upstreamResponse.body)
          responseBody.on('error', () => {
            if (!res.destroyed) res.destroy()
          })
          responseBody.pipe(res)
        } catch (err) {
          // eslint-disable-next-line no-console
          console.error('Factory API proxy request failed', {
            method: req.method,
            path: req.url,
            targetUrl: targetUrl?.toString(),
            error: err instanceof Error ? err.message : String(err),
            cause:
              err instanceof Error && err.cause instanceof Error ? err.cause.message : undefined,
          })

          if (res.headersSent) {
            if (!res.destroyed) res.destroy(err instanceof Error ? err : undefined)
            return
          }

          res.statusCode = 502
          res.setHeader('Content-Type', 'text/plain; charset=utf-8')
          res.end(err instanceof Error ? err.message : String(err))
        }
      })

      const onError = (err: Error) => {
        if (server === nextServer) server = null
        startPromise = null
        reject(err)
      }

      nextServer.once('error', onError)

      nextServer.listen(0, host, () => {
        nextServer.off('error', onError)
        server = nextServer
        const address = nextServer.address() as AddressInfo | null
        const port = address?.port
        if (!port) {
          startPromise = null
          reject(new Error('Failed to determine Factory API proxy port'))
          return
        }
        baseUrl = `http://${host}:${port}`
        resolve(baseUrl)
      })
    })

    return startPromise
  }

  const close = async (): Promise<void> => {
    await startPromise?.catch(() => {})
    const runningServer = server
    server = null
    startPromise = null
    baseUrl = ''
    if (!runningServer) return
    await new Promise<void>((resolve, reject) => {
      runningServer.close((err) => {
        if (err) reject(err)
        else resolve()
      })
    })
  }

  return {
    getBaseUrl: ensureStarted,
    close,
  }
}
