import { afterEach, describe, expect, test } from 'bun:test'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import type { SimpleStreamOptions } from '@earendil-works/pi-ai'
import { Client, fauxCatalog, start, tempData, type Faux } from './helpers.ts'

/**
 * `model.complete`: one model call with no conversation behind it.
 *
 * The cases are the ones the protocol names — an answer, a schema the answer satisfies, a schema it does
 * not, an unknown model, a provider that failed, a refusal, a deadline, and a cap above what the model
 * takes — plus the invariant that the method stores nothing.
 */

let cleanup: (() => void) | undefined
let kernel: Awaited<ReturnType<typeof start>> | undefined
let client: Client | undefined
let faux: Faux

afterEach(async () => {
  client?.close()
  await kernel?.stop()
  cleanup?.()
  client = undefined
  kernel = undefined
  cleanup = undefined
})

async function open(): Promise<Client> {
  const data = tempData()
  cleanup = data.cleanup
  faux = fauxCatalog()
  kernel = await start({ dir: data.dir, faux })
  client = new Client(kernel.port)
  await client.opened
  return client
}

/** The method's params for the faux model, with `extra` over it. */
function params(extra: Record<string, unknown> = {}): Record<string, unknown> {
  return { model: faux.model, input: 'name this conversation', ...extra }
}

describe('model.complete', () => {
  test('answers the text, the model and the usage, and stores nothing', async () => {
    const c = await open()
    faux.faux.setResponses([fauxAssistantMessage('Kernel protocol fixtures')])
    const answer = await c.call('model.complete', params())
    expect(answer['text']).toBe('Kernel protocol fixtures')
    expect(answer['model']).toEqual(faux.model)
    const usage = answer['usage'] as { input: number; output: number; totalTokens: number }
    expect(typeof usage.input).toBe('number')
    expect(typeof usage.output).toBe('number')
    expect(typeof usage.totalTokens).toBe('number')
    expect(answer['json']).toBeUndefined()
    // No conversation, no entry, no task: a list that saw one would be the record the method promises not to keep.
    const list = await c.call('conversation.list', {})
    expect(list['items']).toEqual([])
  })

  test('a schema the answer satisfies comes back as json, and the text stays', async () => {
    const c = await open()
    faux.faux.setResponses([fauxAssistantMessage('{"title":"Kernel protocol fixtures"}')])
    const answer = await c.call(
      'model.complete',
      params({
        schema: {
          type: 'object',
          required: ['title'],
          additionalProperties: false,
          properties: { title: { type: 'string' } },
        },
      }),
    )
    expect(answer['json']).toEqual({ title: 'Kernel protocol fixtures' })
    expect(answer['text']).toBe('{"title":"Kernel protocol fixtures"}')
  })

  test('a schema the answer misses is -32014, with the issues and what the model said', async () => {
    const c = await open()
    faux.faux.setResponses([fauxAssistantMessage('{"title":7}')])
    const failure = await c.fails(
      'model.complete',
      params({
        schema: {
          type: 'object',
          required: ['title'],
          properties: { title: { type: 'string' } },
        },
      }),
    )
    expect(failure.code).toBe(-32014)
    const data = failure.data as { issues: { path: string; message: string }[]; text: string }
    expect(data.text).toBe('{"title":7}')
    expect(data.issues.length).toBeGreaterThan(0)
    // The kernel is still up: a bad answer is the model's, not the kernel's.
    faux.faux.setResponses([fauxAssistantMessage('still here')])
    expect((await c.call('model.complete', params()))['text']).toBe('still here')
  })

  test('JSON in a Markdown code fence is the JSON', async () => {
    const c = await open()
    const fenced = '```json\n{"title":"Kernel protocol fixtures"}\n```'
    faux.faux.setResponses([fauxAssistantMessage(fenced)])
    const answer = await c.call(
      'model.complete',
      params({
        schema: { type: 'object', required: ['title'], properties: { title: { type: 'string' } } },
      }),
    )
    expect(answer['json']).toEqual({ title: 'Kernel protocol fixtures' })
    expect(answer['text']).toBe(fenced)
  })

  test('an answer that is not JSON is -32014 too, not a parse error', async () => {
    const c = await open()
    faux.faux.setResponses([fauxAssistantMessage('not json at all')])
    const failure = await c.fails('model.complete', params({ schema: { type: 'object' } }))
    expect(failure.code).toBe(-32014)
    expect((failure.data as { text: string }).text).toBe('not json at all')
  })

  test('an unknown model is -32016, and the id is in the data', async () => {
    const c = await open()
    const failure = await c.fails(
      'model.complete',
      params({ model: { provider: faux.model.provider, modelId: 'no-such-model' } }),
    )
    expect(failure.code).toBe(-32016)
    expect((failure.data as { model: string }).model).toBe(`${faux.model.provider}/no-such-model`)
  })

  test('a provider that failed is -32015, with its own words', async () => {
    const c = await open()
    faux.faux.setResponses([
      fauxAssistantMessage('', { stopReason: 'error', errorMessage: '500: upstream exploded' }),
    ])
    const failure = await c.fails('model.complete', params())
    expect(failure.code).toBe(-32015)
    expect(failure.message).toContain('upstream exploded')
  })

  test('a model that declined is -32018, distinct from a failure', async () => {
    const c = await open()
    faux.faux.setResponses([
      fauxAssistantMessage('', {
        stopReason: 'error',
        errorMessage: 'The model refused to complete the request',
      }),
    ])
    const failure = await c.fails('model.complete', params())
    expect(failure.code).toBe(-32018)
  })

  test('a deadline ends the call as -32012', async () => {
    const c = await open()
    // The model never answers unless the request is aborted, which is the deadline reaching it.
    faux.faux.setResponses([
      (_context: unknown, options?: SimpleStreamOptions) =>
        new Promise<never>((_, reject) => {
          options?.signal?.addEventListener('abort', () =>
            reject(new Error('aborted by the deadline')),
          )
        }),
    ])
    const started = Date.now()
    const failure = await c.fails('model.complete', params({ timeoutMs: 100 }))
    expect(failure.code).toBe(-32012)
    expect(failure.data).toEqual({ method: 'model.complete', timeoutMs: 100 })
    expect(Date.now() - started).toBeLessThan(10_000)
  })

  test('maxTokens above what the model takes is -32017', async () => {
    const c = await open()
    const model = faux.models.getModel(faux.model.provider, faux.model.modelId)
    const failure = await c.fails(
      'model.complete',
      params({ maxTokens: (model?.maxTokens ?? 8192) + 1 }),
    )
    expect(failure.code).toBe(-32017)
  })

  test('bad params are -32602, one issue per mistake', async () => {
    const c = await open()
    for (const bad of [
      { input: 42 },
      { input: [{ type: 'image', data: 'x' }] },
      { thinkingLevel: 'highish' },
      { timeoutMs: 900_000 },
      { model: { provider: 'faux' } },
    ]) {
      const failure = await c.fails('model.complete', params(bad))
      expect(failure.code).toBe(-32602)
      expect((failure.data as { issues: { path: string }[] }).issues.length).toBeGreaterThan(0)
    }
  })

  test('text blocks are accepted where a string is', async () => {
    const c = await open()
    faux.faux.setResponses([fauxAssistantMessage('from blocks')])
    const answer = await c.call(
      'model.complete',
      params({ input: [{ type: 'text', text: 'name this conversation' }] }),
    )
    expect(answer['text']).toBe('from blocks')
  })

  test('harness.info reports the models capability', async () => {
    const c = await open()
    const info = await c.call('harness.info', {})
    const capabilities = info['capabilities'] as Record<string, boolean>
    expect(capabilities['models']).toBe(true)
  })
})
