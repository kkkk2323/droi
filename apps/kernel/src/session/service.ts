import type { Context } from '@earendil-works/chord'
import type { AgentChange, ModelRef } from '@earendil-works/pi-durable'
import {
  type Conversation,
  type ConversationId,
  type ConversationView,
  type ConversationWindow,
  type EntryDraft,
  type EntryId,
  type EntryRecord,
  type Harness,
  type Registry,
  type Storage,
  type Tx,
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
  invalidRequest,
} from '../protocol/rpc.ts'
import { completeModel } from '../completion.ts'
import { repoRootOf } from '../workspace.ts'
import type { Dispatcher, Connection, Notifier } from '../protocol/server.ts'
import { ViewWindow, parseMode, parseWindow } from './watch.ts'
import type { PolicyStore } from '../policy/store.ts'
import { parsePolicyChange } from '../policy/store.ts'
import type { Approvals } from '../policy/approvals.ts'
import { loadResources, type LoadedResources } from '../resources/index.ts'
import type { DisabledSkills } from '../resources/disabled.ts'
import { expandSlash } from '../resources/expand.ts'
import type { Asks } from '../ask/asks.ts'
import type { Background } from '../processes/background.ts'

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
/** How many entries one `conversation.import` call takes; published as `limits.maxImportEntriesPerCall`. */
const MAX_IMPORT_ENTRIES = 1000

/** What the live run of one conversation looked like the last time a client was told. */
type BusyState = { run: unknown; runFailure?: unknown }

export type KernelOptions = {
  config: KernelConfig
  /** The kernel's copy of the Host's approval policy. */
  policy: PolicyStore
  /** The calls waiting for a person. */
  approvals: Approvals
  /** The `ask_user` questions waiting for a person. */
  asks: Asks
  /** The `bash_background` processes. */
  background: Background
  db: BunSqliteDatabase
  storage: Storage
  harness: Harness
  indexer: Indexer
  models: MutableModels
  registry: Registry
  /** The skills a person turned off. */
  disabledSkills: DisabledSkills
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
  /** One import key is admitted once at a time, so two calls with it converge instead of racing. */
  readonly #importing = new Map<string, Promise<void>>()
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
    add('conversation.import', (p) => this.#import(p))
    add('conversation.entries', (p) => this.#entries(p))
    add('conversation.usage', (p) => this.#usageOf(p))
    add('task.abort', (p) => this.#abortTask(p))
    add('index.update', (p) => this.#indexUpdate(p))
    add('kernel.shutdown', () => this.#shutdown())
    add('model.complete', (p) => this.#modelComplete(p))
    add('policy.set', (p) => this.#policySet(p))
    add('approval.respond', (p, connection) => {
      this.#writable()
      return this.#options.approvals.respond(p, connection.client)
    })
    add('ask.respond', (p, connection) => {
      this.#writable()
      return this.#options.asks.respond(p, connection.client)
    })
    // The three readers of M3 answer their shape now, so a client can call the whole surface; every
    // write of M3 is gated, and `gated()` answers it `-32009` while its capability is false.
    const readOnlyAnswer =
      <T>(value: T) =>
      () => {
        this.#readable()
        return value
      }
    add('process.list', (p) => this.#processList(p))
    add('process.stop', (p) => this.#processStop(p))
    add('mcp.list', readOnlyAnswer({ servers: [] }))
    add('skills.list', (p) => this.#skillsList(p))
    add('skills.setDisabled', (p) => this.#skillsSetDisabled(p))
    add('resources.list', (p) => this.#resourcesList(p))
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
    await this.#options.approvals.close()
    await this.#options.asks.close()
    await this.#options.background.close()
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
        // A larger import must be split: the kernel refuses it rather than truncating a history.
        maxImportEntriesPerCall: MAX_IMPORT_ENTRIES,
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
      // The default policy as the Host last set it. A viewer judges nothing, so it reports none.
      ...(() => {
        const policy = this.#options.config.readOnly
          ? undefined
          : this.#options.policy.defaultPolicy()
        return policy === undefined ? {} : { policy }
      })(),
    }
  }

  /** `policy.set`: the default policy, or one conversation's own, changed and announced whole. */
  async #policySet(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = optionalNumber(params, 'id')
    const change = parsePolicyChange(params['policy'])
    if (id !== undefined) await this.#conversation(id)
    return { policy: this.#options.policy.set(id ?? null, change) }
  }

  /** What `conversation.open` adds to the view: the policy in effect and the cards still waiting. */
  #openExtras(id: number): Record<string, unknown> {
    if (this.#options.config.readOnly) return {}
    const policy = this.#options.policy.known(id)
    const pending = [
      ...this.#options.approvals.pendingFor(id),
      ...this.#options.asks.pendingFor(id),
    ]
    return {
      ...(policy === undefined ? {} : { policy }),
      ...(pending.length === 0 ? {} : { pending }),
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
      // A head must point at an entry this conversation can see. On a create it cannot: the entries it names do
      // not have ids yet, so only `"self"` is meaningful there.
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
      const repoRoot = resolved.cwd === undefined ? undefined : await repoRootOf(resolved.cwd)
      await this.#options.indexer.touch(id, createdAt, {
        ...(kind === undefined ? {} : { kind }),
        ...(resolved.cwd === undefined ? {} : { workspace: resolved.cwd }),
        ...(repoRoot === undefined ? {} : { repoRoot }),
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
   * Write a transcript that already exists — M2's history constructor and M5's importer for old droid sessions.
   *
   * Without `id` it creates the conversation; with `id` it appends a batch to one. A batch is one commit, the
   * timestamps are the messages' own rather than the time of the import, and a `key` replays instead of writing
   * twice. Nothing here starts a run: an import is history, not a turn
   * (shared/interfaces/conversation-import.md).
   */
  async #import(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const entries = parseImportedEntries(params['entries'])
    const key = optionalString(params, 'key')
    const id = optionalNumber(params, 'id')
    if (key === undefined) {
      return id === undefined
        ? this.#importCreate(params, undefined, entries)
        : this.#importAppend(params, id, undefined, entries)
    }
    return this.#importInTurn(key, () =>
      id === undefined
        ? this.#importCreate(params, key, entries)
        : this.#importAppend(params, id, key, entries),
    )
  }

  /**
   * Run `work` after any other import with the same key, and let the next one start when it settles.
   *
   * Two concurrent calls with one key must converge on one conversation, not race into two. It is the same
   * shape `#creating` gives `conversation.create`, kept here rather than there because an import's identity is
   * its key.
   */
  async #importInTurn<T>(key: string, work: () => Promise<T>): Promise<T> {
    const previous = this.#importing.get(key)
    const run = (async () => {
      if (previous !== undefined) await previous.catch(() => undefined)
      return work()
    })()
    const settled = run.then(
      () => undefined,
      () => undefined,
    )
    this.#importing.set(key, settled)
    try {
      return await run
    } finally {
      // Only the last holder clears the slot, so an earlier call cannot drop the next one's queue.
      if (this.#importing.get(key) === settled) this.#importing.delete(key)
    }
  }

  async #importCreate(
    params: Record<string, unknown>,
    key: string | undefined,
    entries: ImportedEntry[],
  ): Promise<unknown> {
    const kind = optionalString(params, 'kind')
    const cwd = optionalString(params, 'cwd')
    const title = optionalString(params, 'title')
    const archived = optionalBoolean(params, 'archived')
    const agent = this.#importedAgent(params, cwd)
    // The conversation's own clock. History is dated by its messages: without an explicit `createdAt`, a
    // history from last year must not land at the top of a list sorted by activity.
    const createdAt =
      optionalNumber(params, 'createdAt') ?? earliestMessageTime(entries) ?? Date.now()
    // Every field that decides what this import is, including what it contains: the same key with different
    // entries is a different request, not a retry.
    const fingerprint = fingerprintOf({
      agent,
      kind: kind ?? null,
      title: title ?? null,
      createdAt: optionalNumber(params, 'createdAt') ?? null,
      archived: archived ?? null,
      instructions: params['instructions'] ?? null,
      entries: entries.map((entry) => entry.draft),
    })
    if (key !== undefined) {
      const replay = await this.#options.indexer.importKey(key)
      if (replay !== undefined) {
        // The same key with other fields is a different conversation, not a silent answer with the first.
        if (replay.fingerprint !== fingerprint) throw conflict(key)
        return { conversationId: replay.conversationId, entryIds: replay.entryIds, replayed: true }
      }
    }

    // A head must point at an entry this conversation can see. On a create it cannot: the entries it names
    // do not have ids yet, so only `"self"` is meaningful there.
    await this.#checkImportedHeads(undefined, entries)
    const meta: ConversationMeta = {
      createdAt,
      ...(kind === undefined ? {} : { kind }),
      // The resolved cwd, not the top-level one: a nested `agent.cwd` decides where the tools run, so it is
      // also what the index records as the workspace (as `conversation.create` does).
      ...(typeof agent.cwd === 'string' ? { workspace: agent.cwd } : {}),
    }
    let entryIds: number[] = []
    const conversation = await this.#options.harness.createConversation(
      {
        ownership: { kind: 'ownerless' },
        agent,
        // Meta, the entries and the replay memo are one commit with the conversation: a crash leaves all of
        // them or none of them, and the memo — which carries the fingerprint — is what a retry reads.
        init: async (tx, conversationId) => {
          await tx.appendEntry(conversationId, { kind: 'droi.meta', data: meta })
          entryIds = await appendImported(tx, conversationId, entries)
          if (key !== undefined) {
            await tx.appendEntry(conversationId, {
              kind: 'droi.import',
              data: { key, fingerprint, entryIds, scope: 'create', at: createdAt },
            })
          }
        },
      },
      context,
    )
    const id = conversation.id as unknown as number
    await this.#importIndex(id, { createdAt, kind, cwd: meta.workspace, title, archived }, entries)
    await this.track(id)
    info('conversation imported', { id, entries: entries.length, key })
    return { conversationId: id, entryIds }
  }

  async #importAppend(
    params: Record<string, unknown>,
    id: number,
    key: string | undefined,
    entries: ImportedEntry[],
  ): Promise<unknown> {
    // A conversation's identity is fixed at its creation; an append cannot change it.
    const given = ['agent', 'cwd', 'kind', 'title', 'createdAt', 'archived', 'instructions'].filter(
      (field) => params[field] !== undefined,
    )
    if (given.length > 0) {
      throw invalidParams(
        given.map((field) => ({
          path: field,
          message: 'only a call without `id` creates a conversation',
        })),
      )
    }
    await this.#conversation(id)
    await this.#checkImportedHeads(id, entries)
    const fingerprint = fingerprintOf({ entries: entries.map((entry) => entry.draft) })
    if (key !== undefined) {
      const replay = await this.#importedEntries(id, key, fingerprint)
      if (replay !== undefined) return { conversationId: id, entryIds: replay, replayed: true }
    }
    const entryIds = (await this.#options.harness.commit(async (tx) => {
      // Busy is decided inside the commit that writes. Checking first would leave the window a submit can
      // slip through, and an imported entry between a run's question and its answer is corruption.
      const live = await tx.doc(LiveDoc, id as never)
      if (live.run !== undefined) {
        throw new RpcError(Code.conversationBusy, 'conversation is busy', { conversationId: id })
      }
      const ids = await appendImported(tx, id as never, entries)
      if (key !== undefined) {
        await tx.appendEntry(id as never, {
          kind: 'droi.import',
          data: { key, fingerprint, entryIds: ids, scope: 'append', at: Date.now() },
        })
      }
      return ids
    }, context)) as number[]
    await this.#importIndex(id, {}, entries)
    await this.track(id)
    info('conversation imported', { id, entries: entries.length, key })
    return { conversationId: id, entryIds }
  }

  /**
   * The batch's effect on the conversation's row: its activity time, and a title or archive flag it was given.
   *
   * Only a message moves activity. A timeless bookkeeping entry (`droi.note`, or a batch of `[]`) must not
   * push a historical conversation up a list sorted by activity.
   */
  async #importIndex(
    id: number,
    fixed: { createdAt?: number; kind?: string; cwd?: string; title?: string; archived?: boolean },
    entries: ImportedEntry[],
  ): Promise<void> {
    const newest = latestMessageTime(entries)
    const activityAt = newest ?? fixed.createdAt
    if (fixed.createdAt !== undefined) {
      const repoRoot = fixed.cwd === undefined ? undefined : await repoRootOf(fixed.cwd)
      await this.#options.indexer.touch(id, activityAt ?? fixed.createdAt, {
        createdAt: fixed.createdAt,
        // Imported history is the past: the person read it where it was written. The row's activity is the
        // later of the two, and so is what was seen.
        viewedAt: Math.max(activityAt ?? fixed.createdAt, fixed.createdAt),
        ...(fixed.kind === undefined ? {} : { kind: fixed.kind }),
        ...(fixed.cwd === undefined ? {} : { workspace: fixed.cwd }),
        ...(repoRoot === undefined ? {} : { repoRoot }),
      })
    } else if (activityAt !== undefined) {
      await this.#options.indexer.touch(id, activityAt, {})
    }
    if (fixed.title !== undefined || fixed.archived !== undefined) {
      await this.#options.indexer.update(id, {
        ...(fixed.title === undefined ? {} : { title: fixed.title }),
        ...(fixed.archived === undefined ? {} : { archived: fixed.archived }),
      })
    }
  }

  /**
   * Check every imported `head` against what the conversation holds.
   *
   * A create has no entries yet — its own batch is being written — so only `"self"` can refer to anything. An
   * append may point at an entry the conversation already has, but at nothing else: Pi resolves the id when it
   * derives context, and an id that never existed poisons the Session rather than failing the request.
   */
  async #checkImportedHeads(id: number | undefined, entries: ImportedEntry[]): Promise<void> {
    const heads = entries
      .map((entry, index) => ({ head: entry.draft.head, index }))
      .filter((each) => typeof each.head === 'number')
    if (heads.length === 0) return
    if (id === undefined) {
      throw invalidParams(
        heads.map((each) => ({
          path: `entries[${each.index}].head`,
          message: 'a create cannot point at an entry it is creating; `"self"` starts the context',
        })),
      )
    }
    const conversation = await this.#conversation(id)
    for (const { head, index } of heads) {
      const page = await conversation.entries(
        { order: 'ascending', minEntryId: head as EntryId, maxEntryId: head as EntryId },
        1,
        undefined,
        context,
      )
      if (page.items.some((entry) => (entry.id as unknown as number) === head)) continue
      throw invalidParams([
        { path: `entries[${index}].head`, message: `entry ${head} is not in this conversation` },
      ])
    }
  }
  /** The agent an import asks for: `agent` (if any) over `cwd` and `instructions`, over the config defaults. */
  #importedAgent(params: Record<string, unknown>, cwd: string | undefined): AgentChange {
    const top = this.#agentChange(
      {
        ...(cwd === undefined ? {} : { cwd }),
        ...(params['instructions'] === undefined ? {} : { instructions: params['instructions'] }),
      },
      'import',
    )
    const nested =
      params['agent'] === undefined
        ? {}
        : this.#agentChange(requiredObject(params, 'agent'), 'agent')
    return { ...defaultsAsChange(this.#options.config.agent), ...top, ...nested }
  }

  /**
   * The entry ids a replayed `key` wrote in one conversation, from the memo it left there.
   *
   * Only entries this conversation owns: a fork inherits its parent's transcript, and its parent's memos are
   * not the fork's own (`#requestMemo` makes the same distinction).
   */
  async #importedEntries(
    id: number,
    key: string,
    fingerprint: string,
  ): Promise<number[] | undefined> {
    const conversation = await this.#options.harness.conversation(id as never, context)
    if (conversation === undefined) return undefined
    let cursor: never | undefined
    do {
      const page = await conversation.entries({ order: 'descending' }, MAX_PAGE, cursor, context)
      for (const entry of page.items) {
        if ((entry.conversationId as unknown as number) !== id) continue
        if (entry.kind !== 'droi.import') continue
        const data = entry.data as
          | { key?: unknown; fingerprint?: unknown; entryIds?: unknown; scope?: unknown }
          | undefined
        if (data?.key !== key || !Array.isArray(data.entryIds)) continue
        // A create's key is a different identity: an append that reuses one would either replay a create's ids
        // or write under a key someone else owns, so it is refused rather than guessed at.
        if (data.scope === 'create') throw conflict(key)
        // Only an append memo replays here.
        if (data.scope !== 'append') continue // The same key with other entries is a different batch: answering with the old ids would silently
        // discard what the caller asked to append.
        if (data.fingerprint !== fingerprint) throw conflict(key)
        return data.entryIds as number[]
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    return undefined
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
      repoRoot: optionalString(params, 'repoRoot'),
      parent: optionalNumber(params, 'parent'),
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
    // A viewer writes nothing, not even "seen": its data directory is another process's. A client opening a
    // conversation again on a new connection says `seen: false`: nobody looked at it.
    if (!this.#options.config.readOnly && optionalBoolean(params, 'seen') !== false) {
      await this.#options.indexer.markViewed(id)
      // Seen here is seen everywhere: the other clients take the dot off too.
      await this.#indexChanged(id)
    }
    // The connection may have gone while this call was reading. Registering then would leave a
    // subscription no close will ever release, and a watch on a conversation nobody sees.
    if (this.#closing || !connection.open) {
      await watch.stop()
      return { view, ...this.#openExtras(id) }
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
      return { view, ...this.#openExtras(id) }
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
    return { view, ...this.#openExtras(id) }
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
    // After the memo: a retry is the same request even if a template changed since.
    const input =
      content === undefined ? undefined : await this.#expandCommand(conversation, content)
    const draft = (
      wantsContinue === true
        ? { type: 'continue' as const, requestId }
        : {
            type: 'input' as const,
            content: input as never,
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

  /**
   * An input that starts with a slash command, expanded: a prompt template or a skill of the conversation's
   * directories. Only the input's first text block is read; anything that names no command goes as it was.
   */
  async #expandCommand(conversation: Conversation, content: unknown): Promise<unknown> {
    const first = typeof content === 'string' ? content : firstText(content)
    if (first === undefined || !first.startsWith('/')) return content
    const agent = await conversation.agent(context)
    const loaded = loadResources({ userDir: this.#options.config.userDir, workspace: agent.cwd })
    const expanded = expandSlash(first, loaded, this.#options.disabledSkills.names())
    if (expanded === undefined) return content
    if (typeof content === 'string') return expanded
    const blocks = [...(content as { type: string; text?: string }[])]
    const index = blocks.findIndex((block) => block.type === 'text')
    blocks[index] = { ...blocks[index]!, text: expanded }
    return blocks
  }

  async #abort(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    // Protocol `background?` has no Pi counterpart yet (recorded in the contract's change log).
    await conversation.abort(context)
    await this.refreshBusy(id)
    return {}
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
    // Only Pi's own fields. A turn's entries do not carry its submission, and Pi keeps no submission → run task
    // link, so there is no end the Kernel could report for a turn that produced no answer (see
    // shared/design/kernel-index-ownership.md §7). A client bounds a turn by the status: `[entry, answer]` for
    // a done submission, `[entry, head]` while placed, and nothing for one that ended without an answer.
    return {
      submissionId,
      status: record.status,
      ...(entry === undefined ? {} : { entry }),
      ...(record.status === 'done' ? { answer: record.answer } : {}),
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
    // The row's workspace is where the tools run, so a new cwd is a new row as well: the index groups
    // conversations by it, and the repository it belongs to can change with it. A cleared cwd clears both,
    // rather than leave the row claiming a directory the conversation no longer runs in.
    if (typeof resolved.cwd === 'string') {
      await this.#options.indexer.update(id, {
        workspace: resolved.cwd,
        repoRoot: (await repoRootOf(resolved.cwd)) ?? null,
      })
    } else if (resolved.cwd === null) {
      await this.#options.indexer.update(id, { workspace: null, repoRoot: null })
    }
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
    // A fork runs where its source runs, and is the same kind of conversation: its row says so, so the
    // sidebar puts it in the source's repository and a list filtered by workspace finds it.
    const source = await this.#options.indexer.conversation(id)
    const workspace = source?.workspace ?? undefined
    const kind = source?.kind ?? undefined
    const title = source?.title === null || source?.title === '' ? undefined : source?.title
    const meta: ConversationMeta = {
      parentId: id,
      createdAt,
      ...(kind === undefined ? {} : { kind }),
      ...(workspace === undefined ? {} : { workspace }),
    }
    const forked = await conversation.fork(
      at as EntryId,
      {
        ownership: { kind: 'ownerless' },
        // A fork's identity is its own. The parent's `droi.meta` is in the inherited transcript, and taking
        // it would give the fork the parent's requestId — which collides on the unique index at the next
        // start — and the parent's submissions. So the fork writes its own, with the link back.
        init: async (tx, conversationId) => {
          await tx.appendEntry(conversationId, { kind: 'droi.meta', data: meta })
        },
      },
      context,
    )
    const forkedId = forked.id as unknown as number
    const repoRoot = workspace === undefined ? undefined : await repoRootOf(workspace)
    await this.#options.indexer.touch(forkedId, createdAt, {
      parentId: id,
      createdAt,
      ...(kind === undefined ? {} : { kind }),
      ...(workspace === undefined ? {} : { workspace }),
      ...(repoRoot === undefined ? {} : { repoRoot }),
      // The source's name, as a name to replace: the title generator may name the fork once it differs.
      ...(title === undefined ? {} : { title }),
    })
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
    const bounds = {
      order,
      minEntryId: optionalNumber(query, 'minEntryId') as EntryId | undefined,
      maxEntryId: optionalNumber(query, 'maxEntryId') as EntryId | undefined,
    } as const
    const kinds = query['kinds']
    if (kinds !== undefined) {
      if (
        !Array.isArray(kinds) ||
        kinds.length === 0 ||
        kinds.some((kind) => typeof kind !== 'string')
      ) {
        throw invalidParams([
          { path: 'query.kinds', message: 'a non-empty array of entry kinds is required' },
        ])
      }
      if (cursor !== undefined) {
        throw invalidParams([
          { path: 'cursor', message: 'a query with kinds pages by entry id, not by cursor' },
        ])
      }
      return {
        items: await this.#entriesOfKinds(conversation, bounds, new Set(kinds as string[]), limit),
      }
    }
    const page = await conversation.entries(bounds, limit, cursor as never, context)
    const items = page.items.map((entry) => wireEntry(entry))
    return page.next === undefined ? { items } : { items, cursor: page.next }
  }

  /**
   * The first `limit` entries of the given kinds within `bounds`, read here so the client does not carry every
   * other entry over the wire to find them: the turn rail wants a long conversation's questions, not its tool output.
   * There is no cursor: Pi's points after the last entry scanned, which is not the last one returned. The next page
   * narrows `maxEntryId` (descending) or `minEntryId` (ascending) past the last item, and a page shorter than
   * `limit` is the last.
   */
  async #entriesOfKinds(
    conversation: Conversation,
    bounds: { order: 'ascending' | 'descending'; minEntryId?: EntryId; maxEntryId?: EntryId },
    kinds: ReadonlySet<string>,
    limit: number,
  ): Promise<unknown[]> {
    const items: unknown[] = []
    let next: unknown
    do {
      const page = await conversation.entries(bounds, MAX_PAGE, next as never, context)
      for (const entry of page.items) {
        if (!kinds.has(entry.kind)) continue
        items.push(wireEntry(entry))
        if (items.length === limit) return items
      }
      next = page.next
    } while (next !== undefined)
    return items
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

  // ── skills.list, resources.list ──────────────────────────────────────────────────────────────

  /**
   * The skills this kernel can load, in the order a client shows them.
   *
   * Without `id` only the user's own directory is read; with one, that conversation's `cwd` decides the
   * workspace half. Every call reads the files again: a skill a person just wrote is there on the next call,
   * which is what "the files are the source of truth" has to mean.
   */
  async #processList(params: Record<string, unknown>): Promise<unknown> {
    const id = requiredNumber(params, 'id')
    await this.#conversation(id)
    return { processes: await this.#options.background.list(id) }
  }

  async #processStop(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const processId = optionalString(params, 'processId')
    if (processId === undefined || processId === '') {
      throw invalidParams([{ path: 'processId', message: 'a process id is required' }])
    }
    await this.#conversation(id)
    return { status: await this.#options.background.stop(id, processId) }
  }

  async #skillsList(params: Record<string, unknown>): Promise<unknown> {
    this.#readable()
    const loaded = await this.#loadResources(params)
    return { skills: this.#wireSkills(loaded) }
  }

  /** Turn a skill off or on again, by name: everywhere it is found, for every conversation. */
  async #skillsSetDisabled(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const name = optionalString(params, 'name')
    const disabled = optionalBoolean(params, 'disabled')
    if (name === undefined || name === '') {
      throw invalidParams([{ path: 'name', message: 'a skill name is required' }])
    }
    if (disabled === undefined) {
      throw invalidParams([{ path: 'disabled', message: 'true or false is required' }])
    }
    await this.#options.disabledSkills.set(name, disabled)
    // The list the client showed: with `id`, that conversation's workspace skills are in it too.
    return this.#skillsList(params)
  }

  #wireSkills(loaded: LoadedResources): unknown[] {
    const disabled = this.#options.disabledSkills.names()
    return loaded.skills.map((skill) => ({
      name: skill.name,
      scope: skill.scope,
      path: skill.path,
      description: skill.description,
      enabled: !disabled.has(skill.name),
    }))
  }

  /**
   * Everything the kernel would load for a conversation: its skills, the slash commands it expands, and the
   * `AGENTS.md` chain in the order it is applied.
   */
  async #resourcesList(params: Record<string, unknown>): Promise<unknown> {
    this.#readable()
    const loaded = await this.#loadResources(params)
    return {
      skills: this.#wireSkills(loaded),
      prompts: loaded.prompts.map((prompt) => ({
        name: prompt.name,
        scope: prompt.scope,
        path: prompt.path,
        ...(prompt.description === undefined ? {} : { description: prompt.description }),
      })),
      contextFiles: loaded.contextFiles.map((file) => ({ path: file.path, bytes: file.bytes })),
      diagnostics: loaded.diagnostics,
    }
  }

  /** The files around one conversation, or around the user's own directory when no conversation is named. */
  async #loadResources(params: Record<string, unknown>): Promise<LoadedResources> {
    const id = optionalNumber(params, 'id')
    let workspace: string | undefined
    if (id !== undefined) {
      const conversation = await this.#conversation(id)
      const agent = await conversation.agent(context)
      workspace = agent.cwd
    }
    return loadResources({ userDir: this.#options.config.userDir, workspace })
  }

  // ── index.update ──────────────────────────────────────────────────────────────────────────────

  async #indexUpdate(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const change: {
      title?: string | null
      titleSource?: 'auto' | 'chosen'
      archived?: boolean
      kind?: string | null
    } = {}
    if ('title' in params) {
      const title = params['title']
      change.title =
        title === null ? null : typeof title === 'string' ? title : invalidTitle('title')
      // A title someone typed is a choice; a title a generator wrote says so, so it can be replaced again.
      const source = params['titleSource']
      if (source !== undefined && source !== 'auto' && source !== 'chosen') {
        throw invalidParams([{ path: 'titleSource', message: 'auto or chosen is required' }])
      }
      change.titleSource = source ?? 'chosen'
    } else if ('titleSource' in params) {
      throw invalidParams([{ path: 'titleSource', message: 'a title is required' }])
    }
    if ('kind' in params) {
      const kind = params['kind']
      change.kind = kind === null ? null : typeof kind === 'string' ? kind : invalidTitle('kind')
    }
    const archived = optionalBoolean(params, 'archived')
    if (archived !== undefined) change.archived = archived
    // Both of the fields below are checked before the first write: a refusal that had already stored half of
    // the change would be a lie to a caller that reads -32602 as "nothing happened".
    let ownerId: number | null | undefined
    if ('ownerId' in params) {
      const value = params['ownerId']
      if (value !== null && (typeof value !== 'number' || !Number.isInteger(value))) {
        throw invalidParams([{ path: 'ownerId', message: 'a conversation id or null is required' }])
      }
      if (value !== null) await this.#checkOwner(id, value)
      ownerId = value
    }
    let viewedAt: number | null | undefined
    if ('viewedAt' in params) {
      const value = params['viewedAt']
      if (value !== null && (typeof value !== 'number' || !Number.isFinite(value))) {
        throw invalidParams([{ path: 'viewedAt', message: 'a number or null is required' }])
      }
      viewedAt = value
    }
    await this.#options.indexer.update(id, {
      ...change,
      ...(ownerId === undefined ? {} : { ownerId }),
    })
    // The unread dot: a time says "a person has seen everything up to here", `null` asks for it back.
    if (viewedAt !== undefined) await this.#options.indexer.setViewed(id, viewedAt)
    await this.#indexChanged(id)
    return {}
  }

  /**
   * Tell every client what a conversation's row now says: its title, archive, last activity and when a person
   * last saw it. `viewedAt` is `null` while nobody has, which is what an unread dot reads.
   */
  async #indexChanged(id: number): Promise<void> {
    const row = await this.#options.indexer.conversation(id)
    this.#notifier?.broadcast('index.changed', {
      id,
      ...(row?.title === undefined || row.title === null ? {} : { title: row.title }),
      ...(row?.title_source === undefined || row.title_source === null
        ? {}
        : { titleSource: row.title_source }),
      ...(row?.archived_at === undefined ? {} : { archived: row.archived_at !== null }),
      lastActivityAt: row?.last_activity_at ?? Date.now(),
      viewedAt: row?.viewed_at ?? null,
    })
  }

  /**
   * Whether `ownerId` may own `id`: it must be a conversation this kernel has, and not `id` itself nor one of
   * `id`'s own children — two rows that own each other are rows no list can reach.
   *
   * The walk up is bounded by the number of rows it has visited, so a cycle that somehow exists cannot spin
   * here for ever.
   */
  async #checkOwner(id: number, ownerId: number): Promise<void> {
    if (ownerId === id) {
      throw invalidParams([{ path: 'ownerId', message: 'a conversation cannot own itself' }])
    }
    if ((await this.#options.indexer.conversation(ownerId)) === undefined) {
      throw notFound(ownerId)
    }
    const seen = new Set<number>([id])
    let current: number | null = ownerId
    while (current !== null) {
      if (seen.has(current)) {
        throw invalidParams([{ path: 'ownerId', message: 'the two would own each other' }])
      }
      seen.add(current)
      const row = await this.#options.indexer.conversation(current)
      if (row === undefined) return
      current = row.owner_id
    }
  }

  // ── model.complete ──────────────────────────────────────────────────────────────────────────

  /**
   * One model call with no conversation behind it. The whole call lives in `completion.ts`: this method
   * only refuses a read-only kernel, which may not spend a key it was not given.
   */
  async #modelComplete(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    return completeModel(this.#options.models, params)
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
  // A null prototype and `defineProperty`: an own `__proto__` in imported data is a field like any other, and
  // assigning it would set the prototype instead — losing it from the fingerprint so two different payloads
  // look like the same request.
  const out = Object.create(null) as Record<string, unknown>
  for (const key of Object.keys(record).sort()) {
    if (record[key] === undefined) continue
    Object.defineProperty(out, key, {
      value: normalize(record[key]),
      enumerable: true,
      writable: true,
      configurable: true,
    })
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
  'ask.respond': 'asks',
  'mcp.set': 'mcp',
  'mcp.authorize': 'mcp',
  'mcp.authorize.cancel': 'mcp',
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
/** The text of an input's first text block, when it opens with one. */
function firstText(content: unknown): string | undefined {
  if (!Array.isArray(content)) return undefined
  const block = content.find((item: { type?: unknown }) => item.type === 'text') as
    | { text?: unknown }
    | undefined
  return typeof block?.text === 'string' ? block.text : undefined
}

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

/** One entry an import writes, with the usage its messages asked the ledger for. */
type ImportedEntry = {
  draft: EntryDraft
  usage: { key: string; usage: ImportedUsage }[]
}

type ImportedUsage = {
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  totalTokens: number
  cost: { input: number; output: number; cacheRead: number; cacheWrite: number; total: number }
  /** Optional counters, kept when a provider reports them (Pi's `Usage` names them). */
  cacheWrite1h?: number
  reasoning?: number
}

/** The kinds an import may not write: the kernel's own identity, memo and bookkeeping entries. */
const RESERVED_IMPORT_KINDS = new Set(['droi.meta', 'droi.request', 'droi.import'])

/**
 * `entries` of `conversation.import`: Pi's entry drafts, checked for structure and nothing else.
 *
 * Structure only, on purpose: an imported history is somebody else's past, and its odd shapes — a tool result
 * with no call before it, an answer that follows no user message — are what is being preserved. The kinds the
 * kernel owns are refused, because writing one would forge an identity or a memo.
 */
function parseImportedEntries(value: unknown): ImportedEntry[] {
  if (!Array.isArray(value)) {
    throw invalidParams([{ path: 'entries', message: 'an array of entries is required' }])
  }
  if (value.length > MAX_IMPORT_ENTRIES) {
    throw invalidParams([{ path: 'entries', message: `at most ${MAX_IMPORT_ENTRIES} per call` }])
  }
  const issues: { path: string; message: string }[] = []
  const parsed: ImportedEntry[] = []
  for (const [index, raw] of value.entries()) {
    const path = `entries[${index}]`
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      issues.push({ path, message: 'an entry object is required' })
      continue
    }
    const entry = raw as Record<string, unknown>
    const kind = entry['kind']
    if (typeof kind !== 'string' || kind === '') {
      issues.push({ path: `${path}.kind`, message: 'a kind is required' })
      continue
    }
    if (RESERVED_IMPORT_KINDS.has(kind)) {
      issues.push({ path: `${path}.kind`, message: `${kind} is the kernel's own` })
      continue
    }
    const model = entry['model']
    const usage: ImportedEntry['usage'] = []
    let problem: { path: string; message: string } | undefined
    if (model !== undefined) problem = importedModelProblem(model, path)
    if (problem === undefined && entry['edits'] !== undefined) {
      problem = importedEditsProblem(entry['edits'], path)
    }
    if (problem === undefined && entry['head'] !== undefined) {
      problem = importedHeadProblem(entry['head'], path)
    }
    if (problem !== undefined) {
      issues.push(problem)
      continue
    }
    for (const [messageIndex, message] of ((model ?? []) as Record<string, unknown>[]).entries()) {
      const found = message['usage']
      if (found === undefined) continue
      const at = `${path}.model[${messageIndex}].usage`
      const read = readImportedUsage(found, at)
      if (read === undefined) {
        issues.push({ path: at, message: 'a usage object is required' })
        continue
      }
      usage.push({
        key: `${String(message['provider'] ?? 'unknown')}/${String(message['model'] ?? 'unknown')}`,
        usage: read,
      })
    }
    parsed.push({
      draft: {
        kind,
        ...(model === undefined ? {} : { model }),
        ...(entry['data'] === undefined ? {} : { data: entry['data'] }),
        ...(entry['edits'] === undefined ? {} : { edits: entry['edits'] }),
        ...(entry['head'] === undefined ? {} : { head: entry['head'] }),
      } as EntryDraft,
      usage,
    })
  }
  if (issues.length > 0) throw invalidParams(issues)
  return parsed
}

/**
 * What is wrong with one Pi message, or undefined when nothing is.
 *
 * Structural, and only structural: an imported history may hold odd shapes, but a value that Pi's context
 * derivation would throw on — a `null` where a message belongs, an assistant whose `content` is a string —
 * must be refused at the request rather than poison the Session later.
 */
function messageProblem(
  value: unknown,
  path: string,
): { path: string; message: string } | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return { path, message: 'a message object is required' }
  }
  const message = value as Record<string, unknown>
  const role = message['role']
  if (role !== 'system' && role !== 'user' && role !== 'assistant' && role !== 'toolResult') {
    return { path: `${path}.role`, message: 'system, user, assistant or toolResult' }
  }
  const content = message['content']
  const isText = typeof content === 'string'
  const isBlocks =
    Array.isArray(content) && content.every((block) => typeof block === 'object' && block !== null)
  // An assistant's content is always blocks: `content.filter` is what Pi calls on it.
  if (role === 'assistant' ? !isBlocks : !isText && !isBlocks) {
    return {
      path: `${path}.content`,
      message:
        role === 'assistant'
          ? 'an array of content blocks is required'
          : 'a string or an array of blocks is required',
    }
  }
  // A message without its own time would inherit the import's, which is the one thing an import must not do.
  if (typeof message['timestamp'] !== 'number' || !Number.isFinite(message['timestamp'])) {
    return { path: `${path}.timestamp`, message: 'epoch milliseconds are required' }
  }
  return undefined
}

/** What is wrong with an imported entry's `model`, or undefined when nothing is. */
function importedModelProblem(
  value: unknown,
  path: string,
): { path: string; message: string } | undefined {
  if (!Array.isArray(value))
    return { path: `${path}.model`, message: 'an array of messages is required' }
  for (const [index, raw] of value.entries()) {
    const problem = messageProblem(raw, `${path}.model[${index}]`)
    if (problem !== undefined) return problem
    // An entry's own model is chat history, not prompt: a system message here is not what Pi replays.
    const role = (raw as { role?: unknown }).role
    if (role === 'system') {
      return { path: `${path}.model[${index}].role`, message: 'user, assistant or toolResult' }
    }
  }
  return undefined
}
/** What is wrong with an imported entry's `edits`, or undefined when nothing is. */
function importedEditsProblem(
  value: unknown,
  path: string,
): { path: string; message: string } | undefined {
  // `edits` reaches Pi's context derivation, which calls array methods on it: a string here throws there,
  // far from the request that caused it.
  if (!Array.isArray(value))
    return { path: `${path}.edits`, message: 'an array of edits is required' }
  for (const [index, raw] of value.entries()) {
    const at = `${path}.edits[${index}]`
    if (typeof raw !== 'object' || raw === null)
      return { path: at, message: 'an edit object is required' }
    const edit = raw as Record<string, unknown>
    if (
      typeof edit['target'] !== 'number' ||
      !Number.isInteger(edit['target']) ||
      edit['target'] < 1
    ) {
      return { path: `${at}.target`, message: 'an entry ID of 1 or more is required' }
    }
    const action = edit['action']
    if (action !== 'omit' && action !== 'replace') {
      return { path: `${at}.action`, message: 'omit or replace' }
    }
    if (action === 'replace') {
      const messages = edit['messages']
      if (!Array.isArray(messages)) {
        return { path: `${at}.messages`, message: 'an array of messages is required' }
      }
      // Each message is checked, not just the array: a `null` or a string content reaches Pi's context
      // derivation and throws there, far from this request.
      for (const [messageIndex, message] of messages.entries()) {
        const problem = messageProblem(message, `${at}.messages[${messageIndex}]`)
        if (problem !== undefined) return problem
      }
    }
    if (action === 'omit' && edit['messages'] !== undefined) {
      return { path: `${at}.messages`, message: 'an omit edit contributes no messages' }
    }
  }
  return undefined
}

/** What is wrong with an imported entry's `head`, or undefined when nothing is. */
function importedHeadProblem(
  value: unknown,
  path: string,
): { path: string; message: string } | undefined {
  // `head` decides where the active context starts. Pi resolves it to an entry and stores it in an INTEGER
  // column, so a value outside the safe-integer range poisons the Session (`cannot store REAL value`) rather
  // than being a bad request; `1e100` passes `Number.isInteger`.
  const field = `${path}.head`
  if (value === 'self') return undefined
  if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 1) return undefined
  return {
    path: field,
    message: '`"self"` or an entry ID between 1 and 2^53-1 is required',
  }
}
/** One message's `usage`, with the counters Pi's ledger keeps; a missing counter or cost counts as zero. */
function readImportedUsage(value: unknown, path: string): ImportedUsage | undefined {
  if (typeof value !== 'object' || value === null) return undefined
  const usage = value as Record<string, unknown>
  const count = (source: Record<string, unknown>, field: string): number => {
    const raw = source[field]
    return typeof raw === 'number' && Number.isFinite(raw) ? raw : 0
  }
  const optional = (field: string): number | undefined => {
    const raw = usage[field]
    return typeof raw === 'number' && Number.isFinite(raw) ? raw : undefined
  }
  const cost = (
    typeof usage['cost'] === 'object' && usage['cost'] !== null ? usage['cost'] : {}
  ) as Record<string, unknown>
  void path
  const cacheWrite1h = optional('cacheWrite1h')
  const reasoning = optional('reasoning')
  return {
    input: count(usage, 'input'),
    output: count(usage, 'output'),
    cacheRead: count(usage, 'cacheRead'),
    cacheWrite: count(usage, 'cacheWrite'),
    totalTokens: count(usage, 'totalTokens'),
    cost: {
      input: count(cost, 'input'),
      output: count(cost, 'output'),
      cacheRead: count(cost, 'cacheRead'),
      cacheWrite: count(cost, 'cacheWrite'),
      total: count(cost, 'total'),
    },
    // Pi adds a counter once either side reports it, so an omitted one must stay absent rather than read 0.
    ...(cacheWrite1h === undefined ? {} : { cacheWrite1h }),
    ...(reasoning === undefined ? {} : { reasoning }),
  }
}

/** Append one batch, answering its entry ids in order, and record what its messages spent. */
async function appendImported(
  tx: Tx,
  id: ConversationId,
  entries: ImportedEntry[],
): Promise<number[]> {
  const ids: number[] = []
  for (const entry of entries) {
    const record = await tx.appendEntry(id, entry.draft)
    ids.push(record.id as unknown as number)
    if (entry.usage.length === 0) continue
    const state = await tx.doc(UsageDoc, id)
    for (const { key, usage } of entry.usage) addImportedUsage(state, key, usage)
  }
  return ids
}

/** Add one message's usage to the conversation's `pi.usage`, the way Pi records a real answer's. */
function addImportedUsage(state: UsageState, key: string, usage: ImportedUsage): void {
  const total = Object.hasOwn(state.models, key) ? state.models[key] : undefined
  if (total === undefined) {
    // A model key is always `provider/model`, so it can never be `__proto__`; plain assignment is what Pi's
    // own `recordUsage` does through its draft.
    state.models[key] = { ...usage, cost: { ...usage.cost } }
    return
  }
  total.input += usage.input
  total.output += usage.output
  total.cacheRead += usage.cacheRead
  total.cacheWrite += usage.cacheWrite
  total.totalTokens += usage.totalTokens
  if (usage.cacheWrite1h !== undefined)
    total.cacheWrite1h = (total.cacheWrite1h ?? 0) + usage.cacheWrite1h
  if (usage.reasoning !== undefined) total.reasoning = (total.reasoning ?? 0) + usage.reasoning
  total.cost.input += usage.cost.input
  total.cost.output += usage.cost.output
  total.cost.cacheRead += usage.cost.cacheRead
  total.cost.cacheWrite += usage.cost.cacheWrite
  total.cost.total += usage.cost.total
}

/** The oldest message time in a batch: the conversation's own beginning when it is imported without one. */
function earliestMessageTime(entries: ImportedEntry[]): number | undefined {
  let oldest: number | undefined
  for (const entry of entries) {
    for (const message of (entry.draft.model as { timestamp?: unknown }[] | undefined) ?? []) {
      const at = message.timestamp
      if (typeof at !== 'number') continue
      if (oldest === undefined || at < oldest) oldest = at
    }
  }
  return oldest
}
/** The newest message time in a batch, which is the conversation's activity at that point in its history. */
function latestMessageTime(entries: ImportedEntry[]): number | undefined {
  let newest: number | undefined
  for (const entry of entries) {
    for (const message of (entry.draft.model as { timestamp?: unknown }[] | undefined) ?? []) {
      const at = message.timestamp
      if (typeof at !== 'number') continue
      if (newest === undefined || at > newest) newest = at
    }
  }
  return newest
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
    ...(row.repo_root === null ? {} : { repoRoot: row.repo_root }),
    // `parentId` is the fork this one came from — lineage. `ownerId` is the conversation that spawned it,
    // which is what keeps a subagent's conversation out of the list (ADR 0043).
    ...(row.parent_id === null ? {} : { parentId: row.parent_id }),
    ...(row.owner_id === null ? {} : { ownerId: row.owner_id }),
    // Which title this is: the kernel's own guess (`auto`, the first user message) or a person's
    // (`chosen`). A client that wants to let someone rename a conversation needs the difference; the text
    // alone cannot tell them apart.
    ...(row.title_source === null ? {} : { titleSource: row.title_source }),
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
