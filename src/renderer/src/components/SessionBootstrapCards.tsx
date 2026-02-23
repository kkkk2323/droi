import { useEffect, useState } from 'react'
import { AlertTriangle, Check, ChevronDown, Circle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { SessionSetupState } from '@/state/appReducer'

export function SessionBootstrapCards({
  workspacePrepStatus,
  setupScript,
  suppressSetupScriptRunningSpinner,
  onRetrySetupScript,
  onSkipSetupScript,
}: {
  workspacePrepStatus?: 'running' | 'completed' | null
  setupScript?: SessionSetupState | null
  suppressSetupScriptRunningSpinner?: boolean
  onRetrySetupScript?: () => void
  onSkipSetupScript?: () => void
}) {
  const showSetupCard = Boolean(setupScript && setupScript.status !== 'idle')
  const showWorkspacePrepCard = Boolean(workspacePrepStatus)
  if (!showSetupCard && !showWorkspacePrepCard) return null

  return (
    <div className="space-y-5">
      {showWorkspacePrepCard && workspacePrepStatus && (
        <PreparingWorkspaceCard status={workspacePrepStatus} />
      )}

      {showSetupCard && setupScript && (
        <SetupScriptCard
          setupScript={setupScript}
          suppressRunningSpinner={Boolean(suppressSetupScriptRunningSpinner)}
          onRetry={onRetrySetupScript}
          onSkip={onSkipSetupScript}
        />
      )}
    </div>
  )
}

function PreparingWorkspaceCard({
  status,
}: {
  status: 'running' | 'completed'
}) {
  return (
    <div className="mx-auto rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
      <div className="flex items-center gap-2 text-left text-sm font-medium">
        <span className="flex-1">Preparing workspace...</span>

        {status === 'running'
          ? <Loader2 className="size-4 animate-spin text-muted-foreground" />
          : <Check className="size-4 text-emerald-600" />}
      </div>
    </div>
  )
}

function SetupScriptCard({
  setupScript,
  suppressRunningSpinner,
  onRetry,
  onSkip,
}: {
  setupScript: SessionSetupState
  suppressRunningSpinner?: boolean
  onRetry?: () => void
  onSkip?: () => void
}) {
  const output = String(setupScript.output || '')
  const isDone = setupScript.status === 'completed' || setupScript.status === 'skipped'
  const [expanded, setExpanded] = useState(!isDone)

  useEffect(() => {
    if (isDone) setExpanded(false)
    else setExpanded(true)
  }, [isDone])

  return (
    <div className="mx-auto rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left text-sm font-medium"
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="flex-1">
          {setupScript.status === 'running' && 'Running setup script...'}
          {setupScript.status === 'failed' && 'Setup script failed'}
          {setupScript.status === 'completed' && 'Setup script completed'}
          {setupScript.status === 'skipped' && 'Setup script skipped'}
        </span>

        {setupScript.status === 'running' && !suppressRunningSpinner && (
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        )}
        {setupScript.status === 'failed' && <AlertTriangle className="size-4 text-amber-500" />}
        {setupScript.status === 'completed' && <Check className="size-4 text-emerald-600" />}
        {setupScript.status === 'skipped' && <Circle className="size-4 text-muted-foreground" />}

        <ChevronDown className={cn(
          'size-3.5 shrink-0 text-muted-foreground transition-transform duration-300',
          expanded ? 'rotate-180' : 'rotate-0',
        )} />
      </button>

      <div className={cn(
        'grid transition-all duration-300 ease-out',
        expanded ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
      )}>
        <div className="overflow-hidden">
          {setupScript.script && (
            <p className="mt-2 text-xs text-muted-foreground">
              <span className="font-mono">{setupScript.script}</span>
            </p>
          )}

          {setupScript.status === 'failed' && setupScript.error && (
            <p className="mt-2 text-xs text-destructive-foreground">{setupScript.error}</p>
          )}

          {output.trim() && (
            <pre className="mt-2 max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-zinc-950 px-3 py-2 text-[11px] leading-5 text-zinc-200">
              {output}
            </pre>
          )}

          {setupScript.status === 'failed' && (
            <div className="mt-3 flex items-center gap-2">
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); onRetry?.() }}
                className="rounded-md bg-foreground px-2.5 py-1 text-xs text-background transition-all hover:bg-foreground/80 active:scale-[0.98]"
              >
                Retry
              </button>
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); onSkip?.() }}
                className="rounded-md border border-border px-2.5 py-1 text-xs text-foreground transition-colors hover:bg-accent"
              >
                Skip
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
