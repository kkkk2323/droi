// Draft Session: a Session the New session page opens as soon as it knows the
// Workspace, because the Daemon lists skills and commands per Session only
// and the composer wants them before the first send. The draft carries
// DRAFT_TAG, which keeps it out of every Client's list; the first send takes
// it over (drops the tag, applies the page's settings). A connection holds at
// most one draft: opening one for another Workspace closes the previous one.
// A draft in a Scratch Workspace takes its folder along when it is closed
// (the Gateway only removes it while it is empty).
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { DaemonConnection } from './connection'
import { useConnectionState, useDaemonConnection } from './connection-context'
import { LOAD_STATE } from './sdk-enums'
import { DRAFT_TAG, SESSIONS_QUERY_KEY, isScratch, type SessionTag } from './sessions'
import { openSession, type NewSessionSettings } from './use-new-session'

interface Draft {
  path: string
  /** What the Session keeps once taken; DRAFT_TAG comes on top while it is a draft. */
  tags: SessionTag[]
  sessionId: Promise<string | null>
  /** Set once the Daemon answered. */
  settled?: string | null
}

const NO_TAGS: SessionTag[] = []
const drafts = new WeakMap<DaemonConnection, Draft>()

function discard(connection: DaemonConnection, draft: Draft, keepFolder = false): void {
  if (drafts.get(connection) === draft) drafts.delete(connection)
  void draft.sessionId.then(async (id) => {
    if (id) await connection.controller.closeSession(id).catch(() => {})
    if (!keepFolder && isScratch(draft.tags)) {
      await connection.scratch.trash(draft.path).catch(() => {})
    }
  })
}

/** The folder of the connection's draft when it is in a Scratch Workspace, to open again. */
export function scratchDraftFolder(connection: DaemonConnection): string | null {
  const current = drafts.get(connection)
  return current && isScratch(current.tags) ? current.path : null
}

/** Closes the connection's draft, if any: the New session page was left without sending. */
export function closeDraftSession(connection: DaemonConnection): void {
  const current = drafts.get(connection)
  if (current) discard(connection, current)
}

/** The draft for this Workspace, opened (or reopened) when there is none usable. */
function draftFor(
  connection: DaemonConnection,
  path: string,
  tags: SessionTag[],
): Promise<string | null> {
  const current = drafts.get(connection)
  // A drop marks every Session not loaded; the Daemon may have restarted without it.
  const lost =
    current?.settled &&
    connection.sessionState.getSessionLoadState(current.settled) !== LOAD_STATE.loaded
  if (current && current.path === path && !lost) return current.sessionId
  // A lost draft is reopened in the same folder, so that folder stays.
  if (current) discard(connection, current, current.path === path)
  const draft: Draft = {
    path,
    tags,
    // An unusable directory is reported by the ordinary create on send.
    sessionId: openSession(connection, path, { tags: [{ name: DRAFT_TAG }, ...tags] }).then(
      (opened) => ('sessionId' in opened ? opened.sessionId : null),
      () => null,
    ),
  }
  void draft.sessionId.then((id) => (draft.settled = id))
  drafts.set(connection, draft)
  return draft.sessionId
}

export interface DraftSession {
  /** The draft for the Workspace, once the Daemon has opened it. */
  sessionId: string | null
  /** A send is taking the draft over; the page is about to leave. */
  isTaking: boolean
  /**
   * Turns the draft into an ordinary Session with these settings. Resolves to
   * its id, or to null when there is no usable draft (one for another
   * Workspace is closed) and the caller should create the Session itself.
   */
  take(path: string, settings: NewSessionSettings): Promise<string | null>
}

export function useDraftSession(path: string | null, tags: SessionTag[] = NO_TAGS): DraftSession {
  const connection = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const queryClient = useQueryClient()
  // Once taken, the draft must not be reopened while the page is on its way out.
  const [taken, setTaken] = useState(false)

  const query = useQuery({
    queryKey: ['draft-session', path],
    enabled: connected && path !== null && !taken,
    // draftFor is the source of truth and answers repeat calls from memory, so
    // every mount or reconnect may ask again; nothing is kept for other paths.
    staleTime: 0,
    gcTime: 0,
    queryFn: () => draftFor(connection, path!, tags),
  })
  const sessionId = taken ? null : (query.data ?? null)

  const take = async (target: string, settings: NewSessionSettings): Promise<string | null> => {
    const draft = drafts.get(connection)
    if (!draft) return null
    if (draft.path !== target || !draft.settled || draft.settled !== sessionId) {
      // The caller creates the Session itself; this draft would linger unseen.
      // In the same folder the caller is about to use it, so that stays.
      discard(connection, draft, draft.path === target)
      return null
    }
    drafts.delete(connection)
    setTaken(true)
    const id = draft.settled
    try {
      await connection.controller.updateSessionSettings(id, {
        tags: draft.tags,
        ...(settings.modelId ? { modelId: settings.modelId } : {}),
        ...(settings.reasoningEffort ? { reasoningEffort: settings.reasoningEffort as never } : {}),
        ...(settings.autonomyLevel ? { autonomyLevel: settings.autonomyLevel as never } : {}),
      })
    } catch {
      discard(connection, draft, true)
      setTaken(false)
      return null
    }
    await queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
    return id
  }

  return { sessionId, isTaking: taken, take }
}
