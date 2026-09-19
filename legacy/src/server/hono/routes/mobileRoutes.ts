import { Hono } from 'hono'
import type { ServerEnv } from '../types.ts'
import { getLanAddress } from '../../utils/network.ts'

function resolvePort(apiPort: number, pairingWebPort: number | undefined): number {
  const p = Number(pairingWebPort)
  return Number.isFinite(p) && p > 0 ? p : apiPort
}

export function createMobileRoutes() {
  const mobile = new Hono<ServerEnv>()

  mobile.post('/pair', (c) => {
    const deps = c.get('deps')
    const ip = getLanAddress()
    const port = resolvePort(deps.runtimePortRef.value, deps.opts.pairingWebPort)
    const connectUrl = `http://${ip}:${port}/`
    return c.json({ connectUrl })
  })

  return mobile
}
