// What every new Session starts with, as the Daemon keeps it in
// ~/.factory/settings.json (shared with the droid CLI and the Factory App):
// the view the settings pages show, and the pure rules they edit it by.
import { toModelChoices, type ModelChoice } from './use-session-settings'

export const SUBAGENT_TIERS = ['light', 'medium', 'heavy'] as const
export type SubagentTier = (typeof SUBAGENT_TIERS)[number]

export type SubagentModelSettings = Partial<
  Record<`${SubagentTier}Model` | `${SubagentTier}ReasoningEffort`, string>
>

export interface SessionDefaultsView {
  modelId: string | null
  reasoningEffort: string | null
  interactionMode: string
  autonomyLevel: string | null
  specModeModelId: string | null
  specModeReasoningEffort: string | null
  specSaveDir: string | null
  compactionTokenLimit: number | null
  compactionTokenLimitPerModel: Record<string, number>
  compactionModel: string
  compactionThresholdCheckEnabled: boolean
  subagentAutonomyLevel: string
  subagentModelSettings: SubagentModelSettings
  models: ModelChoice[]
  availableAutonomyLevels: string[]
  /** Where the Daemon keeps the user's own files (~/.factory), for the spec folder choices. */
  userFactoryDir: string | null
  /** Settings the organization manages; shown but not editable. */
  locked: ReadonlySet<string>
}

/** What `daemon.update_session_defaults` takes; `null` clears a "same as main" choice. */
export interface SessionDefaultsPatch {
  modelId?: string
  reasoningEffort?: string
  interactionMode?: string
  autonomyLevel?: string
  specModeModelId?: string | null
  specModeReasoningEffort?: string | null
  specSaveDir?: string | null
  compactionTokenLimit?: number
  compactionTokenLimitPerModel?: Record<string, number>
  compactionModel?: string
  compactionThresholdCheckEnabled?: boolean
  subagentAutonomyLevel?: string
  subagentModelSettings?: SubagentModelSettings
}

export const INTERACTION_MODES = [
  { value: 'auto', label: 'Auto' },
  { value: 'spec', label: 'Spec' },
] as const

export const AUTONOMY_DESCRIPTIONS: Record<string, string> = {
  off: 'Require approval for all actions',
  low: 'Allow file edits and read-only commands',
  medium: 'Allow reversible commands',
  high: 'Allow all commands',
}

/** The compaction limits Factory offers; a stored value outside them is still shown. */
export const COMPACTION_LIMITS = [
  100_000, 200_000, 250_000, 300_000, 400_000, 500_000, 600_000, 700_000, 800_000, 900_000,
  1_000_000,
]
export const DEFAULT_COMPACTION_LIMIT = 250_000
/** `compactionModel` for "compact with the Session's own model". */
export const CURRENT_MODEL = 'current-model'

/** `250000` → `250K`, `1000000` → `1M`. */
export function tokenLimitLabel(tokens: number): string {
  return tokens >= 1_000_000 ? `${tokens / 1_000_000}M` : `${tokens / 1_000}K`
}

type RawDefaults = Record<string, unknown>

/** The Daemon's `get_default_settings` answer (or `update_session_defaults`' `defaults`) as the view. */
export function toSessionDefaults(raw: RawDefaults): SessionDefaultsView {
  const str = (key: string) => (typeof raw[key] === 'string' ? (raw[key] as string) : null)
  const presets = raw['specSavePresets'] as { userFactoryDir?: unknown } | undefined
  return {
    modelId: str('modelId'),
    reasoningEffort: str('reasoningEffort'),
    interactionMode: str('interactionMode') ?? 'auto',
    autonomyLevel: str('autonomyLevel'),
    specModeModelId: str('specModeModelId'),
    specModeReasoningEffort: str('specModeReasoningEffort'),
    specSaveDir: str('specSaveDir'),
    compactionTokenLimit:
      typeof raw['compactionTokenLimit'] === 'number' ? raw['compactionTokenLimit'] : null,
    compactionTokenLimitPerModel: { ...(raw['compactionTokenLimitPerModel'] as object) } as Record<
      string,
      number
    >,
    compactionModel: str('compactionModel') ?? CURRENT_MODEL,
    compactionThresholdCheckEnabled: raw['compactionThresholdCheckEnabled'] !== false,
    subagentAutonomyLevel: str('subagentAutonomyLevel') ?? 'inherit',
    subagentModelSettings: { ...(raw['subagentModelSettings'] as object) },
    models: toModelChoices((raw['availableModels'] as never[] | undefined) ?? []),
    availableAutonomyLevels: Array.isArray(raw['availableAutonomyLevels'])
      ? (raw['availableAutonomyLevels'] as string[])
      : ['off', 'low', 'medium', 'high'],
    userFactoryDir: typeof presets?.userFactoryDir === 'string' ? presets.userFactoryDir : null,
    locked: lockedKeys(raw['management']),
  }
}

/** `management` marks org-managed keys `{disabled: true}`; nested groups become `subagent.lightModel`. */
function lockedKeys(management: unknown, prefix = ''): Set<string> {
  const locked = new Set<string>()
  if (typeof management !== 'object' || management === null) return locked
  for (const [key, value] of Object.entries(management)) {
    if (typeof value !== 'object' || value === null) continue
    if ('disabled' in value) {
      if (value.disabled === true) locked.add(prefix + key)
    } else {
      for (const nested of lockedKeys(value, `${prefix}${key}.`)) locked.add(nested)
    }
  }
  return locked
}

/** The view after a patch, before the Daemon confirms it. */
export function applyPatch(
  view: SessionDefaultsView,
  patch: SessionDefaultsPatch,
): SessionDefaultsView {
  const next = { ...view }
  for (const [key, value] of Object.entries(patch)) {
    if (value !== undefined) (next as Record<string, unknown>)[key] = value
  }
  return next
}

/**
 * The reasoning levels a model supports. Without the model (none chosen, or
 * one the Daemon no longer lists) the current value is all there is.
 */
export function reasoningChoices(
  models: readonly ModelChoice[],
  modelId: string | null,
  current: string | null,
): string[] {
  const supported = models.find((m) => m.id === modelId)?.reasoningEfforts ?? []
  if (supported.length > 0) return supported
  return current ? [current] : ['medium']
}

/** Models a setting may pick: listed and not disabled; the router is no use for compaction. */
export function pickableModels(
  models: readonly ModelChoice[],
  { routers = true }: { routers?: boolean } = {},
): ModelChoice[] {
  return models.filter((m) => !m.disabled && (routers || m.provider !== null))
}

/**
 * The Daemon replaces `subagentModelSettings` as a whole, so a change to one
 * tier sends every tier. A null model hands the tier back to the calling
 * Session's model ("Inherit") and drops its reasoning level with it; a null
 * level leaves the tier on its model's own default.
 */
export function withSubagentTier(
  settings: SubagentModelSettings,
  tier: SubagentTier,
  change: { model: string | null } | { reasoningEffort: string | null },
): SubagentModelSettings {
  const next = { ...settings }
  if ('model' in change) {
    delete next[`${tier}ReasoningEffort`]
    if (change.model === null) delete next[`${tier}Model`]
    else next[`${tier}Model`] = change.model
  } else if (change.reasoningEffort === null) {
    delete next[`${tier}ReasoningEffort`]
  } else {
    next[`${tier}ReasoningEffort`] = change.reasoningEffort
  }
  return next
}

export type SpecSaveChoice = 'user' | 'project' | 'custom'

/**
 * Where spec files go, as the Factory App offers it: under the user's
 * ~/.factory/docs (the Daemon's default, stored as nothing), under the
 * project's .factory/docs (a path relative to the Workspace), or a folder of
 * the user's choosing.
 */
export function specSavePaths(userFactoryDir: string | null): { user: string; project: string } {
  const folder = userFactoryDir?.split(/[\\/]/).filter(Boolean).pop() ?? '.factory'
  return { user: `~/${folder}/docs`, project: `${folder}/docs` }
}

export function specSaveChoice(
  stored: string | null,
  paths: { user: string; project: string },
): SpecSaveChoice {
  if (stored === null || stored === paths.user) return 'user'
  if (stored === paths.project) return 'project'
  return 'custom'
}
