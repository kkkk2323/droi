import { useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  LoaderCircle,
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

function HandoffCard({ handoff }: { handoff: ReturnType<typeof getMissionHandoffCards>[number] }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <article
      key={handoff.key}
      data-testid={handoff.testId}
      className="rounded-lg border border-border/70 bg-background/60 px-3 py-3"
    >
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex min-w-0 items-center gap-2">
          {expanded ? (
            <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate text-sm font-medium text-foreground">{handoff.title}</span>
        </div>
        <Badge variant={getSuccessBadgeVariant(handoff.successState)}>{handoff.successState}</Badge>
      </button>

      {expanded && (
        <div className="mt-3 space-y-3 border-t border-border/50 pt-3 text-sm">
          <div>
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Summary
            </div>
            <p className="mt-1 text-foreground">{handoff.salientSummary}</p>
          </div>

          <div>
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              What was implemented
            </div>
            <p className="mt-1 text-foreground">{handoff.whatWasImplemented}</p>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <div>
              <div className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                <TerminalSquare className="size-3" />
                Commands
              </div>
              <ul className="mt-1.5 space-y-1 text-muted-foreground">
                {handoff.commandResults.length === 0 ? (
                  <li>None recorded.</li>
                ) : (
                  handoff.commandResults.map((item) => <li key={item}>{item}</li>)
                )}
              </ul>
            </div>
            <div>
              <div className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                <CheckCircle2 className="size-3" />
                Checks
              </div>
              <ul className="mt-1.5 space-y-1 text-muted-foreground">
                {handoff.interactiveResults.length === 0 ? (
                  <li>None recorded.</li>
                ) : (
                  handoff.interactiveResults.map((item) => <li key={item}>{item}</li>)
                )}
              </ul>
            </div>
          </div>

          <div className="text-xs text-muted-foreground/60">
            <span className="font-mono">{handoff.featureId}</span>
          </div>
        </div>
      )}
    </article>
  )
}

export function MissionControlPanel({ mission }: { mission?: MissionState | null }) {
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
      className="flex min-w-0 flex-1 flex-col overflow-y-auto overflow-x-hidden"
    >
      {/* Status header */}
      <div className="sticky top-0 z-10 border-b border-border bg-background/95 px-4 py-3 backdrop-blur">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-3">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1">
            <Badge data-testid="mission-status" variant={getStateBadgeVariant(status.stateLabel)}>
              {status.stateLabel}
            </Badge>
            <span className="text-sm font-medium text-foreground">{status.progressLabel}</span>
            <span className="text-muted-foreground/40">|</span>
            <span className="min-w-0 truncate text-sm text-muted-foreground">
              {status.currentFeatureLabel}
            </span>
            {status.phaseLabel !== 'Implementation in progress' && (
              <>
                <span className="text-muted-foreground/40">|</span>
                <span className="text-xs text-muted-foreground">{status.phaseLabel}</span>
              </>
            )}
          </div>

          <div data-testid="mission-action-bar" className="flex shrink-0 items-center gap-2">
            {actionState.canPause && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                data-testid="mission-pause"
                disabled={pendingAction === 'pause'}
                onClick={handlePauseClick}
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
        </div>
      </div>

      {/* Runtime status banner - only when non-default tone */}
      {runtimeStatus.tone !== 'default' && (
        <div
          data-testid="mission-runtime-status"
          className={cn('border-b px-4 py-2.5', getToneClasses(runtimeStatus.tone))}
        >
          <div className="mx-auto max-w-5xl">
            <p className="text-sm font-medium">{runtimeStatus.title}</p>
            <p className="text-xs opacity-80">{runtimeStatus.description}</p>
          </div>
        </div>
      )}

      {actionError && (
        <div className="border-b border-destructive/30 bg-destructive/5 px-4 py-2">
          <div className="mx-auto flex max-w-5xl items-start gap-2 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            <span>{actionError}</span>
          </div>
        </div>
      )}

      {/* Main content */}
      <div className="min-w-0 px-4 py-5">
        <div className="mx-auto min-w-0 max-w-5xl space-y-8">
          {/* Feature Queue */}
          <section>
            <h3 className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Feature Queue
            </h3>
            <div data-testid="mission-feature-queue" className="space-y-1">
              {featureQueue.length === 0 ? (
                <p className="py-4 text-sm text-muted-foreground/60">No features queued yet.</p>
              ) : (
                featureQueue.map((feature) => (
                  <div
                    key={feature.id}
                    data-testid={feature.testId}
                    className={cn(
                      'flex min-w-0 items-center gap-3 rounded-lg px-3 py-2 transition-colors',
                      feature.isCurrent
                        ? 'bg-primary/5 ring-1 ring-primary/20'
                        : 'hover:bg-muted/30',
                    )}
                  >
                    <span
                      className={cn(
                        'size-2 shrink-0 rounded-full',
                        getFeatureStatusDot(feature.status, feature.isCurrent),
                      )}
                    />
                    <span
                      className="min-w-0 flex-1 truncate text-sm text-foreground"
                      title={`${feature.description} (${feature.id})`}
                    >
                      {feature.description}
                    </span>
                    <div className="flex shrink-0 items-center gap-1.5">
                      {feature.isValidator && (
                        <Badge
                          variant="outline"
                          className="gap-0.5 border-violet-500/40 bg-violet-500/5 text-[10px]"
                        >
                          <ShieldCheck className="size-3" />
                          Validator
                        </Badge>
                      )}
                      <span
                        className={cn(
                          'text-xs',
                          feature.isCurrent ? 'font-medium text-primary' : 'text-muted-foreground',
                        )}
                      >
                        {feature.statusLabel}
                      </span>
                    </div>
                  </div>
                ))
              )}
            </div>
          </section>

          {/* Progress Timeline */}
          <section>
            <h3 className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Timeline
            </h3>
            <div data-testid="mission-progress-timeline" className="space-y-px">
              {timeline.length === 0 ? (
                <p className="py-4 text-sm text-muted-foreground/60">No events recorded yet.</p>
              ) : (
                timeline.map((entry, index) => (
                  <div
                    key={`${entry.timestampLabel}-${entry.eventLabel}-${index}`}
                    className="flex min-w-0 items-baseline gap-3 rounded-lg px-3 py-1.5 hover:bg-muted/20"
                  >
                    <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground/60">
                      {entry.timestampLabel}
                    </span>
                    <span className="shrink-0 text-sm font-medium text-foreground">
                      {entry.eventLabel}
                    </span>
                    {entry.detailLabel && (
                      <span className="min-w-0 truncate text-xs text-muted-foreground">
                        {entry.detailLabel}
                      </span>
                    )}
                  </div>
                ))
              )}
            </div>
          </section>

          {/* Worker Handoffs - only show when there are handoffs */}
          {handoffs.length > 0 && (
            <section>
              <h3 className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Handoffs
              </h3>
              <div className="space-y-2">
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
