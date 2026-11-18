// How the Session list is ordered, how it is laid out and what is pinned. The
// computer's window decides them, through its Gateway, so the phone lists the
// Sessions as the computer does, and a pin made here is made there. A Gateway
// with no window to follow leaves them to this phone's own preferences. The
// computer's Session Times date the Sessions (session-times.ts).
import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import type { PinKind, SessionListOrder } from '@droi/daemon-layer/gateway'
import {
  pinnedSessions,
  pinnedWorkspaces,
  sessionsFirstSeen,
  usePreference,
} from '@droi/daemon-layer/local-preference'
import { sortOrderOf, viewOf, withPin } from '@droi/daemon-layer/session-list-order'
import { useSessionTimes } from '@droi/daemon-layer/session-times'
import type { Pins, SessionView, SortOrder } from '@droi/daemon-layer/sessions'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useFocusEffect } from 'expo-router'
import { useCallback } from 'react'

const LIST_ORDER_KEY = ['session-list-order'] as const

export interface ListOrder {
  pins: Pins
  order: SortOrder
  /** By Workspace or by time, as the computer's window shows it. */
  view: SessionView
  setPinned: (kind: PinKind, key: string, pinned: boolean) => void
}

export function useListOrder(): ListOrder {
  const connection = useDaemonConnection()
  const queryClient = useQueryClient()
  const [localWorkspaces, setLocalWorkspaces] = usePreference(pinnedWorkspaces)
  const [localSessions, setLocalSessions] = usePreference(pinnedSessions)
  const [firstSeen] = usePreference(sessionsFirstSeen)
  const { times } = useSessionTimes()
  const query = useQuery({
    queryKey: LIST_ORDER_KEY,
    queryFn: () => connection.sessionListOrder.read(),
    staleTime: 5_000,
    refetchInterval: 15_000,
  })
  const { refetch } = query
  useFocusEffect(
    useCallback(() => {
      void refetch()
    }, [refetch]),
  )
  const shared = query.data ?? null
  return {
    pins: {
      workspaces: new Set(shared ? shared.pinnedWorkspaces : localWorkspaces),
      sessions: new Set(shared ? shared.pinnedSessions : localSessions),
    },
    order: { ...sortOrderOf(shared, firstSeen), times },
    view: viewOf(shared),
    setPinned: (kind, key, pinned) => {
      if (!shared) {
        const [list, set] =
          kind === 'workspace'
            ? [localWorkspaces, setLocalWorkspaces]
            : [localSessions, setLocalSessions]
        set(pinned ? [...list.filter((k) => k !== key), key] : list.filter((k) => k !== key))
        return
      }
      queryClient.setQueryData<SessionListOrder | null>(LIST_ORDER_KEY, (old) =>
        old ? withPin(old, kind, key, pinned) : old,
      )
      void connection.sessionListOrder
        .setPinned(kind, key, pinned)
        .catch(console.error)
        .finally(() => void queryClient.invalidateQueries({ queryKey: LIST_ORDER_KEY }))
    },
  }
}
