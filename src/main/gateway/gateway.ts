// Gateway: the part of the Desktop Shell that lets a Client reach the Daemon.
// It checks the Pairing Token at the WebSocket upgrade, supplies the Factory
// API key in `daemon.authenticate`, and otherwise forwards frames verbatim.
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { Duplex } from 'node:stream'
import { timingSafeEqual } from 'node:crypto'
import { networkInterfaces } from 'node:os'
import { once } from 'node:events'
import { WebSocket, WebSocketServer, type RawData } from 'ws'
import {
  GATEWAY_API_KEY_PLACEHOLDER,
  GATEWAY_DAEMON_PATH,
  GATEWAY_META_PATH,
  GATEWAY_TOKEN_QUERY,
  type GatewayMeta,
} from '../../shared/gateway'
import { serveStaticFile } from './static-files'
import { proxyHttp, proxyUpgrade } from './dev-proxy'

export type ClientSource =
  | { kind: 'none' }
  | { kind: 'static'; dir: string }
  | { kind: 'proxy'; target: URL }

export type TokenKind = 'pairing' | 'local'

export interface GatewayOptions {
  port: number
  remoteAccess: boolean
  /** Current Daemon WebSocket URL, or null while the Daemon is not running. */
  getDaemonUrl: () => string | null
  /** The Pairing Token Remote Clients present. */
  getPairingToken: () => string
  /**
   * A per-launch token only the Local Client knows. Resetting the Pairing
   * Token then revokes phones without touching the desktop window.
   */
  getLocalToken?: () => string
  /**
   * Credential to put where the Client left its placeholder: a Factory login
   * token or an API key. Async because a token may need refreshing first.
   * Null leaves the frame untouched (the Daemon then rejects it).
   */
  getCredential: () => Promise<GatewayCredential | null>
  getMeta: () => GatewayMeta
  client: ClientSource
}

export type GatewayCredential = { token: string } | { apiKey: string }

export interface Gateway {
  url: string
  port: number
  /** True while LAN listeners are up. */
  readonly remoteAccess: boolean
  /** LAN addresses currently listening; empty when Remote Access is off. */
  readonly lanAddresses: string[]
  /** Bind or unbind the LAN listeners; the loopback listener never moves. */
  setRemoteAccess(enabled: boolean): Promise<void>
  /** Terminate every bridge that authenticated with this kind of token. */
  revoke(kind: TokenKind): void
  close(): Promise<void>
}

const LOOPBACK = '127.0.0.1'

/**
 * The Gateway keeps one listener on loopback for the Local Client and, only
 * while Remote Access is on, one per LAN interface address for Remote Clients.
 * Binding interface addresses rather than 0.0.0.0 lets the loopback listener
 * stay untouched when Remote Access toggles, so the desktop window never
 * loses its bridge, and keeps the port closed to the network otherwise.
 */
export async function startGateway(options: GatewayOptions): Promise<Gateway> {
  const wss = new WebSocketServer({ noServer: true })
  const bridges = new Map<WebSocket, { upstream: WebSocket; kind: TokenKind }>()
  const lanServers = new Map<string, Server>()

  const makeServer = (): Server => {
    const server = createServer((request, response) => handleHttp(options, request, response))
    server.on('upgrade', (request, socket, head) => {
      const url = new URL(request.url ?? '/', 'http://gateway')
      if (url.pathname !== GATEWAY_DAEMON_PATH) {
        if (options.client.kind === 'proxy') {
          proxyUpgrade(options.client.target, request, socket, head)
        } else {
          rejectUpgrade(socket, 404, 'Not Found')
        }
        return
      }
      const kind = classifyToken(url.searchParams.get(GATEWAY_TOKEN_QUERY), options)
      if (!kind) {
        rejectUpgrade(socket, 401, 'Unauthorized')
        return
      }
      const daemonUrl = options.getDaemonUrl()
      if (!daemonUrl) {
        rejectUpgrade(socket, 503, 'Daemon Unavailable')
        return
      }
      wss.handleUpgrade(request, socket, head, (client) => {
        const upstream = bridge(client, daemonUrl, options.getCredential)
        bridges.set(client, { upstream, kind })
        client.on('close', () => bridges.delete(client))
      })
    })
    return server
  }

  const loopback = makeServer()
  loopback.listen(options.port, LOOPBACK)
  await once(loopback, 'listening')
  const address = loopback.address()
  if (!address || typeof address === 'string') throw new Error('Gateway did not bind a TCP port')
  const port = address.port

  const closeServer = (server: Server) =>
    new Promise<void>((resolve) => {
      server.closeIdleConnections()
      server.close(() => resolve())
    })

  const bindLan = async () => {
    for (const lanAddress of lanInterfaceAddresses()) {
      if (lanServers.has(lanAddress)) continue
      const server = makeServer()
      server.listen(port, lanAddress)
      try {
        await once(server, 'listening')
        lanServers.set(lanAddress, server)
      } catch {
        // An interface that refuses to bind is skipped; the others still serve.
      }
    }
  }

  const unbindLan = async () => {
    const closing = [...lanServers.values()].map(closeServer)
    lanServers.clear()
    await Promise.all(closing)
  }

  const revoke = (kind: TokenKind) => {
    for (const [client, entry] of bridges) {
      if (entry.kind !== kind) continue
      client.terminate()
      entry.upstream.terminate()
      bridges.delete(client)
    }
  }

  if (options.remoteAccess) await bindLan()

  return {
    // The URL always points at loopback; a Remote Client is told the LAN
    // address separately, by whoever shows the pairing link.
    url: `http://${LOOPBACK}:${port}`,
    port,
    get remoteAccess() {
      return lanServers.size > 0
    },
    get lanAddresses() {
      return [...lanServers.keys()]
    },
    async setRemoteAccess(enabled) {
      if (enabled) {
        await bindLan()
      } else {
        // Phones lose access at once; the Local Client keeps its bridge.
        revoke('pairing')
        await unbindLan()
      }
    },
    revoke,
    async close() {
      for (const [client, entry] of bridges) {
        client.terminate()
        entry.upstream.terminate()
      }
      bridges.clear()
      wss.close()
      await unbindLan()
      await closeServer(loopback)
    },
  }
}

/** IPv4 addresses of non-internal interfaces, the ones a phone could reach. */
export function lanInterfaceAddresses(): string[] {
  const addresses: string[] = []
  for (const entries of Object.values(networkInterfaces())) {
    for (const entry of entries ?? []) {
      if (entry.family === 'IPv4' && !entry.internal) addresses.push(entry.address)
    }
  }
  return addresses
}

function classifyToken(presented: string | null, options: GatewayOptions): TokenKind | null {
  if (tokenMatches(presented, options.getPairingToken())) return 'pairing'
  const local = options.getLocalToken?.()
  if (local && tokenMatches(presented, local)) return 'local'
  return null
}

function tokenMatches(presented: string | null, expected: string): boolean {
  if (!presented || !expected) return false
  const a = Buffer.from(presented)
  const b = Buffer.from(expected)
  return a.length === b.length && timingSafeEqual(a, b)
}

function rejectUpgrade(socket: Duplex, status: number, reason: string): void {
  socket.write(`HTTP/1.1 ${status} ${reason}\r\nConnection: close\r\n\r\n`)
  socket.destroy()
}

/**
 * Pipe frames between a Client socket and a fresh Daemon socket. Frames the
 * Client sends before the Daemon socket opens are queued in order.
 */
function bridge(
  client: WebSocket,
  daemonUrl: string,
  getCredential: () => Promise<GatewayCredential | null>,
): WebSocket {
  const upstream = new WebSocket(daemonUrl)
  const queue: Array<{ data: RawData | string; binary: boolean }> = []

  const sendUpstream = (data: RawData | string, binary: boolean) => {
    if (upstream.readyState === WebSocket.OPEN) upstream.send(data, { binary })
    else queue.push({ data, binary })
  }

  upstream.on('open', () => {
    for (const frame of queue) upstream.send(frame.data, { binary: frame.binary })
    queue.length = 0
  })
  // Frames are forwarded in arrival order even when one has to wait for a
  // credential; each forward chains on the previous one.
  let inOrder: Promise<void> = Promise.resolve()
  client.on('message', (data, isBinary) => {
    if (isBinary || !data.toString().includes(GATEWAY_API_KEY_PLACEHOLDER)) {
      inOrder = inOrder.then(() => sendUpstream(data, isBinary))
      return
    }
    inOrder = inOrder
      .then(() => getCredential())
      .catch(() => null)
      .then((credential) => sendUpstream(injectCredential(data, credential), false))
  })
  upstream.on('message', (data, isBinary) => {
    if (client.readyState === WebSocket.OPEN) client.send(data, { binary: isBinary })
  })

  const closeBoth = () => {
    if (client.readyState !== WebSocket.CLOSED) client.close()
    if (upstream.readyState !== WebSocket.CLOSED) upstream.close()
  }
  client.on('close', closeBoth)
  upstream.on('close', closeBoth)
  client.on('error', closeBoth)
  upstream.on('error', closeBoth)
  return upstream
}

/**
 * Replace the Client's placeholder with the real credential. The SDK sends it
 * as `apiKey` in `daemon.authenticate` and as `token` (spawn credential) in
 * `daemon.initialize_session` / `daemon.load_session`. A login token goes in
 * `token` in both cases (the Daemon reads `apiKey` strictly as an API key);
 * only a top-level params field holding the exact placeholder is touched, so
 * any other frame stays byte-identical.
 */
export function injectCredential(
  data: RawData,
  credential: GatewayCredential | null,
): RawData | string {
  if (!credential) return data
  const text = data.toString()
  if (!text.includes(GATEWAY_API_KEY_PLACEHOLDER)) return data
  let message: unknown
  try {
    message = JSON.parse(text)
  } catch {
    return data
  }
  if (!hasParams(message)) return data
  const params = { ...message.params }
  let changed = false
  if (params['apiKey'] === GATEWAY_API_KEY_PLACEHOLDER) {
    if ('apiKey' in credential) params['apiKey'] = credential.apiKey
    else {
      delete params['apiKey']
      params['token'] = credential.token
    }
    changed = true
  }
  if (params['token'] === GATEWAY_API_KEY_PLACEHOLDER) {
    params['token'] = 'apiKey' in credential ? credential.apiKey : credential.token
    changed = true
  }
  return changed ? JSON.stringify({ ...message, params }) : data
}

function hasParams(message: unknown): message is { params: Record<string, unknown> } {
  if (typeof message !== 'object' || message === null) return false
  const params = (message as Record<string, unknown>)['params']
  return typeof params === 'object' && params !== null
}

function handleHttp(options: GatewayOptions, request: IncomingMessage, response: ServerResponse) {
  const url = new URL(request.url ?? '/', 'http://gateway')
  if (url.pathname === GATEWAY_DAEMON_PATH) {
    const ok = classifyToken(url.searchParams.get(GATEWAY_TOKEN_QUERY), options) !== null
    // Cross-origin so a Client served elsewhere (vite dev, tests) can check.
    // The answer carries no secret.
    response.writeHead(ok ? 204 : 401, {
      'cache-control': 'no-store',
      'access-control-allow-origin': '*',
    })
    response.end()
    return
  }
  if (url.pathname === GATEWAY_META_PATH) {
    const body = JSON.stringify(options.getMeta())
    response.writeHead(200, {
      'content-type': 'application/json',
      'cache-control': 'no-store',
      'access-control-allow-origin': '*',
    })
    response.end(body)
    return
  }
  switch (options.client.kind) {
    case 'static':
      serveStaticFile(options.client.dir, url.pathname, response)
      return
    case 'proxy':
      proxyHttp(options.client.target, request, response)
      return
    case 'none':
      response.writeHead(404)
      response.end()
  }
}
