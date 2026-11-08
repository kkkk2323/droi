// The conversations this Client has open, kept as the Kernel pushes them. Each is opened
// with `subscribe: "delta"`: the Kernel sends Pi's ops between views, which cost the Relay
// a fraction of whole views, and a `full` view whenever the ops would not follow on from
// what is held. The transcript held is every entry seen, not just the window: entries never
// change once written, so the ones a window drops stay here, and older pages join them.
import type { KernelConnection } from './connection'
import { kernelStore } from './kernel-state'
import type { ConversationId, Entry, OpenResult, View, ViewPush } from './kernel-wire'
import { KernelCode } from './kernel-wire'
import { KernelError } from './kernel-socket'
import { applyOps } from './view-ops'

/** The window a phone asks for: fewer entries than the window's, as each crosses the Relay. */
export const PHONE_WINDOW = { min: 50, max: 100 } as const
/** Entries per older page. */
export const OLDER_PAGE = 100
/** How long a conversation stays open after nothing shows it, so going back to it is instant. */
const LINGER_MS = 15_000

export type LoadState = 'not-loaded' | 'loading' | 'loaded'

/** A message on its way, shown until the transcript or the queue has it. */
export interface Sending {
  requestId: string
  text: string
  images: Array<{ data: string; mediaType: string }>
  /** The newest entry when it was sent: its user entry comes after. */
  since: number
}

/** A queued message a stop took back from the Kernel; it goes again only when sent again. */
export interface Paused {
  requestId: string
  text: string
  images: Array<{ data: string; mediaType: string }>
}

export interface ConversationSnapshot {
  loadState: LoadState
  /** It was loaded once on any socket, so it can take a message even while it opens again. */
  hasLoaded: boolean
  /** Every entry held, oldest first. */
  entries: readonly Entry[]
  /** The documents of the newest view: pi.live, pi.inbox, pi.agent, pi.usage. */
  docs: Readonly<Record<string, unknown>>
  /** Older entries are still to load. */
  hasOlder: boolean
  /** Why it could not be opened, or its view failed. */
  error: string | null
  sending: readonly Sending[]
  paused: readonly Paused[]
}

const NONE: readonly never[] = []

const EMPTY: ConversationSnapshot = {
  loadState: 'not-loaded',
  hasLoaded: false,
  entries: [],
  docs: {},
  hasOlder: false,
  error: null,
  sending: NONE,
  paused: NONE,
}

interface Held {
  watchers: number
  linger: ReturnType<typeof setTimeout> | null
  /** The socket generation it was opened on; -1 while not open. */
  openedOn: number
  opening: boolean
  /** Pushes that arrived while the open was in flight. */
  early: ViewPush[]
  view: View | null
  entries: Entry[]
  reachedStart: boolean
  loadingOlder: Promise<boolean> | null
  hasLoaded: boolean
  error: string | null
  /** The next open is a person looking, which marks it seen. */
  seen: boolean
  sending: Sending[]
  paused: Paused[]
  snapshot: ConversationSnapshot | null
  listeners: Set<() => void>
}

export interface ConversationStore {
  /** Keeps the conversation open while the returned function has not been called. */
  watch(id: ConversationId): () => void
  snapshot(id: ConversationId): ConversationSnapshot
  subscribe(id: ConversationId, listener: () => void): () => void
  /** Loads the next older page; resolves true when it brought any. */
  loadOlder(id: ConversationId): Promise<boolean>
  /** Shows a message on its way until the Kernel has it. */
  addSending(id: ConversationId, send: Sending): void
  dropSending(id: ConversationId, requestId: string): void
  setPaused(id: ConversationId, paused: Paused[]): void
  dropPaused(id: ConversationId, requestId: string): void
}

const stores = new WeakMap<KernelConnection, ConversationStore>()

export function conversationStore(connection: KernelConnection): ConversationStore {
  let store = stores.get(connection)
  if (!store) {
    store = createConversationStore(connection)
    stores.set(connection, store)
  }
  return store
}

/** The transcript held, with a view merged in (Pi's ConversationTranscript.merge, without gap rows). */
export function mergeEntries(
  held: readonly Entry[],
  view: View,
): { entries: Entry[]; reachedStart?: boolean } {
  const window = view.entries
  const startsTranscript = view.before === undefined
  if (held.length === 0) return { entries: window.slice(), reachedStart: startsTranscript }
  const newest = held[held.length - 1]!.id
  // A window whose `before` lies past what is held does not continue it (a reconnect after
  // a long run): what is between is missing, so the held entries go and the window stands.
  if (view.before !== undefined && view.before > newest)
    return { entries: window.slice(), reachedStart: false }
  if (window.length === 0)
    return { entries: held.slice(), ...(startsTranscript ? { reachedStart: true } : {}) }
  const first = window[0]!.id
  const kept = held.filter((entry) => entry.id < first)
  return { entries: kept.concat(window), ...(startsTranscript ? { reachedStart: true } : {}) }
}

export function createConversationStore(connection: KernelConnection): ConversationStore {
  const held = new Map<ConversationId, Held>()
  const kernel = kernelStore(connection)

  const entry = (id: ConversationId): Held => {
    let h = held.get(id)
    if (!h) {
      h = {
        watchers: 0,
        linger: null,
        openedOn: -1,
        opening: false,
        early: [],
        view: null,
        entries: [],
        reachedStart: false,
        loadingOlder: null,
        hasLoaded: false,
        error: null,
        seen: true,
        sending: [],
        paused: [],
        snapshot: null,
        listeners: new Set(),
      }
      held.set(id, h)
    }
    return h
  }

  const changed = (h: Held) => {
    h.snapshot = null
    for (const listener of h.listeners) listener()
  }

  // A send is shown until a user entry after it holds its text, or the queue does.
  const settleSending = (h: Held) => {
    if (h.sending.length === 0) return
    const inbox = (
      (h.view?.docs['pi.inbox'] as { items?: Array<{ content?: unknown }> } | undefined)?.items ??
      []
    ).map((item) => plainText(item.content))
    const users = h.entries.filter((e) => e.kind === 'pi.user')
    h.sending = h.sending.filter(
      (send) =>
        !inbox.includes(send.text) &&
        !users.some((e) => e.id > send.since && plainText(e.model?.[0]?.content) === send.text),
    )
  }

  const take = (h: Held, view: View) => {
    h.view = view
    const merged = mergeEntries(h.entries, view)
    h.entries = merged.entries
    if (merged.reachedStart !== undefined) h.reachedStart = merged.reachedStart
    settleSending(h)
  }

  const apply = (id: ConversationId, h: Held, push: ViewPush) => {
    if (push.kind === 'failed') {
      h.error = push.error?.message ?? 'The computer could not read this session.'
      return
    }
    if (push.value) {
      take(h, push.value)
      h.error = null
      return
    }
    if (!h.view) return
    try {
      take(h, applyOps(h.view, push.ops))
    } catch {
      // The ops do not follow on from the view held: open again for a whole one.
      h.openedOn = -1
      void open(id, h)
    }
  }

  const open = async (id: ConversationId, h: Held) => {
    if (h.opening || h.watchers === 0) return
    const gen = connection.generation()
    if (connection.getState().status !== 'connected' || h.openedOn === gen) return
    h.opening = true
    h.early = []
    if (!h.view) changed(h)
    try {
      const seen = h.seen
      const result = await connection.call<OpenResult>('conversation.open', {
        id,
        window: PHONE_WINDOW,
        subscribe: 'delta',
        ...(seen ? {} : { seen: false }),
      })
      if (connection.generation() !== gen) return
      // Opening it again on this socket is not a person looking.
      h.seen = false
      h.openedOn = gen
      take(h, result.view)
      for (const push of h.early) apply(id, h, push)
      h.early = []
      h.hasLoaded = true
      h.error = null
      kernel.opened(id, result.pending ?? [], result.policy)
    } catch (error) {
      if (connection.generation() === gen) {
        h.error =
          error instanceof KernelError && error.code === KernelCode.conversationNotFound
            ? 'Session not found'
            : error instanceof Error
              ? error.message
              : String(error)
      }
    } finally {
      h.opening = false
      changed(h)
    }
  }

  const close = (id: ConversationId, h: Held) => {
    h.linger = null
    if (h.watchers > 0) return
    const wasOpen =
      h.openedOn === connection.generation() && connection.getState().status === 'connected'
    held.delete(id)
    if (wasOpen) void connection.call('conversation.unwatch', { id }).catch(() => {})
  }

  connection.onNotification(({ method, params }) => {
    if (method !== 'conversation.view') return
    const push = params as unknown as ViewPush
    const h = held.get(push.id)
    if (!h) return
    if (h.opening) {
      h.early.push(push)
      return
    }
    if (h.openedOn !== connection.generation()) return
    apply(push.id, h, push)
    changed(h)
  })
  // A new socket holds no subscriptions: every conversation shown opens again.
  connection.subscribe(() => {
    if (connection.getState().status !== 'connected') return
    for (const [id, h] of held) if (h.watchers > 0) void open(id, h)
  })

  return {
    watch(id) {
      const h = entry(id)
      h.watchers += 1
      if (h.linger) {
        clearTimeout(h.linger)
        h.linger = null
      }
      void open(id, h)
      let released = false
      return () => {
        if (released) return
        released = true
        h.watchers -= 1
        if (h.watchers === 0 && !h.linger) h.linger = setTimeout(() => close(id, h), LINGER_MS)
      }
    },
    snapshot(id) {
      const h = held.get(id)
      if (!h) return EMPTY
      if (h.snapshot) return h.snapshot
      const loaded = h.openedOn === connection.generation() && h.view !== null
      h.snapshot = {
        loadState: loaded ? 'loaded' : h.opening ? 'loading' : 'not-loaded',
        hasLoaded: h.hasLoaded,
        entries: h.entries,
        docs: h.view?.docs ?? {},
        hasOlder: !h.reachedStart && h.entries.length > 0,
        error: h.error,
        sending: h.sending.length > 0 ? h.sending : NONE,
        paused: h.paused.length > 0 ? h.paused : NONE,
      }
      return h.snapshot
    },
    addSending(id, send) {
      const h = entry(id)
      h.sending = [...h.sending, send]
      changed(h)
    },
    dropSending(id, requestId) {
      const h = held.get(id)
      if (!h) return
      h.sending = h.sending.filter((s) => s.requestId !== requestId)
      changed(h)
    },
    setPaused(id, paused) {
      const h = entry(id)
      h.paused = [...h.paused, ...paused]
      changed(h)
    },
    dropPaused(id, requestId) {
      const h = held.get(id)
      if (!h) return
      h.paused = h.paused.filter((p) => p.requestId !== requestId)
      changed(h)
    },
    subscribe(id, listener) {
      const h = entry(id)
      h.listeners.add(listener)
      return () => void h.listeners.delete(listener)
    },
    loadOlder(id) {
      const h = held.get(id)
      if (!h || h.reachedStart || h.entries.length === 0) return Promise.resolve(false)
      if (h.loadingOlder) return h.loadingOlder
      const oldest = h.entries[0]!.id
      h.loadingOlder = connection
        .call<{ items: Entry[] }>('conversation.entries', {
          id,
          limit: OLDER_PAGE,
          query: { order: 'descending', maxEntryId: oldest - 1 },
        })
        .then((page) => {
          // The transcript may have been replaced meanwhile; then the page is not its start.
          if (h.entries[0]?.id !== oldest) return false
          const older = page.items.slice().reverse()
          h.entries = older.concat(h.entries)
          if (page.items.length < OLDER_PAGE) h.reachedStart = true
          return older.length > 0
        })
        .finally(() => {
          h.loadingOlder = null
          changed(h)
        })
      return h.loadingOlder
    },
  }
}

/** The text of a message's content: a string, or its text blocks joined. */
export function plainText(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .filter(
      (c): c is { type: 'text'; text: string } =>
        typeof c === 'object' && c !== null && (c as { type?: unknown }).type === 'text',
    )
    .map((c) => c.text)
    .join('\n')
}
