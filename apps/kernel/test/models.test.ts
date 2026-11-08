import { afterEach, describe, expect, test } from 'bun:test'
import type { Api, Model } from '@earendil-works/pi-ai'
import type { ModelConfig, ProviderConfig } from '../src/config.ts'
import { buildModels } from '../src/models.ts'

/**
 * A model's thinking level reaches the provider as the Host's config says: pi-ai sends a level the model's
 * `thinkingLevelMap` leaves out as another (xhigh and max as high), and budgets Claude's thinking unless the model
 * is marked adaptive, which Haiku 5.5 and Opus 4.7 and later refuse with a 400.
 */

const real = globalThis.fetch
afterEach(() => {
  globalThis.fetch = real
  delete process.env['MODELS_TEST_KEY']
})

/** The body pi-ai sends for one turn of `model` at `reasoning`. */
async function sent(
  api: string,
  model: ModelConfig,
  reasoning?: 'off' | 'low' | 'high' | 'xhigh' | 'max',
): Promise<Record<string, unknown>> {
  let body: Record<string, unknown> | undefined
  globalThis.fetch = (async (_input: unknown, init?: RequestInit) => {
    body = JSON.parse(String(init?.body))
    return new Response('{"error":{"type":"x","message":"stop"}}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    })
  }) as typeof fetch
  process.env['MODELS_TEST_KEY'] = 'k'
  const provider: ProviderConfig = {
    id: 'p',
    api,
    baseUrl: 'http://127.0.0.1:9/v1',
    apiKeyEnv: 'MODELS_TEST_KEY',
    models: [model],
  }
  const models = buildModels([provider])
  const m = models.getModel('p', model.id) as Model<Api>
  const stream = models.streamSimple(
    m,
    { messages: [{ role: 'user', content: 'hi', timestamp: 0 }] },
    { maxTokens: 1024, ...(reasoning === undefined || reasoning === 'off' ? {} : { reasoning }) },
  )
  for await (const _ of stream);
  if (body === undefined) throw new Error('nothing was sent')
  return body
}

const levels = (map: Record<string, string | null>) => map

const haiku: ModelConfig = {
  id: 'claude-haiku-5-5',
  contextWindow: 872000,
  maxTokens: 128000,
  reasoning: true,
  thinkingLevelMap: levels({
    off: null,
    minimal: null,
    low: 'low',
    medium: 'medium',
    high: 'high',
    xhigh: 'xhigh',
    max: 'max',
  }),
  compat: { forceAdaptiveThinking: true },
}

describe('a thinking level reaches the provider', () => {
  test("Claude marked adaptive thinks adaptively, at the level's effort, max included", async () => {
    for (const level of ['low', 'high', 'xhigh', 'max'] as const) {
      const body = await sent('anthropic-messages', haiku, level)
      expect(body['thinking']).toMatchObject({ type: 'adaptive' })
      expect(body['output_config']).toEqual({ effort: level })
    }
  })

  test('Claude not marked adaptive gets a budget, which newer Claude models refuse', async () => {
    const { compat: _, ...budgeted } = haiku
    const body = await sent('anthropic-messages', budgeted, 'low')
    expect(body['thinking']).toMatchObject({ type: 'enabled' })
  })

  test('Claude that refuses off is sent no thinking at off, not thinking turned off', async () => {
    const body = await sent('anthropic-messages', haiku, 'off')
    expect(body['thinking']).toBeUndefined()
    expect(body['output_config']).toBeUndefined()
  })

  test('Responses gets xhigh and max as they are, and off as none only where the model takes none', async () => {
    const luna: ModelConfig = {
      id: 'gpt-6-luna',
      contextWindow: 1050000,
      maxTokens: 128000,
      reasoning: true,
      thinkingLevelMap: levels({
        off: 'none',
        minimal: null,
        low: 'low',
        medium: 'medium',
        high: 'high',
        xhigh: 'xhigh',
        max: 'max',
      }),
    }
    expect((await sent('openai-responses', luna, 'max'))['reasoning']).toMatchObject({
      effort: 'max',
    })
    expect((await sent('openai-responses', luna, 'xhigh'))['reasoning']).toMatchObject({
      effort: 'xhigh',
    })
    expect((await sent('openai-responses', luna, 'off'))['reasoning']).toEqual({ effort: 'none' })
    const gpt55: ModelConfig = {
      ...luna,
      id: 'gpt-5.5',
      thinkingLevelMap: { ...luna.thinkingLevelMap, off: null, max: null },
    }
    expect((await sent('openai-responses', gpt55, 'off'))['reasoning']).toBeUndefined()
  })

  test('chat completions gets max as it is', async () => {
    const kimi: ModelConfig = {
      id: 'kimi-k3',
      contextWindow: 262144,
      maxTokens: 65536,
      reasoning: true,
      thinkingLevelMap: levels({
        off: 'none',
        minimal: null,
        low: 'low',
        medium: null,
        high: 'high',
        xhigh: null,
        max: 'max',
      }),
    }
    expect((await sent('openai-completions', kimi, 'max'))['reasoning_effort']).toBe('max')
  })
})
