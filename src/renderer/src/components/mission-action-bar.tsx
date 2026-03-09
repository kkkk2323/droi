import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, LoaderCircle, PauseCircle, SquareX } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { getMissionActionState, getMissionRuntimeStatus } from '@/lib/missionUiSemantics'
import { useActions, useActiveSessionId, useAppStore } from '@/store'

const EMPTY_MESSAGES: never[] = []

function getToneClasses(tone: 'default' | 'warning' | 'danger' | 'success'): string {
  if (tone === 'danger') return 'border-destructive/40 bg-destructive/5 text-destructive'
  if (tone === 'warning')
    return 'border-amber-500/40 bg-amber-500/5 text-amber-700 dark:text-amber-400'
  if (tone === 'success')
    return 'border-emerald-500/40 bg-emerald-500/5 text-emerald-700 dark:text-emerald-400'
  return 'border-border/70 bg-muted/30 text-foreground'
}

export function MissionActionBar() {
  const activeSessionId = useActiveSessionId()
  const sessionBuffer = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId) : undefined,
  )
  const mission = sessionBuffer?.mission
  const messages = sessionBuffer?.messages ?? EMPTY_MESSAGES
  const { handleCancel, handleKillWorker } = useActions()
  const [pendingAction, setPendingAction] = useState<'pause' | 'kill' | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const actionState = useMemo(() => getMissionActionState(mission), [mission])
  const runtimeStatus = useMemo(
    () => getMissionRuntimeStatus({ mission, messages, pendingAction }),
    [messages, mission, pendingAction],
  )

  useEffect(() => {
    if (!mission) {
      setPendingAction(null)
      setActionError(null)
      return
    }

    if (pendingAction === 'pause' && !actionState.canPause) {
      setPendingAction(null)
    }

    if (
      pendingAction === 'kill' &&
      (!actionState.canKillWorker ||
        runtimeStatus.kind === 'paused-after-user-kill' ||
        runtimeStatus.kind === 'daemon-failed' ||
        runtimeStatus.kind === 'paused-by-user' ||
        runtimeStatus.kind === 'ready-to-continue')
    ) {
      setPendingAction(null)
    }
  }, [actionState.canKillWorker, actionState.canPause, mission, pendingAction, runtimeStatus.kind])

  if (!mission) return null

  const handlePauseClick = () => {
    setActionError(null)
    setPendingAction('pause')
    handleCancel()
  }

  const handleKillClick = () => {
    if (!actionState.workerSessionId) return
    setActionError(null)
    setPendingAction('kill')
    void handleKillWorker(actionState.workerSessionId).catch((error) => {
      const message = error instanceof Error ? error.message : String(error)
      setActionError(message || 'Failed to kill worker session.')
      setPendingAction(null)
    })
  }

  return (
    <div className="border-t border-border/70 px-4 py-3" data-testid="mission-action-bar">
      <div className="mx-auto flex max-w-5xl flex-col gap-3 rounded-2xl border border-border/70 bg-card/80 p-3 shadow-sm">
        <div
          data-testid="mission-runtime-status"
          className={cn('rounded-xl border px-3 py-3', getToneClasses(runtimeStatus.tone))}
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <p className="text-sm font-semibold">{runtimeStatus.title}</p>
              <p className="text-sm text-muted-foreground">{runtimeStatus.description}</p>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {actionState.canPause ? (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  data-testid="mission-pause"
                  disabled={pendingAction === 'pause'}
                  onClick={handlePauseClick}
                >
                  {pendingAction === 'pause' ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <PauseCircle className="size-4" />
                  )}
                  Pause
                </Button>
              ) : null}

              {actionState.canKillWorker ? (
                <Button
                  type="button"
                  size="sm"
                  variant="destructive"
                  data-testid="mission-kill-worker"
                  disabled={pendingAction === 'kill'}
                  onClick={handleKillClick}
                >
                  {pendingAction === 'kill' ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <SquareX className="size-4" />
                  )}
                  Kill Worker
                </Button>
              ) : null}
            </div>
          </div>
        </div>

        {actionError ? (
          <div className="flex items-start gap-2 rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <span>{actionError}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}
