// The scripted model a Fake Kernel talks to: an OpenAI-compatible endpoint this suite serves, so the Kernel
// is the production one and the model is the only fake part (as apps/native/pkg/kernel/fakekernel does for
// Go). Each conversation request takes the first Step whose `match` its last user message contains; a
// model.complete request (a title, say) takes a Completion instead and is never a miss.
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'

export const PROVIDER_ID = 'fake'
export const MODEL_ID = 'fake-1'

export interface Call {
  name: string
  arguments?: Record<string, unknown>
  id?: string
}

export interface Step {
  /** A substring of the last user message; absent matches any request. A step is taken once. */
  match?: string
  /** The reply, streamed as deltas. */
  text?: string
  /** Text's deltas, instead of 40-character pieces. */
  chunks?: string[]
  /** A tool call instead of text. */
  call?: Call
  /** Fails the request: a status, or 0 to drop the connection. */
  failure?: { status?: number; body?: string }
  /** Waited before each delta. */
  delayMs?: number
}

export interface Completion {
  /** A substring of the whole prompt; absent matches any. */
  match?: string
  text: string
}

export interface ModelRequest {
  messages: Array<{ role: string; content: unknown }>
  stream: boolean
  headers: Record<string, string>
  /** The last user message's text. */
  prompt: string
  /** The names of the tools the model was offered. */
  tools: string[]
}

/** What an unscripted model.complete answers: a valid title. */
export const FAKE_TITLE = '{"title":"Fake title"}'

export class ScriptedModel {
  readonly requests: ModelRequest[] = []
  readonly misses: string[] = []
  #steps: Step[]
  #completions: Completion[]
  #server: Server
  #waiters: Array<() => void> = []

  private constructor(steps: Step[], completions: Completion[], server: Server) {
    this.#steps = [...steps]
    this.#completions = [...completions]
    this.#server = server
  }

  static async start(steps: Step[] = [], completions: Completion[] = []): Promise<ScriptedModel> {
    const server = createServer()
    const model = new ScriptedModel(steps, completions, server)
    server.on('request', (req, res) => void model.#serve(req, res))
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
    return model
  }

  get baseUrl(): string {
    return `http://127.0.0.1:${(this.#server.address() as AddressInfo).port}/v1`
  }

  /** More steps, after those still unplayed. */
  push(...steps: Step[]): void {
    this.#steps.push(...steps)
  }

  /** The conversation requests so far, model.complete ones left out. */
  turns(): ModelRequest[] {
    return this.requests.filter((r) => r.stream && r.headers['x-droi-call'] !== 'model.complete')
  }

  /** Resolves once a conversation request's last user message has `text`. */
  async waitForTurn(text: string, timeoutMs = 20_000): Promise<ModelRequest> {
    const deadline = Date.now() + timeoutMs
    for (;;) {
      const found = this.turns().find((r) => r.prompt.includes(text))
      if (found) return found
      if (Date.now() > deadline)
        throw new Error(`the model was never asked about ${JSON.stringify(text)}`)
      await new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, 100)
        this.#waiters.push(() => {
          clearTimeout(timer)
          resolve()
        })
      })
    }
  }

  stop(): Promise<void> {
    this.#server.closeAllConnections()
    return new Promise((resolve) => this.#server.close(() => resolve()))
  }

  async #serve(req: IncomingMessage, res: ServerResponse): Promise<void> {
    if (!req.url?.endsWith('/chat/completions')) {
      res.writeHead(404).end()
      return
    }
    const chunks: Buffer[] = []
    for await (const chunk of req) chunks.push(chunk as Buffer)
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as {
      messages?: Array<{ role: string; content: unknown }>
      stream?: boolean
      tools?: Array<{ function?: { name?: string } }>
    }
    const headers: Record<string, string> = {}
    for (const [key, value] of Object.entries(req.headers)) {
      if (typeof value === 'string') headers[key.toLowerCase()] = value
    }
    const messages = body.messages ?? []
    const request: ModelRequest = {
      messages,
      stream: body.stream === true,
      headers,
      prompt: lastUserText(messages),
      tools: (body.tools ?? []).map((tool) => tool.function?.name ?? ''),
    }
    this.requests.push(request)
    for (const wake of this.#waiters.splice(0)) wake()

    if (!request.stream || headers['x-droi-call'] === 'model.complete') {
      const whole = messages.map((m) => textOf(m.content)).join('\n')
      const index = this.#completions.findIndex((c) => !c.match || whole.includes(c.match))
      const answer = index === -1 ? FAKE_TITLE : this.#completions.splice(index, 1)[0]!.text
      if (request.stream) await stream(res, { text: answer }, req)
      else answerWhole(res, answer)
      return
    }
    const index = this.#steps.findIndex((s) => !s.match || request.prompt.includes(s.match))
    let step: Step
    if (index === -1) {
      this.misses.push(request.prompt)
      step = { text: `[fake model: the prompt ${JSON.stringify(request.prompt)} matched no Step]` }
    } else {
      step = this.#steps.splice(index, 1)[0]!
    }
    if (step.failure) {
      if (!step.failure.status) {
        req.socket.destroy()
        return
      }
      res.writeHead(step.failure.status, { 'content-type': 'application/json' })
      res.end(JSON.stringify({ error: { message: step.failure.body ?? 'failed' } }))
      return
    }
    await stream(res, step, req)
  }
}

function lastUserText(messages: Array<{ role: string; content: unknown }>): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i]!.role === 'user') return textOf(messages[i]!.content)
  }
  return ''
}

function textOf(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .map((part: { type?: string; text?: string }) =>
      part.type === 'text' ? (part.text ?? '') : '',
    )
    .join('')
}

const usage = { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 }

function answerWhole(res: ServerResponse, text: string): void {
  res.writeHead(200, { 'content-type': 'application/json' })
  res.end(
    JSON.stringify({
      id: 'chatcmpl-fake',
      object: 'chat.completion',
      created: 1,
      model: MODEL_ID,
      choices: [{ index: 0, finish_reason: 'stop', message: { role: 'assistant', content: text } }],
      usage,
    }),
  )
}

async function stream(res: ServerResponse, step: Step, req: IncomingMessage): Promise<void> {
  res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
  let gone = false
  req.on('close', () => (gone = true))
  const send = (
    delta: Record<string, unknown>,
    finish: string | null,
    extra: Record<string, unknown> = {},
  ) => {
    if (gone) return
    res.write(
      `data: ${JSON.stringify({
        id: 'chatcmpl-fake',
        object: 'chat.completion.chunk',
        created: 1,
        model: MODEL_ID,
        choices: [{ index: 0, delta, finish_reason: finish }],
        ...extra,
      })}\n\n`,
    )
  }
  const pause = () =>
    step.delayMs ? new Promise((resolve) => setTimeout(resolve, step.delayMs)) : Promise.resolve()

  await pause()
  send({ role: 'assistant', content: '' }, null)
  if (step.call) {
    await pause()
    send(
      {
        tool_calls: [
          {
            index: 0,
            id: step.call.id ?? 'call_fake',
            type: 'function',
            function: {
              name: step.call.name,
              arguments: JSON.stringify(step.call.arguments ?? {}),
            },
          },
        ],
      },
      null,
    )
    await pause()
    send({}, 'tool_calls')
  } else {
    const text = step.text ?? ''
    const pieces = step.chunks ?? text.match(/[\s\S]{1,40}/g) ?? []
    for (const piece of pieces) {
      await pause()
      if (gone) return
      if (piece) send({ content: piece }, null)
    }
    send({}, 'stop')
  }
  send({}, null, { choices: [], usage })
  if (!gone) res.end('data: [DONE]\n\n')
}
