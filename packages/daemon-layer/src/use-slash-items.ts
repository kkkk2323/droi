// What "/" can start in the composer: the commands the Client runs itself, and the prompt
// templates and skills the Kernel expands (`resources.list`). The Kernel expands "/name args"
// itself when the message arrives, so for those the Client only has to offer the names.
// Without a Session only the personal ones are listed: the Workspace's need a conversation
// whose folder decides them.
import { useQuery } from '@tanstack/react-query'
import { useConnectionState, useKernelConnection } from './connection-context'
import { conversationIdOf } from './kernel-wire'

export interface SlashItem {
  name: string
  description: string
  /** Hint for what follows the name, e.g. "command arguments". */
  argumentHint: string | null
  kind: 'command' | 'skill'
}

/** Commands the composer handles before anything reaches the Kernel (see session-screen.tsx). */
export const BUILTIN_SLASH_ITEMS: SlashItem[] = [
  {
    name: 'compact',
    description: 'Summarise the conversation to free up context',
    argumentHint: '[instructions]',
    kind: 'command',
  },
]

export const SLASH_ITEMS_QUERY_KEY = ['slash-items'] as const

interface Resources {
  prompts: Array<{ name: string; description?: string }>
  skills: Array<{ name: string; description?: string; enabled?: boolean }>
}

/**
 * `builtins` are the commands the caller handles itself; they come first and
 * win over a Workspace command of the same name.
 */
export function useSlashItems(
  sessionId: string | null,
  builtins: SlashItem[] = BUILTIN_SLASH_ITEMS,
): SlashItem[] {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const id = sessionId === null ? null : conversationIdOf(sessionId)
  const query = useQuery({
    queryKey: [...SLASH_ITEMS_QUERY_KEY, sessionId],
    enabled: connected,
    staleTime: 60_000,
    queryFn: async (): Promise<SlashItem[]> =>
      slashItemsOf(await connection.call<Resources>('resources.list', id === null ? {} : { id })),
  })
  return mergeSlashItems(builtins, query.data ?? NO_ITEMS)
}

const NO_ITEMS: SlashItem[] = []

/** The Kernel's templates, then its enabled skills; a template wins over a skill of its name. */
export function slashItemsOf(resources: Resources): SlashItem[] {
  const items: SlashItem[] = []
  const taken = new Set<string>()
  for (const prompt of resources.prompts) {
    if (taken.has(prompt.name)) continue
    taken.add(prompt.name)
    items.push({
      name: prompt.name,
      description: prompt.description ?? '',
      argumentHint: null,
      kind: 'command',
    })
  }
  for (const skill of resources.skills) {
    if (skill.enabled === false || taken.has(skill.name)) continue
    taken.add(skill.name)
    items.push({
      name: skill.name,
      description: skill.description ?? '',
      argumentHint: null,
      kind: 'skill',
    })
  }
  return items
}

export function mergeSlashItems(builtins: SlashItem[], fromKernel: SlashItem[]): SlashItem[] {
  if (fromKernel.length === 0) return builtins
  const names = new Set(builtins.map((i) => i.name))
  return [...builtins, ...fromKernel.filter((i) => !names.has(i.name))]
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

/**
 * The command or skill a message starts with, once its name is complete
 * ("/name " then the rest): the composer shows it as a tag before the text.
 */
export function pickedSlashItem(
  text: string,
  items: readonly SlashItem[],
): { item: SlashItem; rest: string } | null {
  const match = /^\/(\S+) /.exec(text)
  const item = match ? items.find((i) => i.name === match[1]) : undefined
  return match && item ? { item, rest: text.slice(match[0].length) } : null
}
