import type { Context } from '@earendil-works/chord'
import type { AgentChange, ModelRef } from '@earendil-works/pi-durable'
import {
  type Conversation,
  type ConversationView,
  type ConversationWindow,
  type EntryId,
  type EntryRecord,
  type Harness,
  type Registry,
  type Storage,
  LiveDoc,
  UsageDoc,
  type UsageState,
} from '@earendil-works/pi-durable'
import type { MutableModels } from '@earendil-works/pi-ai'
import { BACKGROUND_CONTEXT } from '@earendil-works/chord/context'
import type { Indexer } from '../indexer/index.ts'
import type { ConversationMeta } from '../indexer/schema.ts'
import type { ConversationRow } from '../indexer/schema.ts'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import type { AgentDefaults, KernelConfig } from '../config.ts'
import { info, warn } from '../log.ts'
import {
  Code,
  RpcError,
  conflict,
  invalidParams,
  notFound,
  optionalBoolean,
  optionalNumber,
  optionalObject,
  optionalString,
  requiredNumber,
  requiredObject,
  unsupported,
  invalidRequest,
} from '../protocol/rpc.ts'
import type { Dispatcher, Connection, Notifier } from '../protocol/server.ts'
import { ViewWindow, parseMode, parseWindow } from './watch.ts'

/** Pi's background context: work the kernel starts carries no client's cancellation. */
export const context: Context = BACKGROUND_CONTEXT

const DEFAULT_WINDOW: ConversationWindow = { min: 100, max: 200 }
/**
 * The most rows one page returns. A page is capped so a client cannot ask the kernel to serialize a whole
 * history into one frame, and the cap is published in `harness.info.limits` — a caller that asks for more
 * gets the cap and a cursor, and must page. Silence here once cost a long run its last round: a test asked
 * for 5000 entries, got the newest 200 in ascending order, and never saw the entries it was waiting for.
 */
const MAX_PAGE = 200

/** What the live run of one conversation looked like the last time a client was told. */
type BusyState = { run: unknown; runFailure?: unknown }

export type KernelOptions = {
  config: KernelConfig
  db: BunSqliteDatabase
  storage: Storage
  harness: Harness
  indexer: Indexer
  models: MutableModels
  registry: Registry
  /** The tools this build offers, by name, for `harness.info`. */
  tools: string[]
  version: string
  piVersion: string
  capabilities: Record<string, boolean>
  /**
   * Close the kernel from the inside, for `kernel.shutdown`. Windows has no SIGTERM a supervisor can
   * send, so the Host asks over the connection it already holds; the answer goes out before this runs.
   */
  shutdown: () => Promise<void>
  /**
   * The kernel failed for good (its storage). The request that finds it gets `-32010`, and the process
   * exits non-zero so the Supervisor restarts it: a kernel that is up but cannot write is worse than one
   * that is down, because nothing tells the Host to replace it.
   */
  onFailed?: (error: unknown) => void
}

/**
 * The 27 methods of the Kernel protocol.
 *
 * The shape follows Pi: a conversation is an ID with a handle, a view is a window over its transcript,
 * and a run is something a submission starts and an abort stops. What Droi adds is the list, the search,
 * the metadata writes and the per-connection view subscription.
 */
export class Kernel implements Dispatcher {
  readonly methods: ReadonlyMap<string, import('../protocol/server.ts').Handler>
  readonly #options: KernelOptions
  #notifier: Notifier | undefined
  /** The last busy state each conversation reached, so one broadcast per change. */
  readonly #busy = new Map<number, BusyState>()
  /** The live-run observation of each conversation, and who is watching it. */
  readonly #live = new Map<number, { detach: () => void; viewers: Set<number> }>()
  /** Who is showing each conversation: connection IDs, so a replacement or a close cannot miscount. */
  readonly #viewers = new Map<number, Set<number>>()
  /** Creates in flight, by `requestId`: two racing creates converge instead of colliding on the index. */
  readonly #creating = new Map<string, Promise<number>>()
  #closing = false
  /** Set when the Session failed: every request answers `-32010` from then on. */
  #failure: unknown
  #tasks: { detach: () => void } | undefined

  constructor(options: KernelOptions) {
    this.#options = options
    const handlers = new Map<string, import('../protocol/server.ts').Handler>()
    const add = (name: string, handler: import('../protocol/server.ts').Handler) =>
      handlers.set(name, handler)
    add('harness.info', () => {
      this.#readable()
      return this.#info()
    })
    add('conversation.create', (p) => this.#create(p))
    add('conversation.list', (p) => this.#list(p))
    add('conversation.open', (p, connection) => this.#open(p, connection))
    add('conversation.unwatch', (p, connection) => this.#unwatch(p, connection))
    add('conversation.submit', (p) => this.#submit(p))
    add('conversation.abort', (p) => this.#abort(p))
    add('submission.abort', (p) => this.#abortSubmission(p))
    add('submission.get', (p) => this.#submissionGet(p))
    add('conversation.configure', (p) => this.#configure(p))
    add('conversation.compact', (p) => this.#compact(p))
    add('conversation.fork', (p) => this.#fork(p))
    add('conversation.entries', (p) => this.#entries(p))
    add('conversation.usage', (p) => this.#usageOf(p))
    add('task.abort', (p) => this.#abortTask(p))
    add('index.update', (p) => this.#indexUpdate(p))
    add('kernel.shutdown', () => this.#shutdown())
    add('model.complete', (p) => this.#modelComplete(p))
    // The three readers of M3 answer their shape now, so a client can call the whole surface; every
    // write of M3 is gated, and `gated()` answers it `-32009` while its capability is false.
    const readOnlyAnswer =
      <T>(value: T) =>
      () => {
        this.#readable()
        return value
      }
    add('process.list', readOnlyAnswer({ processes: [] }))
    add('mcp.list', readOnlyAnswer({ servers: [] }))
    add('skills.list', readOnlyAnswer({ skills: [] }))
    this.methods = handlers
  }

  /** Where notifications go. The server hands itself over once it is listening. */
  attach(notifier: Notifier): void {
    this.#notifier = notifier
  }

  /**
   * Follow the task graph, so `task.changed` reaches clients as compaction, child conversations and
   * `afterRun` tasks start and end. Without it a client only sees the entries a task writes and never
   * learns that the work finished.
   */
  async observeTasks(): Promise<void> {
    const graph = await this.#options.harness.watchTaskGraph(context)
    // The graph holds the live tasks; a task that vanished from it ended. Diffing is what turns that
    // into `task.changed`, since a terminal task leaves no node to read its outcome from.
    const known = new Map<string, { kind: string; status: string; conversationId: number }>()
    const advance = (value: {
      tasks?: Record<
        string,
        { id: number; kind: string; conversationId: number; state: { status: string } }
      >
    }): void => {
      const tasks = value.tasks ?? {}
      const seen = new Set<string>()
      for (const [key, node] of Object.entries(tasks)) {
        seen.add(key)
        const status = node.state.status
        const previous = known.get(key)
        known.set(key, {
          kind: node.kind,
          status,
          conversationId: node.conversationId as unknown as number,
        })
        if (previous?.status === status) continue
        this.#notifier?.broadcast('task.changed', {
          taskId: node.id,
          kind: taskKind(node.kind),
          status,
          conversationId: node.conversationId,
        })
      }
      for (const [key, node] of known) {
        if (seen.has(key)) continue
        known.delete(key)
        this.#notifier?.broadcast('task.changed', {
          taskId: Number(key),
          kind: taskKind(node.kind),
          status: 'terminal',
          conversationId: node.conversationId,
        })
      }
    }
    advance(graph.value as never)
    graph.start(async (call) => {
      if (call.kind === 'value') advance(call.value as never)
    })
    this.#tasks = { detach: () => void graph.stop() }
  }

  /** The Session failed: answer every further request with the reason, and let the Host replace us. */
  fail(error: unknown): void {
    if (this.#failure !== undefined) return
    this.#failure = error
    this.#notifier?.broadcast('warning', {
      kind: 'kernel_failed',
      message: error instanceof Error ? error.message : String(error),
    })
    this.#options.onFailed?.(error)
  }

  /**
   * The gate every request passes before its method is even looked up.
   *
   * A failed Session is not a working kernel, so nothing is answered from it — not `harness.info`, not a
   * write-gated method (`-32009` would say "this build lacks the feature", which is false), not an unknown
   * method (`-32601` says the same), and not `kernel.shutdown`, which must not be able to turn a fatal
   * exit into a clean one.
   */
  gate(): void {
    if (this.#failure === undefined) return
    throw new RpcError(Code.kernelFailed, 'the kernel failed and is being restarted', {
      reason: this.#failure instanceof Error ? this.#failure.message : String(this.#failure),
    })
  }

  /** How many conversations the kernel is observing, for a test that pins the release. */
  get liveObservations(): number {
    return this.#live.size
  }

  /** Every method name of the contract: answered here, or gated with its capability. */
  methodNames(): string[] {
    return [...this.methods.keys(), ...Object.keys(GATED)]
  }

  /** The capability gating a method, for the `-32009` this build answers. */
  gated(method: string): string | undefined {
    const capability = gateOf(method)
    if (capability === undefined) return undefined
    const enabled = this.#options.capabilities[capability] === true
    return enabled ? undefined : capability
  }

  /**
   * Follow one conversation's live run so `conversation.busy` reaches every connection, whether or not any
   * of them has its view open.
   *
   * This is deliberately separate from a view subscription: a client that calls `conversation.unwatch`
   * stops receiving that conversation's views, and must still hear that its run ended. The observation is
   * released only when the conversation is idle and nobody is watching it, which matters on a Host that
   * opens twenty conversations and closes the window.
   */
  async track(id: number): Promise<void> {
    const existing = this.#live.get(id)
    if (existing !== undefined) return
    const conversation = await this.#options.harness.conversation(id as never, context)
    if (conversation === undefined) return
    const observation = await this.#options.harness.observeConversation(
      id as never,
      {
        advance: (call) => {
          if (call.kind === 'value') this.#advance(id, call.value)
        },
        end: () => this.#live.delete(id),
      },
      context,
      { min: 1, max: 2 },
    )
    // A viewer that subscribed while the observation was being taken is kept.
    const viewers = this.#viewers.get(id) ?? new Set<number>()
    this.#live.set(id, { detach: () => observation.detach(), viewers })
    this.#advance(id, observation.value)
  }

  /** A connection now shows this conversation. Idempotent: opening it twice is still one viewer. */
  addViewer(id: number, connectionId: number): void {
    const viewers = this.#viewers.get(id) ?? new Set<number>()
    viewers.add(connectionId)
    this.#viewers.set(id, viewers)
    const live = this.#live.get(id)
    if (live !== undefined) live.viewers = viewers
  }

  /** A connection stopped showing it. An idle conversation nobody watches gives its observation back. */
  releaseViewer(id: number, connectionId: number): void {
    const viewers = this.#viewers.get(id)
    if (viewers !== undefined) {
      viewers.delete(connectionId)
      if (viewers.size === 0) this.#viewers.delete(id)
    }
    this.#releaseIfIdle(id)
  }

  /** Every viewer of a connection is gone: it closed. */
  releaseConnection(connectionId: number): void {
    for (const [id, viewers] of [...this.#viewers]) {
      if (!viewers.delete(connectionId)) continue
      if (viewers.size === 0) this.#viewers.delete(id)
      this.#releaseIfIdle(id)
    }
  }

  #releaseIfIdle(id: number): void {
    if ((this.#viewers.get(id)?.size ?? 0) > 0) return
    if (this.#busy.get(id)?.run != null) return
    const live = this.#live.get(id)
    if (live === undefined) return
    live.detach()
    this.#live.delete(id)
    this.#busy.delete(id)
  }

  /**
   * Read a conversation's live state and tell the clients about it.
   *
   * A view observer folds what it missed: a run that starts and ends between two advances reaches a client
   * as one call, and a fast run — or an immediate answer — would never show as busy. So the admission that
   * starts a run asks the live document directly instead of waiting to be told.
   */
  async refreshBusy(id: number): Promise<void> {
    const live = await this.#options.harness.snapshot(LiveDoc, id as never, context)
    this.#advance(id, { run: live?.run ?? null, runFailure: live?.runFailure } as never)
  }

  async close(): Promise<void> {
    this.#closing = true
    for (const live of this.#live.values()) live.detach()
    this.#live.clear()
    this.#tasks?.detach()
  }

  #advance(id: number, source: ConversationView | { run: unknown; runFailure?: unknown }): void {
    const live =
      'docs' in source
        ? (source.docs['pi.live'] as { run?: unknown; runFailure?: unknown } | undefined)
        : source
    const run = live?.run ?? null
    const runFailure = live?.runFailure
    const previous = this.#busy.get(id)
    const same =
      previous !== undefined &&
      JSON.stringify(previous.run) === JSON.stringify(run) &&
      JSON.stringify(previous.runFailure) === JSON.stringify(runFailure)
    if (same) return
    this.#busy.set(id, { run, runFailure })
    const payload: Record<string, unknown> = { id, run }
    if (runFailure !== undefined) payload['runFailure'] = runFailure
    this.#notifier?.broadcast('conversation.busy', payload)
    // A run that ended on a conversation nobody watches is the last thing its observation was for.
    if (run === null) this.#releaseIfIdle(id)
  }

  // ── harness.info ────────────────────────────────────────────────────────────────────────────────

  #info(): Record<string, unknown> {
    const { config } = this.#options
    const providers = config.providers.map((provider) => ({
      id: provider.id,
      name: provider.name ?? provider.id,
      models: provider.models.map((model) => model.id),
    }))
    // The fields the fixture and the Go client's ModelInfo name, not pi-ai's: `maxOutput` and a
    // `thinking` list. A Go struct with different tags decodes a missing field as a zero value without
    // complaint, so the model picker would simply have no context window and no thinking levels.
    const models = config.providers.flatMap((provider) =>
      provider.models.map((model) => ({
        provider: provider.id,
        id: model.id,
        name: model.name ?? model.id,
        contextWindow: model.contextWindow,
        maxOutput: model.maxTokens,
        input: model.input ?? ['text'],
        thinking:
          model.thinking ?? (model.reasoning === true ? ['off', 'low', 'medium', 'high'] : ['off']),
      })),
    )
    return {
      version: this.#options.version,
      limits: {
        viewPushesPerSecond: this.#options.config.viewPushesPerSecond ?? 5,
        viewWindowDefault: { min: DEFAULT_WINDOW.min, max: DEFAULT_WINDOW.max },
        // A page larger than this comes back at this size with a cursor: page through it.
        maxEntriesPerPage: MAX_PAGE,
        maxConversationsPerPage: MAX_PAGE,
      },
      kernel: {
        name: '@droi/kernel',
        version: this.#options.version,
        pi: { durable: this.#options.piVersion },
      },
      capabilities: this.#options.capabilities,
      providers,
      models,
      extensions:
        this.#options.tools.length === 0
          ? []
          : [{ name: 'coding', tools: this.#options.tools, sections: ['environment', 'cwd'] }],
    }
  }

  // ── conversation.create / list / open / unwatch ─────────────────────────────────────────────────

  /**
   * Create a conversation.
   *
   * The agent is resolved from three places, each overriding the one before: the config file's `agent`
   * defaults, then the top-level `cwd`/`instructions` the protocol allows, then the `agent` object. The
   * top level is not a convenience to ignore — the Go client sends `cwd` there — and the final `cwd` is
   * also what the index records as the conversation's workspace, so filtering by workspace agrees with
   * where the tools actually run.
   */
  async #create(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const requestId = optionalString(params, 'requestId')
    const kind = optionalString(params, 'kind')
    const resolved = this.#resolveCreateAgent(params)
    const fingerprint = fingerprintOf({
      kind,
      ...(resolved.fingerprint as Record<string, unknown>),
    })
    if (requestId !== undefined) {
      // Two creates racing on one key must converge on one conversation, not one of them failing on the
      // unique index. A single process means a promise per key is enough.
      const previous = this.#creating.get(requestId)
      if (previous !== undefined) await previous.catch(() => undefined)
    }
    const existing =
      requestId === undefined ? undefined : await this.#options.indexer.byRequestId(requestId)
    if (existing !== undefined) {
      // The same key with different params is a conflict, not a silent answer with the first result.
      if (existing.request_fingerprint !== null && existing.request_fingerprint !== fingerprint) {
        throw conflict(requestId!)
      }
      return { conversationId: existing.id, view: await this.#view(existing.id, DEFAULT_WINDOW) }
    }
    const run = async (): Promise<number> => {
      const createdAt = Date.now()
      const meta: ConversationMeta = {
        ...(requestId === undefined ? {} : { requestId, requestFingerprint: fingerprint }),
        ...(kind === undefined ? {} : { kind }),
        ...(resolved.cwd === undefined ? {} : { workspace: resolved.cwd }),
        createdAt,
      }
      const conversation = await this.#options.harness.createConversation(
        {
          ownership: { kind: 'ownerless' },
          agent: resolved.change,
          // The transcript entry is written in the same commit as the conversation, so it cannot be lost
          // between Pi storing the conversation and the index catching up. It is the record; the row in
          // `droi_conversations` is the fast path, written afterwards and rebuilt from this entry whenever
          // it is missing.
          init: async (tx, conversationId) => {
            await tx.appendEntry(conversationId, { kind: 'droi.meta', data: meta })
          },
        },
        context,
      )
      const id = conversation.id as unknown as number
      await this.#options.indexer.touch(id, createdAt, {
        ...(kind === undefined ? {} : { kind }),
        ...(resolved.cwd === undefined ? {} : { workspace: resolved.cwd }),
        ...(requestId === undefined ? {} : { requestId, requestFingerprint: fingerprint }),
        createdAt,
      })
      await this.track(id)
      info('conversation created', { id, kind, requestId })
      return id
    }
    const id = requestId === undefined ? await run() : await this.#serialise(requestId, run)
    return { conversationId: id, view: await this.#view(id, DEFAULT_WINDOW) }
  }

  /**
   * What a conversation already knows about one `requestId`: its fingerprint and the submission it
   * produced.
   *
   * The table answers first. When it does not — an index rebuilt from scratch, or a row lost — the
   * transcript does, from the `droi.request` entry the admission wrote in its own commit. That is why the
   * answer survives a crash between Pi accepting the submission and any index catching up.
   */
  async #requestMemo(
    id: number,
    requestId: string,
  ): Promise<{ fingerprint: string; submissionId?: number } | undefined> {
    const row = await this.#options.indexer.request(id, requestId)
    if (row !== undefined) return row
    const conversation = await this.#options.harness.conversation(id as never, context)
    if (conversation === undefined) return undefined
    let cursor: never | undefined
    do {
      const page = await conversation.entries({ order: 'descending' }, MAX_PAGE, cursor, context)
      for (const entry of page.items) {
        // The inherited transcript of a fork carries its parent's memos; they belong to the parent.
        if ((entry.conversationId as unknown as number) !== id) continue
        if (entry.kind !== 'droi.request') continue
        const data = entry.data as {
          requestId?: unknown
          fingerprint?: unknown
          submissionId?: unknown
        }
        if (data?.requestId !== requestId) continue
        return {
          fingerprint: String(data.fingerprint ?? ''),
          ...(typeof data.submissionId === 'number' ? { submissionId: data.submissionId } : {}),
        }
      }
      if (page.items.length === 0) return undefined
      cursor = page.next as never
    } while (cursor !== undefined)
    return undefined
  }

  /** Run `fn` as the only create for `requestId` at a time, and remember it until it settles. */
  async #serialise(requestId: string, fn: () => Promise<number>): Promise<number> {
    const run = fn()
    this.#creating.set(requestId, run)
    try {
      return await run
    } finally {
      if (this.#creating.get(requestId) === run) this.#creating.delete(requestId)
    }
  }

  async #list(params: Record<string, unknown>): Promise<unknown> {
    this.#readable()
    const limit = optionalNumber(params, 'limit')
    const page = await this.#options.indexer.list({
      kind: optionalString(params, 'kind'),
      archived: optionalBoolean(params, 'archived'),
      workspace: optionalString(params, 'workspace'),
      text: optionalString(params, 'query'),
      cursor: parseListCursor(optionalObject(params, 'cursor')),
      limit: limit === undefined ? undefined : Math.min(limit, MAX_PAGE),
    })
    const items = page.items.map(wireSummary)
    return page.next === undefined ? { items } : { items, cursor: page.next }
  }

  async #open(params: Record<string, unknown>, connection: Connection): Promise<unknown> {
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const window = parseWindow(optionalObject(params, 'window'))
    const mode = parseMode(params['subscribe'])
    // One watch answers the call and feeds the subscription. Two watches would be two mounts, and a
    // commit between them would leave a delta subscriber's baseline one step behind the ops it gets.
    const watch = await conversation.watch(context, window)
    const view = watch.value
    // A viewer writes nothing, not even "seen": its data directory is another process's.
    if (!this.#options.config.readOnly) await this.#options.indexer.markViewed(id)
    // The connection may have gone while this call was reading. Registering then would leave a
    // subscription no close will ever release, and a watch on a conversation nobody sees.
    if (this.#closing || !connection.open) {
      await watch.stop()
      return { view }
    }
    const pusher = new ViewWindow({
      connection,
      conversationId: id,
      mode,
      perSecond: this.#options.config.viewPushesPerSecond ?? 5,
    })
    // A second open on one connection replaces its window: stop the old watch first, so the viewer count
    // never sees a release for a viewer that is still there. The socket can close while that awaits —
    // its close handler releases this connection's viewers — so the state is checked again after it.
    await connection.unsubscribe(id)
    if (this.#closing || !connection.open) {
      await watch.stop()
      return { view }
    }
    connection.subscribe(id, async () => {
      pusher.stop()
      await watch.stop()
    })
    this.addViewer(id, connection.id)
    watch.start(async (call) => {
      if (call.kind === 'value') {
        this.#advance(id, call.value)
        pusher.advance(call.value, call.ops, call.retired)
      } else {
        pusher.fail(call.error)
      }
    })
    void watch.closed.then((end) => {
      if (end.kind === 'failed') pusher.fail(end.error)
    })
    await this.track(id)
    return { view }
  }

  async #unwatch(params: Record<string, unknown>, connection: Connection): Promise<unknown> {
    const id = requiredNumber(params, 'id')
    await connection.unsubscribe(id)
    this.releaseViewer(id, connection.id)
    return {}
  }

  // ── conversation.submit / abort / submission.abort ──────────────────────────────────────────────

  async #submit(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const wantsContinue = optionalBoolean(params, 'continue')
    const requestId = optionalString(params, 'requestId')
    const whenBusy = optionalString(params, 'whenBusy')
    const whenIdle = optionalString(params, 'whenIdle')
    if (wantsContinue !== true && params['input'] === undefined) {
      throw invalidParams([{ path: 'input', message: 'input or continue is required' }])
    }
    if (whenBusy !== undefined && !['steer', 'followUp', 'reject'].includes(whenBusy)) {
      throw invalidParams([{ path: 'whenBusy', message: 'steer, followUp or reject' }])
    }
    if (whenIdle !== undefined && whenIdle !== 'reject') {
      throw invalidParams([{ path: 'whenIdle', message: 'reject' }])
    }
    const content = wantsContinue === true ? undefined : parseInput(params['input'])
    // A repeated key with the same params answers the first submission; with other params it is a conflict.
    const fingerprint = fingerprintOf({
      continue: wantsContinue === true,
      input: content ?? null,
      whenBusy: whenBusy ?? null,
      whenIdle: whenIdle ?? null,
    })
    if (requestId !== undefined) {
      const known = await this.#requestMemo(id, requestId)
      if (known !== undefined) {
        if (known.fingerprint !== fingerprint) throw conflict(requestId)
        if (known.submissionId !== undefined) return { submissionId: known.submissionId }
      }
    }
    const draft = (
      wantsContinue === true
        ? { type: 'continue' as const, requestId }
        : {
            type: 'input' as const,
            content: content as never,
            requestId,
            whenBusy: whenBusy as 'steer' | 'followUp' | 'reject' | undefined,
            whenIdle: whenIdle as 'reject' | undefined,
          }
    ) as never
    // Admitted in a commit of our own, so the fingerprint is stored in the same transaction as the
    // submission. A crash between the two would otherwise leave Pi knowing a submission the kernel does
    // not, and a retry would be admitted twice instead of answered from the memo.
    let submissionId: number
    try {
      submissionId = (await this.#options.harness.commit(async (tx) => {
        const admitted = await conversation.submit(tx, draft)
        if (requestId !== undefined) {
          const admittedId = admitted as unknown as number
          // The transcript entry is the record, the table is the fast path; both are in this commit, so
          // neither can outlive the other. A rebuilt index finds them here.
          await tx.appendEntry(id as never, {
            kind: 'droi.request',
            data: { requestId, fingerprint, submissionId: admittedId },
          })
        }
        return admitted as unknown as number
      }, context)) as number
    } catch (error) {
      throw submissionError(error, id)
    }
    if (requestId !== undefined) {
      // A cache of what the transcript now records: losing this write costs one scan, not a duplicate.
      await this.#options.indexer
        .rememberRequest(id, requestId, fingerprint, submissionId)
        .catch(() => undefined)
    }
    await this.#options.indexer.touch(id, Date.now(), {})
    await this.track(id)
    await this.refreshBusy(id)
    return { submissionId }
  }

  async #abort(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    // The submissions this abort may settle, remembered before it runs: afterwards they are terminal, and the
    // end of a turn can only be read at the moment it ends.
    const live = await this.#liveSubmissions(id)
    // Protocol `background?` has no Pi counterpart yet (recorded in the contract's change log).
    await conversation.abort(context)
    await this.#recordEnds(id, live)
    await this.refreshBusy(id)
    return {}
  }

  /** Submissions of a conversation that have not settled yet: the ones an abort can end. */
  async #liveSubmissions(id: number): Promise<number[]> {
    const page = await this.#options.storage.scanSubmissions(
      { conversationId: id as never, order: 'ascending' },
      MAX_PAGE,
      undefined,
      context,
    )
    const live: number[] = []
    for (const record of page.items) {
      if (record.status === 'queued' || record.status === 'placed') {
        live.push(record.id as unknown as number)
      }
    }
    return live
  }

  /**
   * Fix where each of `submissions` ended, now that they have.
   *
   * Only for those that ended without an answer: a done submission's end is its `answer`, which Pi keeps. The
   * value is the conversation's head entry at this instant — the last entry the turn could have written; a
   * turn's entries do not carry its submission, so nothing later can be attributed back to it.
   */
  async #recordEnds(id: number, submissions: readonly number[]): Promise<void> {
    if (submissions.length === 0) return
    const head = await this.#headEntry(id)
    if (head === 0) return
    for (const submissionId of submissions) {
      const record = await this.#options.storage.submission(submissionId as never, context)
      if (record === undefined || record.status !== 'unanswered') continue
      if (!('entry' in record) || record.entry === undefined) continue
      await this.#options.indexer.recordSubmissionEnd(id, submissionId, head)
    }
  }

  /** The conversation's newest entry, or 0 when it holds none. */
  async #headEntry(id: number): Promise<number> {
    const page = await this.#options.storage.scanEntries(
      { conversationId: id as never, order: 'descending' },
      1,
      undefined,
      context,
    )
    const newest = page.items[0]
    return newest === undefined ? 0 : (newest.id as unknown as number)
  }

  async #submissionGet(params: Record<string, unknown>): Promise<unknown> {
    this.#readable()
    const id = requiredNumber(params, 'id')
    const submissionId = requiredNumber(params, 'submissionId')
    const record = await this.#options.storage.submission(submissionId as never, context)
    if (record === undefined || (record.conversationId as unknown as number) !== id) {
      throw new RpcError(Code.submissionNotFound, 'submission not found', {
        conversationId: id,
        submissionId,
      })
    }
    const entry = 'entry' in record && record.entry !== undefined ? record.entry : undefined
    // `last` is where this submission's turn ended, fixed when it ended:
    //
    // - a done submission's end is Pi's `answer`, which is durable;
    // - one that ended without an answer — aborted, withdrawn — has the value the Kernel recorded at that
    //   moment (see `Indexer.recordSubmissionEnd`). A submission settled before the Kernel could record it
    //   answers its own entry, which under-reports rather than borrowing the next turn's reply.
    const last =
      record.status === 'done'
        ? record.answer
        : entry === undefined
          ? undefined
          : ((await this.#options.indexer.submissionEnd(id, submissionId)) ?? entry)
    return {
      submissionId,
      status: record.status,
      ...(entry === undefined ? {} : { entry }),
      ...(record.status === 'done' ? { answer: record.answer } : {}),
      ...(last === undefined ? {} : { last }),
      ...(record.status === 'unanswered' ? { reason: record.reason } : {}),
    }
  }
  async #abortSubmission(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const submissionId = requiredNumber(params, 'submissionId')
    const status = await this.#options.harness.abortSubmission(
      submissionId as never,
      context,
      id as never,
    )
    // A withdrawn submission that had been placed ends here; where it ended is known only now.
    if (status === 'aborted') await this.#recordEnds(id, [submissionId])
    switch (status) {
      case 'aborted':
        return { status }
      case 'already_placed':
        // The input is already in the transcript; the contract names that state, rather than calling it a
        // successful abort.
        throw new RpcError(Code.submissionPlaced, 'the submission already entered the transcript', {
          submissionId,
        })
      case 'settled':
        throw new RpcError(Code.submissionSettled, 'the submission already settled', {
          submissionId,
          status: 'settled',
        })
      case 'not_found':
        throw new RpcError(Code.submissionNotFound, 'submission not found', {
          conversationId: id,
          submissionId,
        })
    }
  }

  // ── conversation.configure / compact / fork ────────────────────────────────────────────────────

  async #configure(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const change = requiredObject(params, 'change')
    const resolved = this.#agentChange(change, 'change')
    await conversation.configure(resolved, context)
    const agent = await conversation.agent(context)
    return { agent: wireAgent(agent) }
  }

  async #compact(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const taskId = await conversation.compact(optionalString(params, 'instructions'), context)
    return { taskId }
  }

  async #fork(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    // `at` is required by the contract, and validated here rather than inside Pi: Pi's message for a
    // missing entry ("Entry 0 is not visible") reached the client as an internal error, which says nothing
    // about what to fix.
    const at = requiredEntry(params, 'at')
    const conversation = await this.#conversation(id)
    // Asked by ID, not by page: a valid target may be older than the newest page, and the newest page is
    // not a window onto the whole transcript.
    const probe = await conversation.entries(
      { order: 'ascending', minEntryId: at as EntryId, maxEntryId: at as EntryId },
      1,
      undefined,
      context,
    )
    if (probe.items.length === 0) {
      throw invalidParams([{ path: 'at', message: `entry ${at} is not in this conversation` }])
    }
    const createdAt = Date.now()
    const forked = await conversation.fork(
      at as EntryId,
      {
        ownership: { kind: 'ownerless' },
        // A fork's identity is its own. The parent's `droi.meta` is in the inherited transcript, and taking
        // it would give the fork the parent's requestId — which collides on the unique index at the next
        // start — and the parent's submissions. So the fork writes its own, with the link back.
        init: async (tx, conversationId) => {
          await tx.appendEntry(conversationId, {
            kind: 'droi.meta',
            data: { parentId: id, createdAt } satisfies ConversationMeta,
          })
        },
      },
      context,
    )
    const forkedId = forked.id as unknown as number
    await this.#options.indexer.touch(forkedId, createdAt, { parentId: id, createdAt })
    await this.track(forkedId)
    return { conversationId: forkedId, view: await this.#view(forkedId, DEFAULT_WINDOW) }
  }

  // ── conversation.entries / usage ───────────────────────────────────────────────────────────────

  async #entries(params: Record<string, unknown>): Promise<unknown> {
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const query = optionalObject(params, 'query') ?? {}
    const limit = Math.min(Math.max(optionalNumber(params, 'limit') ?? 50, 1), MAX_PAGE)
    const cursor = optionalObject(params, 'cursor')
    const order = query['order'] === undefined ? 'descending' : query['order']
    if (order !== 'ascending' && order !== 'descending') {
      throw invalidParams([{ path: 'query.order', message: 'ascending or descending' }])
    }
    const page = await conversation.entries(
      {
        order,
        minEntryId: optionalNumber(query, 'minEntryId') as EntryId | undefined,
        maxEntryId: optionalNumber(query, 'maxEntryId') as EntryId | undefined,
      },
      limit,
      cursor as never,
      context,
    )
    const items = page.items.map((entry) => wireEntry(entry))
    return page.next === undefined ? { items } : { items, cursor: page.next }
  }

  async #usageOf(params: Record<string, unknown>): Promise<unknown> {
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const usage =
      (await this.#options.harness.snapshot(UsageDoc, conversation.id, context)) ??
      ({ models: {}, tools: {} } satisfies UsageState)
    const view = await conversation.context(context)
    const agent = await conversation.agent(context)
    const model =
      agent.model === undefined
        ? undefined
        : this.#options.models.getModel(agent.model.provider, agent.model.modelId)
    const used = approximateTokens(view.messages)
    const limit = model?.contextWindow ?? 0
    const reserve = 16_000
    return {
      models: usage.models,
      tools: usage.tools,
      context: {
        head: view.head?.id ?? null,
        entries: view.entries.length,
        used,
        limit,
        compactionAt: limit === 0 ? null : Math.max(limit - reserve, 0),
      },
    }
  }

  // ── task.abort ────────────────────────────────────────────────────────────────────────────────

  async #abortTask(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const taskId = requiredNumber(params, 'taskId')
    // `background?` has no Pi counterpart: `abortTask()` marks the task and its ordinary owned work.
    const task = await this.#options.harness.getTask(taskId as never, context)
    if (task === undefined) {
      throw new RpcError(Code.taskNotFound, 'task not found', { taskId })
    }
    const status = await this.#options.harness.abortTask(taskId as never, context)
    return { status }
  }

  // ── index.update ──────────────────────────────────────────────────────────────────────────────

  async #indexUpdate(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const change: { title?: string | null; archived?: boolean; kind?: string | null } = {}
    if ('title' in params) {
      const title = params['title']
      change.title =
        title === null ? null : typeof title === 'string' ? title : invalidTitle('title')
    }
    if ('kind' in params) {
      const kind = params['kind']
      change.kind = kind === null ? null : typeof kind === 'string' ? kind : invalidTitle('kind')
    }
    const archived = optionalBoolean(params, 'archived')
    if (archived !== undefined) change.archived = archived
    await this.#options.indexer.update(id, change)
    const row = await this.#options.indexer.conversation(id)
    this.#notifier?.broadcast('index.changed', {
      id,
      ...(row?.title === undefined ? {} : { title: row.title }),
      ...(row?.archived_at === undefined ? {} : { archived: row.archived_at !== null }),
      lastActivityAt: row?.last_activity_at ?? Date.now(),
    })
    return {}
  }

  // ── model.complete ──────────────────────────────────────────────────────────────────────────

  /**
   * One model call with no conversation behind it, for a caller that wants an answer and not a session
   * (Memory's summary is the first). M4 implements it; the capability is false until then.
   */
  #modelComplete(_params: Record<string, unknown>): never {
    throw unsupported('models')
  }

  // ── kernel.shutdown ──────────────────────────────────────────────────────────────────────────

  /**
   * Stop taking work, close the harness and then the storage, and exit.
   *
   * The reply is written first: the method answers `{"status":"closing"}` and the closing starts on the
   * next tick, so a client that waits for its answer is not cut off by the process going away. Every later
   * write is refused, because `#closing` is already set.
   */
  #shutdown(): Record<string, unknown> {
    // Belt and braces with the gate: a failed kernel keeps its exit status 3, whatever a client asks.
    this.gate()
    if (this.#closing) return { status: 'closing' }
    this.#closing = true
    setTimeout(() => {
      void this.#options
        .shutdown()
        .then(() => process.exit(0))
        .catch(() => process.exit(1))
    }, 10)
    return { status: 'closing' }
  }

  // ── helpers ───────────────────────────────────────────────────────────────────────────────────

  /** Refuse a request this build cannot serve: a dead Session, a closing kernel, or a read-only one. */
  #writable(): void {
    if (this.#failure !== undefined) {
      throw new RpcError(Code.kernelFailed, 'the kernel failed and is being restarted', {
        reason: this.#failure instanceof Error ? this.#failure.message : String(this.#failure),
      })
    }
    if (this.#closing) throw new RpcError(Code.readOnly, 'the kernel is shutting down')
    if (!this.#options.config.readOnly) return
    throw new RpcError(Code.readOnly, 'the kernel is read-only')
  }

  /**
   * Reads are allowed on a read-only kernel and refused once the Session has failed: a failed kernel is
   * not a working one, and answering `harness.info` from it would tell a client it is fine.
   */
  #readable(): void {
    if (this.#failure === undefined) return
    throw new RpcError(Code.kernelFailed, 'the kernel failed and is being restarted', {
      reason: this.#failure instanceof Error ? this.#failure.message : String(this.#failure),
    })
  }

  async #conversation(id: number): Promise<Conversation> {
    this.#readable()
    const conversation = await this.#options.harness.conversation(id as never, context)
    if (conversation === undefined) throw notFound(id)
    return conversation
  }

  async #view(id: number, window: ConversationWindow): Promise<ConversationView> {
    const conversation = await this.#conversation(id)
    const watch = await conversation.watch(context, window)
    const value = watch.value
    await watch.stop()
    return value
  }

  /**
   * The agent a create asks for: the config's defaults, then the request's top-level fields, then its
   * `agent` object. The result is one change, applied in the creating commit.
   */
  #resolveCreateAgent(params: Record<string, unknown>): {
    change: AgentChange
    cwd: string | undefined
    fingerprint: unknown
  } {
    const defaults = this.#options.config.agent
    const top = this.#agentChange(params, 'create')
    const nested =
      params['agent'] !== undefined
        ? this.#agentChange(requiredObject(params, 'agent'), 'agent')
        : {}
    const change: AgentChange = { ...defaultsAsChange(defaults), ...top, ...nested }
    // Droi's own `kind` and the resolved agent decide whether two creates are the same request.
    const cwd = typeof change.cwd === 'string' ? change.cwd : undefined
    return {
      change,
      cwd,
      fingerprint: {
        model: change.model ?? null,
        thinkingLevel: change.thinkingLevel ?? null,
        cwd: cwd ?? null,
        instructions: change.instructions ?? null,
        maxTurns: change.maxTurns ?? null,
        // Every field that changes what the conversation will do belongs in the fingerprint: two creates
        // that differ in their tools are two different requests, and answering the second with the first
        // conversation would silently drop the tools it asked for.
        extensions: namesOf(change.extensions),
        modelTools: namesOf(change.modelTools),
      },
    }
  }

  /**
   * The wire form of an agent change. Extensions and tools cross as names, which the registry resolves
   * here; a name this build does not have is an invalid request rather than a silent no-op.
   */
  #agentChange(source: Record<string, unknown>, prefix: string): AgentChange {
    const cwd = source['cwd']
    const workspace = source['workspace']
    if (cwd !== undefined && workspace !== undefined && cwd !== workspace) {
      throw invalidParams([{ path: `${prefix}.cwd`, message: 'cwd and workspace disagree' }])
    }
    const instructions = source['instructions']
    const model = source['model']
    const thinking = source['thinkingLevel']
    const maxTurns = source['maxTurns']
    if (cwd !== undefined && cwd !== null && typeof cwd !== 'string') {
      throw invalidParams([{ path: `${prefix}.cwd`, message: 'a string is required' }])
    }
    if (workspace !== undefined && workspace !== null && typeof workspace !== 'string') {
      throw invalidParams([{ path: `${prefix}.workspace`, message: 'a string is required' }])
    }
    if (instructions !== undefined && instructions !== null && typeof instructions !== 'string') {
      throw invalidParams([{ path: `${prefix}.instructions`, message: 'a string is required' }])
    }
    if (thinking !== undefined && thinking !== null && typeof thinking !== 'string') {
      throw invalidParams([{ path: `${prefix}.thinkingLevel`, message: 'a string is required' }])
    }
    if (
      maxTurns !== undefined &&
      maxTurns !== null &&
      (typeof maxTurns !== 'number' || !Number.isInteger(maxTurns) || maxTurns < 1)
    ) {
      throw invalidParams([
        { path: `${prefix}.maxTurns`, message: 'an integer of 1 or more is required' },
      ])
    }
    const path = cwd === undefined ? workspace : cwd
    return {
      ...(path === undefined ? {} : { cwd: path as string | null }),
      ...(instructions === undefined ? {} : { instructions: instructions as string | null }),
      ...(model === undefined ? {} : { model: model === null ? null : parseModel(model, prefix) }),
      ...(thinking === undefined ? {} : { thinkingLevel: thinking as never }),
      ...(maxTurns === undefined ? {} : { maxTurns: maxTurns as number | null }),
      ...(source['extensions'] === undefined
        ? {}
        : { extensions: this.#resolveExtensions(source['extensions'], prefix) }),
      ...(source['modelTools'] === undefined
        ? {}
        : { modelTools: this.#resolveModelTools(source['modelTools'], prefix) }),
    }
  }

  #resolveExtensions(value: unknown, prefix: string): AgentChange['extensions'] {
    const registry = this.#options.registry
    const byName = (name: unknown) => {
      const extension = typeof name === 'string' ? registry.snapshot().extension(name) : undefined
      if (extension === undefined) {
        throw invalidParams([
          { path: `${prefix}.extensions`, message: `no such extension: ${String(name)}` },
        ])
      }
      return extension
    }
    if (value === null) return null
    if (Array.isArray(value)) return value.map(byName)
    if (typeof value === 'object') {
      const add = (value as { add?: unknown }).add
      const remove = (value as { remove?: unknown }).remove
      const result: { add?: never[]; remove?: never[] } = {}
      if (Array.isArray(add)) (result as { add?: unknown }).add = add.map(byName)
      if (Array.isArray(remove)) (result as { remove?: unknown }).remove = remove.map(byName)
      return result as AgentChange['extensions']
    }
    throw invalidParams([
      { path: `${prefix}.extensions`, message: 'names, or {add, remove}, are required' },
    ])
  }

  #resolveModelTools(value: unknown, prefix: string): AgentChange['modelTools'] {
    const snapshot = this.#options.registry.snapshot()
    const byName = (name: unknown) => {
      const found = snapshot.tools().find((entry) => entry.tool.name === name)
      if (found === undefined) {
        throw invalidParams([
          { path: `${prefix}.modelTools`, message: `no such tool: ${String(name)}` },
        ])
      }
      return found.tool
    }
    if (value === null) return null
    if (Array.isArray(value)) return value.map(byName)
    if (typeof value === 'object') {
      const add = (value as { add?: unknown }).add
      const remove = (value as { remove?: unknown }).remove
      const result: Record<string, unknown> = {}
      if (Array.isArray(add)) result['add'] = add.map(byName)
      if (Array.isArray(remove)) result['remove'] = remove.map(byName)
      return result as AgentChange['modelTools']
    }
    throw invalidParams([
      { path: `${prefix}.modelTools`, message: 'names, or {add, remove}, are required' },
    ])
  }
}

// ── free functions ──────────────────────────────────────────────────────────────────────────────

/**
 * A task kind without Pi's namespace: `pi.compaction` crosses the wire as `compaction`, which is what the
 * contract's fixture says and what a client switches on. Pi reserves the `pi.` prefix for itself and Droi
 * adds `droi.` kinds, so the prefix carries no information a client needs.
 */
function taskKind(kind: string): string {
  return kind.startsWith('pi.') ? kind.slice(3) : kind
}

/**
 * An agent change's extensions or tools as names, for a fingerprint: they cross the protocol as names, so
 * the comparison is of names and not of registry objects.
 */
function namesOf(value: unknown): unknown {
  if (value === null || value === undefined) return value ?? null
  if (Array.isArray(value))
    return value.map((item) => (item as { name?: string }).name ?? String(item))
  if (typeof value === 'object') {
    const record = value as { add?: unknown[]; remove?: unknown[] }
    return {
      add: (record.add ?? []).map((item) => (item as { name?: string }).name ?? String(item)),
      remove: (record.remove ?? []).map((item) => (item as { name?: string }).name ?? String(item)),
    }
  }
  return value
}

/** The config file's default agent, as a change the creating commit can apply first. */
function defaultsAsChange(defaults: AgentDefaults | undefined): AgentChange {
  if (defaults === undefined) return {}
  return {
    ...(defaults.provider === undefined || defaults.modelId === undefined
      ? {}
      : { model: { provider: defaults.provider, modelId: defaults.modelId } }),
    ...(defaults.thinkingLevel === undefined
      ? {}
      : { thinkingLevel: defaults.thinkingLevel as never }),
    ...(defaults.cwd === undefined ? {} : { cwd: defaults.cwd }),
    ...(defaults.instructions === undefined ? {} : { instructions: defaults.instructions }),
  }
}

/** A stable string for "the same request": key order does not matter, so a client may resend freely. */
export function fingerprintOf(value: unknown): string {
  return JSON.stringify(normalize(value))
}

function normalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalize)
  if (typeof value !== 'object' || value === null) return value
  const record = value as Record<string, unknown>
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(record).sort()) {
    if (record[key] === undefined) continue
    out[key] = normalize(record[key])
  }
  return out
}

/**
 * Every method of the contract this build does not implement, and the capability that says so.
 *
 * The whole surface is named here rather than inferred from a prefix, so the list can be checked against
 * the fixtures: a method the contract has and this table forgets would answer `-32601 method not found`,
 * which reads like a typo instead of a build that lacks the feature.
 */
const GATED: Readonly<Record<string, string>> = {
  'model.complete': 'models',
  'process.stop': 'processes',
  'approval.respond': 'approvals',
  'ask.respond': 'asks',
  'mcp.set': 'mcp',
  'mcp.authorize': 'mcp',
  'mcp.authorize.cancel': 'mcp',
  'skills.setDisabled': 'skills',
}

function gateOf(method: string): string | undefined {
  return GATED[method]
}

function invalidTitle(field: string): never {
  throw invalidParams([{ path: field, message: 'a string or null is required' }])
}

function parseModel(value: unknown, prefix: string): ModelRef {
  if (typeof value !== 'object' || value === null) {
    throw invalidParams([{ path: `${prefix}.model`, message: 'provider and modelId are required' }])
  }
  const model = value as { provider?: unknown; modelId?: unknown; id?: unknown }
  const provider = model.provider
  const modelId = model.modelId ?? model.id
  if (
    typeof provider !== 'string' ||
    provider === '' ||
    typeof modelId !== 'string' ||
    modelId === ''
  ) {
    throw invalidParams([{ path: `${prefix}.model`, message: 'provider and modelId are required' }])
  }
  return { provider, modelId }
}

/** A positive integer entry ID: the value Pi takes, checked before it is read as one. */
function requiredEntry(params: Record<string, unknown>, key: string): number {
  const value = params[key]
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 1) {
    throw invalidParams([{ path: key, message: 'an entry ID of 1 or more is required' }])
  }
  return value
}

/** The list cursor as the contract spells it: `{"before": <conversationId>}`. */
export function parseListCursor(
  value: Record<string, unknown> | undefined,
): { before?: number } | undefined {
  if (value === undefined) return undefined
  const before = value['before']
  if (before === undefined || before === null) return undefined
  if (typeof before !== 'number' || !Number.isInteger(before) || before < 1) {
    throw invalidParams([{ path: 'cursor.before', message: 'a conversation ID is required' }])
  }
  return { before }
}

/** `input` as Pi takes it: a string, or text and image blocks. */
function parseInput(value: unknown): unknown {
  if (typeof value === 'string') return value
  if (!Array.isArray(value)) {
    throw invalidParams([
      { path: 'input', message: 'a string or an array of content blocks is required' },
    ])
  }
  for (const [index, block] of value.entries()) {
    if (typeof block !== 'object' || block === null) {
      throw invalidParams([{ path: `input.${index}`, message: 'a content block is required' }])
    }
    const type = (block as { type?: unknown }).type
    if (type !== 'text' && type !== 'image') {
      throw invalidParams([{ path: `input.${index}.type`, message: 'text or image' }])
    }
  }
  return value
}

/** Pi's two admission refusals, as the protocol spells them. */
function submissionError(error: unknown, conversationId: number): unknown {
  const name = error instanceof Error ? error.name : ''
  if (name === 'ConversationBusy') {
    return new RpcError(Code.conversationBusy, 'conversation is busy', { conversationId })
  }
  if (name === 'ConversationIdle') {
    return new RpcError(Code.conversationIdle, 'conversation is idle', { conversationId })
  }
  return error
}

/** One conversation, as the list shows it. */
function wireSummary(row: ConversationRow): Record<string, unknown> {
  return {
    id: row.id,
    title: row.title,
    kind: row.kind,
    workspace: row.workspace,
    archived: row.archived_at !== null,
    createdAt: row.created_at,
    lastActivityAt: row.last_activity_at,
    viewedAt: row.viewed_at,
    preview: row.preview,
    ...(row.parent_id === null ? {} : { parentId: row.parent_id }),
  }
}

function wireEntry(entry: EntryRecord): Record<string, unknown> {
  return {
    id: entry.id,
    conversationId: entry.conversationId,
    kind: entry.kind,
    ...(entry.model === undefined ? {} : { model: entry.model }),
    ...(entry.data === undefined ? {} : { data: entry.data }),
    ...(entry.head === undefined ? {} : { head: entry.head }),
    ...(entry.edits === undefined ? {} : { edits: entry.edits }),
    ...(entry.byTaskId === undefined ? {} : { byTaskId: entry.byTaskId }),
  }
}

/** The resolved agent, with extensions and tools as names. */
function wireAgent(agent: {
  model?: ModelRef
  thinkingLevel: string
  extensions: readonly { name: string }[]
  tools: readonly { name: string }[]
  instructions?: string
  cwd?: string
  maxTurns?: number
}): Record<string, unknown> {
  return {
    ...(agent.model === undefined ? {} : { model: agent.model }),
    thinkingLevel: agent.thinkingLevel,
    extensions: agent.extensions.map((extension) => extension.name),
    tools: agent.tools.map((tool) => tool.name),
    ...(agent.instructions === undefined ? {} : { instructions: agent.instructions }),
    ...(agent.cwd === undefined ? {} : { cwd: agent.cwd }),
    ...(agent.maxTurns === undefined ? {} : { maxTurns: agent.maxTurns }),
  }
}

/** Roughly how many tokens a context holds: four characters to a token, which is close for code. */
function approximateTokens(messages: readonly unknown[]): number {
  if (messages.length === 0) return 0
  let characters = 0
  for (const message of messages) characters += JSON.stringify(message).length
  return Math.ceil(characters / 4)
}

export { warn, invalidRequest }
