// What a Client copies to point another Session at this one: the Session's
// id, or a few lines with its title, Workspace and transcript file. Only the
// Gateway knows where that file is on the computer.
import { gatewaySessionFileUrl, type SessionFileFound } from './gateway'
import type { SessionSummary } from './sessions'

export function createSessionFileLookup(
  config: { gatewayUrl: string; pairingToken: string | null },
  fetchImpl: typeof fetch,
): (sessionId: string) => Promise<string | null> {
  return async (sessionId) => {
    try {
      const response = await fetchImpl(
        gatewaySessionFileUrl(config.gatewayUrl, config.pairingToken ?? '', sessionId),
        { cache: 'no-store' },
      )
      if (!response.ok) return null
      return ((await response.json()) as SessionFileFound).path
    } catch {
      return null
    }
  }
}

/** The lines "Copy session details" puts on the clipboard; unknown ones are left out. */
export function sessionDetails(
  session: Pick<SessionSummary, 'sessionId' | 'title' | 'cwd'>,
  file: string | null,
): string {
  return [
    `Title: ${session.title}`,
    `Session ID: ${session.sessionId}`,
    session.cwd ? `Workspace: ${session.cwd}` : null,
    file ? `Transcript: ${file}` : null,
  ]
    .filter((line) => line !== null)
    .join('\n')
}
