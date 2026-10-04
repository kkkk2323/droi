// Every message the user sent in a Session. A Session opens with its latest
// messages only (LOADED_MESSAGE_LIMIT), and a long run of tool calls can fill
// those without one of the user's. The Daemon filters a page by role, so
// asking for the user's alone costs a request or two even in a Session of
// thousands of messages.
import { filterMessagesForUI, type DaemonSessionController } from '@factory/droid-sdk'
import { useQuery } from '@tanstack/react-query'
import { useDaemonConnection } from './connection-context'
import { USER_ROLE } from './sdk-enums'
import { buildTranscript, type TranscriptEntry } from './transcript'
import { OLDER_MESSAGES_PAGE } from './use-older-messages'

const NONE: readonly TranscriptEntry[] = []

type PageMessage = Awaited<
  ReturnType<DaemonSessionController['getSessionMessages']>
>['messages'][number]

/** The user's messages as transcript entries, oldest first; read only while `enabled`. */
export function useUserMessages(sessionId: string, enabled: boolean): readonly TranscriptEntry[] {
  const { controller } = useDaemonConnection()
  const query = useQuery({
    queryKey: ['user-messages', sessionId],
    enabled,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => {
      const pages: PageMessage[][] = []
      let cursor: string | undefined
      for (;;) {
        const page = await controller.getSessionMessages({
          sessionId,
          role: USER_ROLE,
          limit: OLDER_MESSAGES_PAGE,
          ...(cursor ? { cursor } : {}),
        })
        pages.push(page.messages)
        cursor = page.nextCursor ?? page.messages.at(-1)?.id
        if (!page.hasMore || !cursor) break
      }
      // As the SDK shows a loaded Session: system reminders and hidden messages left out.
      return buildTranscript(filterMessagesForUI(pages.flat().reverse()))
    },
  })
  return (enabled && query.data) || NONE
}

/**
 * The user's messages from before the loaded ones: those ahead of the first
 * the transcript holds. When it holds none, all of them are, since a Session
 * loads its latest messages; messages sent since the list was read are in
 * the transcript and not in `all`.
 */
export function unloadedUserMessages(
  all: readonly TranscriptEntry[],
  transcript: readonly TranscriptEntry[],
): readonly TranscriptEntry[] {
  if (all.length === 0) return NONE
  const loaded = new Set(transcript.map((entry) => entry.id))
  const first = all.findIndex((entry) => loaded.has(entry.id))
  if (first === 0) return NONE
  return first < 0 ? all : all.slice(0, first)
}
