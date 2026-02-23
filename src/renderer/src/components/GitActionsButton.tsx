import React, { useState } from 'react'
import { GitCommitHorizontal } from 'lucide-react'
import { cn } from '@/lib/utils'
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
  const buttonDisabled = !hasGitChanges

  if (!projectDir) return null

  return (
    <>
      <button
        type="button"
        onClick={() => setCommitOpen(true)}
        disabled={buttonDisabled}
        className={cn(
          'flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent hover:border-border/80',
          buttonDisabled && 'opacity-50 pointer-events-none',
        )}
      >
        <GitCommitHorizontal className="size-3.5" />
        <span>Commit</span>
      </button>

      <CommitWizard open={commitOpen} onOpenChange={setCommitOpen} projectDir={projectDir} />
    </>
  )
}
