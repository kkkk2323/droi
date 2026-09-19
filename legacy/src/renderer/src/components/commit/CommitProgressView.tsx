import React from 'react'
import { Check, X, Loader2, Circle } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { WorkflowStepName, WorkflowStepStatus } from '@/types'

export interface StepState {
  step: WorkflowStepName
  label: string
  status: WorkflowStepStatus
  detail?: string
}

interface CommitProgressViewProps {
  steps: StepState[]
  error?: string
}

const STATUS_ICON: Record<WorkflowStepStatus, React.ReactNode> = {
  pending: <Circle className="size-4 text-muted-foreground/40" />,
  running: <Loader2 className="size-4 animate-spin text-primary" />,
  done: <Check className="size-4 text-emerald-500" />,
  error: <X className="size-4 text-destructive-foreground" />,
}

const STEP_LABELS: Record<WorkflowStepName, string> = {
  stage: 'Stage files',
  commit: 'Create commit',
  merge: 'Merge branch',
  push: 'Push to origin',
  create_pr: 'Create PR',
}

export function getStepLabel(step: WorkflowStepName): string {
  return STEP_LABELS[step] || step
}

export function CommitProgressView({ steps, error }: CommitProgressViewProps) {
  return (
    <div className="space-y-1 py-2">
      {steps.map((s) => (
        <div
          key={s.step}
          className={cn(
            'flex items-center gap-3 rounded-md px-3 py-2 text-xs transition-colors',
            s.status === 'running' && 'bg-primary/5',
            s.status === 'error' && 'bg-destructive/5',
          )}
        >
          <span className="shrink-0">{STATUS_ICON[s.status]}</span>
          <span
            className={cn(
              'flex-1 font-medium',
              s.status === 'pending' && 'text-muted-foreground/50',
              s.status === 'running' && 'text-foreground',
              s.status === 'done' && 'text-foreground/70',
              s.status === 'error' && 'text-destructive-foreground',
            )}
          >
            {s.label}
          </span>
          {s.detail && s.status === 'done' && (
            <span className="shrink-0 font-mono text-muted-foreground text-[11px] max-w-[200px] truncate">
              {s.detail.length > 12 ? s.detail.slice(0, 12) + '…' : s.detail}
            </span>
          )}
        </div>
      ))}
      {error && (
        <div className="mt-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive-foreground">
          {error}
        </div>
      )}
    </div>
  )
}
