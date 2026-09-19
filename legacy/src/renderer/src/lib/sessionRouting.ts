import {
  isMissionSessionProtocol,
  type SessionProtocolFields,
} from '../../../shared/sessionProtocol.ts'

export type SessionRouteTarget = '/' | '/mission'

type SessionRouteSource = SessionProtocolFields | null | undefined

export function getSessionRouteTarget(session?: SessionRouteSource): SessionRouteTarget {
  return isMissionSessionProtocol(session) ? '/mission' : '/'
}

export function getAppRouteTarget(opts: {
  hasPendingNewSession?: boolean
  activeSession?: SessionRouteSource
}): SessionRouteTarget {
  if (opts.hasPendingNewSession) return '/'
  return getSessionRouteTarget(opts.activeSession)
}

export function getSessionSidebarTestId(session: { id: string } & SessionProtocolFields): string {
  return isMissionSessionProtocol(session)
    ? `session-mission-${session.id}`
    : `session-${session.id}`
}
