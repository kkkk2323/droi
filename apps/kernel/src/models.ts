import { createModels, createProvider } from '@earendil-works/pi-ai/models'
import type { Api, Model, MutableModels } from '@earendil-works/pi-ai'
import { anthropicMessagesApi } from '@earendil-works/pi-ai/api/anthropic-messages.lazy'
import { googleGenerativeAIApi } from '@earendil-works/pi-ai/api/google-generative-ai.lazy'
import { openAICompletionsApi } from '@earendil-works/pi-ai/api/openai-completions.lazy'
import { openAIResponsesApi } from '@earendil-works/pi-ai/api/openai-responses.lazy'
import type { ProviderConfig } from './config.ts'

/**
 * The pi-ai chat APIs the kernel can speak. The Host names one per provider; the kernel maps it to the
 * implementation pi-ai ships, so a provider is data on this side.
 *
 * The **lazy** modules, not the plain ones: each dialect's adapter is a few megabytes of code that only
 * matters for the providers actually configured, and a kernel that talks to Factory does not need the
 * Gemini or Anthropic adapters to be resident. `lazyApi` loads one on its first request and keeps it.
 * Measured: importing the four plain adapters costs about 38 MB of RSS at startup, which is a third of
 * what an idle kernel uses.
 */
const apis = {
  'openai-completions': openAICompletionsApi,
  'openai-responses': openAIResponsesApi,
  'anthropic-messages': anthropicMessagesApi,
  'google-generative-ai': googleGenerativeAIApi,
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
      ...(model.thinkingLevelMap === undefined
        ? {}
        : { thinkingLevelMap: model.thinkingLevelMap as Model<Api>['thinkingLevelMap'] }),
      ...(model.compat === undefined ? {} : { compat: model.compat as Model<Api>['compat'] }),
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
        api: apis[config.api](),
      }),
    )
  }
  return models
}
