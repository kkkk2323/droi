import { normalize, resolve, sep } from 'path'

export function safeJoin(rootDir: string, urlPath: string): string | null {
  const cleaned = urlPath.split('?')[0].split('#')[0]
  let decoded = cleaned
  try {
    decoded = decodeURIComponent(cleaned)
  } catch {
    return null
  }
  const normalized = normalize(decoded).replace(/^([/\\])+/, '')
  const absRoot = resolve(rootDir)
  const absPath = resolve(rootDir, normalized)
  if (!absPath.startsWith(`${absRoot}${sep}`) && absPath !== absRoot) return null
  return absPath
}

export function getContentType(filePath: string): string {
  const lower = filePath.toLowerCase()
  if (lower.endsWith('.html')) return 'text/html; charset=utf-8'
  if (lower.endsWith('.js')) return 'application/javascript; charset=utf-8'
  if (lower.endsWith('.css')) return 'text/css; charset=utf-8'
  if (lower.endsWith('.json')) return 'application/json; charset=utf-8'
  if (lower.endsWith('.svg')) return 'image/svg+xml'
  if (lower.endsWith('.png')) return 'image/png'
  if (lower.endsWith('.jpg') || lower.endsWith('.jpeg')) return 'image/jpeg'
  if (lower.endsWith('.ico')) return 'image/x-icon'
  if (lower.endsWith('.map')) return 'application/json; charset=utf-8'
  if (lower.endsWith('.woff')) return 'font/woff'
  if (lower.endsWith('.woff2')) return 'font/woff2'
  if (lower.endsWith('.ttf')) return 'font/ttf'
  if (lower.endsWith('.gif')) return 'image/gif'
  if (lower.endsWith('.webp')) return 'image/webp'
  if (lower.endsWith('.bmp')) return 'image/bmp'
  return 'application/octet-stream'
}

export function sanitizeFilename(filename: string): string {
  const base =
    String(filename || '')
      .split(/[\\/]/)
      .pop() || ''
  const clean = base
    .replace(/[^A-Za-z0-9._-]/g, '_')
    .replace(/_+/g, '_')
    .slice(0, 200)
  return clean || 'upload.bin'
}
