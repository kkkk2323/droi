import React from 'react'
import {
  GitBranch,
  FileEdit,
  FilePlus,
  FileX,
  FileCode,
  ChevronDown,
  Check,
  X,
  Loader2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import type { GitStatusFile } from '@/types'
import type { StepState } from './CommitProgressView'

const STATUS_ICON_MAP: Record<string, { icon: typeof FileEdit; color: string; label: string }> = {
  M: { icon: FileEdit, color: 'text-amber-500', label: 'Modified' },
  A: { icon: FilePlus, color: 'text-emerald-500', label: 'Added' },
  D: { icon: FileX, color: 'text-red-500', label: 'Deleted' },
  '??': { icon: FileCode, color: 'text-muted-foreground', label: 'Untracked' },
}

function StepStatusIndicator({ steps }: { steps: StepState[] }) {
  if (steps.length === 0) return null
  const hasError = steps.some((s) => s.status === 'error')
  const hasRunning = steps.some((s) => s.status === 'running')
  const allDone = steps.every((s) => s.status === 'done')
  if (hasError) return <X className="size-3.5 text-destructive-foreground" />
  if (hasRunning) return <Loader2 className="size-3.5 animate-spin text-primary" />
  if (allDone) return <Check className="size-3.5 text-emerald-500" />
  return null
}

interface CommitReviewStepProps {
  branch: string
  filesToCommit: GitStatusFile[]
  unstagedCount: number
  includeUnstaged: boolean
  onIncludeUnstagedChange: (checked: boolean) => void
  disabled?: boolean
  executingSteps?: StepState[]
  locked?: boolean
}

export function CommitReviewStep({
  branch,
  filesToCommit,
  unstagedCount,
  includeUnstaged,
  onIncludeUnstagedChange,
  disabled,
  executingSteps = [],
  locked = false,
}: CommitReviewStepProps) {
  const [open, setOpen] = React.useState(true)
  const effectiveOpen = locked ? false : open

  return (
    <Collapsible open={effectiveOpen} onOpenChange={locked ? undefined : setOpen}>
      <CollapsibleTrigger
        render={<button type="button" />}
        className={cn(
          'flex w-full items-center justify-between rounded-md px-3 py-2 text-xs font-medium text-foreground/80 hover:bg-muted/50 transition-colors',
          locked && 'cursor-default hover:bg-transparent',
        )}
        disabled={locked}
      >
        <span className="flex items-center gap-2">
          <span className="flex size-5 items-center justify-center rounded-full bg-primary/10 text-primary text-[10px] font-bold">
            1
          </span>
          Changes
          {!effectiveOpen && (
            <span className="text-muted-foreground font-normal ml-1">
              {filesToCommit.length} file{filesToCommit.length !== 1 ? 's' : ''} on{' '}
              {branch || 'unknown'}
            </span>
          )}
        </span>
        <span className="flex items-center gap-1.5">
          {locked && <StepStatusIndicator steps={executingSteps} />}
          {!locked && (
            <ChevronDown
              className={cn(
                'size-3.5 text-muted-foreground transition-transform',
                effectiveOpen && 'rotate-180',
              )}
            />
          )}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="space-y-3 px-3 pb-2 overflow-hidden">
          {/* Branch */}
          <div className="flex items-center gap-2 rounded-md bg-muted/50 px-3 py-2">
            <GitBranch className="size-3.5 text-muted-foreground" />
            <span className="text-xs text-muted-foreground">Branch:</span>
            <span className="text-xs font-medium font-mono text-foreground">
              {branch || 'unknown'}
            </span>
          </div>

          {/* Changed files */}
          <div>
            <div className="mb-1.5 flex items-center justify-between">
              <span className="text-xs font-medium text-foreground/80">
                Changed files ({filesToCommit.length})
              </span>
            </div>
            <div className="max-h-40 overflow-y-auto overflow-x-hidden rounded-md border border-border">
              {filesToCommit.length === 0 ? (
                <div className="px-3 py-4 text-center text-xs text-muted-foreground">
                  No changes to commit
                </div>
              ) : (
                filesToCommit.map((file) => {
                  const info = STATUS_ICON_MAP[file.status] || STATUS_ICON_MAP['M']!
                  const Icon = info.icon
                  return (
                    <div
                      key={file.path}
                      className="flex items-center gap-2 px-3 py-1 text-xs text-muted-foreground hover:bg-muted/40 transition-colors min-w-0"
                    >
                      <Icon className={cn('size-3 shrink-0', info.color)} />
                      <span className="truncate font-mono flex-1 min-w-0">{file.path}</span>
                      <span className="shrink-0 tabular-nums">
                        {file.additions > 0 && (
                          <span className="text-emerald-500">+{file.additions}</span>
                        )}
                        {file.additions > 0 && file.deletions > 0 && (
                          <span className="text-muted-foreground/40"> </span>
                        )}
                        {file.deletions > 0 && (
                          <span className="text-red-500">-{file.deletions}</span>
                        )}
                      </span>
                    </div>
                  )
                })
              )}
            </div>
          </div>

          {/* Include unstaged */}
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={includeUnstaged}
              onChange={(e) => onIncludeUnstagedChange(e.target.checked)}
              className="size-3.5 rounded border-border accent-primary"
              disabled={disabled}
            />
            <span className="text-xs text-muted-foreground">
              Include unstaged / untracked files
              {unstagedCount > 0 && (
                <span className="text-muted-foreground/60"> ({unstagedCount})</span>
              )}
            </span>
          </label>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
