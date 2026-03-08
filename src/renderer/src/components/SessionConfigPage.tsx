import React from 'react'
import { GitBranch, FolderOpen, GitFork } from 'lucide-react'
import { useGitBranchQuery } from '@/hooks/useGitStatus'
import { SessionBootstrapCards } from '@/components/SessionBootstrapCards'
import { usePendingNewSession, useIsCreatingSession, useWorkspaceError, useActions } from '@/store'
import type { PendingNewSessionMode } from '@/store'
import { cn } from '@/lib/utils'

function ModeOption({
  selected,
  onSelect,
  icon,
  label,
  description,
  'data-testid': testId,
}: {
  selected: boolean
  onSelect: () => void
  icon: React.ReactNode
  label: string
  description: React.ReactNode
  'data-testid'?: string
}) {
  return (
    <button
      type="button"
      data-testid={testId}
      className={cn(
        'flex items-start gap-3 rounded-lg border px-3.5 py-3 text-left transition-all active:scale-[0.995] w-full',
        selected
          ? 'border-foreground/30 bg-accent/60'
          : 'border-border bg-background hover:bg-accent/40',
      )}
      onClick={onSelect}
    >
      <div
        className={cn(
          'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors',
          selected ? 'border-foreground bg-foreground' : 'border-muted-foreground/40',
        )}
      >
        {selected && <div className="size-1.5 rounded-full bg-background" />}
      </div>
      <div className="flex flex-1 items-start gap-2">
        <div className="mt-0.5 shrink-0 text-muted-foreground">{icon}</div>
        <div className="min-w-0">
          <div className="text-sm font-medium text-foreground">{label}</div>
          <div className="mt-0.5 text-xs text-muted-foreground">{description}</div>
        </div>
      </div>
    </button>
  )
}

function BranchLabel({ branch, loading }: { branch: string; loading: boolean }) {
  if (loading) {
    return (
      <span className="inline-block h-3.5 w-16 animate-pulse rounded bg-muted-foreground/15 align-middle" />
    )
  }
  return <span className="font-mono">{branch}</span>
}

export function SessionConfigPage() {
  const pending = usePendingNewSession()
  const isCreatingSession = useIsCreatingSession()
  const workspaceError = useWorkspaceError()
  const { updatePendingNewSession } = useActions()

  const repoRoot = String(pending?.repoRoot || '').trim()

  const { data: currentBranch, isLoading: loadingBranch } = useGitBranchQuery(
    repoRoot,
    Boolean(repoRoot),
  )
  const branchDisplay = String(currentBranch || '').trim() || 'unknown'

  if (!pending) return null

  const repoName = repoRoot.split('/').pop() || repoRoot
  const mode: PendingNewSessionMode = pending.mode || 'local'

  const setMode = (m: PendingNewSessionMode) => {
    updatePendingNewSession({ mode: m, branch: '', isExistingBranch: false })
  }

  return (
    <div className="flex flex-1 items-center justify-center px-6">
      <div className="w-full max-w-md space-y-5">
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-muted">
            <GitBranch className="size-7 text-muted-foreground" />
          </div>
          <div>
            <h2 className="text-base font-medium text-foreground">New Session</h2>
            <p className="mt-1 text-sm text-muted-foreground">Send your first message to start</p>
          </div>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground/70">
            <FolderOpen className="size-3" />
            <span className="font-mono">{repoName}</span>
          </span>
        </div>

        <div className="space-y-2">
          <ModeOption
            data-testid="session-mode-local"
            selected={mode === 'local'}
            onSelect={() => setMode('local')}
            icon={<FolderOpen className="size-3.5" />}
            label="Work from Local"
            description={
              <span>
                Use current directory and branch (
                <BranchLabel branch={branchDisplay} loading={loadingBranch} />)
              </span>
            }
          />
          <ModeOption
            data-testid="session-mode-new-worktree"
            selected={mode === 'new-worktree'}
            onSelect={() => setMode('new-worktree')}
            icon={<GitFork className="size-3.5" />}
            label="New WorkTree"
            description="Create an isolated worktree with a new branch"
          />
        </div>

        {isCreatingSession && <SessionBootstrapCards workspacePrepStatus="running" />}

        {workspaceError && (
          <div className="text-sm text-center text-destructive-foreground">{workspaceError}</div>
        )}
      </div>
    </div>
  )
}
