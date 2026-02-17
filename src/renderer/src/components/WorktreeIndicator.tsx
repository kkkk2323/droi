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

  const isWorktree = meta.workspaceType === 'worktree'

  return (
    <div className="flex items-center gap-1 rounded-md border-border bg-card/80 px-2 py-0.5 text-xs text-muted-foreground cursor-default">
      <GitBranch className="size-3.5 text-muted-foreground" />
      <span className="max-w-[220px] truncate font-mono">{meta.branch || 'branch'}</span>
      {isWorktree && (
        <span className="ml-1 rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">wt</span>
      )}
    </div>
  )
}

