import type { Context } from '@earendil-works/chord'
import type { ModelRef } from '@earendil-works/pi-durable'
import {
  type AgentChange,
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
import type { ConversationRow } from '../indexer/schema.ts'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import type { KernelConfig } from '../config.ts'
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
} from '../protocol/rpc.ts'
import type { Connection, Dispatcher, Notifier } from '../protocol/server.ts'
import { ViewWindow, parseMode, parseWindow } from './watch.ts'

/** Pi's background context: work the kernel starts carries no client's cancellation. */
export const context: Context = BACKGROUND_CONTEXT

const DEFAULT_WINDOW: ConversationWindow = { min: 100, max: 200 }
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
}

/**
 * The 25 methods of the Kernel protocol.
 *
 * The shape follows Pi: a conversation is an ID with a handle, a view is a window over its transcript,
 * and a run is something a submission starts and an abort stops. What Droi adds is the list, the search,
 * the metadata writes and the per-connection view subscription.
 */
export class Kernel implements Dispatcher {
  readonly methods: ReadonlyMap<string, import('../protocol/server.ts').Handler>
  readonly #options: KernelOptions
  #notifier: Notifier | undefined
  /** The conversation view each connection is watching, and the last busy state broadcast. */
  readonly #busy = new Map<number, BusyState>()
  readonly #live = new Map<number, { detach: () => void }>()
  #closing = false

  constructor(options: KernelOptions) {
    this.#options = options
    const handlers = new Map<string, import('../protocol/server.ts').Handler>()
    const add = (name: string, handler: import('../protocol/server.ts').Handler) =>
      handlers.set(name, handler)
    add('harness.info', () => this.#info())
    add('conversation.create', (p) => this.#create(p))
    add('conversation.list', (p) => this.#list(p))
    add('conversation.open', (p, connection) => this.#open(p, connection))
    add('conversation.unwatch', (p, connection) => this.#unwatch(p, connection))
    add('conversation.submit', (p) => this.#submit(p))
    add('conversation.abort', (p) => this.#abort(p))
    add('submission.abort', (p) => this.#abortSubmission(p))
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
    add('process.list', () => ({ processes: [] }))
    add('mcp.list', () => ({ servers: [] }))
    add('skills.list', () => ({ skills: [] }))
    this.methods = handlers
  }

  /** Where notifications go. The server hands itself over once it is listening. */
  attach(notifier: Notifier): void {
    this.#notifier = notifier
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
   * Follow every conversation that is doing something, so `conversation.busy` reaches connections that
   * have not opened it. Called at startup for live tasks and whenever a run starts.
   */
  async track(id: number): Promise<void> {
    if (this.#live.has(id)) return
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
    this.#live.set(id, { detach: () => observation.detach() })
    this.#advance(id, observation.value)
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
  }

  // ── harness.info ────────────────────────────────────────────────────────────────────────────────

  #info(): Record<string, unknown> {
    const { config } = this.#options
    const providers = config.providers.map((provider) => ({
      id: provider.id,
      name: provider.name ?? provider.id,
      models: provider.models.map((model) => model.id),
    }))
    const models = config.providers.flatMap((provider) =>
      provider.models.map((model) => ({
        provider: provider.id,
        id: model.id,
        name: model.name ?? model.id,
        contextWindow: model.contextWindow,
        maxTokens: model.maxTokens,
        input: model.input ?? ['text'],
        reasoning: model.reasoning ?? false,
      })),
    )
    return {
      version: this.#options.version,
      limits: {
        viewPushesPerSecond: this.#options.config.viewPushesPerSecond ?? 5,
        viewWindowDefault: { min: DEFAULT_WINDOW.min, max: DEFAULT_WINDOW.max },
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
        this.#options.tools.length === 0 ? [] : [{ name: 'coding', tools: this.#options.tools }],
    }
  }

  // ── conversation.create / list / open / unwatch ─────────────────────────────────────────────────

  async #create(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const requestId = optionalString(params, 'requestId')
    const kind = optionalString(params, 'kind')
    const workspace = optionalString(params, 'workspace')
    const agent = this.#agentChange(params)
    if (requestId !== undefined) {
      const existing = await this.#options.indexer.byRequestId(requestId)
      if (existing !== undefined) {
        if (kind !== undefined && existing.kind !== null && existing.kind !== kind)
          throw conflict(requestId)
        return { conversationId: existing.id, view: await this.#view(existing.id, DEFAULT_WINDOW) }
      }
    }
    const conversation = await this.#options.harness.createConversation(
      { ownership: { kind: 'ownerless' }, agent: agent.change, init: undefined },
      context,
    )
    const now = Date.now()
    await this.#options.indexer.touch(conversation.id as unknown as number, now, {
      kind,
      workspace,
      requestId,
      title: agent.instructions === undefined ? undefined : undefined,
    })
    await this.track(conversation.id as unknown as number)
    info('conversation created', { id: conversation.id, kind, requestId })
    return {
      conversationId: conversation.id,
      view: await this.#view(conversation.id as unknown as number, DEFAULT_WINDOW),
    }
  }

  async #list(params: Record<string, unknown>): Promise<unknown> {
    const cursor = optionalNumber(params, 'cursor')
    const limit = optionalNumber(params, 'limit')
    const page = await this.#options.indexer.list({
      kind: optionalString(params, 'kind'),
      archived: optionalBoolean(params, 'archived'),
      workspace: optionalString(params, 'workspace'),
      text: optionalString(params, 'query'),
      cursor: cursor === undefined ? undefined : Number(cursor),
      limit: limit === undefined ? undefined : Number(limit),
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
    await this.#options.indexer.markViewed(id)
    if (this.#closing) {
      await watch.stop()
      return { view }
    }
    const pusher = new ViewWindow({
      connection,
      conversationId: id,
      mode,
      perSecond: this.#options.config.viewPushesPerSecond ?? 5,
    })
    connection.subscribe(id, async () => {
      pusher.stop()
      await watch.stop()
    })
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
    // The last subscriber left: the kernel stops watching too, and re-reads on the next open.
    const stillWatched =
      this.#notifier?.connections().some((other) => other.subscribed(id)) ?? false
    if (!stillWatched) {
      this.#live.get(id)?.detach()
      this.#live.delete(id)
    }
    return {}
  }

  // ── conversation.submit / abort / submission.abort ──────────────────────────────────────────────

  async #submit(params: Record<string, unknown>): Promise<unknown> {
    this.#writable()
    const id = requiredNumber(params, 'id')
    const conversation = await this.#conversation(id)
    const wantsContinue = optionalBoolean(params, 'continue') === true
    const requestId = optionalString(params, 'requestId')
    const whenBusy = optionalString(params, 'whenBusy')
    const whenIdle = optionalString(params, 'whenIdle')
    const submission = wantsContinue
      ? await admit(conversation, { type: 'continue', requestId })
      : await admit(conversation, {
          type: 'input',
          content: parseInput(params['input']),
          requestId,
          whenBusy: whenBusy as 'steer' | 'followUp' | 'reject' | undefined,
          whenIdle: whenIdle as 'reject' | undefined,
        })
    await this.#options.indexer.touch(id, Date.now(), {})
    await this.track(id)
    await this.refreshBusy(id)
    return { submissionId: submission.id }
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
      case 'already_placed':
      case 'settled':
        return { status }
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
    const resolved = this.#agentChange(change, conversation)
    await conversation.configure(resolved.change, context)
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
    const at = requiredNumber(params, 'at')
    const conversation = await this.#conversation(id)
    const forked = await conversation.fork(
      at as EntryId,
      { ownership: { kind: 'ownerless' } },
      context,
    )
    const forkedId = forked.id as unknown as number
    await this.#options.indexer.touch(forkedId, Date.now(), { parentId: id })
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
    const order = query['order'] === 'ascending' ? 'ascending' : 'descending'
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
    const id = requiredNumber(params, 'id')
    const change: { title?: string | null; archived?: boolean; kind?: string | null } = {}
    if ('title' in params) {
      const title = params['title']
      change.title = title === null ? null : typeof title === 'string' ? title : invalidTitle()
    }
    if ('kind' in params) {
      const kind = params['kind']
      change.kind = kind === null ? null : typeof kind === 'string' ? kind : invalidTitle()
    }
    const archived = optionalBoolean(params, 'archived')
    if (archived !== undefined) change.archived = archived
    await this.#options.indexer.update(id, change)
    this.#notifier?.broadcast('index.changed', {
      id,
      ...(change.title === undefined ? {} : { title: change.title }),
      ...(change.archived === undefined ? {} : { archived: change.archived }),
      lastActivityAt: Date.now(),
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
   * The reply is written first: the method answers `{}` and the closing starts on the next tick, so a
   * client that waits for its answer is not cut off by the process going away. Every later request is
   * `-32008 read-only`-style refused, because `#closing` is already set.
   */
  #shutdown(): Record<string, unknown> {
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

  #writable(): void {
    if (this.#closing) throw new RpcError(Code.readOnly, 'the kernel is shutting down')
    if (!this.#options.config.readOnly) return
    throw new RpcError(Code.readOnly, 'the kernel is read-only')
  }

  async #conversation(id: number): Promise<Conversation> {
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
   * The wire form of an agent change. Extensions and tools cross as names, which the registry resolves
   * here; a name this build does not have is an invalid request rather than a silent no-op.
   */
  #agentChange(
    params: Record<string, unknown>,
    conversation?: Conversation,
  ): { change: AgentChange; instructions?: string } {
    void conversation
    const nested =
      params['agent'] !== undefined &&
      typeof params['agent'] === 'object' &&
      params['agent'] !== null
        ? (params['agent'] as Record<string, unknown>)
        : undefined
    const source = nested ?? params
    const cwd = source['cwd'] ?? source['workspace']
    const instructions = source['instructions']
    const model = source['model']
    const thinking = source['thinkingLevel']
    const maxTurns = source['maxTurns']
    if (
      maxTurns !== undefined &&
      maxTurns !== null &&
      (typeof maxTurns !== 'number' || !Number.isInteger(maxTurns) || maxTurns < 1)
    ) {
      throw invalidParams([
        { path: 'agent.maxTurns', message: 'an integer of 1 or more is required' },
      ])
    }
    const change: AgentChange = {
      ...(cwd === undefined ? {} : { cwd: cwd === null ? null : String(cwd) }),
      ...(instructions === undefined
        ? {}
        : { instructions: instructions === null ? null : String(instructions) }),
      ...(model === undefined ? {} : { model: model === null ? null : parseModel(model) }),
      ...(thinking === undefined
        ? {}
        : { thinkingLevel: thinking === null ? null : (String(thinking) as never) }),
      ...(maxTurns === undefined ? {} : { maxTurns: maxTurns as number | null }),
      ...(source['extensions'] === undefined
        ? {}
        : { extensions: this.#resolveExtensions(source['extensions']) }),
      ...(source['modelTools'] === undefined
        ? {}
        : { modelTools: this.#resolveModelTools(source['modelTools']) }),
    }
    const text = typeof change.instructions === 'string' ? change.instructions : undefined
    return text === undefined ? { change } : { change, instructions: text }
  }

  #resolveExtensions(value: unknown): AgentChange['extensions'] {
    const registry = this.#options.registry
    const byName = (name: unknown) => {
      const extension = typeof name === 'string' ? registry.snapshot().extension(name) : undefined
      if (extension === undefined) {
        throw invalidParams([
          { path: 'agent.extensions', message: `no such extension: ${String(name)}` },
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
      { path: 'agent.extensions', message: 'names, or {add, remove}, are required' },
    ])
  }

  #resolveModelTools(value: unknown): AgentChange['modelTools'] {
    const snapshot = this.#options.registry.snapshot()
    const byName = (name: unknown) => {
      const found = snapshot.tools().find((entry) => entry.tool.name === name)
      if (found === undefined) {
        throw invalidParams([
          { path: 'agent.modelTools', message: `no such tool: ${String(name)}` },
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
      { path: 'agent.modelTools', message: 'names, or {add, remove}, are required' },
    ])
  }
}

// ── free functions ──────────────────────────────────────────────────────────────────────────────

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

function invalidTitle(): never {
  throw invalidParams([{ path: 'title', message: 'a string or null is required' }])
}

function parseModel(value: unknown): ModelRef {
  if (typeof value !== 'object' || value === null) {
    throw invalidParams([{ path: 'agent.model', message: 'provider and modelId are required' }])
  }
  const model = value as { provider?: unknown; modelId?: unknown; id?: unknown }
  const provider = model.provider ?? model.id
  const modelId = model.modelId ?? model.id
  if (typeof provider !== 'string' || typeof modelId !== 'string') {
    throw invalidParams([{ path: 'agent.model', message: 'provider and modelId are required' }])
  }
  return { provider, modelId }
}

/** `input` as Pi takes it: a string, or text and image blocks. */
function parseInput(
  value: unknown,
): string | { type: 'text'; text: string }[] | (never | { type: string })[] {
  if (typeof value === 'string') return value
  if (value === undefined) {
    throw invalidParams([{ path: 'input', message: 'input or continue is required' }])
  }
  if (!Array.isArray(value)) {
    throw invalidParams([
      { path: 'input', message: 'a string or an array of content blocks is required' },
    ])
  }
  return value as never
}

async function admit(
  conversation: Conversation,
  submission:
    | { type: 'continue'; requestId?: string }
    | {
        type: 'input'
        content: unknown
        requestId?: string
        whenBusy?: 'steer' | 'followUp' | 'reject'
        whenIdle?: 'reject'
      },
): Promise<{ id: number }> {
  try {
    const admitted = await conversation.submit(submission as never, context)
    return { id: admitted.id as unknown as number }
  } catch (error) {
    throw submissionError(error, conversation.id as unknown as number)
  }
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

export { warn }
