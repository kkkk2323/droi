// Fake Daemon: a scripted stand-in for the Gateway plus Daemon used by the E2E
// tests. It speaks the Daemon JSON-RPC protocol over WebSocket, checks the
// Pairing Token at upgrade exactly like the Gateway does, replays a Scenario
// instead of running an agent, and validates every message it sends with the
// zod schemas exported by @factory/droid-sdk so the Client is exercised with
// realistic data. (See ADR 0002.)
import { createServer, type IncomingMessage, type Server } from 'node:http'
import { once } from 'node:events'
import { randomUUID } from 'node:crypto'
import { WebSocket, WebSocketServer, type RawData } from 'ws'
import { GATEWAY_DAEMON_PATH, GATEWAY_TOKEN_QUERY } from '../../src/shared/gateway'
import { validateOutbound, validateInboundEnvelope } from './schemas'
import { createScenario, HOST_ID, type Scenario, type ScenarioInput } from './scenario'
import type { JsonRpcRequest } from './protocol'

export interface RecordedRequest {
  connectionId: number
  method: string
  params: unknown
  id: string | number | null
}

export class FakeDaemon {
  readonly token: string
  readonly url: string
  readonly requests: RecordedRequest[] = []
  readonly scenario: Scenario
  #server: Server
  #wss: WebSocketServer
  #connections = new Map<number, WebSocket>()
  #nextConnectionId = 1
  #down = false
  #tokenRevoked = false
  #pendingClientAnswers = new Map<string, (message: Record<string, unknown>) => void>()

  private constructor(server: Server, wss: WebSocketServer, port: number, scenario: Scenario) {
    this.#server = server
    this.#wss = wss
    this.scenario = scenario
    this.token = `test-token-${randomUUID()}`
    this.url = `http://127.0.0.1:${port}`
  }

  static async start(input: ScenarioInput = {}): Promise<FakeDaemon> {
    const server = createServer((request, response) => {
      // Mirror the Gateway's pairing check so the Client can distinguish a bad
      // token from an unreachable Daemon.
      const url = new URL(request.url ?? '/', 'http://fake')
      if (url.pathname === GATEWAY_DAEMON_PATH) {
        response.writeHead(daemon.acceptsToken(url) ? 204 : 401, {
          'access-control-allow-origin': '*',
        })
        response.end()
        return
      }
      response.writeHead(404)
      response.end()
    })
    const wss = new WebSocketServer({ noServer: true })
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    const address = server.address()
    if (!address || typeof address === 'string') throw new Error('no port')
    const daemon = new FakeDaemon(server, wss, address.port, createScenario(input))

    server.on('upgrade', (request, socket, head) => {
      const url = new URL(request.url ?? '/', 'http://fake')
      if (url.pathname !== GATEWAY_DAEMON_PATH || !daemon.acceptsToken(url)) {
        socket.write('HTTP/1.1 401 Unauthorized\r\nConnection: close\r\n\r\n')
        socket.destroy()
        return
      }
      if (daemon.#down) {
        socket.write('HTTP/1.1 503 Daemon Unavailable\r\nConnection: close\r\n\r\n')
        socket.destroy()
        return
      }
      wss.handleUpgrade(request, socket, head, (ws) => daemon.#accept(ws, request))
    })
    return daemon
  }

  acceptsToken(url: URL): boolean {
    return !this.#tokenRevoked && url.searchParams.get(GATEWAY_TOKEN_QUERY) === this.token
  }

  /** Simulate the Daemon process dying: sockets drop and upgrades fail. */
  goDown(): void {
    this.#down = true
    for (const ws of this.#connections.values()) ws.terminate()
  }

  /** Simulate the Desktop Shell restarting the Daemon. */
  comeBack(): void {
    this.#down = false
  }

  /** Simulate the Pairing Token being reset in the Desktop Shell. */
  revokeToken(): void {
    this.#tokenRevoked = true
  }

  get connectionCount(): number {
    return this.#connections.size
  }

  /** Wait until the Client has sent a request with this method (the nth one, 1-based). */
  async waitForRequest(method: string, nth = 1, timeoutMs = 5_000): Promise<RecordedRequest> {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      const found = this.requests.filter((r) => r.method === method)[nth - 1]
      if (found) return found
      await new Promise((r) => setTimeout(r, 25))
    }
    throw new Error(`Fake Daemon never received ${method}`)
  }

  /** Push a Session notification to every connection. */
  notify(sessionId: string, notification: Record<string, unknown>): void {
    this.#broadcast({
      jsonrpc: '2.0',
      factoryApiVersion: '1.0.0',
      factoryProtocolVersion: '1.217.0',
      type: 'notification',
      method: 'daemon.session_notification',
      params: { sessionId, notification },
    })
  }

  /** Daemon-level (not Session-level) notification about archive state. */
  notifyArchiveState(sessionId: string, archivedAt: string | undefined): void {
    this.#broadcast({
      jsonrpc: '2.0',
      factoryApiVersion: '1.0.0',
      factoryProtocolVersion: '1.217.0',
      type: 'notification',
      method: 'daemon.session.archive_state_changed',
      params: { sessionId, ...(archivedAt ? { archivedAt } : {}) },
    })
  }

  /**
   * Send a Daemon-originated request (permission or ask-user) to every
   * connection and resolve with the first answer, mirroring ADR 0003.
   */
  request(
    method: string,
    params: Record<string, unknown>,
  ): {
    id: string
    answer: Promise<Record<string, unknown>>
  } {
    const id = randomUUID()
    const answer = new Promise<Record<string, unknown>>((resolve) => {
      this.#pendingClientAnswers.set(id, resolve)
    })
    this.#broadcast({
      jsonrpc: '2.0',
      factoryApiVersion: '1.0.0',
      factoryProtocolVersion: '1.217.0',
      type: 'request',
      id,
      method,
      params,
    })
    return { id, answer }
  }

  async stop(): Promise<void> {
    for (const ws of this.#connections.values()) ws.terminate()
    this.#wss.close()
    this.#server.closeIdleConnections()
    await new Promise<void>((resolve) => this.#server.close(() => resolve()))
  }

  #accept(ws: WebSocket, _request: IncomingMessage): void {
    const connectionId = this.#nextConnectionId++
    this.#connections.set(connectionId, ws)
    ws.on('close', () => this.#connections.delete(connectionId))
    ws.on('message', (data: RawData) => void this.#handleMessage(connectionId, ws, data))
  }

  async #handleMessage(connectionId: number, ws: WebSocket, data: RawData): Promise<void> {
    const text = data.toString()
    let message: Record<string, unknown>
    try {
      message = JSON.parse(text) as Record<string, unknown>
    } catch {
      throw new Error(`Fake Daemon received non-JSON frame: ${text.slice(0, 200)}`)
    }

    // Answer from the Client to one of our own requests (permission, ask-user).
    if (message['type'] === 'response' && typeof message['id'] === 'string') {
      const resolve = this.#pendingClientAnswers.get(message['id'])
      if (resolve) {
        this.#pendingClientAnswers.delete(message['id'])
        resolve(message)
      } else {
        // Late answer, exactly what the real Daemon does (ADR 0003).
        this.#send(ws, {
          jsonrpc: '2.0',
          factoryApiVersion: '1.0.0',
          type: 'response',
          id: message['id'],
          error: { code: -32602, message: `No pending request found with ID ${message['id']}` },
        })
      }
      return
    }

    const request = validateInboundEnvelope(message)
    this.requests.push({
      connectionId,
      method: request.method,
      params: request.params,
      id: request.id,
    })

    if (request.method === 'daemon.authenticate') {
      this.#send(ws, rpcResponse(request, { userId: 'user_test', orgId: 'org_test' }))
      this.#send(ws, {
        jsonrpc: '2.0',
        factoryApiVersion: '1.0.0',
        type: 'notification',
        method: 'daemon.connection_status',
        params: {
          isDroidCLIInPath: true,
          droidCLIVersion: '0.223.0',
          homedir: '/Users/test',
          platform: 'darwin',
          hostId: HOST_ID,
        },
      })
      return
    }

    try {
      const result = await this.scenario.handle(request, { daemon: this, connectionId })
      this.#send(ws, rpcResponse(request, result))
    } catch (error) {
      if (error instanceof RpcError) {
        this.#send(ws, {
          jsonrpc: '2.0',
          factoryApiVersion: '1.0.0',
          type: 'response',
          id: request.id,
          error: { code: error.code, message: error.message },
        })
        return
      }
      throw error
    }
  }

  #broadcast(message: Record<string, unknown>): void {
    for (const ws of this.#connections.values()) this.#send(ws, message)
  }

  #send(ws: WebSocket, message: Record<string, unknown>): void {
    validateOutbound(message)
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(message))
  }
}

export class RpcError extends Error {
  constructor(
    readonly code: number,
    message: string,
  ) {
    super(message)
  }
}

function rpcResponse(request: JsonRpcRequest, result: unknown): Record<string, unknown> {
  return {
    jsonrpc: '2.0',
    factoryApiVersion: '1.0.0',
    factoryProtocolVersion: '1.217.0',
    type: 'response',
    id: request.id,
    result,
  }
}
