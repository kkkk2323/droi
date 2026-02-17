import React, { useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { GitCommitHorizontal, Upload, ChevronDown, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { getDroidClient } from '@/droidClient'
import { useProjects, useActiveSessionId } from '@/store'
import { useGitStatusQuery, useGitBranchQuery } from '@/hooks/useGitStatus'
import { CommitWizard } from '@/components/commit/CommitWizard'

interface GitActionsButtonProps {
  projectDir: string
  isRunning: boolean
}

export function GitActionsButton({ projectDir, isRunning }: GitActionsButtonProps) {
  const projects = useProjects()
  const activeSessionId = useActiveSessionId()
  const queryClient = useQueryClient()

  const { data: gitStatusFiles = [] } = useGitStatusQuery(projectDir, !isRunning)
  const hasGitChanges = gitStatusFiles.length > 0

  // Commit dialog
  const [commitOpen, setCommitOpen] = useState(false)

  // Push dialog
  const [pushOpen, setPushOpen] = useState(false)
  const [pushing, setPushing] = useState(false)
  const [pushError, setPushError] = useState('')
  const [pushResult, setPushResult] = useState('')

  // Dropdown
  const [dropdownOpen, setDropdownOpen] = useState(false)

  // Push branch resolution
  const { data: resolvedBranch = '' } = useGitBranchQuery(projectDir, pushOpen)
  const sessionBranch = useMemo(() => {
    for (const p of projects) {
      const s = p.sessions.find((x) => x.id === activeSessionId)
      if (s?.branch) return s.branch
    }
    return ''
  }, [projects, activeSessionId])
  const effectiveBranch = resolvedBranch || sessionBranch
  const pushDisabled = !projectDir || isRunning || !effectiveBranch

  const onPush = async () => {
    if (!projectDir || pushing) return
    setPushing(true)
    setPushError('')
    setPushResult('')
    try {
      const res = await getDroidClient().pushBranch({ projectDir })
      setPushResult(`Pushed ${res.branch} to ${res.remote}`)
      void queryClient.invalidateQueries({ queryKey: ['gitStatus', projectDir] })
    } catch (err) {
      setPushError(err instanceof Error ? err.message : String(err))
    } finally {
      setPushing(false)
    }
  }

  const buttonDisabled = !hasGitChanges

  if (!projectDir) return null

  return (
    <>
      {/* Split Button */}
      <div className={cn(
        'flex items-center rounded-md border border-border bg-card/80 overflow-hidden transition-colors hover:border-border/80',
        buttonDisabled && 'opacity-50'
      )}>
        <button
          type="button"
          onClick={() => setCommitOpen(true)}
          disabled={buttonDisabled}
          className={cn(
            'flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent',
            buttonDisabled && 'pointer-events-none'
          )}
        >
          <GitCommitHorizontal className="size-3.5" />
          <span>Commit</span>
        </button>

        <div className="w-px h-5 bg-border" />

        <DropdownMenu open={dropdownOpen} onOpenChange={setDropdownOpen}>
          <DropdownMenuTrigger
            render={<button type="button" />}
            className="flex items-center px-1.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent"
          >
            <ChevronDown className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="p-0 min-w-[108px]">
            <DropdownMenuItem
              onClick={() => { setDropdownOpen(false); setPushOpen(true) }}
              disabled={pushDisabled}
              render={(
                <button
                  type="button"
                  disabled={pushDisabled}
                  className={cn(
                    'cursor-pointer flex w-full items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent',
                    pushDisabled && 'pointer-events-none opacity-50'
                  )}
                >
                  <Upload className="size-3.5" />
                  <span>Push</span>
                </button>
              )}
            />
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Commit Wizard Dialog */}
      <CommitWizard
        open={commitOpen}
        onOpenChange={setCommitOpen}
        projectDir={projectDir}
      />

      {/* Push Dialog */}
      <AlertDialog open={pushOpen} onOpenChange={(v) => { setPushOpen(v); if (!v) { setPushError(''); setPushResult('') } }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Git Push</AlertDialogTitle>
            <AlertDialogDescription>
              Push this worktree branch to <span className="font-mono">origin</span> and set upstream.
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="space-y-2">
            <div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
              Branch: <span className="font-mono text-foreground">{effectiveBranch || 'unknown'}</span>
            </div>
            {pushError && <div className="text-xs text-red-500">{pushError}</div>}
            {pushResult && <div className="text-xs text-emerald-600">{pushResult}</div>}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel disabled={pushing}>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={pushing || !effectiveBranch} onClick={onPush}>
              {pushing ? (
                <>
                  <Loader2 className="mr-2 size-3.5 animate-spin" />
                  Pushing…
                </>
              ) : (
                'Push'
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
