import type {
  AskUserRequestParams,
  DroidMessage,
  ToolConfirmationInfo,
  ToolConfirmationOutcome,
} from '@factory/droid-sdk'
import type {
  DroidAskUserQuestion,
  DroidAskUserRequestPayload,
  DroidPermissionOption,
  DroidPermissionOptionMeta,
  DroidPermissionRequestPayload,
} from '../../shared/protocol.ts'

export type DroidBackendEvent =
  | { type: 'message'; sessionId: string; message: DroidMessage }
  | { type: 'permission-request'; sessionId: string; request: DroidPermissionRequestPayload }
  | { type: 'ask-user-request'; sessionId: string; request: DroidAskUserRequestPayload }
  | { type: 'error'; sessionId: string; message: string }
  | { type: 'turn-end'; sessionId: string; code: number }
  | { type: 'debug'; sessionId: string; message: string }

export function normalizeExecEnv(
  env?: Record<string, string | undefined>,
): Record<string, string> | undefined {
  if (!env) return undefined
  const out: Record<string, string> = {}
  for (const [key, value] of Object.entries(env)) {
    if (typeof value === 'string') out[key] = value
  }
  return Object.keys(out).length > 0 ? out : undefined
}

function normalizePermissionOption(value: unknown): DroidPermissionOption | null {
  const candidate = String(value || '').trim() as DroidPermissionOption
  if (!candidate) return null
  return candidate
}

export function buildPermissionRequestPayload(
  params: Record<string, unknown>,
): DroidPermissionRequestPayload {
  const toolUses = Array.isArray(params.toolUses) ? (params.toolUses as ToolConfirmationInfo[]) : []
  const rawOptions = Array.isArray(params.options) ? params.options : []
  const optionsMeta: DroidPermissionOptionMeta[] = []
  for (const item of rawOptions) {
    if (!item || typeof item !== 'object') continue
    const value = normalizePermissionOption((item as any).value)
    if (!value) continue
    optionsMeta.push({
      value: value as DroidPermissionOption,
      label: typeof (item as any).label === 'string' ? (item as any).label : value,
      selectedColor:
        typeof (item as any).selectedColor === 'string' ? (item as any).selectedColor : undefined,
      selectedPrefix:
        typeof (item as any).selectedPrefix === 'string' ? (item as any).selectedPrefix : undefined,
    })
  }

  const fallback: DroidPermissionOptionMeta[] =
    optionsMeta.length > 0
      ? optionsMeta
      : [
          { value: 'proceed_once' as DroidPermissionOption, label: 'Proceed once' },
          { value: 'proceed_always' as DroidPermissionOption, label: 'Proceed always' },
          { value: 'proceed_auto_run_low' as DroidPermissionOption, label: 'Auto-run (Low)' },
          { value: 'proceed_auto_run_medium' as DroidPermissionOption, label: 'Auto-run (Medium)' },
          { value: 'proceed_auto_run_high' as DroidPermissionOption, label: 'Auto-run (High)' },
          { value: 'cancel' as DroidPermissionOption, label: 'Cancel' },
        ]

  const first = toolUses[0]
  const confirmationType =
    typeof (params as any).confirmationType === 'string'
      ? String((params as any).confirmationType).trim() || undefined
      : typeof first?.confirmationType === 'string'
        ? String(first.confirmationType).trim() || undefined
        : undefined

  return {
    requestKey: crypto.randomUUID(),
    toolUses,
    confirmationType,
    optionsMeta: fallback,
  }
}

export function buildAskUserRequestPayload(
  params: AskUserRequestParams | Record<string, unknown>,
): DroidAskUserRequestPayload {
  const rawQuestions = Array.isArray((params as any)?.questions) ? (params as any).questions : []
  const questions: DroidAskUserQuestion[] = rawQuestions
    .map((item: any, index: number) => ({
      index: typeof item?.index === 'number' ? item.index : index,
      topic: typeof item?.topic === 'string' ? item.topic : undefined,
      question: String(item?.question || ''),
      options: Array.isArray(item?.options)
        ? item.options.map((option: unknown) => String(option))
        : [],
    }))
    .filter((item: DroidAskUserQuestion) => item.question.trim().length > 0)

  return {
    requestKey: crypto.randomUUID(),
    toolCallId: String((params as any)?.toolCallId || ''),
    questions,
  }
}

export function resolvePermissionOutcome(value: DroidPermissionOption): ToolConfirmationOutcome {
  return value as ToolConfirmationOutcome
}
