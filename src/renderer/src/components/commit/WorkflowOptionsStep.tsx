import React from 'react'
import { ChevronDown, GitMerge, Check, X, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { CommitWorkflow, GitToolsInfo } from '@/types'
import type { StepState } from './CommitProgressView'

interface WorkflowOptionsStepProps {
  commitWorkflow: CommitWorkflow
  onWorkflowChange: (wf: CommitWorkflow) => void
  gitTools: GitToolsInfo
  prBaseBranch: string
  onPrBaseBranchChange: (branch: string) => void
  mergeEnabled: boolean
  onMergeEnabledChange: (enabled: boolean) => void
  mergeBranch: string
  onMergeBranchChange: (branch: string) => void
  localBranches: string[]
  disabled?: boolean
  executingSteps?: StepState[]
  locked?: boolean
}

const WORKFLOW_LABELS: Record<CommitWorkflow, string> = {
  commit: 'Commit',
  commit_push: 'Commit & Push',
  commit_push_pr: 'Commit, Push & Create PR',
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

export function WorkflowOptionsStep({
  commitWorkflow,
  onWorkflowChange,
  gitTools,
  prBaseBranch,
  onPrBaseBranchChange,
  mergeEnabled,
  onMergeEnabledChange,
  mergeBranch,
  onMergeBranchChange,
  localBranches,
  disabled,
  executingSteps = [],
  locked = false,
}: WorkflowOptionsStepProps) {
  const [open, setOpen] = React.useState(true)
  const effectiveOpen = locked ? false : open
  const requiresPrBaseBranch = commitWorkflow === 'commit_push_pr'

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
            3
          </span>
          Workflow
          {!effectiveOpen && (
            <span className="text-muted-foreground font-normal ml-1">
              {WORKFLOW_LABELS[commitWorkflow]}
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
        <div className="space-y-3 px-3 pb-2">
          {/* Workflow select */}
          <div className="space-y-2">
            <span className="text-xs font-medium text-foreground/80">Action</span>
            <Select
              value={commitWorkflow}
              onValueChange={(v) => v && onWorkflowChange(v as CommitWorkflow)}
              disabled={disabled}
            >
              <SelectTrigger className="w-full text-xs">
                <SelectValue placeholder="Select workflow..." />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="commit" className="text-xs">
                  Commit
                </SelectItem>
                <SelectItem value="commit_push" className="text-xs">
                  Commit and Push
                </SelectItem>
                <SelectItem value="commit_push_pr" className="text-xs" disabled={!gitTools.prTool}>
                  Commit and Push and Create PR{gitTools.prTool ? ` (${gitTools.prTool})` : ''}
                </SelectItem>
              </SelectContent>
            </Select>
            {!gitTools.prTool && (
              <div className="text-[11px] text-muted-foreground">
                {gitTools.prDisabledReason ||
                  'Install `gh` (GitHub) or `flow` (Yunxiao) to enable PR creation.'}
              </div>
            )}
          </div>

          {/* PR target branch */}
          {requiresPrBaseBranch && (
            <div className="space-y-2">
              <span className="text-xs font-medium text-foreground/80">PR target branch</span>
              <Select
                value={prBaseBranch}
                onValueChange={(v) => onPrBaseBranchChange(v || '')}
                disabled={disabled}
              >
                <SelectTrigger className="w-full text-xs font-mono">
                  <SelectValue placeholder="Select target branch..." />
                </SelectTrigger>
                <SelectContent>
                  {localBranches.map((b) => (
                    <SelectItem key={b} value={b} className="text-xs font-mono">
                      {b}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Merge to branch */}
          <div className="space-y-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={mergeEnabled}
                onChange={(e) => {
                  onMergeEnabledChange(e.target.checked)
                  if (!e.target.checked) onMergeBranchChange('')
                }}
                className="size-3.5 rounded border-border accent-primary"
                disabled={disabled}
              />
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <GitMerge className="size-3.5" />
                Merge to local branch after commit
              </span>
            </label>
            {mergeEnabled && (
              <Select
                value={mergeBranch}
                onValueChange={(v) => onMergeBranchChange(v || '')}
                disabled={disabled}
              >
                <SelectTrigger className="w-full text-xs font-mono">
                  <SelectValue placeholder="Select a branch..." />
                </SelectTrigger>
                <SelectContent>
                  {localBranches.map((b) => (
                    <SelectItem key={b} value={b} className="text-xs font-mono">
                      {b}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
