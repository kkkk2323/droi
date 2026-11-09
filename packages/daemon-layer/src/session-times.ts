// Unread marks, as the Kernel keeps them (apps/kernel/protocol/README.md, "The list"): each
// conversation's `viewedAt`, when a person last saw everything up to there, beside its
// `lastActivityAt`. Opening a conversation marks it seen by itself; a Client marks it again
// when an answer arrives while it is on screen. Every Client hears the change (`index.changed`),
// so a Session read here reads on every one.
import { useCallback } from 'react'
import { useKernelConnection } from './connection-context'
import { conversationIdOf } from './kernel-wire'
import type { SessionSummary } from './sessions'

/** Activity after a person last saw it, or a dot someone asked for. */
export function isUnread(session: Pick<SessionSummary, 'activityAt' | 'viewedAt'>): boolean {
  return session.viewedAt === null || session.activityAt > session.viewedAt
}

/**
 * Marks a Session seen up to now. The time is never before its own last activity, which is
 * the computer's clock: this phone's may run behind it.
 */
export function useMarkViewed(): (
  session: Pick<SessionSummary, 'sessionId' | 'activityAt'>,
) => void {
  const connection = useKernelConnection()
  return useCallback(
    (session) => {
      const id = conversationIdOf(session.sessionId)
      if (id === null) return
      const viewedAt = Math.max(Date.now(), session.activityAt)
      void connection.call('index.update', { id, viewedAt }).catch(() => {})
    },
    [connection],
  )
}
