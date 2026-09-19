import { createReadStream, statSync } from 'node:fs'
import { extname, join, normalize, sep } from 'node:path'
import type { ServerResponse } from 'node:http'

const CONTENT_TYPES: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.webmanifest': 'application/manifest+json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
  '.woff': 'font/woff',
  '.map': 'application/json',
}

/**
 * Serve the built Client. Unknown paths fall back to index.html so Client-side
 * routes deep-link correctly; hashed assets get a long cache lifetime.
 */
export function serveStaticFile(dir: string, pathname: string, response: ServerResponse): void {
  const root = normalize(dir + sep)
  let filePath = normalize(join(dir, decodeURIComponent(pathname)))
  if (!filePath.startsWith(root)) {
    response.writeHead(403)
    response.end()
    return
  }
  if (!isFile(filePath)) filePath = join(dir, 'index.html')
  if (!isFile(filePath)) {
    response.writeHead(404)
    response.end()
    return
  }
  const extension = extname(filePath)
  response.writeHead(200, {
    'content-type': CONTENT_TYPES[extension] ?? 'application/octet-stream',
    'cache-control': filePath.includes(`${sep}assets${sep}`)
      ? 'public, max-age=31536000, immutable'
      : 'no-cache',
  })
  createReadStream(filePath).pipe(response)
}

function isFile(path: string): boolean {
  try {
    return statSync(path).isFile()
  } catch {
    return false
  }
}
