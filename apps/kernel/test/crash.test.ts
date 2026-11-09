import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import type { SimpleStreamOptions } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'

/** A model call that never answers, unless the run is aborted — then it rejects, as a provider would. */
const hang = () => async (_context: unknown, options?: SimpleStreamOptions) =>
  new Promise<never>((_, reject) => {
    options?.signal?.addEventListener('abort', () => reject(new Error('aborted by the run')))
  })
import { reconcilePending } from '../src/reconcile.ts'
import type { KernelHandle } from '../src/kernel.ts'
import { context } from '../src/session/service.ts'

const bun = process.execPath
const childPath = new URL('./crash-child.ts', import.meta.url).pathname

type Child = { process: ReturnType<typeof Bun.spawn>; conversationId: number; port: number }

/** Start the child kernel and wait for the line that says it is running a conversation. */
async function startChild(dir: string): Promise<Child> {
  const child = Bun.spawn([bun, 'run', childPath, dir], {
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const reader = child.stdout.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value)
    const line = buffer.split('\n').find((candidate) => candidate.startsWith('{'))
    if (line !== undefined) {
      const handshake = JSON.parse(line) as { port: number; conversationId: number }
      return { process: child, ...handshake }
    }
  }
  child.kill(9)
  throw new Error(
    `the child kernel never reported: ${buffer}\n${await new Response(child.stderr).text()}`,
  )
}

let data: ReturnType<typeof tempData>
let faux: Faux
let kernel: KernelHandle

beforeEach(() => {
  data = tempData()
  faux = fauxCatalog()
})

afterEach(async () => {
  await kernel?.stop()
  data?.cleanup()
})

describe('a crashed kernel', () => {
  test('returns with its entries intact and finishes the run it was cut off in', async () => {
    const child = await startChild(data.dir)
    const conversationId = child.conversationId
    // SIGKILL: no close, no flush, nothing the kernel can do about it.
    child.process.kill(9)
    await child.process.exited

    // A fresh process over the same directory, with the answer this time.
    faux.faux.setResponses([fauxAssistantMessage('finished after the restart')])
    kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)

    const page = await client.call('conversation.entries', {
      id: conversationId,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as { kind: string; model?: { content: unknown }[] }[]
    const users = entries.filter((entry) => entry.kind === 'pi.user')
    // The input the dead process admitted is still there.
    expect(users.length).toBe(1)

    // `resume()` picks the interrupted run up; it answers and the conversation goes idle.
    const answer = await waitForAnswer(client, conversationId, 0)
    expect(answer).toContain('finished after the restart')

    const idle = await client.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === conversationId &&
        note.params['run'] === null,
      20_000,
    )
    expect(idle.params['run']).toBeNull()

    // The conversation is still usable afterwards.
    faux.faux.setResponses([fauxAssistantMessage('after recovery')])
    await client.call('conversation.submit', { id: conversationId, input: 'are you there' })
    expect(await waitForAnswer(client, conversationId, 1)).toContain('after recovery')
    client.close()
  }, 60_000)

  test('closes the questions a dead process left pending', async () => {
    kernel = await start({ dir: data.dir, faux })
    const client = new Client(kernel.port)
    const created = await client.call('conversation.create', {
      agent: { model: faux.model, cwd: process.cwd() },
    })
    const conversationId = created['conversationId'] as number
    // A run that never finishes, so its conversation has a live task at the next start.
    faux.faux.setResponses([hang()])
    await client.call('conversation.submit', { id: conversationId, input: 'hold on' })
    await client.waitFor(
      (note) =>
        note.method === 'conversation.busy' &&
        note.params['id'] === conversationId &&
        note.params['run'] != null,
      10_000,
    )

    const live = kernel.harness
    // The entry an approval leaves while it waits for a human.
    await live.commit(async (tx) => {
      await tx.appendEntry(conversationId as never, {
        kind: 'droi.approval',
        data: { requestId: 'req-crash-1', tool: 'bash', status: 'pending' },
      })
    }, context)
    // The next start reconciles while the interrupted run is still live, which is what a crash leaves:
    // the run task is still running in storage until resume() touches it.
    const closed = await reconcilePending(live, context)
    expect(closed).toBe(1)
    const page = await live.conversation(conversationId as never, context)
    const after = await page!.entries({ order: 'ascending' }, 200, undefined, context)
    const approvals = after.items
      .filter((entry) => entry.kind === 'droi.approval')
      .map((entry) => entry.data as { requestId: string; status: string })
    expect(approvals.at(-1)?.status).toBe('abandoned')
    expect(approvals.at(-1)?.requestId).toBe('req-crash-1')
    // Reconciling twice closes nothing more: the last entry per request is now `abandoned`.
    expect(await reconcilePending(live, context)).toBe(0)

    // End the hanging run, so closing the kernel has nothing left to wait for.
    await client.call('conversation.abort', { id: conversationId })
    client.close()
  }, 60_000)
})

async function waitForAnswer(client: Client, id: number, index: number): Promise<string> {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    const page = await client.call('conversation.entries', {
      id,
      query: { order: 'ascending' },
      limit: 200,
    })
    const entries = page['items'] as { kind: string; model?: { content: unknown }[] }[]
    const answers = entries.filter((entry) => entry.kind === 'pi.assistant')
    const found = answers[index]
    if (found !== undefined) return textOf(found.model?.[0]?.content)
    await Bun.sleep(20)
  }
  throw new Error(`conversation ${id} never produced answer ${index}`)
}

function textOf(content: unknown): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .map((part) =>
      typeof part === 'object' && part !== null && (part as { type?: string }).type === 'text'
        ? String((part as { text?: unknown }).text ?? '')
        : '',
    )
    .join('')
}
