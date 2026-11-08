import type { Context } from '@earendil-works/chord'
import { awaitWithContext } from '@earendil-works/chord/context'
import type { Harness } from '@earendil-works/pi-durable'
import { randomBytes } from 'node:crypto'
import { decide, type Call, type Platform, type Rule } from './decide.ts'
import type { PolicyStore } from './store.ts'
import { Code, RpcError, invalidParams } from '../protocol/rpc.ts'
import { info, warn } from '../log.ts'

/** One choice on an approval card. `scope` is what an "always" answer remembers. */
export type ApprovalOption = {
  id: 'proceed_once' | 'proceed_always_tool' | 'proceed_always_file' | 'cancel'
  label: string
  scope: { tool: string } | { path: string } | null
}

/** A call waiting for a person, as `approval.requested` and `conversation.open`'s `pending[]` carry it. */
export type ApprovalRequest = {
  requestId: string
  id: number
  tool: string
  args: Record<string, unknown>
  options: ApprovalOption[]
  scopeHint: string
}

type Waiting = ApprovalRequest & {
  settle: (outcome: string) => void
  timer: ReturnType<typeof setTimeout>
}

export type ApprovalsOptions = {
  harness: Harness
  policy: PolicyStore
  /** Every connection hears approval.requested and approval.resolved. */
  notify: (method: string, payload: unknown) => void
  /** The conversation's working directory, which a relative file argument is resolved against. */
  cwdOf: (conversationId: number) => Promise<string | undefined>
  /** How long a card waits before the kernel answers it "cancel" itself. */
  timeoutMs?: number
  platform?: Platform
  context: Context
}

/** The ten minutes droid gave a person to answer. */
export const APPROVAL_TIMEOUT_MS = 10 * 60 * 1000

/**
 * The calls that wait for a person, between the tool hook that stops them and the `approval.respond` that
 * lets them go.
 *
 * Every change is an entry first and a notification second, so a client that reads the transcript and one
 * that hears the notifications agree: `droi.approval` `pending`, then `allowed`, `denied` or `abandoned`.
 * The first answer wins; the table entry is taken before anything is awaited, so a second answer cannot
 * find it.
 */
export class Approvals {
  readonly #options: ApprovalsOptions
  readonly #waiting = new Map<string, Waiting>()
  #closed = false

  constructor(options: ApprovalsOptions) {
    this.#options = options
  }

  /** The tool hook: undefined lets the call run, `{block}` ends it with that sentence. */
  async beforeTool(
    call: Call,
    conversationId: number,
    context: Context,
  ): Promise<{ block: string } | undefined> {
    await awaitWithContext(this.#options.policy.ready(), context)
    const cwd = await this.#options.cwdOf(conversationId)
    const policy = this.#options.policy.effective(conversationId)
    const decision = decide(call, policy, cwd, this.#options.platform)
    if (decision.verdict === 'allow') return undefined
    if (this.#closed) return { block: 'The kernel is closing; the call did not run.' }

    const request: ApprovalRequest = {
      requestId: `req-${randomBytes(6).toString('hex')}`,
      id: conversationId,
      tool: call.name,
      args: call.arguments ?? {},
      options: optionsFor(call.name, decision.file),
      scopeHint: decision.scopeHint,
    }
    let settle!: (outcome: string) => void
    const answered = new Promise<string>((resolve) => {
      settle = resolve
    })
    await this.#record(request, { status: 'pending' })
    const timer = setTimeout(
      () => void this.#expire(request.requestId, 'timeout'),
      this.#timeoutMs(),
    )
    timer.unref?.()
    this.#waiting.set(request.requestId, { ...request, settle, timer })
    this.#options.notify('approval.requested', request)

    let outcome: string
    try {
      outcome = await awaitWithContext(answered, context)
    } catch (error) {
      // An abort, or the kernel closing: the card is over, and nobody answered it.
      await this.#expire(request.requestId, 'aborted')
      throw error
    }
    if (outcome === 'cancel') return { block: 'The user denied this call.' }
    if (outcome === 'closing') return { block: 'The kernel is closing; the call did not run.' }
    if (outcome === 'timeout') {
      return {
        block: `No answer came in ${Math.round(this.#timeoutMs() / 60_000)} minutes; the call did not run.`,
      }
    }
    return undefined
  }

  /** `approval.respond`: the first answer for a request decides it. */
  async respond(params: Record<string, unknown>, by: string): Promise<{ status: 'accepted' }> {
    const requestId = params['requestId']
    if (typeof requestId !== 'string') {
      throw invalidParams([{ path: 'requestId', message: 'a string is required' }])
    }
    const outcome = params['outcome']
    const waiting = this.#waiting.get(requestId)
    if (waiting === undefined) {
      throw new RpcError(Code.requestNotFound, 'request not found', { requestId })
    }
    const option = waiting.options.find((candidate) => candidate.id === outcome)
    if (option === undefined) {
      throw invalidParams([
        { path: 'outcome', message: waiting.options.map((candidate) => candidate.id).join(', ') },
      ])
    }
    this.#waiting.delete(requestId)
    clearTimeout(waiting.timer)
    if (option.scope !== null) {
      const rule: Rule =
        'tool' in option.scope
          ? { kind: 'tool', tool: option.scope.tool }
          : { kind: 'file', path: option.scope.path }
      this.#options.policy.addRule(waiting.id, rule)
    }
    // The answer stands even when the transcript cannot take its record: the card is already out of the table,
    // and a run left waiting on it would wait for good.
    await this.#record(waiting, {
      status: option.id === 'cancel' ? 'denied' : 'allowed',
      outcome: option.id,
      ...(option.scope === null ? {} : { scope: option.scope }),
    }).catch((failure: unknown) =>
      warn('approval answer not recorded', { requestId, error: String(failure) }),
    )
    this.#options.notify('approval.resolved', {
      requestId,
      id: waiting.id,
      outcome: option.id,
      by,
    })
    waiting.settle(option.id)
    return { status: 'accepted' }
  }

  /** The cards still waiting in a conversation, for `conversation.open`. */
  pendingFor(conversationId: number): (ApprovalRequest & { kind: 'approval' })[] {
    const out: (ApprovalRequest & { kind: 'approval' })[] = []
    for (const waiting of this.#waiting.values()) {
      if (waiting.id !== conversationId) continue
      out.push({
        kind: 'approval',
        requestId: waiting.requestId,
        id: waiting.id,
        tool: waiting.tool,
        args: waiting.args,
        options: waiting.options,
        scopeHint: waiting.scopeHint,
      })
    }
    return out
  }

  /**
   * The kernel is closing: every card is over. The runs that wait on them are interrupted by the close, and Pi
   * asks again, with a new card, when it resumes them.
   */
  async close(): Promise<void> {
    this.#closed = true
    for (const requestId of [...this.#waiting.keys()]) await this.#expire(requestId, 'closing')
  }

  /** End a card nobody answered: `abandoned` in the transcript, "cancel" by the kernel on the wire. */
  async #expire(requestId: string, why: 'timeout' | 'aborted' | 'closing'): Promise<void> {
    const waiting = this.#waiting.get(requestId)
    if (waiting === undefined) return
    this.#waiting.delete(requestId)
    clearTimeout(waiting.timer)
    info('approval not answered', { requestId, conversationId: waiting.id, why })
    await this.#record(waiting, { status: 'abandoned' }).catch(() => undefined)
    this.#options.notify('approval.resolved', {
      requestId,
      id: waiting.id,
      outcome: 'cancel',
      by: 'kernel',
    })
    // An abort has already ended the wait; a timeout and a close end it here, so a close never waits on a run
    // that keeps its context.
    if (why !== 'aborted') waiting.settle(why === 'timeout' ? 'timeout' : 'closing')
  }

  async #record(request: ApprovalRequest, change: Record<string, unknown>): Promise<void> {
    await this.#options.harness.commit(async (tx) => {
      await tx.appendEntry(request.id as never, {
        kind: 'droi.approval',
        data: {
          requestId: request.requestId,
          tool: request.tool,
          args: request.args,
          ...change,
          at: Date.now(),
        } as never,
      })
    }, this.#options.context)
  }

  #timeoutMs(): number {
    return this.#options.timeoutMs ?? APPROVAL_TIMEOUT_MS
  }
}

/**
 * A card's choices. "Always" for a file only when the call names one: a command names no file the kernel can
 * trust, so a command's card offers once, always this tool, and cancel.
 */
function optionsFor(tool: string, file: string | undefined): ApprovalOption[] {
  const options: ApprovalOption[] = [
    { id: 'proceed_once', label: 'Allow once', scope: null },
    { id: 'proceed_always_tool', label: `Always allow ${tool}`, scope: { tool } },
  ]
  if (file !== undefined) {
    options.push({
      id: 'proceed_always_file',
      label: `Always allow ${file}`,
      scope: { path: file },
    })
  }
  options.push({ id: 'cancel', label: 'Cancel', scope: null })
  return options
}
