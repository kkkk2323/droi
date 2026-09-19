import type { Context } from 'hono'

export function isAllowedOrigin(origin: string | null): string | null {
  if (!origin) return null
  const allowEnv = (process.env['DROID_APP_CORS_ORIGINS'] || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  if (allowEnv.length) return allowEnv.includes(origin) ? origin : null
  if (/^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(origin)) return origin
  return null
}

export function applyCorsHeaders(c: Context): void {
  const origin = isAllowedOrigin(c.req.header('origin') || null)
  if (origin) {
    c.header('Access-Control-Allow-Origin', origin)
    c.header('Vary', 'Origin')
  }
  c.header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
  c.header('Access-Control-Allow-Headers', 'Content-Type, Authorization')
}

export function jsonError(c: Context, status: number, message: string) {
  return c.json({ error: message }, status as any)
}

export async function readJsonBody<T = any>(c: Context): Promise<T> {
  const text = await c.req.text()
  if (!text.trim()) return {} as T
  return JSON.parse(text) as T
}
