import React, { useState } from 'react'
import { GitCommitHorizontal } from 'lucide-react'
import { useGitStatusQuery } from '@/hooks/useGitStatus'
import { CommitWizard } from '@/components/commit/CommitWizard'

interface GitActionsButtonProps {
  projectDir: string
  isRunning: boolean
}

export function GitActionsButton({ projectDir, isRunning }: GitActionsButtonProps) {
  const { data: gitStatusFiles = [] } = useGitStatusQuery(projectDir, !isRunning)
  const hasGitChanges = gitStatusFiles.length > 0
  const [commitOpen, setCommitOpen] = useState(false)

  if (!projectDir || !hasGitChanges) return null

  return (
    <>
      <button
        type="button"
        onClick={() => setCommitOpen(true)}
        className="flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent hover:border-border/80"
      >
        <GitCommitHorizontal className="size-3.5" />
        <span>Commit</span>
      </button>

      <CommitWizard open={commitOpen} onOpenChange={setCommitOpen} projectDir={projectDir} />
    </>
  )
}
