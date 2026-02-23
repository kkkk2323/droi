import React from 'react'
import { ChevronDown, Check, X, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import type { StepState } from './CommitProgressView'

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

interface CommitMessageStepProps {
  commitMessage: string
  onCommitMessageChange: (msg: string) => void
  onGenerate: () => void
  generating: boolean
  disabled?: boolean
  hasFiles: boolean
  executingSteps?: StepState[]
  locked?: boolean
}

export function CommitMessageStep({
  commitMessage,
  onCommitMessageChange,
  onGenerate,
  generating,
  disabled,
  hasFiles,
  executingSteps = [],
  locked = false,
}: CommitMessageStepProps) {
  const [open, setOpen] = React.useState(false)
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
            2
          </span>
          Commit Message
          {!effectiveOpen && commitMessage.trim() && (
            <span className="text-muted-foreground font-normal ml-1 truncate max-w-[200px]">
              {commitMessage.trim().split('\n')[0]}
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
        <div className="space-y-2 px-3 pb-2">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium text-foreground/80">Message</span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 px-2 text-xs"
              disabled={disabled || generating || !hasFiles}
              onClick={onGenerate}
            >
              {generating ? 'Generating…' : 'Generate'}
            </Button>
          </div>
          <Textarea
            value={commitMessage}
            onChange={(e) => onCommitMessageChange(e.target.value)}
            placeholder="Leave empty to auto-generate on commit"
            className="min-h-20 text-xs font-mono"
            disabled={disabled || generating}
          />
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
