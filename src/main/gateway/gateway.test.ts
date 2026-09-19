import { afterEach, beforeEach, describe, expect, test } from 'vitest'
import { WebSocket, WebSocketServer, type RawData } from 'ws'
import { once } from 'node:events'
import {
  gatewayDaemonUrl,
  gatewayPairingCheckUrl,
  GATEWAY_API_KEY_PLACEHOLDER,
} from '../../shared/gateway'
import { startGateway, type Gateway } from './gateway'

const TOKEN = 'correct-pairing-token'
const API_KEY = 'fk-secret-key'

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
      getApiKey: () => API_KEY,
      getMeta: () => ({ app: 'Droi', version: '1.2.3', remoteAccess }),
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

  test('/meta describes the shell without secrets', async () => {
    const response = await fetch(new URL('/meta', gateway.url))
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body).toEqual({ app: 'Droi', version: '1.2.3', remoteAccess: false })
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

  test('binds loopback only while Remote Access is off', () => {
    expect(gateway.host).toBe('127.0.0.1')
  })

  test('fails the upgrade with 503 when no Daemon is running', async () => {
    await gateway.close()
    gateway = await startGateway({
      port: 0,
      remoteAccess: false,
      getDaemonUrl: () => null,
      getPairingToken: () => TOKEN,
      getApiKey: () => API_KEY,
      getMeta: () => ({ app: 'Droi', version: '1.2.3', remoteAccess: false }),
      client: { kind: 'none' },
    })
    const socket = new WebSocket(gatewayDaemonUrl(gateway.url, TOKEN))
    const [error] = (await once(socket, 'error')) as [Error]
    expect(error.message).toMatch(/503/)
  })
})

describe('Gateway with Remote Access on', () => {
  test('binds all interfaces', async () => {
    const daemon = await startStandInDaemon()
    const gateway = await startGateway({
      port: 0,
      remoteAccess: true,
      getDaemonUrl: () => daemon.url,
      getPairingToken: () => TOKEN,
      getApiKey: () => API_KEY,
      getMeta: () => ({ app: 'Droi', version: '1.2.3', remoteAccess: true }),
      client: { kind: 'none' },
    })
    expect(gateway.host).toBe('0.0.0.0')
    await gateway.close()
    await daemon.close()
  })
})
