// Which company a model comes from, for grouping and the brand mark. The
// Daemon's `modelProvider` is the wire dialect (a GLM served through Factory
// still says "anthropic" or "generic-chat-completion-api"), so the model id's
// prefix wins and the provider is only a fallback.
export type Brand =
  | 'anthropic'
  | 'openai'
  | 'google'
  | 'xai'
  | 'zhipu'
  | 'moonshot'
  | 'deepseek'
  | 'minimax'
  | 'other'

export const BRAND_LABELS: Record<Brand, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  google: 'Google',
  xai: 'xAI',
  zhipu: 'Zhipu',
  moonshot: 'Moonshot',
  deepseek: 'DeepSeek',
  minimax: 'MiniMax',
  other: 'Other',
}

/** Rail order in the picker; brands the Daemon does not offer are skipped. */
export const BRAND_ORDER: Brand[] = [
  'anthropic',
  'openai',
  'google',
  'xai',
  'zhipu',
  'moonshot',
  'deepseek',
  'minimax',
  'other',
]

const ID_PREFIXES: Array<[string, Brand]> = [
  ['claude', 'anthropic'],
  ['gpt', 'openai'],
  ['o1', 'openai'],
  ['o3', 'openai'],
  ['o4', 'openai'],
  ['gemini', 'google'],
  ['grok', 'xai'],
  ['glm', 'zhipu'],
  ['kimi', 'moonshot'],
  ['deepseek', 'deepseek'],
  ['minimax', 'minimax'],
]

const PROVIDER_BRANDS: Record<string, Brand> = {
  anthropic: 'anthropic',
  openai: 'openai',
  google: 'google',
  xai: 'xai',
}

export function brandOf(modelId: string, provider: string | null): Brand {
  const id = modelId.toLowerCase()
  for (const [prefix, brand] of ID_PREFIXES) {
    if (id.startsWith(prefix)) return brand
  }
  return (provider ? PROVIDER_BRANDS[provider] : undefined) ?? 'other'
}
