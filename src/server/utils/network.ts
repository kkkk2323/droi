import type { IncomingHttpHeaders } from 'http'
import { networkInterfaces } from 'os'

export function isLoopbackAddress(addr: string | undefined | null): boolean {
  if (!addr) return false
  if (addr === '127.0.0.1' || addr === '::1') return true
  if (addr.startsWith('::ffff:') && addr.endsWith('127.0.0.1')) return true
  return false
}

function parseXForwardedFor(value: string | string[] | undefined): string | null {
  const raw = Array.isArray(value) ? value[0] : value
  if (!raw) return null
  const first = raw.split(',')[0]?.trim() || ''
  return first || null
}

export function getEffectiveRemoteAddress(params: {
  directAddress: string | undefined
  headers: IncomingHttpHeaders
}): string | undefined {
  const direct = params.directAddress
  if (!isLoopbackAddress(direct)) return direct
  const xff = parseXForwardedFor(params.headers['x-forwarded-for'])
  return xff || direct
}

export function getLanAddress(): string {
  const nets = networkInterfaces()
  for (const name of Object.keys(nets)) {
    const addrs = nets[name] || []
    for (const a of addrs) {
      if (a.family !== 'IPv4') continue
      if (a.internal) continue
      if (a.address.startsWith('169.254.')) continue
      return a.address
    }
  }
  return '127.0.0.1'
}
