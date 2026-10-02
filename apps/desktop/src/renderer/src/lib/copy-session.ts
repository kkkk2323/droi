import { useDaemonConnection } from '@droi/daemon-layer/connection-context'
import { sessionDetails } from '@droi/daemon-layer/session-file'
import type { SessionSummary } from '@droi/daemon-layer/sessions'

export type CopyWhat = 'id' | 'details'

export type CopySession = (
  session: Pick<SessionSummary, 'sessionId' | 'title' | 'cwd'>,
  what: CopyWhat,
) => Promise<void>

/** Puts a Session's id, or its details with the transcript path, on the clipboard. */
export function useCopySession(): CopySession {
  const connection = useDaemonConnection()
  return async (session, what) => {
    const text =
      what === 'id'
        ? session.sessionId
        : sessionDetails(session, await connection.sessionFile(session.sessionId))
    await navigator.clipboard.writeText(text)
  }
}
