import type { Context } from '@earendil-works/chord'
import { awaitWithContext } from '@earendil-works/chord/context'
import type { Harness } from '@earendil-works/pi-durable'
import { randomBytes } from 'node:crypto'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import { Code, RpcError, invalidParams } from '../protocol/rpc.ts'
import { info, warn } from '../log.ts'

/** How long a blocking question waits before the kernel ends it, as an approval does. */
export const ASK_TIMEOUT_MS = 10 * 60_000

export type AskOption = { id: string; label: string }
export type AskQuestion = {
  questionId: string
  question: string
  options?: AskOption[]
  multiSelect?: boolean
}
export type AskAnswer = { questionId: string; selected?: string[]; text?: string | null }

/** `ask.requested`'s payload, and what `conversation.open` lists while it waits. */
export type AskRequest = {
  requestId: string
  id: number
  questions: AskQuestion[]
  context: string
  blocking: boolean
}

/** What the tool hands over: the model's arguments, already checked against the schema. */
export type AskArgs = {
  context: string
  blocking?: boolean
  questions: {
    questionId?: string
    question: string
    options?: AskOption[]
    multiSelect?: boolean
  }[]
}

export type AskResult = { output: { type: 'text'; text: string }[]; isError?: boolean }

type Waiting = AskRequest & {
  /** A blocking question's run, waiting on the answer; absent for one that does not block. */
  settle?: (outcome: Outcome) => void
  timer?: ReturnType<typeof setTimeout>
}

type Outcome =
  | { kind: 'answered'; answers: AskAnswer[] }
  | { kind: 'cancelled' }
  | { kind: 'timeout' }
  | { kind: 'closing' }

export type AsksOptions = {
  harness: Harness
  db: BunSqliteDatabase
  notify: (method: string, payload: unknown) => void
  /** A conversation that got input from a non-blocking answer: the kernel tells clients about its run. */
  submitted: (conversationId: number) => Promise<void>
  timeoutMs?: number
  context: Context
}

/**
 * The `ask_user` tool's questions (design/m3-kernel.md §3).
 *
 * A blocking question is an approval with answers: the run waits, the first `ask.respond` decides, and the
 * answer is the tool's result. A non-blocking one returns at once; its answer arrives later as the next user
 * input of the conversation, which the kernel submits itself so every client only answers. It outlives the run
 * and the process, so it is kept in `droi_open_questions` until it is answered.
 */
export class Asks {
  readonly #options: AsksOptions
  readonly #waiting = new Map<string, Waiting>()
  #closed = false

  constructor(options: AsksOptions) {
    this.#options = options
  }

  /** Make the table, and take back the non-blocking questions a previous process left open. */
  async open(): Promise<void> {
    await this.#options.db.exec(`CREATE TABLE IF NOT EXISTS droi_open_questions (
      request_id TEXT PRIMARY KEY,
      conversation_id INTEGER NOT NULL,
      request TEXT NOT NULL,
      at INTEGER NOT NULL
    )`)
    const rows = await this.#options.db.all<{ request: string }>(
      'SELECT request FROM droi_open_questions ORDER BY at',
    )
    for (const row of rows) {
      try {
        const request = JSON.parse(row.request) as AskRequest
        this.#waiting.set(request.requestId, request)
      } catch {
        // A row this build cannot read is a question nobody can answer here.
      }
    }
  }

  /** The non-blocking questions still open, which the start-up clean-up must leave alone. */
  openIds(): Set<string> {
    return new Set([...this.#waiting.values()].filter((w) => !w.blocking).map((w) => w.requestId))
  }

  /** The tool: ask, and for a blocking question wait for the answer. */
  async ask(args: AskArgs, conversationId: number, context: Context): Promise<AskResult> {
    if (args.context.trim() === '') {
      return error('context must say what the answers are for; ask again with it.')
    }
    const questions: AskQuestion[] = args.questions.map((q, index) => ({
      questionId: q.questionId ?? `q${index + 1}`,
      question: q.question,
      ...(q.options === undefined || q.options.length === 0 ? {} : { options: q.options }),
      ...(q.multiSelect === undefined ? {} : { multiSelect: q.multiSelect }),
    }))
    if (new Set(questions.map((q) => q.questionId)).size !== questions.length) {
      return error('Each question needs its own questionId; ask again.')
    }
    if (this.#closed) return error('The kernel is closing; the question was not asked.')

    const request: AskRequest = {
      requestId: `req-ask-${randomBytes(6).toString('hex')}`,
      id: conversationId,
      questions,
      context: args.context.trim(),
      blocking: args.blocking !== false,
    }
    await this.#record(request, { status: 'pending' })

    if (!request.blocking) {
      this.#waiting.set(request.requestId, request)
      await this.#options.db
        .run(
          'INSERT OR REPLACE INTO droi_open_questions (request_id, conversation_id, request, at) VALUES (?, ?, ?, ?)',
          request.requestId,
          conversationId,
          JSON.stringify(request),
          Date.now(),
        )
        .catch((failure: unknown) =>
          warn('open question not kept', { requestId: request.requestId, error: String(failure) }),
        )
      this.#options.notify('ask.requested', request)
      return {
        output: [
          {
            type: 'text',
            text: 'The question is shown to the user; the answer will arrive as a message. Go on with the work.',
          },
        ],
      }
    }

    let settle!: (outcome: Outcome) => void
    const answered = new Promise<Outcome>((resolve) => {
      settle = resolve
    })
    const timer = setTimeout(
      () => void this.#expire(request.requestId, 'timeout'),
      this.#timeoutMs(),
    )
    timer.unref?.()
    this.#waiting.set(request.requestId, { ...request, settle, timer })
    this.#options.notify('ask.requested', request)

    let outcome: Outcome
    try {
      outcome = await awaitWithContext(answered, context)
    } catch (failure) {
      await this.#expire(request.requestId, 'aborted')
      throw failure
    }
    switch (outcome.kind) {
      case 'answered':
        return {
          output: [
            { type: 'text', text: `The user answered.\n\n${answerText(request, outcome.answers)}` },
          ],
        }
      case 'cancelled':
        return error('The user dismissed the question without answering.')
      case 'timeout':
        return error(`No answer came in ${Math.round(this.#timeoutMs() / 60_000)} minutes.`)
      case 'closing':
        return error('The kernel is closing; the question was not answered.')
    }
  }

  /** `ask.respond`: the first answer for a question decides it. */
  async respond(params: Record<string, unknown>, by: string): Promise<{ status: 'accepted' }> {
    const requestId = params['requestId']
    if (typeof requestId !== 'string') {
      throw invalidParams([{ path: 'requestId', message: 'a string is required' }])
    }
    const waiting = this.#waiting.get(requestId)
    if (waiting === undefined) {
      throw new RpcError(Code.requestNotFound, 'request not found', { requestId })
    }
    const cancelled = params['cancelled'] === true
    const answers = cancelled ? [] : parseAnswers(waiting, params['answers'])
    this.#waiting.delete(requestId)
    if (waiting.timer !== undefined) clearTimeout(waiting.timer)

    const change = cancelled
      ? { status: 'answered', cancelled: true }
      : { status: 'answered', answers }
    if (waiting.blocking) {
      // The answer stands even when the transcript cannot take its record: a run left waiting would wait for
      // good.
      await this.#record(waiting, change).catch((failure: unknown) =>
        warn('ask answer not recorded', { requestId, error: String(failure) }),
      )
    } else {
      await this.#answerLater(waiting, change, cancelled ? undefined : answerText(waiting, answers))
    }
    this.#options.notify('ask.resolved', {
      requestId,
      id: waiting.id,
      ...(cancelled ? {} : { answers }),
      cancelled,
      by,
    })
    waiting.settle?.(cancelled ? { kind: 'cancelled' } : { kind: 'answered', answers })
    return { status: 'accepted' }
  }

  /** The questions still waiting in a conversation, for `conversation.open`. */
  pendingFor(conversationId: number): (AskRequest & { kind: 'ask' })[] {
    const out: (AskRequest & { kind: 'ask' })[] = []
    for (const waiting of this.#waiting.values()) {
      if (waiting.id !== conversationId) continue
      out.push({
        kind: 'ask',
        requestId: waiting.requestId,
        id: waiting.id,
        questions: waiting.questions,
        context: waiting.context,
        blocking: waiting.blocking,
      })
    }
    return out
  }

  /**
   * The kernel is closing: a blocking question ends with its run, which Pi asks again when it resumes. A
   * non-blocking one stays open in the table, for the next process to take back.
   */
  async close(): Promise<void> {
    this.#closed = true
    for (const waiting of [...this.#waiting.values()]) {
      if (waiting.blocking) await this.#expire(waiting.requestId, 'closing')
    }
  }

  /**
   * A non-blocking answer: the record and the input it becomes, in one commit, so the conversation never has
   * one without the other. The table's row goes after.
   */
  async #answerLater(
    waiting: AskRequest,
    change: Record<string, unknown>,
    text: string | undefined,
  ): Promise<void> {
    const harness = this.#options.harness
    const conversation = await harness.conversation(waiting.id as never, this.#options.context)
    await harness.commit(async (tx) => {
      // Pi admits a submission before any write of the same commit.
      if (text !== undefined && conversation !== undefined) {
        await conversation.submit(tx, {
          type: 'input',
          content: `My answer to your question (${waiting.context})\n\n${text}`,
          whenBusy: 'followUp',
        } as never)
      }
      await tx.appendEntry(waiting.id as never, {
        kind: 'droi.ask',
        data: this.#data(waiting, change) as never,
      })
    }, this.#options.context)
    await this.#options.db
      .run('DELETE FROM droi_open_questions WHERE request_id = ?', waiting.requestId)
      .catch(() => undefined)
    if (text !== undefined) await this.#options.submitted(waiting.id).catch(() => undefined)
  }

  /** End a blocking question nobody answered: `abandoned` in the transcript, cancelled by the kernel on the wire. */
  async #expire(requestId: string, why: 'timeout' | 'aborted' | 'closing'): Promise<void> {
    const waiting = this.#waiting.get(requestId)
    if (waiting === undefined) return
    this.#waiting.delete(requestId)
    if (waiting.timer !== undefined) clearTimeout(waiting.timer)
    info('question not answered', { requestId, conversationId: waiting.id, why })
    await this.#record(waiting, { status: 'abandoned' }).catch(() => undefined)
    this.#options.notify('ask.resolved', {
      requestId,
      id: waiting.id,
      cancelled: true,
      by: 'kernel',
    })
    if (why !== 'aborted') waiting.settle?.({ kind: why })
  }

  async #record(request: AskRequest, change: Record<string, unknown>): Promise<void> {
    await this.#options.harness.commit(async (tx) => {
      await tx.appendEntry(request.id as never, {
        kind: 'droi.ask',
        data: this.#data(request, change) as never,
      })
    }, this.#options.context)
  }

  #data(request: AskRequest, change: Record<string, unknown>): Record<string, unknown> {
    return {
      requestId: request.requestId,
      questions: request.questions,
      context: request.context,
      blocking: request.blocking,
      ...change,
      at: Date.now(),
    }
  }

  #timeoutMs(): number {
    return this.#options.timeoutMs ?? ASK_TIMEOUT_MS
  }
}

function error(text: string): AskResult {
  return { output: [{ type: 'text', text }], isError: true }
}

/** `ask.respond`'s answers, checked against the questions: every question answered, every option one it offered. */
function parseAnswers(request: AskRequest, value: unknown): AskAnswer[] {
  if (!Array.isArray(value)) {
    throw invalidParams([{ path: 'answers', message: 'a list of answers, or cancelled: true' }])
  }
  const byId = new Map<string, AskAnswer>()
  value.forEach((raw, index) => {
    const answer = raw as { questionId?: unknown; selected?: unknown; text?: unknown } | null
    const question = request.questions.find((q) => q.questionId === answer?.questionId)
    if (answer === null || question === undefined) {
      throw invalidParams([
        { path: `answers.${index}.questionId`, message: 'not a question of this request' },
      ])
    }
    const selected = answer.selected ?? []
    if (!Array.isArray(selected) || selected.some((id) => typeof id !== 'string')) {
      throw invalidParams([{ path: `answers.${index}.selected`, message: 'a list of option ids' }])
    }
    const offered = new Set((question.options ?? []).map((option) => option.id))
    if ((selected as string[]).some((id) => !offered.has(id))) {
      throw invalidParams([
        { path: `answers.${index}.selected`, message: [...offered].join(', ') || 'no options' },
      ])
    }
    if (question.multiSelect !== true && selected.length > 1) {
      throw invalidParams([{ path: `answers.${index}.selected`, message: 'one option' }])
    }
    const text =
      typeof answer.text === 'string' && answer.text.trim() !== '' ? answer.text.trim() : undefined
    if (selected.length === 0 && text === undefined) {
      throw invalidParams([
        { path: `answers.${index}`, message: 'an option or a text is required' },
      ])
    }
    byId.set(question.questionId, {
      questionId: question.questionId,
      ...(selected.length === 0 ? {} : { selected: selected as string[] }),
      ...(text === undefined ? {} : { text }),
    })
  })
  const missing = request.questions.find((q) => !byId.has(q.questionId))
  if (missing !== undefined) {
    throw invalidParams([{ path: 'answers', message: `no answer to ${missing.questionId}` }])
  }
  return request.questions.map((q) => byId.get(q.questionId)!)
}

/** The answers as the model reads them: each question, then what the person chose or wrote. */
export function answerText(request: AskRequest, answers: AskAnswer[]): string {
  return request.questions
    .map((question) => {
      const answer = answers.find((a) => a.questionId === question.questionId)
      const labels = (answer?.selected ?? []).map(
        (id) => question.options?.find((option) => option.id === id)?.label ?? id,
      )
      const parts = [...labels, ...(answer?.text ? [answer.text] : [])]
      return `Q: ${question.question}\nA: ${parts.join('; ')}`
    })
    .join('\n\n')
}
