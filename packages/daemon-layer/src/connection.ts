// The Client's one connection to the computer's Kernel, through the Gateway (ADR 0038):
// a WebSocket speaking the Kernel's JSON-RPC, a small state machine over it, a pairing
// check that tells a bad Pairing Token apart from an unreachable computer, the choice
// between the computer's network address and its Relay, and a patient poll for a computer
// that is away. What the Kernel holds is read through the stores built on it
// (kernel-state.ts, kernel-conversations.ts).
import {
  GATEWAY_META_PATH,
  gatewayEndpoint,
  gatewayKernelUrl,
  gatewayPairingCheckUrl,
  type GatewayMeta,
} from './gateway'
import {
  openKernelSocket,
  SocketClosedError,
  type KernelSocket,
  type Notification,
} from './kernel-socket'
import { DIRECT_WAIT_MS } from './pairing'
import { createScratchWorkspaces, type ScratchWorkspaces } from './scratch-workspaces'
import { createSessionFileLookup } from './session-file'
import { createSessionListOrder, type SessionListOrderSource } from './session-list-order'
import { createWorkspaces, type Workspaces } from './workspaces'

/** Where a Client reaches the Gateway. */
export interface ClientConfig {
  kind: 'local' | 'remote'
  gatewayUrl: string
  pairingToken: string | null
  /**
   * The computer's Relay address (ADR 0034), if it has one: used when gatewayUrl, its
   * network address, does not answer within a moment.
   */
  relayUrl?: string | null
}

export type ConnectionState =
  | { status: 'connecting' }
  | { status: 'connected' }
  | { status: 'reconnecting' }
  | { status: 'unpaired'; reason: 'missing-token' | 'rejected-token' }
  /** `initial`: never connected in this Client's lifetime, so the computer may just be starting. */
  | { status: 'unreachable'; detail: string; initial: boolean }
  /** The computer answers, but its Droi runs no Kernel yet. */
  | { status: 'no-kernel' }

/** Before the first connection: the computer is most likely still starting. */
export function isStartingUp(state: ConnectionState): boolean {
  return state.status === 'connecting' || (state.status === 'unreachable' && state.initial)
}

/** A request made while no socket is open, that did not get one in time. */
export class NotConnectedError extends Error {
  constructor() {
    super('Not connected to the computer.')
    this.name = 'NotConnectedError'
  }
}

export interface KernelConnection {
  /**
   * A request to the Kernel. Made while the connection is down, it waits for the next
   * socket a while, then fails; one in flight when its socket closes fails at once.
   */
  call<T = Record<string, unknown>>(
    method: string,
    params?: Record<string, unknown>,
    timeoutMs?: number,
  ): Promise<T>
  /** Hears every notification, in order, from every socket. */
  onNotification(listener: (note: Notification) => void): () => void
  /** Counts the sockets opened so far: a new number is a new socket, whose state starts afresh. */
  generation(): number
  /** The Gateway's Scratch Workspaces on the same computer (ADR 0008). */
  readonly scratch: ScratchWorkspaces
  /** The computer's folders and their git. */
  readonly workspaces: Workspaces
  /** Where the computer keeps a Session's transcript; null when the Gateway cannot say. */
  sessionFile(sessionId: string): Promise<string | null>
  /** How the computer's window lists its Sessions (see session-list-order.ts). */
  readonly sessionListOrder: SessionListOrderSource
  getState(): ConnectionState
  /** The address the connection goes through now: the network's, or the Relay's. */
  gatewayUrl(): string
  subscribe(listener: () => void): () => void
  start(): void
  /**
   * The host went to the background (an iPhone app): let the socket go and
   * stop polling for the Gateway. What the stores hold is kept.
   */
  suspend(): void
  /** Back in the foreground: re-check the pairing and connect again at once. */
  resume(): void
  dispose(): void
}

const RECOVERY_POLL_MS = 2_000
/** How long a request made while disconnected waits for a socket. */
const WAIT_FOR_SOCKET_MS = 15_000

type Pairing = 'ok' | 'rejected' | 'unreachable' | 'no-kernel'

export function createKernelConnection(
  config: ClientConfig,
  fetchImpl: typeof fetch = (input, init) => fetch(input, init),
  openSocket: typeof openKernelSocket = openKernelSocket,
): KernelConnection {
  // The address in use: the network's, or the Relay's once the network's did not answer.
  let route = config.gatewayUrl
  const live = {
    get gatewayUrl() {
      return route
    },
    pairingToken: config.pairingToken,
  }

  let state: ConnectionState = { status: 'connecting' }
  const listeners = new Set<() => void>()
  const noteListeners = new Set<(note: Notification) => void>()
  let socket: KernelSocket | null = null
  let gen = 0
  let disposed = false
  let suspended = false
  let everConnected = false
  // The socket dropped under a screen in use: a computer that does not answer yet keeps the banner up.
  let dropped = false
  let connecting = false
  let recoveryTimer: ReturnType<typeof setTimeout> | null = null
  let waiters: Array<() => void> = []

  const setState = (next: ConnectionState) => {
    state = next
    for (const listener of listeners) listener()
  }

  const checkPairing = async (gatewayUrl: string, waitMs?: number): Promise<Pairing> => {
    const abort = waitMs === undefined ? null : new AbortController()
    const timer = abort ? setTimeout(() => abort.abort(), waitMs) : null
    try {
      const response = await fetchImpl(
        gatewayPairingCheckUrl(gatewayUrl, config.pairingToken ?? ''),
        {
          cache: 'no-store',
          ...(abort ? { signal: abort.signal } : {}),
        },
      )
      if (response.status === 204) return 'ok'
      if (response.status === 401) return 'rejected'
      // A Gateway without the Kernel's path is a Droi older than it.
      if (response.status === 404) return 'no-kernel'
      return 'unreachable'
    } catch {
      return 'unreachable'
    } finally {
      if (timer) clearTimeout(timer)
    }
  }

  /** Whether the computer says it runs a Kernel; null when it does not answer. */
  const runsKernel = async (gatewayUrl: string): Promise<boolean | null> => {
    try {
      const response = await fetchImpl(gatewayEndpoint(gatewayUrl, GATEWAY_META_PATH).toString(), {
        cache: 'no-store',
      })
      if (!response.ok) return null
      return ((await response.json()) as GatewayMeta).kernel === true
    } catch {
      return null
    }
  }

  /**
   * The network address first, then the Relay: the Relay when the network's does not answer,
   * or answers as another computer would, by refusing the token. The Relay's answer then
   * stands, except that it cannot reach a computer whose network address refused the token.
   */
  const choose = async (): Promise<{ via: string; pairing: Pairing }> => {
    const relay = config.relayUrl
    if (!relay) return { via: config.gatewayUrl, pairing: await checkPairing(config.gatewayUrl) }
    const direct = await checkPairing(config.gatewayUrl, DIRECT_WAIT_MS)
    if (direct === 'ok' || direct === 'no-kernel')
      return { via: config.gatewayUrl, pairing: direct }
    const viaRelay = await checkPairing(relay)
    if (viaRelay === 'unreachable' && direct === 'rejected')
      return { via: config.gatewayUrl, pairing: direct }
    return { via: relay, pairing: viaRelay }
  }

  const onClose = (closed: KernelSocket) => {
    if (socket !== closed) return
    socket = null
    if (disposed || suspended) return
    dropped = true
    setState({ status: 'reconnecting' })
    void connect()
  }

  const connect = async () => {
    if (disposed || suspended || connecting || socket) return
    connecting = true
    try {
      const { via, pairing } = await choose()
      if (disposed || suspended) return
      route = via
      if (pairing === 'rejected') {
        setState({ status: 'unpaired', reason: 'rejected-token' })
        return
      }
      if (pairing === 'no-kernel') {
        setState({ status: 'no-kernel' })
        scheduleRecovery()
        return
      }
      if (pairing === 'unreachable') {
        if (!dropped)
          setState({
            status: 'unreachable',
            detail: 'Gateway not reachable',
            initial: !everConnected,
          })
        scheduleRecovery()
        return
      }
      let opened: KernelSocket
      try {
        opened = await openSocket(gatewayKernelUrl(via, config.pairingToken ?? ''), {
          onNotification: (note) => {
            if (socket !== opened) return
            for (const listener of noteListeners) listener(note)
          },
          onClose: () => onClose(opened),
        })
      } catch (error) {
        if (disposed || suspended) return
        // The Gateway refuses the socket while the Kernel is down, and always on a computer without one.
        if ((await runsKernel(via)) === false) setState({ status: 'no-kernel' })
        else if (!dropped)
          setState({ status: 'unreachable', detail: describe(error), initial: !everConnected })
        scheduleRecovery()
        return
      }
      if (disposed || suspended) {
        opened.close()
        return
      }
      socket = opened
      gen += 1
      everConnected = true
      dropped = false
      setState({ status: 'connected' })
      const waiting = waiters
      waiters = []
      for (const wake of waiting) wake()
    } finally {
      connecting = false
    }
  }

  const scheduleRecovery = () => {
    if (recoveryTimer || disposed || suspended) return
    recoveryTimer = setTimeout(() => {
      recoveryTimer = null
      void connect()
    }, RECOVERY_POLL_MS)
  }

  const waitForSocket = (): Promise<KernelSocket> => {
    if (socket) return Promise.resolve(socket)
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        waiters = waiters.filter((w) => w !== wake)
        reject(new NotConnectedError())
      }, WAIT_FOR_SOCKET_MS)
      const wake = () => {
        clearTimeout(timer)
        if (socket) resolve(socket)
        else reject(new NotConnectedError())
      }
      waiters.push(wake)
    })
  }

  return {
    async call<T>(method: string, params?: Record<string, unknown>, timeoutMs?: number) {
      const s = await waitForSocket()
      try {
        return await s.call<T>(method, params, timeoutMs)
      } catch (error) {
        if (error instanceof SocketClosedError) throw new NotConnectedError()
        throw error
      }
    },
    onNotification(listener) {
      noteListeners.add(listener)
      return () => void noteListeners.delete(listener)
    },
    generation: () => gen,
    scratch: createScratchWorkspaces(live, fetchImpl),
    workspaces: createWorkspaces(live, fetchImpl),
    sessionFile: createSessionFileLookup(live, fetchImpl),
    sessionListOrder: createSessionListOrder(live, fetchImpl),
    getState: () => state,
    gatewayUrl: () => route,
    subscribe(listener) {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    start() {
      if (!config.pairingToken) {
        setState({ status: 'unpaired', reason: 'missing-token' })
        return
      }
      void connect()
    },
    suspend() {
      if (disposed || suspended) return
      suspended = true
      if (recoveryTimer) clearTimeout(recoveryTimer)
      recoveryTimer = null
      if (state.status === 'unpaired') return
      // What is on screen stays; its Sessions open again on the next socket.
      if (state.status === 'connected') setState({ status: 'reconnecting' })
      const s = socket
      socket = null
      s?.close()
    },
    resume() {
      if (disposed || !suspended) return
      suspended = false
      // Back from the background, a computer that does not answer says so rather than reconnecting on.
      dropped = false
      if (state.status === 'unpaired') return
      // connect() checks the pairing first: the Pairing Token may have been
      // reset while the phone was away.
      void connect()
    },
    dispose() {
      disposed = true
      if (recoveryTimer) clearTimeout(recoveryTimer)
      const s = socket
      socket = null
      s?.close()
      const waiting = waiters
      waiters = []
      for (const wake of waiting) wake()
    },
  }
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
