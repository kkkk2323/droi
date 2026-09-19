import React from 'react'
import { GitCommitHorizontal } from 'lucide-react'
import { useGitStatusQuery } from '@/hooks/useGitStatus'

interface GitActionsButtonProps {
  projectDir: string
  isRunning: boolean
  onOpenCommitDialog: (projectDir: string) => void
}

export function GitActionsButton({
  projectDir,
  isRunning,
  onOpenCommitDialog,
}: GitActionsButtonProps) {
  const { data: gitStatusFiles = [] } = useGitStatusQuery(projectDir, !isRunning)
  const hasGitChanges = gitStatusFiles.length > 0

  if (!projectDir || !hasGitChanges) return null

  return (
    <button
      type="button"
      onClick={() => onOpenCommitDialog(projectDir)}
      className="flex items-center gap-1.5 rounded-md border border-border bg-card/80 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent"
    >
      <GitCommitHorizontal className="size-3.5" />
      <span>Commit</span>
    </button>
  )
}
