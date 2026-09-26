// Archiving from the session list. A Session in a Workspace is archived on its
// own. A Scratch Session is a conversation of its own (ADR 0008): its whole
// compaction chain goes, and its folder with it to the Trash; unarchiving
// brings the folder back first, so the Sessions open again.
import type { DaemonSessionController } from '@factory/droid-sdk'
import type { ScratchWorkspaces } from './scratch-workspaces'
import { continuationChain, isScratch, type SessionSummary } from './sessions'

interface Connection {
  controller: Pick<DaemonSessionController, 'archiveSession' | 'unarchiveSession'>
  scratch: Pick<ScratchWorkspaces, 'trash' | 'restore'>
}

export async function archiveConversation(
  { controller, scratch }: Connection,
  listed: readonly SessionSummary[],
  session: SessionSummary,
): Promise<void> {
  if (!isScratch(session.tags)) {
    await controller.archiveSession(session.sessionId)
    return
  }
  for (const each of [session, ...continuationChain(listed, session)]) {
    if (!each.archivedAt) await controller.archiveSession(each.sessionId)
  }
  if (session.cwd) await scratch.trash(session.cwd)
}

export async function unarchiveConversation(
  { controller, scratch }: Connection,
  listed: readonly SessionSummary[],
  session: SessionSummary,
): Promise<void> {
  if (!isScratch(session.tags)) {
    await controller.unarchiveSession(session.sessionId)
    return
  }
  if (session.cwd) await scratch.restore(session.cwd)
  for (const each of [session, ...continuationChain(listed, session)]) {
    await controller.unarchiveSession(each.sessionId)
  }
}
