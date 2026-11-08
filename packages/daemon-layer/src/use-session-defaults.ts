// What a new Session starts with: the models the Kernel offers, the computer's default model and
// thinking level (the Session defaults, which its Host gives the Kernel as `harness.info.agent`),
// and the autonomy of its default approval policy.
import { useMemo } from 'react'
import { clientAutonomy } from './autonomy'
import { useKernelSnapshot } from './kernel-state'
import { useModelChoices, type SessionSettingsView } from './use-session-settings'

export function useSessionDefaults(): SessionSettingsView {
  const { policies, info } = useKernelSnapshot()
  const models = useModelChoices()
  const autonomy = policies.get(null)?.autonomy
  const agent = info?.agent
  return useMemo(
    () => ({
      modelId: agent?.model.modelId ?? null,
      reasoningEffort: agent?.thinkingLevel ?? null,
      autonomyLevel: clientAutonomy(autonomy),
      models,
    }),
    [agent, autonomy, models],
  )
}
