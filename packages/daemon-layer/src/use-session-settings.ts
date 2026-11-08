// A Session's settings: its model and thinking level (the conversation's agent, pi.agent)
// and its autonomy (the Kernel's approval policy for it). Changes go to the Kernel, whose
// next view or policy.changed shows them.
import { useCallback, useMemo, useState, useSyncExternalStore } from 'react'
import { clientAutonomy, kernelAutonomy } from './autonomy'
import { useKernelConnection } from './connection-context'
import { conversationStore } from './kernel-conversations'
import { useKernelSnapshot } from './kernel-state'
import { conversationIdOf, type AgentDoc, type ModelInfo } from './kernel-wire'

export interface ModelChoice {
  id: string
  label: string
  reasoningEfforts: string[]
  disabled: boolean
  /** The provider the Kernel reaches the model through; null for none. */
  provider: string | null
  /** Factory's usage multiplier; the Kernel lists none. */
  multiplier: number | null
}

export interface SessionSettingsView {
  modelId: string | null
  reasoningEffort: string | null
  autonomyLevel: string | null
  models: ModelChoice[]
}

export const AUTONOMY_LEVELS = ['off', 'low', 'medium', 'high'] as const

export function toModelChoices(models: readonly ModelInfo[]): ModelChoice[] {
  return models.map((model) => ({
    id: model.id,
    label: model.name || model.id,
    reasoningEfforts: model.thinking ?? [],
    disabled: false,
    provider: model.provider,
    multiplier: null,
  }))
}

const NO_MODELS: ModelChoice[] = []

/** The models the Kernel offers, as the pickers list them. */
export function useModelChoices(): ModelChoice[] {
  const { info } = useKernelSnapshot()
  return useMemo(() => (info ? toModelChoices(info.models) : NO_MODELS), [info])
}

export function useSessionSettings(sessionId: string): SessionSettingsView {
  const store = conversationStore(useKernelConnection())
  const { policies } = useKernelSnapshot()
  const models = useModelChoices()
  const id = conversationIdOf(sessionId)
  const subscribe = useCallback(
    (listener: () => void) => (id === null ? () => {} : store.subscribe(id, listener)),
    [store, id],
  )
  const getAgent = useCallback(
    () => (id === null ? undefined : (store.snapshot(id).docs['pi.agent'] as AgentDoc | undefined)),
    [store, id],
  )
  const agent = useSyncExternalStore(subscribe, getAgent, getAgent)
  const policy = (id !== null ? policies.get(id) : undefined) ?? policies.get(null)
  return useMemo(
    () => ({
      modelId: agent?.model?.modelId ?? null,
      reasoningEffort: agent?.thinkingLevel ?? null,
      autonomyLevel: clientAutonomy(policy?.autonomy),
      models,
    }),
    [agent, policy, models],
  )
}

export interface SessionSettingsActions {
  setModel(modelId: string): Promise<void>
  setReasoningEffort(effort: string): Promise<void>
  setAutonomyLevel(level: string): Promise<void>
  rename(title: string): Promise<void>
  error: string | null
}

export function useSessionSettingsActions(sessionId: string): SessionSettingsActions {
  const connection = useKernelConnection()
  const models = useKernelSnapshot().info?.models
  const [error, setError] = useState<string | null>(null)
  const id = conversationIdOf(sessionId)

  const run = useCallback(
    async (method: string, params: Record<string, unknown>) => {
      setError(null)
      try {
        await connection.call(method, { id, ...params })
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [connection, id],
  )

  return {
    setModel: async (modelId) => {
      const model = models?.find((m) => m.id === modelId)
      if (!model) {
        setError(`The computer does not offer ${modelId}.`)
        return
      }
      await run('conversation.configure', {
        change: { model: { provider: model.provider, modelId } },
      })
    },
    setReasoningEffort: (effort) =>
      run('conversation.configure', { change: { thinkingLevel: effort } }),
    setAutonomyLevel: (level) => run('policy.set', { policy: { autonomy: kernelAutonomy(level) } }),
    rename: (title) => run('index.update', { title }),
    error,
  }
}
