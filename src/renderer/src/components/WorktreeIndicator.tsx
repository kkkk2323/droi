import React, { useMemo } from 'react'
import { GitBranch } from 'lucide-react'
import { useProjects, useActiveSessionId } from '@/store'

export function WorktreeIndicator() {
  const projects = useProjects()
  const activeSessionId = useActiveSessionId()

  const meta = useMemo(() => {
    for (const p of projects) {
      const s = p.sessions.find((x) => x.id === activeSessionId)
      if (s) return s
    }
    return null
  }, [projects, activeSessionId])

  if (!meta) return null

  return (
    <div className="flex items-center gap-1 px-2 py-0.5 text-xs text-muted-foreground cursor-default">
      <GitBranch className="size-3.5 text-muted-foreground" />
      <span className="max-w-[220px] truncate font-mono">{meta.branch || 'branch'}</span>
    </div>
  )
}
