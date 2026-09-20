// What "/" can start in the composer: the commands the Client runs itself,
// the Workspace's custom commands and the skills a user may invoke. The Daemon
// expands "/name args" itself when the message arrives, so for the latter two
// the Client only has to offer the names.
import { useQuery } from '@tanstack/react-query'
import { useConnectionState, useDaemonConnection } from './connection-context'

export interface SlashItem {
  name: string
  description: string
  /** Hint for what follows the name, e.g. "command arguments". */
  argumentHint: string | null
  kind: 'command' | 'skill'
}

/** Commands the composer handles before anything reaches the Daemon (see session-view.tsx). */
export const BUILTIN_SLASH_ITEMS: SlashItem[] = [
  {
    name: 'compact',
    description: 'Summarise the conversation and continue in a new session',
    argumentHint: '[instructions]',
    kind: 'command',
  },
]

export function useSlashItems(sessionId: string): SlashItem[] {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: ['slash-items', sessionId],
    enabled: connected,
    staleTime: 60_000,
    queryFn: async (): Promise<SlashItem[]> => {
      const [commands, skills] = await Promise.all([
        controller.listCommands(sessionId),
        controller.listSkills(sessionId),
      ])
      const items: SlashItem[] = [...BUILTIN_SLASH_ITEMS]
      const taken = new Set(items.map((i) => i.name))
      for (const c of commands.commands) {
        if (taken.has(c.name)) continue
        taken.add(c.name)
        items.push({
          name: c.name,
          description: c.description,
          argumentHint: c.argumentHint ?? null,
          kind: 'command',
        })
      }
      for (const skill of skills.skills) {
        if (skill.userInvocable === false || skill.enabled === false || taken.has(skill.name)) {
          continue
        }
        items.push({
          name: skill.name,
          description: skill.description ?? '',
          argumentHint: null,
          kind: 'skill',
        })
      }
      return items
    },
  })
  return query.data ?? BUILTIN_SLASH_ITEMS
}

/** The "/word" being typed at the start of the composer, or null. */
export function slashQuery(text: string, caret: number): string | null {
  const head = text.slice(0, caret)
  const match = /^\/(\S*)$/.exec(head)
  if (!match || /\s/.test(text.slice(caret))) return null
  return match[1] ?? ''
}

export function filterSlashItems(items: SlashItem[], query: string, limit = 8): SlashItem[] {
  const q = query.toLowerCase()
  const starts = items.filter((i) => i.name.toLowerCase().startsWith(q))
  const contains = items.filter((i) => !starts.includes(i) && i.name.toLowerCase().includes(q))
  return [...starts, ...contains].slice(0, limit)
}
