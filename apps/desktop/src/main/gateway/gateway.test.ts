import { afterEach, beforeEach, describe, expect, test } from 'vitest'
import { WebSocket, WebSocketServer, type RawData } from 'ws'
import { once } from 'node:events'
import {
  gatewayDaemonUrl,
  gatewayPairingCheckUrl,
  GATEWAY_API_KEY_PLACEHOLDER,
} from '@droi/daemon-layer/gateway'
import { lanInterfaceAddresses, startGateway, type Gateway } from './gateway'

const TOKEN = 'correct-pairing-token'
const LOCAL_TOKEN = 'local-window-token'
const API_KEY = 'fk-secret-key'
const META = {
  app: 'Droi',
  version: '1.2.3',
  name: 'Studio Mac',
  computerId: 'c0ffee00-0000-4000-8000-000000000000',
} as const

interface StandInDaemon {
  url: string
  received: string[]
  sockets: WebSocket[]
  close(): Promise<void>
}

async function startStandInDaemon(): Promise<StandInDaemon> {
  const wss = new WebSocketServer({ host: '127.0.0.1', port: 0 })
  await once(wss, 'listening')
  const received: string[] = []
  const sockets: WebSocket[] = []
  wss.on('connection', (socket) => {
    sockets.push(socket)
    socket.on('message', (data: RawData) => {
      const text = data.toString()
      received.push(text)
      // Echo back so tests can check the daemon -> client direction.
      socket.send(`echo:${text}`)
    })
  })
  const address = wss.address()
  if (!address || typeof address === 'string') throw new Error('unexpected unix socket')
  return {
    url: `ws://127.0.0.1:${address.port}`,
    received,
    sockets,
    close: () =>
      new Promise((resolve) => {
        for (const s of wss.clients) s.terminate()
        wss.close(() => resolve())
      }),
  }
}

async function nextMessage(socket: WebSocket): Promise<string> {
  const [data] = (await once(socket, 'message')) as [RawData]
  return data.toString()
}

async function connectClient(url: string): Promise<WebSocket> {
  const socket = new WebSocket(url)
  await once(socket, 'open')
  return socket
}

describe('Gateway', () => {
  let daemon: StandInDaemon
  let gateway: Gateway
  let remoteAccess = false

  beforeEach(async () => {
    daemon = await startStandInDaemon()
    gateway = await startGateway({
      port: 0,
      remoteAccess,
      getDaemonUrl: () => daemon.url,
      getPairingToken: () => TOKEN,
      getLocalToken: () => LOCAL_TOKEN,
      getCredential: async () => ({ apiKey: API_KEY }),
      getMeta: () => ({ ...META, remoteAccess }),
      client: { kind: 'none' },
    })
  })

  afterEach(async () => {
    await gateway.close()
    await daemon.close()
    remoteAccess = false
  })

  test('rejects a bad token at upgrade and forwards nothing', async () => {
    const socket = new WebSocket(gatewayDaemonUrl(gateway.url, 'wrong-token'))
    const [error] = (await once(socket, 'error')) as [Error]
    expect(error.message).toMatch(/401/)
    await new Promise((r) => setTimeout(r, 50))
    expect(daemon.sockets).toHaveLength(0)
    expect(daemon.received).toHaveLength(0)
  })

  test('rejects a missing token', async () => {
    const socket = new WebSocket(new URL('/daemon', gateway.url).toString().replace('http', 'ws'))
    const [error] = (await once(socket, 'error')) as [Error]
    expect(error.message).toMatch(/401/)
  })

  test('accepts the correct token and swaps the API key into daemon.authenticate', async () => {
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    const authenticate = JSON.stringify({
      jsonrpc: '2.0',
      id: '1',
      method: 'daemon.authenticate',
      params: { apiKey: GATEWAY_API_KEY_PLACEHOLDER, caller: 'sdk' },
    })
    socket.send(authenticate)
    const echoed = await nextMessage(socket)
    expect(daemon.received).toHaveLength(1)
    const forwarded = JSON.parse(daemon.received[0]!)
    expect(forwarded.params.apiKey).toBe(API_KEY)
    expect(forwarded.method).toBe('daemon.authenticate')
    expect(forwarded.id).toBe('1')
    expect(echoed).toBe(`echo:${daemon.received[0]}`)
    socket.close()
  })

  test('swaps the placeholder spawn credential in initialize_session and load_session', async () => {
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    for (const method of ['daemon.initialize_session', 'daemon.load_session']) {
      socket.send(
        JSON.stringify({
          jsonrpc: '2.0',
          id: method,
          method,
          params: { cwd: '/x', token: GATEWAY_API_KEY_PLACEHOLDER },
        }),
      )
      await nextMessage(socket)
    }
    expect(daemon.received.map((f) => JSON.parse(f).params.token)).toEqual([API_KEY, API_KEY])
    socket.close()
  })

  test('appends the Shell’s system prompt text to initialize_session only', async () => {
    await gateway.close()
    let append: string | null = 'Answer in French.'
    gateway = await startGateway({
      port: 0,
      remoteAccess: false,
      getDaemonUrl: () => daemon.url,
      getPairingToken: () => TOKEN,
      getCredential: async () => ({ apiKey: API_KEY }),
      getAppendSystemPrompt: () => append,
      getMeta: () => ({ ...META, remoteAccess: false }),
      client: { kind: 'none' },
    })
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    const send = async (method: string, params: Record<string, unknown>) => {
      socket.send(JSON.stringify({ jsonrpc: '2.0', id: method, method, params }))
      await nextMessage(socket)
      return JSON.parse(daemon.received.at(-1)!).params
    }
    const initialize = { cwd: '/x', token: GATEWAY_API_KEY_PLACEHOLDER }

    expect(await send('daemon.initialize_session', initialize)).toEqual({
      cwd: '/x',
      token: API_KEY,
      systemPrompt: { type: 'preset', preset: 'droid', append: 'Answer in French.' },
    })
    expect(await send('daemon.load_session', initialize)).not.toHaveProperty('systemPrompt')
    // A Client's own choice stands.
    expect(
      await send('daemon.initialize_session', { ...initialize, systemPrompt: 'Be terse.' }),
    ).toMatchObject({ systemPrompt: 'Be terse.' })
    append = '  '
    expect(await send('daemon.initialize_session', initialize)).not.toHaveProperty('systemPrompt')
    socket.close()
  })

  test('a login token replaces apiKey with token and keeps frames in order', async () => {
    await gateway.close()
    let resolveToken: (t: { token: string }) => void = () => {}
    gateway = await startGateway({
      port: 0,
      remoteAccess: false,
      getDaemonUrl: () => daemon.url,
      getPairingToken: () => TOKEN,
      getCredential: () => new Promise((resolve) => (resolveToken = resolve)),
      getMeta: () => ({ ...META, remoteAccess: false }),
      client: { kind: 'none' },
    })
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    socket.send(
      JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: 'daemon.authenticate',
        params: { apiKey: GATEWAY_API_KEY_PLACEHOLDER, caller: 'sdk' },
      }),
    )
    // Sent while the credential is still being fetched; must not overtake it.
    socket.send('{"jsonrpc":"2.0","id":"2","method":"daemon.list_available_sessions","params":{}}')
    await new Promise((r) => setTimeout(r, 50))
    expect(daemon.received).toHaveLength(0)
    resolveToken({ token: 'eyJ.login.jwt' })
    await expect.poll(() => daemon.received.length).toBe(2)
    const [first, second] = daemon.received.map((f) => JSON.parse(f))
    expect(first.method).toBe('daemon.authenticate')
    expect(first.params).toEqual({ token: 'eyJ.login.jwt', caller: 'sdk' })
    expect(second.id).toBe('2')
    socket.close()
  })

  test('forwards every other frame byte-identical in both directions', async () => {
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    const frames = [
      JSON.stringify({
        jsonrpc: '2.0',
        id: '2',
        method: 'daemon.list_available_sessions',
        params: {},
      }),
      '{"jsonrpc":"2.0",  "id":"3","method":"x","params":{"apiKey":"not-an-auth-frame"}}',
      '{"jsonrpc":"2.0","id":"4","method":"daemon.add_user_message","params":{"text":"say droi-gateway please"}}',
      'not even json',
    ]
    for (const frame of frames) {
      socket.send(frame)
      expect(await nextMessage(socket)).toBe(`echo:${frame}`)
    }
    expect(daemon.received).toEqual(frames)
    socket.close()
  })

  test('closing the daemon side closes the client side', async () => {
    const socket = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    socket.send('ping')
    await nextMessage(socket)
    daemon.sockets[0]!.close()
    await once(socket, 'close')
    expect(socket.readyState).toBe(WebSocket.CLOSED)
  })

  test('/meta describes the shell and names the computer without secrets or a token', async () => {
    const response = await fetch(new URL('/meta', gateway.url))
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body).toEqual({
      app: 'Droi',
      version: '1.2.3',
      remoteAccess: false,
      name: 'Studio Mac',
      computerId: 'c0ffee00-0000-4000-8000-000000000000',
    })
    const text = JSON.stringify(body)
    expect(text).not.toContain(API_KEY)
    expect(text).not.toContain(TOKEN)
  })

  test('GET /daemon answers 204 for the right token and 401 otherwise', async () => {
    const ok = await fetch(gatewayPairingCheckUrl(gateway.url, TOKEN))
    expect(ok.status).toBe(204)
    const bad = await fetch(gatewayPairingCheckUrl(gateway.url, 'nope'))
    expect(bad.status).toBe(401)
  })

  test('accepts the Local Token too, and revoking pairing spares the local bridge', async () => {
    const local = await connectClient(gatewayDaemonUrl(gateway.url, LOCAL_TOKEN))
    const remote = await connectClient(gatewayDaemonUrl(gateway.url, TOKEN))
    local.send('a')
    remote.send('b')
    await nextMessage(local)
    await nextMessage(remote)

    gateway.revoke('pairing')
    await once(remote, 'close')
    expect(local.readyState).toBe(WebSocket.OPEN)
    local.send('still here')
    expect(await nextMessage(local)).toBe('echo:still here')
    local.close()
  })

  test('Remote Access binds LAN addresses on demand and never touches the local bridge', async () => {
    const lan = lanInterfaceAddresses()
    const local = await connectClient(gatewayDaemonUrl(gateway.url, LOCAL_TOKEN))
    expect(gateway.remoteAccess).toBe(false)
    if (lan[0]) {
      await expect(fetch(`http://${lan[0]}:${gateway.port}/meta`)).rejects.toThrow()
    }

    await gateway.setRemoteAccess(true)
    expect(gateway.lanAddresses).toEqual(lan)
    if (lan[0]) {
      const viaLan = await fetch(`http://${lan[0]}:${gateway.port}/meta`)
      expect(viaLan.status).toBe(200)
      const phone = await connectClient(gatewayDaemonUrl(`http://${lan[0]}:${gateway.port}`, TOKEN))
      phone.send('from phone')
      expect(await nextMessage(phone)).toBe('echo:from phone')
      await gateway.setRemoteAccess(false)
      await once(phone, 'close')
      await expect(fetch(`http://${lan[0]}:${gateway.port}/meta`)).rejects.toThrow()
    } else {
      await gateway.setRemoteAccess(false)
    }
    expect(gateway.remoteAccess).toBe(false)
    local.send('still local')
    expect(await nextMessage(local)).toBe('echo:still local')
    local.close()
  })

  test('binds loopback only while Remote Access is off', () => {
    expect(gateway.remoteAccess).toBe(false)
    expect(gateway.lanAddresses).toEqual([])
  })

  test('fails the upgrade with 503 when no Daemon is running', async () => {
    await gateway.close()
    gateway = await startGateway({
      port: 0,
      remoteAccess: false,
      getDaemonUrl: () => null,
      getPairingToken: () => TOKEN,
      getCredential: async () => ({ apiKey: API_KEY }),
      getMeta: () => ({ ...META, remoteAccess: false }),
      client: { kind: 'none' },
    })
    const socket = new WebSocket(gatewayDaemonUrl(gateway.url, TOKEN))
    const [error] = (await once(socket, 'error')) as [Error]
    expect(error.message).toMatch(/503/)
  })
})

describe('Gateway with Remote Access on', () => {
  test('binds every LAN address from the start', async () => {
    const daemon = await startStandInDaemon()
    const gateway = await startGateway({
      port: 0,
      remoteAccess: true,
      getDaemonUrl: () => daemon.url,
      getPairingToken: () => TOKEN,
      getCredential: async () => ({ apiKey: API_KEY }),
      getMeta: () => ({ ...META, remoteAccess: true }),
      client: { kind: 'none' },
    })
    expect(gateway.lanAddresses).toEqual(lanInterfaceAddresses())
    await gateway.close()
    await daemon.close()
  })
})
