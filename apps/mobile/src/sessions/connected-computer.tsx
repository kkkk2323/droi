// What every page of the connected computer shares: its Session list, the
// Sessions started here that the Daemon does not list yet, unread marks and
// subagent runs. It sits above the stack, so alerts are raised once, and the
// Session on screen is the one the route names.
import { usePreference } from '@droi/daemon-layer/local-preference'
import type { SessionSummary } from '@droi/daemon-layer/sessions'
import {
  SubagentLinksProvider,
  subagentsByToolUse,
  useSubagentRuns,
  type SubagentRun,
} from '@droi/daemon-layer/subagents'
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
  runs: ReadonlyMap<string, SubagentRun>
  /** A listed Session, or one started here that the Daemon lists only after its first message. */
  find: (sessionId: string) => SessionSummary | null
  /** Keeps a Session just started here until the Daemon lists it. */
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
  const unread = usePhoneAlerts(onScreen)
  const runs = useSubagentRuns(
    sessions.sessions.filter((s) => s.callingSessionId).map((s) => s.sessionId),
  )
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
      runs,
      find: (sessionId) =>
        sessions.sessions.find((s) => s.sessionId === sessionId) ?? created.get(sessionId) ?? null,
      started: (session) => setCreated((prev) => new Map(prev).set(session.sessionId, session)),
      open,
    }
  }, [computer, sessions, unread, runs, created, navigation, router])
  return (
    <ConnectedComputerContext value={value}>
      <SubagentLinksProvider
        value={{ byToolUse: subagentsByToolUse(sessions.sessions), runs, open: value.open }}
      >
        {children}
      </SubagentLinksProvider>
    </ConnectedComputerContext>
  )
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
