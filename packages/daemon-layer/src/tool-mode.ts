// Whether a Session's model calls tools directly or from a Script program.
// The Daemon fixes the mode when it creates the Session and offers no way to
// change it later, so it is chosen only for new Sessions. Without a choice,
// droid decides: `toolExecutionMode` in ~/.factory/settings.json, else
// Factory's per-model list, else direct.
import { createPreference } from './local-preference'

export const TOOL_MODES = ['direct_only', 'script_only'] as const
export type ToolMode = (typeof TOOL_MODES)[number]

export const TOOL_MODE_LABELS: Record<ToolMode, string> = {
  direct_only: 'Direct',
  script_only: 'Script',
}

export function isToolMode(value: unknown): value is ToolMode {
  return TOOL_MODES.includes(value as ToolMode)
}

/**
 * A new Session page's mode: the one it shows, and the one it asks the
 * Daemon for. The page's own choice wins, then this device's default; when
 * droid decides, the page shows what the Daemon gave the draft.
 */
export function newSessionToolMode(
  choice: ToolMode | undefined,
  fallback: ToolMode | null,
  draft: ToolMode | null,
): { shown: ToolMode | null; requested: ToolMode | null } {
  const requested = choice ?? fallback
  return { shown: requested ?? draft, requested }
}

/** What new Sessions on this device start with; null leaves it to droid. */
export const defaultToolMode = createPreference<ToolMode | null>('droi.toolExecutionMode', null, {
  parse: (raw) => (isToolMode(raw) ? raw : null),
  serialize: (value) => value ?? '',
})
