// What the Kernel tells every connection, kept per connection: its models (harness.info),
// which conversations are running, the approvals and questions waiting for a person, and
// the approval policy. One store per connection, built on its notifications; the hooks read
// it with useSyncExternalStore.
import { useCallback, useSyncExternalStore } from 'react'
import type { KernelConnection } from './connection'
import { useKernelConnection } from './connection-context'
import type { Notification } from './kernel-socket'
import type {
  ApprovalRequest,
  AskRequest,
  ConversationId,
  HarnessInfo,
  LiveRun,
  PendingRequest,
  Policy,
} from './kernel-wire'

export interface KernelSnapshot {
  /** The socket this was heard on: a new one starts afresh. */
  generation: number
  /** The Kernel's harness.info; null until the first answer on this socket. */
  info: HarnessInfo | null
  /** The conversations running now, with the run. */
  busy: ReadonlyMap<ConversationId, LiveRun>
  approvals: ReadonlyMap<string, ApprovalRequest>
  asks: ReadonlyMap<string, AskRequest>
  /** Each conversation's policy as the Kernel last said; the default under null. */
  policies: ReadonlyMap<ConversationId | null, Policy>
}

export interface KernelStore {
  snapshot(): KernelSnapshot
  subscribe(listener: () => void): () => void
  /** What a conversation.open answered: the requests still open there, and its policy. */
  opened(id: ConversationId, pending: readonly PendingRequest[], policy: Policy | undefined): void
  /** Running conversations as a list read saw them, for those no notification has told of. */
  listed(items: ReadonlyArray<{ id: ConversationId; busy?: boolean }>): void
  /** A request this Client answered: it goes now, ahead of the Kernel's resolved. */
  answered(requestId: string): void
  /** Reads harness.info again. */
  refreshInfo(): void
}

const stores = new WeakMap<KernelConnection, KernelStore>()

export function kernelStore(connection: KernelConnection): KernelStore {
  let store = stores.get(connection)
  if (!store) {
    store = createKernelStore(connection)
    stores.set(connection, store)
  }
  return store
}

export function createKernelStore(connection: KernelConnection): KernelStore {
  let info: HarnessInfo | null = null
  let busy = new Map<ConversationId, LiveRun>()
  // Conversations a notification spoke of on this socket: a list read older than it must not undo it.
  let heardBusy = new Set<ConversationId>()
  let approvals = new Map<string, ApprovalRequest>()
  let asks = new Map<string, AskRequest>()
  // Requests answered or resolved on this socket: a late pending list must not bring one back.
  let settled = new Set<string>()
  let policies = new Map<ConversationId | null, Policy>()
  let gen = -1
  let snapshot: KernelSnapshot | null = null
  const listeners = new Set<() => void>()

  const changed = () => {
    snapshot = null
    for (const listener of listeners) listener()
  }

  // A new socket starts afresh: what the Kernel said on the old one may have changed meanwhile.
  const sync = () => {
    const now = connection.generation()
    if (now === gen || connection.getState().status !== 'connected') return
    gen = now
    busy = new Map()
    heardBusy = new Set()
    approvals = new Map()
    asks = new Map()
    settled = new Set()
    changed()
    readInfo()
  }

  let reading = false
  const readInfo = () => {
    if (reading) return
    reading = true
    const asked = gen
    connection
      .call<HarnessInfo>('harness.info')
      .then((answer) => {
        if (asked !== gen) return
        info = answer
        if (answer.policy && !policies.has(null))
          policies = new Map(policies).set(null, answer.policy)
        changed()
      })
      .catch(() => {})
      .finally(() => {
        reading = false
        if (asked !== gen) readInfo()
      })
  }

  const onNote = ({ method, params }: Notification) => {
    switch (method) {
      case 'conversation.busy': {
        const id = params['id'] as ConversationId
        const run = params['run'] as LiveRun | null
        heardBusy.add(id)
        if (run === null && !busy.has(id)) return
        busy = new Map(busy)
        if (run) busy.set(id, run)
        else busy.delete(id)
        changed()
        return
      }
      case 'approval.requested': {
        const request = params as unknown as ApprovalRequest
        if (settled.has(request.requestId)) return
        approvals = new Map(approvals).set(request.requestId, request)
        changed()
        return
      }
      case 'ask.requested': {
        const request = params as unknown as AskRequest
        if (settled.has(request.requestId)) return
        asks = new Map(asks).set(request.requestId, request)
        changed()
        return
      }
      case 'approval.resolved':
      case 'ask.resolved':
        forget(params['requestId'] as string)
        return
      case 'policy.changed': {
        const id = (params['id'] ?? null) as ConversationId | null
        policies = new Map(policies).set(id, params['policy'] as Policy)
        changed()
        return
      }
      case 'harness.changed':
        readInfo()
        return
    }
  }

  const forget = (requestId: string) => {
    settled.add(requestId)
    if (approvals.has(requestId)) {
      approvals = new Map(approvals)
      approvals.delete(requestId)
    }
    if (asks.has(requestId)) {
      asks = new Map(asks)
      asks.delete(requestId)
    }
    changed()
  }

  connection.onNotification(onNote)
  connection.subscribe(sync)
  sync()

  return {
    snapshot() {
      snapshot ??= { generation: gen, info, busy, approvals, asks, policies }
      return snapshot
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => void listeners.delete(listener)
    },
    opened(id, pending, policy) {
      // The answer is the Kernel's table of open requests for this conversation, now.
      const nextApprovals = new Map([...approvals].filter(([, r]) => r.id !== id))
      const nextAsks = new Map([...asks].filter(([, r]) => r.id !== id))
      for (const request of pending) {
        if (settled.has(request.requestId)) continue
        const { kind, ...rest } = request
        if (kind === 'approval') nextApprovals.set(request.requestId, rest as ApprovalRequest)
        else nextAsks.set(request.requestId, rest as AskRequest)
      }
      approvals = nextApprovals
      asks = nextAsks
      if (policy) policies = new Map(policies).set(id, policy)
      changed()
    },
    listed(items) {
      let next: Map<ConversationId, LiveRun> | null = null
      for (const item of items) {
        const running = item.busy === true
        if (heardBusy.has(item.id) || running === busy.has(item.id)) continue
        next ??= new Map(busy)
        if (running) next.set(item.id, { taskId: 0, inputs: [], turns: 0 })
        else next.delete(item.id)
      }
      if (next) {
        busy = next
        changed()
      }
    },
    answered: forget,
    refreshInfo: readInfo,
  }
}

/** The connection's Kernel store, as React reads it. */
export function useKernelSnapshot(): KernelSnapshot {
  const connection = useKernelConnection()
  const store = kernelStore(connection)
  const subscribe = useCallback((listener: () => void) => store.subscribe(listener), [store])
  const get = useCallback(() => store.snapshot(), [store])
  return useSyncExternalStore(subscribe, get, get)
}
