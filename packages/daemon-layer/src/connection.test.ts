import { afterEach, describe, expect, test } from 'vitest'
import { createKernelConnection, NotConnectedError, type KernelConnection } from './connection'
import type { KernelSocket, Notification } from './kernel-socket'

const LAN = 'http://192.168.5.123:41417'
const RELAY = 'https://relay.example.com/c/abcdefghijklmnop'

type Answer = number | 'down' | 'hang'

/** A fetch that answers each address's pairing check as told, and records what was asked. */
function gateways(answers: Record<string, Answer>, meta: { kernel?: boolean } = { kernel: true }) {
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
    if (url.includes('/meta')) return Promise.resolve(Response.json(meta))
    return Promise.resolve(new Response(null, { status: answer }))
  }) as typeof fetch
  return { impl, asked }
}

/** Sockets the test opens and closes by hand. */
function sockets(opens = true) {
  const opened: Array<{
    url: string
    socket: KernelSocket
    close: () => void
    notify: (note: Notification) => void
    calls: string[]
  }> = []
  const open = async (
    url: string,
    handlers: { onNotification(note: Notification): void; onClose(): void },
  ): Promise<KernelSocket> => {
    if (!opens) throw new Error('The computer refused the connection.')
    let closed = false
    const calls: string[] = []
    const close = () => {
      if (closed) return
      closed = true
      handlers.onClose()
    }
    const socket: KernelSocket = {
      get closed() {
        return closed
      },
      call: async <T>(method: string) => {
        calls.push(method)
        return { method } as T
      },
      close,
    }
    opened.push({ url, socket, close, notify: handlers.onNotification, calls })
    return socket
  }
  return { open, opened }
}

let live: KernelConnection | null = null
afterEach(() => {
  live?.dispose()
  live = null
})

function connectTo(
  relayUrl: string | null,
  impl: typeof fetch,
  open = sockets().open,
): KernelConnection {
  live = createKernelConnection(
    { kind: 'remote', gatewayUrl: LAN, pairingToken: 'tok', relayUrl },
    impl,
    open,
  )
  live.start()
  return live
}

describe('a computer with a Relay', () => {
  test('is reached on its network when the network answers, and the Relay is not asked', async () => {
    const { impl, asked } = gateways({ [LAN]: 204, [RELAY]: 204 })
    const c = connectTo(RELAY, impl)
    await expect.poll(() => c.getState().status).toBe('connected')
    expect(c.gatewayUrl()).toBe(LAN)
    expect(asked.every((u) => u.startsWith(`${LAN}/kernel?token=tok`))).toBe(true)
  })

  test('is reached through the Relay when its network address is down', async () => {
    const { impl, asked } = gateways({ [LAN]: 'down', [RELAY]: 204 })
    const { open, opened } = sockets()
    const c = connectTo(RELAY, impl, open)
    await expect.poll(() => c.gatewayUrl()).toBe(RELAY)
    expect(asked).toContain(`${RELAY}/kernel?token=tok`)
    await expect
      .poll(() => opened[0]?.url)
      .toBe(`wss://relay.example.com/c/abcdefghijklmnop/kernel?token=tok`)
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

test('a Droi older than the Kernel’s path says it runs no Kernel', async () => {
  const { impl } = gateways({ [LAN]: 404 })
  const c = connectTo(null, impl)
  await expect.poll(() => c.getState()).toEqual({ status: 'no-kernel' })
})

test('a Gateway that refuses the socket while its Droi runs only the Daemon says so', async () => {
  const { impl } = gateways({ [LAN]: 204 }, { kernel: false })
  const c = connectTo(null, impl, sockets(false).open)
  await expect.poll(() => c.getState()).toEqual({ status: 'no-kernel' })
})

describe('the socket', () => {
  test('a request made before it opens waits for it; notifications reach every listener', async () => {
    const { impl } = gateways({ [LAN]: 204 })
    const { open, opened } = sockets()
    live = createKernelConnection(
      { kind: 'local', gatewayUrl: LAN, pairingToken: 'tok' },
      impl,
      open,
    )
    const heard: string[] = []
    live.onNotification((note) => heard.push(note.method))
    const answer = live.call<{ method: string }>('harness.info')
    live.start()
    await expect(answer).resolves.toEqual({ method: 'harness.info' })
    opened[0]!.notify({ method: 'conversation.busy', params: {} })
    expect(heard).toEqual(['conversation.busy'])
    expect(live.generation()).toBe(1)
  })

  test('a socket that closes is replaced, and the new one counts as a new generation', async () => {
    const { impl } = gateways({ [LAN]: 204 })
    const { open, opened } = sockets()
    const c = connectTo(null, impl, open)
    await expect.poll(() => c.getState().status).toBe('connected')
    opened[0]!.close()
    await expect.poll(() => opened.length).toBe(2)
    await expect.poll(() => c.getState().status).toBe('connected')
    expect(c.generation()).toBe(2)
  })

  test('a socket that drops while the computer is away keeps reconnecting until it is back', async () => {
    const answers: Record<string, Answer> = { [LAN]: 204 }
    const { impl } = gateways(answers)
    const { open, opened } = sockets()
    const c = connectTo(null, impl, open)
    await expect.poll(() => c.getState().status).toBe('connected')
    answers[LAN] = 'down'
    opened[0]!.close()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(c.getState().status).toBe('reconnecting')
    answers[LAN] = 204
    await expect.poll(() => c.getState().status, { timeout: 5_000 }).toBe('connected')
    expect(opened).toHaveLength(2)
  })

  test('resumed while the computer is away, it says it cannot reach it', async () => {
    const answers: Record<string, Answer> = { [LAN]: 204 }
    const { impl } = gateways(answers)
    const c = connectTo(null, impl)
    await expect.poll(() => c.getState().status).toBe('connected')
    c.suspend()
    answers[LAN] = 'down'
    c.resume()
    await expect.poll(() => c.getState().status).toBe('unreachable')
  })

  test('suspended, it lets the socket go; resumed, it opens another', async () => {
    const { impl } = gateways({ [LAN]: 204 })
    const { open, opened } = sockets()
    const c = connectTo(null, impl, open)
    await expect.poll(() => c.getState().status).toBe('connected')
    c.suspend()
    expect(c.getState().status).toBe('reconnecting')
    expect(opened[0]!.socket.closed).toBe(true)
    c.resume()
    await expect.poll(() => c.getState().status).toBe('connected')
    expect(opened).toHaveLength(2)
  })

  test('a request made while disposed fails as not connected', async () => {
    const { impl } = gateways({ [LAN]: 'down' })
    const c = connectTo(null, impl)
    const pending = c.call('harness.info')
    c.dispose()
    await expect(pending).rejects.toBeInstanceOf(NotConnectedError)
  })
})
