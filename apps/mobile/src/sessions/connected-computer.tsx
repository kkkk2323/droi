// What every page of the connected computer shares: its Session list, the
// Sessions started here that the Kernel does not list yet, and unread marks. It
// sits above the stack, so alerts are raised once, and the Session on screen is
// the one the route names.
import { usePreference } from '@droi/daemon-layer/local-preference'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import { useNavigationContainerRef, usePathname, useRouter } from 'expo-router'
import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { usePhoneAlerts } from '../alerts/use-phone-alerts'
import {
  pairedComputers,
  selectedComputer,
  selectedComputerId,
  type PairedComputer,
} from '../computers/store'
import { useComputerSessions, type ComputerSessions } from './use-computer-sessions'

export interface ConnectedComputer {
  computer: PairedComputer
  sessions: ComputerSessions
  unread: ReadonlySet<string>
  /** A listed Session, or one started here that the list does not have yet. */
  find: (sessionId: string) => SessionSummary | null
  /** Keeps a Session just started here until the list has it. */
  started: (session: SessionSummary) => void
  /** Shows a Session: back to it when it is already on the stack, pushed otherwise. */
  open: (sessionId: string) => void
}

const ConnectedComputerContext = createContext<ConnectedComputer | null>(null)

export function useConnectedComputer(): ConnectedComputer | null {
  return useContext(ConnectedComputerContext)
}

export function useSelectedComputer(): PairedComputer | null {
  const [computers] = usePreference(pairedComputers)
  const [selectedId] = usePreference(selectedComputerId)
  return selectedComputer(computers, selectedId)
}

export function sessionPath(sessionId: string): string {
  return `/session/${sessionId}`
}

export function ConnectedComputerProvider({
  computer,
  children,
}: {
  computer: PairedComputer
  children: ReactNode
}) {
  const router = useRouter()
  const navigation = useNavigationContainerRef()
  const pathname = usePathname()
  const sessions = useComputerSessions(computer.id)
  const [created, setCreated] = useState<ReadonlyMap<string, SessionSummary>>(new Map())
  const onScreen = pathname.startsWith('/session/') ? pathname.slice('/session/'.length) : null
  const unread = usePhoneAlerts(onScreen, sessions.sessions)
  const value = useMemo<ConnectedComputer>(() => {
    const open = (sessionId: string) => {
      if (stackedSessions(navigation.getRootState()).includes(sessionId)) {
        router.dismissTo(sessionPath(sessionId))
      } else {
        router.push(sessionPath(sessionId))
      }
    }
    return {
      computer,
      sessions,
      unread,
      find: (sessionId) =>
        sessions.sessions.find((s) => s.sessionId === sessionId) ?? created.get(sessionId) ?? null,
      started: (session) => setCreated((prev) => new Map(prev).set(session.sessionId, session)),
      open,
    }
  }, [computer, sessions, unread, created, navigation, router])
  return <ConnectedComputerContext value={value}>{children}</ConnectedComputerContext>
}

interface RouteState {
  name: string
  params?: object
  state?: { routes: RouteState[] }
}

/** The Sessions on the connected computer's stack, bottom first. */
function stackedSessions(root: { routes: readonly RouteState[] } | undefined): string[] {
  const group = root?.routes.find((r) => r.name === '(connected)')
  return (group?.state?.routes ?? []).flatMap((r) => {
    const id = r.name === 'session/[id]' ? (r.params as { id?: unknown } | undefined)?.id : null
    return typeof id === 'string' ? [id] : []
  })
}
