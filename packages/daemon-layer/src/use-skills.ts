// The skills of a Session, read from and switched through the Daemon. The
// list is cached per Session and the "/" menu (use-slash-items.ts) is told to
// refresh with it, so a skill turned off leaves the menu at once.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useConnectionState, useDaemonConnection } from './connection-context'
import type { Skill, SkillLevel } from './skills'
import { SLASH_ITEMS_QUERY_KEY } from './use-slash-items'

export const SKILLS_QUERY_KEY = ['skills'] as const

export interface SkillsView {
  skills: readonly Skill[]
  /** Whether the Session's Workspace can hold project-level switches. */
  projectAvailable: boolean
  isLoading: boolean
  error: string | null
}

export function useSkills(sessionId: string): SkillsView {
  const { controller } = useDaemonConnection()
  const connected = useConnectionState().status === 'connected'
  const query = useQuery({
    queryKey: [...SKILLS_QUERY_KEY, sessionId],
    enabled: connected,
    // The droid CLI or the Factory App may have switched one meanwhile.
    staleTime: 30_000,
    queryFn: () => controller.listSkills(sessionId),
  })
  return {
    skills: query.data?.skills ?? NO_SKILLS,
    projectAvailable: query.data?.projectAvailable === true,
    isLoading: query.isPending,
    error: query.error ? messageOf(query.error) : null,
  }
}

const NO_SKILLS: Skill[] = []

export interface SkillActions {
  setDisabled(skillName: string, disabled: boolean, level: SkillLevel): Promise<void>
  /** The skill a switch is being written for, if any. */
  saving: string | null
  error: string | null
}

export function useSkillActions(sessionId: string): SkillActions {
  const { controller } = useDaemonConnection()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: (input: { skillName: string; disabled: boolean; level: SkillLevel }) =>
      controller.setSkillDisabled({
        sessionId,
        skillName: input.skillName,
        disabled: input.disabled,
        settingsLevel: input.level as never,
      }),
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: [...SKILLS_QUERY_KEY, sessionId] }),
        queryClient.invalidateQueries({ queryKey: [...SLASH_ITEMS_QUERY_KEY, sessionId] }),
      ]),
  })
  return {
    setDisabled: async (skillName, disabled, level) => {
      await mutation.mutateAsync({ skillName, disabled, level }).catch(() => undefined)
    },
    saving: mutation.isPending ? mutation.variables.skillName : null,
    error: mutation.error ? messageOf(mutation.error) : null,
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
