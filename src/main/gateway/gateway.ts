// Gateway: the part of the Desktop Shell that lets a Client reach the Daemon.
// It checks the Pairing Token at the WebSocket upgrade, supplies the Factory
// API key in `daemon.authenticate`, and otherwise forwards frames verbatim.
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { Duplex } from 'node:stream'
import { timingSafeEqual } from 'node:crypto'
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

export interface GatewayOptions {
  port: number
  remoteAccess: boolean
  /** Current Daemon WebSocket URL, or null while the Daemon is not running. */
  getDaemonUrl: () => string | null
  getPairingToken: () => string
  /** Factory API key to inject; null leaves `daemon.authenticate` untouched. */
  getApiKey: () => string | null
  getMeta: () => GatewayMeta
  client: ClientSource
}

export interface Gateway {
  url: string
  host: string
  port: number
  close(): Promise<void>
}

const LOOPBACK = '127.0.0.1'
const ALL_INTERFACES = '0.0.0.0'

export async function startGateway(options: GatewayOptions): Promise<Gateway> {
  const host = options.remoteAccess ? ALL_INTERFACES : LOOPBACK
  const server = createServer((request, response) => handleHttp(options, request, response))
  const wss = new WebSocketServer({ noServer: true })
  const upstreams = new Set<WebSocket>()

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
    if (!tokenMatches(url.searchParams.get(GATEWAY_TOKEN_QUERY), options.getPairingToken())) {
      rejectUpgrade(socket, 401, 'Unauthorized')
      return
    }
    const daemonUrl = options.getDaemonUrl()
    if (!daemonUrl) {
      rejectUpgrade(socket, 503, 'Daemon Unavailable')
      return
    }
    wss.handleUpgrade(request, socket, head, (client) => {
      const upstream = bridge(client, daemonUrl, options.getApiKey)
      upstreams.add(upstream)
      upstream.on('close', () => upstreams.delete(upstream))
    })
  })

  server.listen(options.port, host)
  await once(server, 'listening')
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('Gateway did not bind a TCP port')

  return {
    // The URL always points at loopback; a Remote Client is told the LAN
    // address separately, by whoever shows the pairing link.
    url: `http://${LOOPBACK}:${address.port}`,
    host,
    port: address.port,
    close: () =>
      new Promise((resolve, reject) => {
        for (const client of wss.clients) client.terminate()
        for (const upstream of upstreams) upstream.terminate()
        wss.close()
        server.closeIdleConnections()
        server.close((error) => (error ? reject(error) : resolve()))
      }),
  }
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
function bridge(client: WebSocket, daemonUrl: string, getApiKey: () => string | null): WebSocket {
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
  client.on('message', (data, isBinary) => {
    sendUpstream(isBinary ? data : injectApiKey(data, getApiKey()), isBinary)
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
 * Replace the Client's placeholder credential with the real Factory API key.
 * The SDK sends the credential as `apiKey` in `daemon.authenticate` and as
 * `token` (spawn credential) in `daemon.initialize_session` and
 * `daemon.load_session`; only a top-level params field holding the exact
 * placeholder is touched, so any other frame stays byte-identical.
 */
export function injectApiKey(data: RawData, apiKey: string | null): RawData | string {
  if (!apiKey) return data
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
  for (const field of ['apiKey', 'token'] as const) {
    if (params[field] === GATEWAY_API_KEY_PLACEHOLDER) {
      params[field] = apiKey
      changed = true
    }
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
    const ok = tokenMatches(url.searchParams.get(GATEWAY_TOKEN_QUERY), options.getPairingToken())
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

export type { Server }
