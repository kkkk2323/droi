import { readFile, stat } from 'fs/promises'
import type { Hono } from 'hono'
import type { ServerEnv } from '../types.ts'
import { getContentType, safeJoin } from '../../utils/path.ts'

async function serveStaticFile(params: { webRootDir: string; reqPath: string }): Promise<Response> {
  const { webRootDir, reqPath } = params
  const filePath = safeJoin(webRootDir, reqPath)
  if (!filePath) return new Response('Bad Request', { status: 400 })

  const tryPath = async (path: string): Promise<Buffer | null> => {
    try {
      const s = await stat(path)
      if (!s.isFile()) return null
      return await readFile(path)
    } catch {
      return null
    }
  }

  const buf = await tryPath(filePath)
  if (buf) {
    const isAsset = reqPath.startsWith('/assets/')
    return new Response(buf, {
      status: 200,
      headers: {
        'Content-Type': getContentType(filePath),
        'Cache-Control': isAsset ? 'public, max-age=31536000, immutable' : 'no-cache',
      },
    })
  }

  const looksLikeFile = /\.[A-Za-z0-9]+$/.test(reqPath.split('/').pop() || '')
  if (reqPath.startsWith('/assets/') || looksLikeFile) {
    return new Response('Not Found', { status: 404 })
  }

  const indexPath = safeJoin(webRootDir, '/index.html')
  if (!indexPath) return new Response('Server misconfigured', { status: 500 })
  const indexBuf = await tryPath(indexPath)
  if (!indexBuf) return new Response('Not Found', { status: 404 })

  const rawHtml = indexBuf.toString('utf-8')
  const html = rawHtml.includes('<base ')
    ? rawHtml
    : rawHtml.replace('<head>', '<head><base href="/" />')

  return new Response(html, {
    status: 200,
    headers: {
      'Content-Type': 'text/html; charset=utf-8',
      'Cache-Control': 'no-cache',
    },
  })
}

export function registerStaticRoutes(app: Hono<ServerEnv>, webRootDir: string | null | undefined) {
  if (!webRootDir) return

  app.get('*', async (c) => {
    const reqPath = c.req.path
    if (reqPath.startsWith('/api/') || reqPath.startsWith('/mobile/')) return c.notFound()
    return await serveStaticFile({ webRootDir, reqPath })
  })
}
