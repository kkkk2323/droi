import type { AvailableModelConfig, CustomModelDef } from '@/types'

export type ModelProviderIcon =
  | 'claude'
  | 'openai'
  | 'gemini'
  | 'factory'
  | 'xai'
  | 'custom'
  | 'unknown'

export interface RuntimeModelOption {
  value: string
  label: string
  shortLabel?: string
  provider: string
  providerIcon: ModelProviderIcon
  groupKey: string
  groupLabel: string
  multiplier: string | null
  supportedReasoningEfforts: string[]
  defaultReasoningEffort: string
  isCustom: boolean
}

export interface RuntimeModelGroup {
  key: string
  label: string
  providerIcon: ModelProviderIcon
  options: RuntimeModelOption[]
}

const PROVIDER_GROUP_ORDER = ['anthropic', 'openai', 'google', 'factory', 'xai', 'other', 'custom']

function normalizeString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function toTitleCase(value: string): string {
  if (!value) return 'Other'
  return value
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function formatMultiplier(value: unknown): string | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return null
  const normalized = Number.isInteger(value) ? String(value) : String(Number(value.toFixed(2)))
  return `${normalized}×`
}

function getGroupMeta(model: Pick<AvailableModelConfig, 'modelProvider' | 'isCustom'>): {
  key: string
  label: string
  icon: ModelProviderIcon
} {
  const provider = normalizeString(model.modelProvider) || 'other'
  const isCustom = model.isCustom === true
  if (isCustom) {
    return { key: 'custom', label: 'Custom', icon: 'custom' }
  }
  if (provider === 'anthropic') return { key: provider, label: 'Anthropic', icon: 'claude' }
  if (provider === 'openai') return { key: provider, label: 'OpenAI', icon: 'openai' }
  if (provider === 'google') return { key: provider, label: 'Google', icon: 'gemini' }
  if (provider === 'factory') return { key: provider, label: 'Factory', icon: 'factory' }
  if (provider === 'xai') return { key: provider, label: 'xAI', icon: 'xai' }
  return { key: 'other', label: toTitleCase(provider), icon: 'unknown' }
}

function normalizeReasoningEfforts(levels: unknown): string[] {
  if (!Array.isArray(levels)) return []
  return levels
    .map((level) => normalizeString(level))
    .filter(Boolean)
    .filter((level, index, array) => array.indexOf(level) === index)
}

function toRuntimeOption(model: AvailableModelConfig): RuntimeModelOption | null {
  const value = normalizeString(model.id) || normalizeString(model.modelId)
  if (!value) return null
  const { key, label, icon } = getGroupMeta(model)
  return {
    value,
    label: normalizeString(model.displayName) || value,
    shortLabel: normalizeString(model.shortDisplayName) || undefined,
    provider: normalizeString(model.modelProvider) || 'other',
    providerIcon: icon,
    groupKey: key,
    groupLabel: label,
    multiplier: formatMultiplier(model.tokenMultiplier),
    supportedReasoningEfforts: normalizeReasoningEfforts(model.supportedReasoningEfforts),
    defaultReasoningEffort: normalizeString(model.defaultReasoningEffort),
    isCustom: model.isCustom === true,
  }
}

function toSyntheticCustomOption(model: CustomModelDef): RuntimeModelOption | null {
  const value = normalizeString(model.id)
  if (!value) return null
  return {
    value,
    label: normalizeString(model.displayName) || value,
    shortLabel: undefined,
    provider: normalizeString(model.provider) || 'custom',
    providerIcon: 'custom',
    groupKey: 'custom',
    groupLabel: 'Custom',
    multiplier: null,
    supportedReasoningEfforts: [],
    defaultReasoningEffort: '',
    isCustom: true,
  }
}

export function buildRuntimeModelCatalog(params: {
  availableModels?: AvailableModelConfig[]
  customModels?: CustomModelDef[]
}): RuntimeModelGroup[] {
  const groups = new Map<string, RuntimeModelGroup>()
  const seen = new Set<string>()

  for (const model of params.availableModels ?? []) {
    const option = toRuntimeOption(model)
    if (!option || seen.has(option.value)) continue
    seen.add(option.value)
    const existing = groups.get(option.groupKey)
    if (existing) {
      existing.options.push(option)
    } else {
      groups.set(option.groupKey, {
        key: option.groupKey,
        label: option.groupLabel,
        providerIcon: option.providerIcon,
        options: [option],
      })
    }
  }

  for (const model of params.customModels ?? []) {
    const option = toSyntheticCustomOption(model)
    if (!option || seen.has(option.value)) continue
    seen.add(option.value)
    const existing = groups.get(option.groupKey)
    if (existing) {
      existing.options.push(option)
    } else {
      groups.set(option.groupKey, {
        key: option.groupKey,
        label: option.groupLabel,
        providerIcon: option.providerIcon,
        options: [option],
      })
    }
  }

  return Array.from(groups.values())
    .sort((a, b) => {
      const left = PROVIDER_GROUP_ORDER.indexOf(a.key)
      const right = PROVIDER_GROUP_ORDER.indexOf(b.key)
      if (left === right) return a.label.localeCompare(b.label)
      if (left === -1) return 1
      if (right === -1) return -1
      return left - right
    })
    .map((group) => ({
      ...group,
      options: [...group.options].sort((a, b) => a.label.localeCompare(b.label)),
    }))
}

export function findRuntimeModelOption(
  modelId: string,
  params: {
    availableModels?: AvailableModelConfig[]
    customModels?: CustomModelDef[]
  },
): RuntimeModelOption | null {
  const target = normalizeString(modelId)
  if (!target) return null

  for (const model of params.availableModels ?? []) {
    const option = toRuntimeOption(model)
    if (!option) continue
    if (option.value === target || normalizeString(model.modelId) === target) return option
  }

  for (const model of params.customModels ?? []) {
    const option = toSyntheticCustomOption(model)
    if (!option) continue
    if (option.value === target) return option
  }

  return null
}

export function getRuntimeModelLabel(
  modelId: string,
  params: {
    availableModels?: AvailableModelConfig[]
    customModels?: CustomModelDef[]
  },
): string {
  return findRuntimeModelOption(modelId, params)?.label ?? normalizeString(modelId)
}

export function getRuntimeModelReasoningLevels(
  modelId: string,
  availableModels?: AvailableModelConfig[],
): string[] | null {
  const option = findRuntimeModelOption(modelId, { availableModels })
  if (!option) return null
  if (
    option.supportedReasoningEfforts.length === 0 ||
    option.supportedReasoningEfforts.every((level) => level === 'none')
  ) {
    return null
  }
  return option.supportedReasoningEfforts
}

export function getRuntimeModelDefaultReasoning(
  modelId: string,
  availableModels?: AvailableModelConfig[],
): string {
  const option = findRuntimeModelOption(modelId, { availableModels })
  if (!option) return ''
  if (option.supportedReasoningEfforts.every((level) => level === 'none')) return ''
  return option.defaultReasoningEffort || option.supportedReasoningEfforts[0] || ''
}
