import { afterEach, describe, expect, test } from 'vitest'
import { createDaemonConnection, type DaemonConnection } from './connection'

const LAN = 'http://192.168.5.123:41417'
const RELAY = 'https://relay.example.com/c/abcdefghijklmnop'

type Answer = number | 'down' | 'hang'

/** A fetch that answers each address's pairing check as told, and records what was asked. */
function gateways(answers: Record<string, Answer>) {
  const asked: string[] = []
  const impl = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    asked.push(url)
    const base = Object.keys(answers).find((b) => url.startsWith(`${b}/`))
    const answer = base ? answers[base] : 'down'
    if (answer === 'hang') {
      return new Promise<Response>((_, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
      })
    }
    if (answer === 'down' || answer === undefined) {
      return Promise.reject(new TypeError('Network request failed'))
    }
    return Promise.resolve(new Response(null, { status: answer }))
  }) as typeof fetch
  return { impl, asked }
}

let open: DaemonConnection | null = null
afterEach(() => {
  open?.dispose()
  open = null
})

function connectTo(relayUrl: string | null, impl: typeof fetch): DaemonConnection {
  open = createDaemonConnection(
    { kind: 'remote', gatewayUrl: LAN, pairingToken: 'tok', relayUrl },
    impl,
  )
  open.start()
  return open
}

describe('a computer with a Relay', () => {
  test('is reached on its network when the network answers, and the Relay is not asked', async () => {
    const { impl, asked } = gateways({ [LAN]: 204, [RELAY]: 204 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => asked.length).toBeGreaterThan(0)
    await new Promise((r) => setTimeout(r, 50))
    expect(c.gatewayUrl()).toBe(LAN)
    expect(asked.every((u) => u.startsWith(`${LAN}/daemon?token=tok`))).toBe(true)
  })

  test('is reached through the Relay when its network address is down', async () => {
    const { impl, asked } = gateways({ [LAN]: 'down', [RELAY]: 204 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => c.gatewayUrl()).toBe(RELAY)
    expect(asked).toContain(`${RELAY}/daemon?token=tok`)
  })

  test('is reached through the Relay when its network address does not answer in time', async () => {
    const { impl } = gateways({ [LAN]: 'hang', [RELAY]: 204 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => c.gatewayUrl(), { timeout: 5_000 }).toBe(RELAY)
  })

  test('is no longer paired when the Relay refuses the token too', async () => {
    const { impl } = gateways({ [LAN]: 'down', [RELAY]: 401 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => c.getState()).toEqual({ status: 'unpaired', reason: 'rejected-token' })
  })

  test('another computer at the network address sends it through the Relay', async () => {
    const { impl } = gateways({ [LAN]: 401, [RELAY]: 204 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => c.gatewayUrl()).toBe(RELAY)
  })
})

test('a computer without a Relay keeps its network address', async () => {
  const { impl, asked } = gateways({ [LAN]: 'down' })
  const c = connectTo(null, impl)
  await expect.poll(() => c.getState().status).toBe('unreachable')
  expect(c.gatewayUrl()).toBe(LAN)
  expect(asked.every((u) => u.startsWith(LAN))).toBe(true)
})
