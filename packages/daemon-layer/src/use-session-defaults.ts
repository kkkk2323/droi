// What a new Session starts with, read from and saved to the Daemon. The start
// pages take the model, reasoning effort and autonomy from here; the settings
// pages edit the whole set.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { useConnectionState, useDaemonConnection } from './connection-context'
import {
  applyPatch,
  toSessionDefaults,
  type SessionDefaultsPatch,
  type SessionDefaultsView,
} from './session-defaults'
import type { SessionSettingsView } from './use-session-settings'

const QUERY_KEY = ['session-defaults'] as const

const EMPTY: SessionSettingsView = {
  modelId: null,
  reasoningEffort: null,
  autonomyLevel: null,
  models: [],
}

function useDefaultsQuery({ fresh }: { fresh: boolean }) {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  return useQuery({
    queryKey: QUERY_KEY,
    enabled: connected,
    // Another Client, the droid CLI or the Factory App may have changed them.
    staleTime: fresh ? 0 : 60_000,
    queryFn: async () =>
      toSessionDefaults(
        (await controller.getDefaultSettings()) as unknown as Record<string, unknown>,
      ),
  })
}

export function useSessionDefaults(): SessionSettingsView {
  const data = useDefaultsQuery({ fresh: false }).data
  if (!data) return EMPTY
  return {
    modelId: data.modelId,
    reasoningEffort: data.reasoningEffort,
    autonomyLevel: data.autonomyLevel,
    models: data.models,
  }
}

export interface SessionDefaultsEditor {
  /** Null until the Daemon has answered. */
  defaults: SessionDefaultsView | null
  /** Shows the change at once; the Daemon's answer replaces it, a failure puts it back. */
  update(patch: SessionDefaultsPatch): Promise<void>
  error: string | null
}

export function useSessionDefaultsEditor(): SessionDefaultsEditor {
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const query = useDefaultsQuery({ fresh: true })
  const [saveError, setSaveError] = useState<string | null>(null)

  const update = useCallback(
    async (patch: SessionDefaultsPatch) => {
      setSaveError(null)
      const before = queryClient.getQueryData<SessionDefaultsView>(QUERY_KEY)
      if (before) queryClient.setQueryData(QUERY_KEY, applyPatch(before, patch))
      try {
        const result = await controller.updateSessionDefaults(patch as never)
        queryClient.setQueryData(
          QUERY_KEY,
          toSessionDefaults(result.defaults as unknown as Record<string, unknown>),
        )
      } catch (cause) {
        if (before) queryClient.setQueryData(QUERY_KEY, before)
        setSaveError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [controller, queryClient],
  )

  return {
    defaults: query.data ?? null,
    update,
    error: saveError ?? query.error?.message ?? null,
  }
}
