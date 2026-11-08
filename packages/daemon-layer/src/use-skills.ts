// The skills of a Session, read from and switched through the Kernel (`skills.list`,
// `skills.setDisabled`). The list is cached per Session and the "/" menu (use-slash-items.ts)
// is told to refresh with it, so a skill turned off leaves the menu at once.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useConnectionState, useKernelConnection } from './connection-context'
import { conversationIdOf } from './kernel-wire'
import type { Skill } from './skills'
import { SLASH_ITEMS_QUERY_KEY } from './use-slash-items'

export const SKILLS_QUERY_KEY = ['skills'] as const

export interface SkillsView {
  skills: readonly Skill[]
  isLoading: boolean
  error: string | null
}

const NO_SKILLS: Skill[] = []

export function useSkills(sessionId: string): SkillsView {
  const connection = useKernelConnection()
  const connected = useConnectionState().status === 'connected'
  const id = conversationIdOf(sessionId)
  const query = useQuery({
    queryKey: [...SKILLS_QUERY_KEY, sessionId],
    enabled: connected,
    // The computer's window may have switched one meanwhile.
    staleTime: 30_000,
    queryFn: async () =>
      (await connection.call<{ skills: Skill[] }>('skills.list', id === null ? {} : { id })).skills,
  })
  return {
    skills: query.data ?? NO_SKILLS,
    isLoading: query.isPending,
    error: query.error ? messageOf(query.error) : null,
  }
}

export interface SkillActions {
  setDisabled(skillName: string, disabled: boolean): Promise<void>
  /** The skill a switch is being written for, if any. */
  saving: string | null
  error: string | null
}

export function useSkillActions(sessionId: string): SkillActions {
  const connection = useKernelConnection()
  const queryClient = useQueryClient()
  const id = conversationIdOf(sessionId)
  const mutation = useMutation({
    mutationFn: (input: { skillName: string; disabled: boolean }) =>
      connection.call<{ skills: Skill[] }>('skills.setDisabled', {
        name: input.skillName,
        disabled: input.disabled,
        ...(id === null ? {} : { id }),
      }),
    onSuccess: (answer) =>
      queryClient.setQueryData([...SKILLS_QUERY_KEY, sessionId], answer.skills),
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: SKILLS_QUERY_KEY }),
        queryClient.invalidateQueries({ queryKey: SLASH_ITEMS_QUERY_KEY }),
      ]),
  })
  return {
    setDisabled: async (skillName, disabled) => {
      await mutation.mutateAsync({ skillName, disabled }).catch(() => undefined)
    },
    saving: mutation.isPending ? mutation.variables.skillName : null,
    error: mutation.error ? messageOf(mutation.error) : null,
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
