import { createModels, createProvider } from '@earendil-works/pi-ai'
import type { Api, Model, MutableModels } from '@earendil-works/pi-ai'
import * as anthropicMessages from '@earendil-works/pi-ai/api/anthropic-messages'
import * as googleGenerativeAi from '@earendil-works/pi-ai/api/google-generative-ai'
import * as openaiCompletions from '@earendil-works/pi-ai/api/openai-completions'
import * as openaiResponses from '@earendil-works/pi-ai/api/openai-responses'
import type { ProviderConfig } from './config.ts'

/**
 * The pi-ai chat APIs the kernel can speak. The Host names one per provider; the kernel maps it to the
 * implementation pi-ai ships, so a provider is data rather than code on this side.
 */
const apis = {
  'openai-completions': openaiCompletions,
  'openai-responses': openaiResponses,
  'anthropic-messages': anthropicMessages,
  'google-generative-ai': googleGenerativeAi,
} as const

export type SupportedApi = keyof typeof apis

export function supportedApi(api: string): api is SupportedApi {
  return api in apis
}

/**
 * The model catalog the Host configured, as pi-ai's `Models`.
 *
 * Keys arrive through the environment, never the wire: the kernel reads `apiKeyEnv` when pi-ai resolves
 * a credential, so the key never enters a log line or a stored record.
 */
export function buildModels(providers: readonly ProviderConfig[]): MutableModels {
  const models = createModels()
  for (const config of providers) {
    if (!supportedApi(config.api)) {
      throw new Error(`provider ${config.id} names an api the kernel does not ship: ${config.api}`)
    }
    const api = config.api
    const list: Model<Api>[] = config.models.map((model) => ({
      id: model.id,
      name: model.name ?? model.id,
      api,
      provider: config.id,
      baseUrl: config.baseUrl ?? '',
      input: model.input ?? ['text'],
      reasoning: model.reasoning ?? false,
      contextWindow: model.contextWindow,
      maxTokens: model.maxTokens,
      cost: {
        input: model.cost?.input ?? 0,
        output: model.cost?.output ?? 0,
        cacheRead: model.cost?.cacheRead ?? 0,
        cacheWrite: model.cost?.cacheWrite ?? 0,
      },
      headers: config.headers,
    }))
    models.setProvider(
      createProvider({
        id: config.id,
        name: config.name ?? config.id,
        baseUrl: config.baseUrl,
        headers: config.headers,
        auth: {
          apiKey: {
            name: `${config.name ?? config.id} API key`,
            resolve: async ({ ctx }) => {
              if (config.apiKeyEnv === undefined) return undefined
              const key = await ctx.env(config.apiKeyEnv)
              return key === undefined || key === ''
                ? undefined
                : { auth: { apiKey: key }, source: config.apiKeyEnv }
            },
          },
        },
        models: list,
        api: apis[config.api],
      }),
    )
  }
  return models
}

/** The models the kernel may use, in the order the config lists them: what `harness.info` shows. */
export function describeModels(providers: readonly ProviderConfig[]): {
  provider: string
  id: string
  name: string
  contextWindow: number
  maxTokens: number
  input: string[]
  reasoning: boolean
}[] {
  return providers.flatMap((provider) =>
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
}
