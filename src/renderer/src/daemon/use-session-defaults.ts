// What a new Session starts with: the Daemon's default model, reasoning effort
// and autonomy, plus the models on offer, so the start page can let the user
// change them before the Session exists.
import { useQuery } from '@tanstack/react-query'
import { useConnectionState, useDaemonConnection } from './connection-context'
import { toModelChoices, type SessionSettingsView } from './use-session-settings'

const EMPTY: SessionSettingsView = {
  modelId: null,
  reasoningEffort: null,
  autonomyLevel: null,
  models: [],
}

export function useSessionDefaults(): SessionSettingsView {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: ['session-defaults'],
    enabled: connected,
    staleTime: 60_000,
    queryFn: async (): Promise<SessionSettingsView> => {
      const defaults = await controller.getDefaultSettings()
      return {
        modelId: defaults.modelId ?? null,
        reasoningEffort: defaults.reasoningEffort ?? null,
        autonomyLevel: defaults.autonomyLevel ?? null,
        models: toModelChoices(defaults.availableModels ?? []),
      }
    },
  })
  return query.data ?? EMPTY
}
