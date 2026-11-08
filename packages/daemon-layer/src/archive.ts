// Archiving from the session list: `index.update` with `archived`. A Scratch Session is a
// conversation of its own (ADR 0008): its folder goes with it to the Trash, and unarchiving
// brings the folder back first, so the Session opens again.
import type { KernelConnection } from './connection'
import { conversationIdOf } from './kernel-wire'
import type { ScratchWorkspaces } from './scratch-workspaces'
import { isScratchSession, type SessionSummary } from './sessions'

type Connection = Pick<KernelConnection, 'call'> & {
  scratch: Pick<ScratchWorkspaces, 'trash' | 'restore'>
}

async function setArchived(connection: Connection, sessionId: string, archived: boolean) {
  const id = conversationIdOf(sessionId)
  if (id === null) throw new Error('Session not found')
  await connection.call('index.update', { id, archived })
}

export async function archiveConversation(
  connection: Connection,
  session: SessionSummary,
): Promise<void> {
  await setArchived(connection, session.sessionId, true)
  if (isScratchSession(session) && session.cwd) await connection.scratch.trash(session.cwd)
}

export async function unarchiveConversation(
  connection: Connection,
  session: SessionSummary,
): Promise<void> {
  if (isScratchSession(session) && session.cwd) await connection.scratch.restore(session.cwd)
  await setArchived(connection, session.sessionId, false)
}
