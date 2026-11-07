import type { ConversationWindow, ConversationView } from '@earendil-works/pi-durable'
import type { Op } from '@earendil-works/chord/delta'
import type { Connection } from '../protocol/server.ts'

/** How a connection asked to be kept up to date: whole views, or the deltas between them. */
export type SubscribeMode = 'view' | 'delta'

/** What one `conversation.view` notification carries. */
export type ViewNotification = {
  id: number
  kind: 'value' | 'full' | 'failed'
  /** The whole view, for `view`; `null` for `delta` and `full`. */
  value: ConversationView | null
  /** Ops that turn the client's last value into this one; empty on `full`, whose `value` replaces it. */
  ops: readonly Op[]
  retired?: boolean
  error?: unknown
}

type Pending = {
  /** The newest value; undefined only when the push is a failure. */
  value?: ConversationView
  ops: Op[]
  retired: boolean
  failure?: unknown
}

/**
 * One connection's window into one conversation.
 *
 * Two rules make a view push bearable on a busy conversation:
 *
 * 1. **At most five a second.** A run with two tools in parallel commits dozens of times a second, and
 *    every commit is a whole window the client would redraw. Pushes are coalesced inside the interval:
 *    the newest value replaces the pending one and the ops accumulate in order, so what arrives is one
 *    consistent step forward. The last state is always delivered — the timer keeps running until nothing
 *    is pending, even if that takes several intervals.
 * 2. **`full` when deltas cannot be trusted.** A `delta` subscriber holds the value from the answer to
 *    `conversation.open` and applies ops to it. Everything between two pushes folds into one exact set of
 *    ops. What is not exact is a retirement: the client's value was replaced, not changed, so it cannot
 *    place the following ops. Then the kernel sends `full` with the whole value.
 */
export class ViewWindow {
  readonly conversationId: number
  readonly mode: SubscribeMode
  #connection: Connection
  #intervalMs: number
  #pending: Pending | undefined
  #timer: ReturnType<typeof setTimeout> | undefined
  #nextAllowedAt = 0
  #stopped = false
  #pushed = 0

  constructor(options: {
    connection: Connection
    conversationId: number
    mode: SubscribeMode
    /** How many pushes a second this window allows. */
    perSecond: number
  }) {
    this.#connection = options.connection
    this.conversationId = options.conversationId
    this.mode = options.mode
    this.#intervalMs = Math.ceil(1000 / Math.max(options.perSecond, 1))
  }

  /** Notifications this window has sent, for a test and for the log. */
  get pushed(): number {
    return this.#pushed
  }

  /** The number of pushes a second this window allows. */
  get perSecond(): number {
    return Math.ceil(1000 / this.#intervalMs)
  }

  /** A commit that carries a new value. */
  advance(value: ConversationView, ops: readonly Op[], retired: boolean): void {
    if (this.#stopped) return
    if (this.#pending === undefined || this.#pending.failure !== undefined) {
      this.#pending = { value, ops: [...ops], retired }
    } else {
      this.#pending.value = value
      this.#pending.ops.push(...ops)
      this.#pending.retired ||= retired
    }
    this.#schedule()
  }

  /** The source could not produce a value: the client gets `failed` and keeps what it holds. */
  fail(error: unknown): void {
    if (this.#stopped) return
    this.#pending = { ops: [], retired: false, failure: error }
    this.#schedule()
  }

  stop(): void {
    this.#stopped = true
    if (this.#timer !== undefined) clearTimeout(this.#timer)
    this.#timer = undefined
    this.#pending = undefined
  }

  #schedule(): void {
    if (this.#stopped || this.#timer !== undefined) return
    const wait = Math.max(0, this.#nextAllowedAt - Date.now())
    this.#timer = setTimeout(() => {
      this.#timer = undefined
      this.#flush()
    }, wait)
    this.#timer.unref?.()
  }

  #flush(): void {
    if (this.#stopped) return
    const pending = this.#pending
    if (pending === undefined) return
    const now = Date.now()
    if (now < this.#nextAllowedAt) {
      this.#schedule()
      return
    }
    this.#pending = undefined
    this.#nextAllowedAt = now + this.#intervalMs
    this.#send(pending)
    // Something arrived while this one was sending: it waits for the next interval, not for a commit.
    if (this.#pending !== undefined) this.#schedule()
  }

  #send(pending: Pending): void {
    if (!this.#connection.open) {
      // Nobody is listening. The window stops rather than collect a queue nothing will read.
      this.stop()
      return
    }
    this.#pushed++
    if (pending.failure !== undefined) {
      this.#connection.notify('conversation.view', {
        id: this.conversationId,
        kind: 'failed',
        value: null,
        ops: [],
        error: describe(pending.failure),
      } satisfies ViewNotification)
      return
    }
    const value = pending.value
    if (value === undefined) return
    if (this.mode === 'view') {
      this.#connection.notify('conversation.view', {
        id: this.conversationId,
        kind: 'value',
        value,
        ops: pending.ops,
        retired: pending.retired,
      } satisfies ViewNotification)
      return
    }
    if (pending.retired) {
      this.#connection.notify('conversation.view', {
        id: this.conversationId,
        kind: 'full',
        value,
        ops: [],
      } satisfies ViewNotification)
      return
    }
    this.#connection.notify('conversation.view', {
      id: this.conversationId,
      kind: 'value',
      value: null,
      ops: pending.ops,
    } satisfies ViewNotification)
  }
}

function describe(error: unknown): unknown {
  if (error instanceof Error) return { name: error.name, message: error.message }
  return typeof error === 'object' && error !== null ? error : { message: String(error) }
}

/** The window a `conversation.open` asks for, with the default the contract fixes. */
export function parseWindow(value: Record<string, unknown> | undefined): ConversationWindow {
  if (value === undefined) return { min: 100, max: 200 }
  const min = typeof value['min'] === 'number' ? value['min'] : 100
  const max = typeof value['max'] === 'number' ? value['max'] : Math.max(min, 200)
  return { min, max }
}

/** The mode a `conversation.open` asks for; `view` when absent or unknown would hide a typo. */
export function parseMode(value: unknown): SubscribeMode {
  if (value === undefined || value === null) return 'view'
  if (value === 'view' || value === 'delta') return value
  return 'view'
}
