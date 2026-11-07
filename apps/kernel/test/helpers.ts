import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  createModels,
  fauxAssistantMessage,
  fauxProvider,
  fauxToolCall,
} from '@earendil-works/pi-ai'
import type { MutableModels } from '@earendil-works/pi-ai'
import type { KernelConfig } from '../src/config.ts'
import { startKernel, type KernelHandle } from '../src/kernel.ts'

export const TOKEN = 'test-token'

/** A data directory under the system temp dir, removed afterwards. */
export function tempData(): { dir: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'kernel-test-'))
  return {
    dir,
    cleanup: () => {
      try {
        rmSync(dir, { recursive: true, force: true, maxRetries: 20, retryDelay: 50 })
      } catch {
        // Windows releases the SQLite file a moment after close; a leftover temp dir is not a failure.
      }
    },
  }
}

export function config(dir: string, overrides: Partial<KernelConfig> = {}): KernelConfig {
  return {
    port: 0,
    host: '127.0.0.1',
    dataDir: dir,
    readOnly: false,
    providers: [],
    ...overrides,
  }
}

export type Faux = {
  models: MutableModels
  faux: ReturnType<typeof fauxProvider>
  model: { provider: string; modelId: string }
}

/** A catalog whose only provider answers from a script the test sets, with no network. */
export function fauxCatalog(): Faux {
  const faux = fauxProvider()
  const models = createModels()
  models.setProvider(faux.provider)
  const model = faux.getModel()
  return { models, faux, model: { provider: model.provider, modelId: model.id } }
}

export async function start(options: {
  dir: string
  faux: Faux
  overrides?: Partial<KernelConfig>
  platform?: 'darwin' | 'linux' | 'win32'
}): Promise<KernelHandle> {
  return startKernel(config(options.dir, options.overrides), {
    token: TOKEN,
    models: options.faux.models,
    noLock: true,
    platform: options.platform,
  })
}

/** A JSON-RPC client over one WebSocket, the way the Host speaks to the kernel. */
export class Client {
  #ws: WebSocket
  #closed = false
  #nextId = 1
  #pending = new Map<
    number,
    { resolve: (value: unknown) => void; reject: (error: Error) => void }
  >()
  readonly notifications: { method: string; params: Record<string, unknown> }[] = []
  #waiters: {
    test: (n: { method: string; params: Record<string, unknown> }) => boolean
    resolve: (n: { method: string; params: Record<string, unknown> }) => void
  }[] = []
  readonly opened: Promise<void>

  constructor(port: number, token = TOKEN) {
    this.#ws = new WebSocket(`ws://127.0.0.1:${port}/rpc`, {
      headers: { Authorization: `Bearer ${token}` },
    } as never)
    this.opened = new Promise<void>((resolve, reject) => {
      this.#ws.addEventListener('open', () => resolve(), { once: true })
      // A socket that errors after the client was closed is teardown, not a failure: rejecting on it
      // would be an unhandled rejection in every test that closes before opening.
      this.#ws.addEventListener(
        'error',
        () => {
          if (this.#closed) return
          reject(new Error('the websocket failed'))
        },
        { once: true },
      )
    })
    // An `opened` nobody awaited must not surface as an unhandled rejection; `call` awaits it and sees it.
    this.opened.catch(() => undefined)
    this.#ws.addEventListener('message', (event) => {
      const frame = JSON.parse(String(event.data)) as Record<string, unknown>
      if (typeof frame['method'] === 'string') {
        const note = {
          method: frame['method'] as string,
          params: (frame['params'] ?? {}) as Record<string, unknown>,
        }
        this.notifications.push(note)
        for (const waiter of [...this.#waiters]) {
          if (!waiter.test(note)) continue
          this.#waiters = this.#waiters.filter((other) => other !== waiter)
          waiter.resolve(note)
        }
        return
      }
      const id = Number(frame['id'])
      const entry = this.#pending.get(id)
      if (entry === undefined) return
      this.#pending.delete(id)
      const error = frame['error'] as { code: number; message: string; data?: unknown } | undefined
      if (error !== undefined) {
        entry.reject(
          Object.assign(new Error(error.message), { code: error.code, data: error.data }),
        )
      } else {
        entry.resolve(frame['result'])
      }
    })
  }

  async call(method: string, params?: Record<string, unknown>): Promise<Record<string, unknown>> {
    await this.opened
    const id = this.#nextId++
    const answer = new Promise<unknown>((resolve, reject) => {
      this.#pending.set(id, { resolve, reject })
    })
    this.#ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    return (await answer) as Record<string, unknown>
  }

  /** Send a call and expect it to fail, returning the JSON-RPC error. */
  async fails(
    method: string,
    params?: Record<string, unknown>,
  ): Promise<{ code: number; message: string; data?: unknown }> {
    try {
      await this.call(method, params)
    } catch (error) {
      return error as { code: number; message: string; data?: unknown }
    }
    throw new Error(`${method} was expected to fail`)
  }

  /** The next notification matching `test`, or a timeout. */
  async waitFor(
    test: (n: { method: string; params: Record<string, unknown> }) => boolean,
    timeoutMs = 20_000,
  ): Promise<{ method: string; params: Record<string, unknown> }> {
    const seen = this.notifications.find(test)
    if (seen !== undefined) return seen
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        reject(
          new Error(
            `no notification in ${timeoutMs} ms; saw ${this.notifications.map((n) => n.method).join(', ')}`,
          ),
        )
      }, timeoutMs)
      this.#waiters.push({
        test,
        resolve: (note) => {
          clearTimeout(timer)
          resolve(note)
        },
      })
    })
  }

  // Convenience: the completion notice of one conversation moved so far. The third name keeps the
  // intent readable at the call site.
  waitForMethod(method: string, timeoutMs = 20_000) {
    return this.waitFor((note) => note.method === method, timeoutMs)
  }

  close(): void {
    this.#closed = true
    this.#ws.close()
  }
}

/** Run one conversation to its first answer, with `rounds` prompts of two turns each. */
export async function conversationRounds(
  client: Client,
  faux: Faux,
  rounds: number,
  text: (round: number) => string,
): Promise<{ conversationId: number; answers: string[] }> {
  faux.faux.setResponses([])
  const created = await client.call('conversation.create', {
    agent: { model: faux.model, cwd: process.cwd() },
  })
  const conversationId = created['conversationId'] as number
  const answers: string[] = []
  for (let round = 0; round < rounds; round++) {
    faux.faux.appendResponses([fauxAssistantMessage(text(round))])
    const submitted = await client.call('conversation.submit', {
      id: conversationId,
      input: `round ${round}`,
    })
    await waitSettled(client, conversationId, answers.length, submitted['submissionId'] as number)
    answers.push(text(round))
  }
  return { conversationId, answers }
}

/** Wait until a conversation is idle again, which is when a run has committed its answer. */
export async function waitIdle(
  client: Client,
  conversationId: number,
  timeoutMs = 20_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const usage = await client.call('conversation.usage', { id: conversationId })
    void usage
    const busy = client.notifications.filter(
      (note) => note.method === 'conversation.busy' && note.params['id'] === conversationId,
    )
    const last = busy.at(-1)
    if (last !== undefined && last.params['run'] === null) return
    await Bun.sleep(20)
  }
  throw new Error(`conversation ${conversationId} never went idle`)
}

async function waitSettled(
  client: Client,
  conversationId: number,
  index: number,
  submissionId: number,
): Promise<void> {
  void submissionId
  // The answer arrives as a `pi.assistant` entry in the view; the tenth of a second granularity of the
  // progress policy makes polling for it the simplest reliable signal.
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id: conversationId,
      query: { order: 'ascending' },
      limit: 200,
    })
    const items = page['items'] as { kind: string }[]
    const answers = items.filter((entry) => entry.kind === 'pi.assistant')
    if (answers.length > index) return
    await Bun.sleep(20)
  }
  throw new Error(`conversation ${conversationId} never answered round ${index}`)
}

export { fauxToolCall }

/** Every entry of a conversation, following the cursor: a page is capped at `limits.maxEntriesPerPage`. */
export async function allEntries(
  client: Client,
  id: number,
  order: 'ascending' | 'descending' = 'ascending',
  limit = 200,
): Promise<{ id: number; kind: string; model?: { role: string; content: unknown }[] }[]> {
  const items: { id: number; kind: string; model?: { role: string; content: unknown }[] }[] = []
  let cursor: unknown = undefined
  for (;;) {
    const params: Record<string, unknown> = { id, query: { order }, limit }
    if (cursor !== undefined) params['cursor'] = cursor
    const page = await client.call('conversation.entries', params)
    items.push(...(page['items'] as typeof items))
    if (page['cursor'] === undefined) return items
    cursor = page['cursor']
  }
}
