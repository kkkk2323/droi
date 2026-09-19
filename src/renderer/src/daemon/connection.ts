// The Client's one connection to the Daemon (through the Gateway). Wraps the
// SDK's DaemonSessionController, which owns the WebSocket, authentication and
// short-lived reconnection, and adds what the UI needs on top: a small state
// machine, a pairing check that tells a bad Pairing Token apart from an
// unreachable Daemon, and a patient poll loop for when the Daemon is away
// longer than the SDK is willing to wait.
import {
  DaemonSessionController,
  LOCAL_MACHINE_ID,
  MachineType,
  MultiSessionStateManager,
} from '@factory/droid-sdk'
import {
  GATEWAY_API_KEY_PLACEHOLDER,
  gatewayDaemonUrl,
  gatewayPairingCheckUrl,
} from '@shared/gateway'
import type { ClientConfig } from '../lib/client-config'

export type ConnectionState =
  | { status: 'connecting' }
  | { status: 'connected' }
  | { status: 'reconnecting' }
  | { status: 'unpaired'; reason: 'missing-token' | 'rejected-token' }
  | { status: 'unreachable'; detail: string }

export interface DaemonConnection {
  readonly controller: DaemonSessionController
  readonly sessionState: MultiSessionStateManager
  getState(): ConnectionState
  subscribe(listener: () => void): () => void
  start(): void
  dispose(): void
}

const RECOVERY_POLL_MS = 2_000

export function createDaemonConnection(
  config: ClientConfig,
  fetchImpl: typeof fetch = (input, init) => fetch(input, init),
): DaemonConnection {
  const sessionState = new MultiSessionStateManager()
  const controller = new DaemonSessionController({
    sessionStateManager: sessionState,
    config: {
      machineId: LOCAL_MACHINE_ID,
      machineType: MachineType.Local,
      url: gatewayDaemonUrl(config.gatewayUrl, config.pairingToken ?? ''),
      clientType: config.kind === 'local' ? 'desktop' : 'web',
      getCredential: async () => ({ apiKey: GATEWAY_API_KEY_PLACEHOLDER }),
      connectionTimeoutMs: 5_000,
      requestTimeout: 30_000,
      maxPollAttempts: 15,
      maxReconnectAttempts: 3,
      reconnectInterval: 1_000,
      maxReconnectDelay: 10_000,
      reconnectBackoffFactor: 1.5,
      supportsTerminalRestoreOnLoad: false,
    },
  })

  let state: ConnectionState = { status: 'connecting' }
  const listeners = new Set<() => void>()
  let disposed = false
  let recoveryTimer: ReturnType<typeof setTimeout> | null = null

  const setState = (next: ConnectionState) => {
    state = next
    for (const listener of listeners) listener()
  }

  const checkPairing = async (): Promise<'ok' | 'rejected' | 'unreachable'> => {
    try {
      const response = await fetchImpl(
        gatewayPairingCheckUrl(config.gatewayUrl, config.pairingToken ?? ''),
        { cache: 'no-store' },
      )
      if (response.status === 204) return 'ok'
      if (response.status === 401) return 'rejected'
      return 'unreachable'
    } catch {
      return 'unreachable'
    }
  }

  const connect = async () => {
    if (disposed) return
    const pairing = await checkPairing()
    if (disposed) return
    if (pairing === 'rejected') {
      setState({ status: 'unpaired', reason: 'rejected-token' })
      return
    }
    if (pairing === 'unreachable') {
      setState({ status: 'unreachable', detail: 'Gateway not reachable' })
      scheduleRecovery()
      return
    }
    try {
      await controller.attemptInitialConnection()
    } catch (error) {
      if (disposed) return
      setState({ status: 'unreachable', detail: describe(error) })
      scheduleRecovery()
    }
  }

  const scheduleRecovery = () => {
    if (recoveryTimer || disposed) return
    recoveryTimer = setTimeout(() => {
      recoveryTimer = null
      void connect()
    }, RECOVERY_POLL_MS)
  }

  controller.on('connectionStatusChanged', (status) => {
    if (disposed) return
    if (status.transport === 'connected' && status.isAuthenticated) {
      setState({ status: 'connected' })
      return
    }
    if (status.recovery?.isReconnecting) {
      setState({ status: 'reconnecting' })
      return
    }
    if (state.status === 'connected' || state.status === 'reconnecting') {
      // The SDK gave up; keep the banner up and poll the Gateway ourselves.
      setState({ status: 'reconnecting' })
      scheduleRecovery()
    }
  })

  return {
    controller,
    sessionState,
    getState: () => state,
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
    dispose() {
      disposed = true
      if (recoveryTimer) clearTimeout(recoveryTimer)
      controller.destroy()
    },
  }
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
