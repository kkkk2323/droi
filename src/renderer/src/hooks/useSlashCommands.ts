import { useQuery } from '@tanstack/react-query'
import { getDroidClient } from '@/droidClient'
import type { SlashCommandDef, SkillDef } from '@/types'

const droid = getDroidClient()

export function useSlashCommandsQuery(projectDir: string, enabled = true) {
  return useQuery<SlashCommandDef[]>({
    queryKey: ['slashCommands', projectDir],
    queryFn: () => droid.listSlashCommands(),
    enabled: Boolean(projectDir) && enabled,
    staleTime: 1000,
  })
}

export function useSkillsQuery(projectDir: string, enabled = true) {
  return useQuery<SkillDef[]>({
    queryKey: ['skills', projectDir],
    queryFn: () => droid.listSkills(),
    enabled: Boolean(projectDir) && enabled,
    staleTime: 1000,
  })
}
