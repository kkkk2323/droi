import http from 'http'
import { Readable } from 'stream'
import { URL } from 'url'
import type { KeyStoreAPI } from '../keys/keyStore.ts'

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
])

const DEFAULT_TARGET = 'https://api.factory.ai'
const DEFAULT_ROTATE_PREFIX = '/api/llm/'

export interface KeyRotationProxy {
  port: number
  close: () => Promise<void>
}

export async function startKeyRotationProxy(opts: {
  keyStore: KeyStoreAPI
  targetBaseUrl?: string
  rotatePathPrefix?: string
}): Promise<KeyRotationProxy> {
  const target = new URL(opts.targetBaseUrl || DEFAULT_TARGET)
  const rotatePrefix = opts.rotatePathPrefix || DEFAULT_ROTATE_PREFIX
  const keyStore = opts.keyStore

  const server = http.createServer(async (req, res) => {
    const method = req.method || 'GET'
    try {
      const inUrl = new URL(req.url || '/', 'http://local')
      const upUrl = new URL(inUrl.pathname + inUrl.search, target)
      const shouldRotate = inUrl.pathname.startsWith(rotatePrefix)

      const hdrs = new Headers()
      for (const [name, value] of Object.entries(req.headers)) {
        if (!value) continue
        const lower = name.toLowerCase()
        if (HOP_BY_HOP.has(lower) || lower === 'host' || lower === 'content-length') continue
        if (lower === 'accept-encoding') continue
        if (Array.isArray(value)) {
          for (const v of value) hdrs.append(name, v)
        } else {
          hdrs.set(name, value)
        }
      }

      if (shouldRotate) {
        const existingAuth = hdrs.get('Authorization') || ''
        const isFactoryKey =
          !existingAuth || existingAuth.includes('factorykey-') || existingAuth.includes('fk-')
        if (isFactoryKey) {
          const key = await keyStore.getActiveKey()
          if (key) {
            hdrs.set('Authorization', `Bearer ${key}`)
            console.log(`[key-rotation-proxy] ${method} ${inUrl.pathname}`)
          }
        }
      }

      const hasBody = !['GET', 'HEAD'].includes(method.toUpperCase())

      // Buffer body to avoid stream issues with Node fetch
      let body: Buffer | undefined
      if (hasBody) {
        const chunks: Buffer[] = []
        for await (const chunk of req) chunks.push(typeof chunk === 'string' ? Buffer.from(chunk) : chunk)
        body = Buffer.concat(chunks)
      }

      const upRes = await fetch(upUrl, {
        method,
        headers: hdrs,
        body,
      })

      res.statusCode = upRes.status
      if (upRes.statusText) res.statusMessage = upRes.statusText

      upRes.headers.forEach((value, name) => {
        const lower = name.toLowerCase()
        if (HOP_BY_HOP.has(lower) || lower === 'content-length' || lower === 'content-encoding')
          return
        res.setHeader(name, value)
      })

      if (!upRes.body) {
        res.end()
        return
      }

      Readable.fromWeb(upRes.body as any).pipe(res)
    } catch (err) {
      console.error(`[key-rotation-proxy] ${method} ${req.url} error:`, err)
      if (!res.headersSent) {
        res.statusCode = 502
        res.setHeader('Content-Type', 'text/plain')
      }
      res.end('Bad Gateway')
    }
  })

  const port = await new Promise<number>((resolve, reject) => {
    const onError = (err: unknown) => {
      server.off('error', onError)
      reject(err)
    }
    server.once('error', onError)
    server.listen(0, '127.0.0.1', () => {
      server.off('error', onError)
      const addr = server.address()
      resolve(typeof addr === 'object' && addr ? addr.port : 0)
    })
  })

  return {
    port,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  }
}
