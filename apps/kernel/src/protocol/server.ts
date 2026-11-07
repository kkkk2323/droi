import type { Server, ServerWebSocket } from 'bun'
import { createHash, timingSafeEqual } from 'node:crypto'
import {
  Code,
  type Failure,
  type Notification,
  type RequestId,
  RpcError,
  type Success,
  notification,
  parseRequest,
  requestParams,
  unsupported,
} from './rpc.ts'
import { debug, info, warn } from '../log.ts'

export type Handler = (
  params: Record<string, unknown>,
  connection: Connection,
) => Promise<unknown> | unknown

export type Dispatcher = {
  /** Every method this build answers. */
  methods: ReadonlyMap<string, Handler>
  /** The capability gating a method this build does not implement: `-32009` names it. */
  gated(method: string): string | undefined
}

/** What the session service uses to reach every connection. */
export type Notifier = {
  broadcast(method: string, payload?: unknown): void
  connections(): readonly Connection[]
}

type Socket = ServerWebSocket<{ connection?: Connection }>

/** A conversation's view subscription on one connection. */
type Subscription = { stop: () => Promise<void> }

/**
 * One WebSocket from one client. A client may hold several — the window and the phone each hold one
 * — and the kernel tells them apart by what they subscribe to: `conversation.view` goes to the
 * connections that opened that conversation, and every other notification to all of them.
 */
export class Connection {
  readonly id: number
  #ws: Socket
  #subscriptions = new Map<number, Subscription>()
  #closed = false

  constructor(id: number, ws: Socket) {
    this.id = id
    this.#ws = ws
  }

  get open(): boolean {
    return !this.#closed
  }

  send(message: Notification | Success | Failure): void {
    if (this.#closed) return
    this.#ws.send(JSON.stringify(message))
  }

  notify(method: string, payload?: unknown): void {
    this.send(notification(method, payload))
  }

  subscribed(conversationId: number): boolean {
    return this.#subscriptions.has(conversationId)
  }

  /**
   * Replace this connection's subscription to a conversation. Says whether one was already there, so the
   * caller knows whether this is one subscriber more or the same one moving its window.
   */
  subscribe(conversationId: number, stop: () => Promise<void>): boolean {
    const previous = this.#subscriptions.get(conversationId)
    this.#subscriptions.set(conversationId, { stop })
    if (previous !== undefined) void previous.stop()
    return previous !== undefined
  }

  /** Stop this connection's view of a conversation; says whether it was subscribed. */
  async unsubscribe(conversationId: number): Promise<boolean> {
    const subscription = this.#subscriptions.get(conversationId)
    if (subscription === undefined) return false
    this.#subscriptions.delete(conversationId)
    await subscription.stop()
    return true
  }

  async close(): Promise<void> {
    if (this.#closed) return
    this.#closed = true
    const subscriptions = [...this.#subscriptions.values()]
    this.#subscriptions.clear()
    await Promise.allSettled(subscriptions.map((subscription) => subscription.stop()))
  }
}

export type ServerOptions = {
  host: string
  port: number
  token: string
  dispatch: Dispatcher
  /** Answers `/health` beside `ok`. */
  health: () => Record<string, unknown>
  /**
   * A connection went away. Its view subscriptions stopped already; the kernel uses this to release its
   * per-conversation viewer records, which it counts by connection.
   */
  onConnectionClosed?: (connectionId: number) => void
}

export type ServerHandle = Notifier & {
  readonly port: number
  stop(): Promise<void>
}

/**
 * The kernel's HTTP and WebSocket endpoint.
 *
 * `/health` answers without a token: it says only whether the process is alive, and the Host asks it
 * every 200 ms while it starts. `/rpc` needs the launch token — anything else is a `401` before the
 * upgrade, or a `4401` close after it.
 */
export function serve(options: ServerOptions): ServerHandle {
  const connections = new Set<Connection>()
  let nextConnectionId = 1
  let server: Server<{ connection?: Connection }> | undefined

  const broadcast = (method: string, payload?: unknown) => {
    const frame = notification(method, payload)
    for (const connection of connections) connection.send(frame)
  }

  const handleFrame = async (connection: Connection, frame: string) => {
    let request
    try {
      request = parseRequest(frame)
    } catch (error) {
      const failure =
        error instanceof RpcError
          ? error.toFailure(null)
          : new RpcError(Code.parseError, 'bad frame').toFailure(null)
      connection.send(failure)
      return
    }
    const isNotification = request.id === undefined
    const id = request.id as RequestId
    const run = async (): Promise<unknown> => {
      const handler = options.dispatch.methods.get(request.method)
      if (handler === undefined) {
        const capability = options.dispatch.gated(request.method)
        if (capability !== undefined) throw unsupported(capability)
        throw new RpcError(Code.methodNotFound, `method not found: ${request.method}`, {
          method: request.method,
        })
      }
      try {
        const result = await handler(requestParams(request), connection)
        return result === undefined ? {} : result
      } catch (error) {
        if (error instanceof RpcError) throw error
        // An unexpected throw is a bug in the kernel: log it, keep the process, answer the request.
        warn('method threw', {
          method: request.method,
          error: error instanceof Error ? error.message : String(error),
          stack: error instanceof Error ? error.stack : undefined,
        })
        // A request that itself hit the failed storage says so, rather than looking like a bug: the
        // failure is the kernel's, not the caller's, and the client's answer is to wait for a restart.
        const reason = (
          options.dispatch as { failureReason?: () => string | undefined }
        ).failureReason?.()
        throw new RpcError(
          reason === undefined ? Code.internalError : Code.kernelFailed,
          reason === undefined
            ? 'the kernel failed to run this method'
            : 'the kernel failed and is being restarted',
          reason === undefined
            ? { detail: error instanceof Error ? error.message : String(error) }
            : { reason },
        )
      }
    }
    try {
      const result = await run()
      if (!isNotification) connection.send({ jsonrpc: '2.0', id, result } as Success)
    } catch (error) {
      const failure = (
        error instanceof RpcError
          ? error
          : new RpcError(Code.internalError, 'the kernel failed', {
              detail: error instanceof Error ? error.message : String(error),
            })
      ).toFailure(isNotification ? null : id)
      if (isNotification) {
        debug('notification failed', { method: request.method, code: failure.error.code })
      } else {
        connection.send(failure)
      }
    }
  }

  server = Bun.serve({
    hostname: options.host,
    port: options.port,
    fetch(request) {
      const url = new URL(request.url)
      if (url.pathname === '/health') {
        if (request.method !== 'GET') return new Response('method not allowed', { status: 405 })
        return Response.json({ ok: true, ...options.health() })
      }
      if (url.pathname !== '/rpc') return new Response('not found', { status: 404 })
      if (!authorized(request.headers.get('authorization'), options.token)) {
        return new Response('unauthorized', { status: 401 })
      }
      if (!server!.upgrade(request, { data: {} })) {
        return new Response('websocket upgrade required', { status: 426 })
      }
      return undefined
    },
    websocket: {
      open(ws) {
        const connection = new Connection(nextConnectionId++, ws)
        ws.data.connection = connection
        connections.add(connection)
        info('connection opened', { connection: connection.id, connections: connections.size })
      },
      async message(ws, data) {
        const connection = ws.data.connection
        if (connection === undefined) return
        if (typeof data !== 'string') {
          ws.close(1003, 'text frames only')
          return
        }
        await handleFrame(connection, data)
      },
      async close(ws) {
        const connection = ws.data.connection
        if (connection === undefined) return
        connections.delete(connection)
        // A connection that goes away is unsubscribed from every conversation it opened.
        await connection.close()
        options.onConnectionClosed?.(connection.id)
        info('connection closed', { connection: connection.id, connections: connections.size })
      },
    },
  })

  return {
    get port() {
      return server!.port ?? options.port
    },
    broadcast,
    connections: () => [...connections],
    async stop() {
      for (const connection of connections) {
        connection.notify('warning', { kind: 'host_error', message: 'the kernel is closing' })
        await connection.close()
        try {
          connection.send({
            jsonrpc: '2.0',
            id: null,
            error: { code: 1001, message: 'going away' },
          })
        } catch {
          // The socket may already be gone; nothing to do about a goodbye that cannot be sent.
        }
      }
      connections.clear()
      await server?.stop(true)
      server = undefined
    },
  }
}

/**
 * The token, and only from the `Authorization: Bearer` header.
 *
 * A query string is not accepted even when it carries the right token: URLs end up in logs, in a
 * browser's history and in the Host's own diagnostics, and a secret there is a secret leaked. The
 * comparison is `timingSafeEqual` over fixed-length digests, so neither the length nor the first wrong
 * byte of a guess is observable.
 */
function authorized(header: string | null, token: string): boolean {
  if (header === null) return false
  const match = /^Bearer\s+(.+)$/i.exec(header.trim())
  if (match === null) return false
  return sameSecret(match[1]!, token)
}

function sameSecret(given: string, expected: string): boolean {
  const a = createHash('sha256').update(given).digest()
  const b = createHash('sha256').update(expected).digest()
  return timingSafeEqual(a, b)
}
