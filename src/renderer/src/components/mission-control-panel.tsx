import { useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  LoaderCircle,
  MessagesSquare,
  PauseCircle,
  ShieldCheck,
  SquareX,
  TerminalSquare,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  getMissionControlStatus,
  getMissionFeatureQueueItems,
  getMissionHandoffCards,
  getMissionProgressTimelineItems,
} from '@/lib/missionControl'
import { getMissionActionState, getMissionRuntimeStatus } from '@/lib/missionUiSemantics'
import type { MissionState } from '@/state/missionState'
import type { MissionViewMode } from '@/lib/missionPage'
import { useActions, useActiveSessionId, useAppStore } from '@/store'

const EMPTY_MESSAGES: never[] = []

function getStateBadgeVariant(
  stateLabel: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  const normalized = stateLabel.toLowerCase()
  if (normalized === 'completed') return 'default'
  if (normalized === 'running') return 'secondary'
  if (normalized === 'validation pending') return 'secondary'
  if (normalized === 'paused') return 'destructive'
  return 'outline'
}

function getFeatureStatusDot(status: string, isCurrent: boolean): string {
  if (isCurrent) return 'bg-primary'
  const normalized = status.toLowerCase()
  if (normalized === 'completed') return 'bg-emerald-500'
  if (normalized === 'cancelled') return 'bg-destructive'
  if (normalized === 'in_progress' || normalized === 'running') return 'bg-sky-500'
  return 'bg-muted-foreground/40'
}

function getSuccessBadgeVariant(
  value: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  const normalized = value.toLowerCase()
  if (normalized === 'success') return 'default'
  if (normalized === 'partial') return 'secondary'
  if (normalized === 'failure') return 'destructive'
  return 'outline'
}

function getToneClasses(tone: 'default' | 'warning' | 'danger' | 'success'): string {
  if (tone === 'danger') return 'border-destructive/40 bg-destructive/5 text-destructive'
  if (tone === 'warning')
    return 'border-amber-500/40 bg-amber-500/5 text-amber-700 dark:text-amber-400'
  if (tone === 'success')
    return 'border-emerald-500/40 bg-emerald-500/5 text-emerald-700 dark:text-emerald-400'
  return 'border-border/70 bg-muted/20 text-muted-foreground'
}

function formatTimeOnly(timestampLabel: string): string {
  const match = timestampLabel.match(/(\d{1,2}:\d{2}:\d{2})\s*(AM|PM)?/i)
  if (!match) return timestampLabel
  return match[2] ? `${match[1]} ${match[2]}` : match[1]
}

function HandoffCard({ handoff }: { handoff: ReturnType<typeof getMissionHandoffCards>[number] }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <article data-testid={handoff.testId} className="rounded-lg border border-border/50 px-3 py-2">
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex min-w-0 items-center gap-2">
          {expanded ? (
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-3 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate text-sm text-foreground">{handoff.title}</span>
        </div>
        <Badge variant={getSuccessBadgeVariant(handoff.successState)}>{handoff.successState}</Badge>
      </button>

      {expanded && (
        <div className="mt-2 space-y-2 border-t border-border/30 pt-2 text-sm text-muted-foreground">
          <p className="text-foreground">{handoff.salientSummary}</p>
          <p>{handoff.whatWasImplemented}</p>

          {(handoff.commandResults.length > 0 || handoff.interactiveResults.length > 0) && (
            <div className="grid gap-3 text-xs md:grid-cols-2">
              {handoff.commandResults.length > 0 && (
                <div>
                  <div className="flex items-center gap-1 text-muted-foreground/60">
                    <TerminalSquare className="size-3" />
                    Commands
                  </div>
                  <ul className="mt-1 space-y-0.5">
                    {handoff.commandResults.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                </div>
              )}
              {handoff.interactiveResults.length > 0 && (
                <div>
                  <div className="flex items-center gap-1 text-muted-foreground/60">
                    <CheckCircle2 className="size-3" />
                    Checks
                  </div>
                  <ul className="mt-1 space-y-0.5">
                    {handoff.interactiveResults.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </article>
  )
}

export function MissionControlPanel({
  mission,
  onViewChange,
}: {
  mission?: MissionState | null
  onViewChange?: (view: MissionViewMode) => void
}) {
  const activeSessionId = useActiveSessionId()
  const messages = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.messages || [] : EMPTY_MESSAGES,
  )
  const { handleCancel, handleKillWorker } = useActions()
  const [pendingAction, setPendingAction] = useState<'pause' | 'kill' | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const status = getMissionControlStatus(mission)
  const featureQueue = getMissionFeatureQueueItems(mission)
  const timeline = getMissionProgressTimelineItems(mission)
  const handoffs = getMissionHandoffCards(mission)
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
    if (pendingAction === 'pause' && !actionState.canPause) setPendingAction(null)
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
    <div
      data-testid="mission-control-view"
      className="flex min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto"
    >
      {/* Unified bar: status + actions + view toggle */}
      <div className="sticky top-0 z-10 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
        <div className="mx-auto flex max-w-5xl items-center gap-3">
          <Badge data-testid="mission-status" variant={getStateBadgeVariant(status.stateLabel)}>
            {status.stateLabel}
          </Badge>
          <span className="text-sm text-muted-foreground">{status.progressLabel}</span>

          <div className="ml-auto flex shrink-0 items-center gap-2">
            <div data-testid="mission-action-bar" className="flex items-center gap-1.5">
              {actionState.canPause && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  data-testid="mission-pause"
                  disabled={pendingAction === 'pause'}
                  onClick={handlePauseClick}
                  className="h-7 px-2 text-xs"
                >
                  {pendingAction === 'pause' ? (
                    <LoaderCircle className="size-3.5 animate-spin" />
                  ) : (
                    <PauseCircle className="size-3.5" />
                  )}
                  Pause
                </Button>
              )}
              {actionState.canKillWorker && (
                <Button
                  type="button"
                  size="sm"
                  variant="destructive"
                  data-testid="mission-kill-worker"
                  disabled={pendingAction === 'kill'}
                  onClick={handleKillClick}
                  className="h-7 px-2 text-xs"
                >
                  {pendingAction === 'kill' ? (
                    <LoaderCircle className="size-3.5 animate-spin" />
                  ) : (
                    <SquareX className="size-3.5" />
                  )}
                  Kill Worker
                </Button>
              )}
            </div>

            {onViewChange && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                data-testid="mission-view-toggle"
                onClick={() => onViewChange('chat')}
                className="h-7 px-2 text-xs text-muted-foreground"
              >
                <MessagesSquare className="size-3.5" />
                Chat
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Alert banner */}
      {runtimeStatus.tone !== 'default' && (
        <div
          data-testid="mission-runtime-status"
          className={cn('border-b px-4 py-2', getToneClasses(runtimeStatus.tone))}
        >
          <div className="mx-auto max-w-5xl text-sm">
            <span className="font-medium">{runtimeStatus.title}</span>
            <span className="ml-2 text-xs opacity-70">{runtimeStatus.description}</span>
          </div>
        </div>
      )}

      {actionError && (
        <div className="border-b border-destructive/30 bg-destructive/5 px-4 py-2">
          <div className="mx-auto flex max-w-5xl items-center gap-2 text-sm text-destructive">
            <AlertTriangle className="size-3.5 shrink-0" />
            <span>{actionError}</span>
          </div>
        </div>
      )}

      {/* Content */}
      <div className="min-w-0 px-4 py-5">
        <div className="mx-auto min-w-0 max-w-5xl space-y-6">
          {/* Feature Queue */}
          <section>
            <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Features
            </h3>
            <div data-testid="mission-feature-queue" className="space-y-0.5">
              {featureQueue.length === 0 ? (
                <p className="py-3 text-sm text-muted-foreground/60">No features queued yet.</p>
              ) : (
                featureQueue.map((feature) => (
                  <div
                    key={feature.id}
                    data-testid={feature.testId}
                    className={cn(
                      'flex min-w-0 items-center gap-2.5 rounded-md px-2.5 py-1.5',
                      feature.isCurrent
                        ? 'bg-primary/5 ring-1 ring-primary/20'
                        : 'hover:bg-muted/30',
                    )}
                  >
                    <span
                      className={cn(
                        'size-1.5 shrink-0 rounded-full',
                        getFeatureStatusDot(feature.status, feature.isCurrent),
                      )}
                    />
                    <span
                      className="min-w-0 flex-1 truncate text-sm text-foreground"
                      title={feature.description}
                    >
                      {feature.description}
                    </span>
                    {feature.isValidator && (
                      <ShieldCheck className="size-3 shrink-0 text-violet-500" />
                    )}
                    <span
                      className={cn(
                        'shrink-0 text-xs',
                        feature.isCurrent ? 'font-medium text-primary' : 'text-muted-foreground',
                      )}
                    >
                      {feature.statusLabel}
                    </span>
                  </div>
                ))
              )}
            </div>
          </section>

          {/* Timeline */}
          {timeline.length > 0 && (
            <section>
              <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Timeline
              </h3>
              <div data-testid="mission-progress-timeline" className="space-y-px">
                {timeline.map((entry, index) => (
                  <div
                    key={`${entry.timestampLabel}-${entry.eventLabel}-${index}`}
                    className="flex min-w-0 items-baseline gap-3 px-2.5 py-1"
                  >
                    <span className="shrink-0 font-mono text-[10px] text-muted-foreground/50">
                      {formatTimeOnly(entry.timestampLabel)}
                    </span>
                    <span className="shrink-0 text-sm text-foreground">{entry.eventLabel}</span>
                    {entry.detailLabel && (
                      <span className="min-w-0 truncate text-xs text-muted-foreground/60">
                        {entry.detailLabel}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* Handoffs */}
          {handoffs.length > 0 && (
            <section>
              <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Handoffs
              </h3>
              <div className="space-y-1.5">
                {handoffs.map((handoff) => (
                  <HandoffCard key={handoff.key} handoff={handoff} />
                ))}
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
