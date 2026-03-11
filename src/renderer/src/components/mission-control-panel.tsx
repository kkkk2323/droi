import { useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  ArrowLeft,
  Check,
  CheckCircle2,
  ChevronRight,
  Circle,
  LoaderCircle,
  MessagesSquare,
  PauseCircle,
  ShieldCheck,
  SquareX,
  TerminalSquare,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
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

function getHandoffIcon(successState: string) {
  const s = successState.toLowerCase()
  if (s === 'success') return <Check className="size-3 shrink-0 text-emerald-600" />
  if (s === 'failure') return <SquareX className="size-3 shrink-0 text-destructive" />
  return <Circle className="size-3 shrink-0 text-muted-foreground" />
}

type HandoffData = ReturnType<typeof getMissionHandoffCards>[number]

function HandoffRow({
  handoff,
  onSelect,
}: {
  handoff: HandoffData
  onSelect: (h: HandoffData) => void
}) {
  return (
    <div data-testid={handoff.testId} className="py-0.5">
      <button
        type="button"
        className={cn(
          'flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors',
          'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
        )}
        onClick={() => onSelect(handoff)}
      >
        {getHandoffIcon(handoff.successState)}
        <span className="font-medium text-foreground">{handoff.title}</span>
        <span className="truncate font-mono opacity-40">
          {handoff.salientSummary.slice(0, 60)}
          {handoff.salientSummary.length > 60 ? '...' : ''}
        </span>
        <Badge className="ml-auto shrink-0" variant={getSuccessBadgeVariant(handoff.successState)}>
          {handoff.successState}
        </Badge>
        <ChevronRight className="size-3 shrink-0 opacity-40" />
      </button>
    </div>
  )
}

function HandoffDetailView({ handoff, onBack }: { handoff: HandoffData; onBack: () => void }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
        <div className="mx-auto flex max-w-5xl items-center gap-3">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onBack}
            className="h-7 gap-1 px-2 text-xs text-muted-foreground"
          >
            <ArrowLeft className="size-3.5" />
            Back
          </Button>
          <span className="text-sm font-medium text-foreground">{handoff.title}</span>
          <Badge className="ml-auto" variant={getSuccessBadgeVariant(handoff.successState)}>
            {handoff.successState}
          </Badge>
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto max-w-5xl space-y-5 px-4 py-5">
          <section>
            <h4 className="mb-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Summary
            </h4>
            <p className="text-sm text-foreground">{handoff.salientSummary}</p>
          </section>

          <section>
            <h4 className="mb-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Implementation
            </h4>
            <p className="text-sm text-muted-foreground">{handoff.whatWasImplemented}</p>
          </section>

          {handoff.commandResults.length > 0 && (
            <section>
              <h4 className="mb-1.5 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                <TerminalSquare className="size-3" />
                Commands
              </h4>
              <ul className="space-y-1 text-sm text-foreground">
                {handoff.commandResults.map((item) => (
                  <li key={item} className="rounded-md bg-muted/40 px-3 py-1.5 font-mono text-xs">
                    {item}
                  </li>
                ))}
              </ul>
            </section>
          )}

          {handoff.interactiveResults.length > 0 && (
            <section>
              <h4 className="mb-1.5 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                <CheckCircle2 className="size-3" />
                Checks
              </h4>
              <ul className="space-y-1 text-sm text-foreground">
                {handoff.interactiveResults.map((item) => (
                  <li key={item} className="rounded-md bg-muted/40 px-3 py-1.5 text-xs">
                    {item}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      </ScrollArea>
    </div>
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
  const [activeHandoff, setActiveHandoff] = useState<HandoffData | null>(null)

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

  const hasTimeline = timeline.length > 0
  const hasHandoffs = handoffs.length > 0

  console.log(hasHandoffs)

  if (activeHandoff) {
    return (
      <div
        data-testid="mission-control-view"
        className="flex min-w-0 flex-1 flex-col overflow-hidden"
      >
        <HandoffDetailView handoff={activeHandoff} onBack={() => setActiveHandoff(null)} />
      </div>
    )
  }

  return (
    <div
      data-testid="mission-control-view"
      className="flex min-w-0 flex-1 flex-col overflow-hidden"
    >
      {/* Header bar: status + actions + view toggle */}
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
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
          className={cn('shrink-0 border-b px-4 py-2', getToneClasses(runtimeStatus.tone))}
        >
          <div className="mx-auto max-w-5xl text-sm">
            <span className="font-medium">{runtimeStatus.title}</span>
            <span className="ml-2 text-xs opacity-70">{runtimeStatus.description}</span>
          </div>
        </div>
      )}

      {actionError && (
        <div className="shrink-0 border-b border-destructive/30 bg-destructive/5 px-4 py-2">
          <div className="mx-auto flex max-w-5xl items-center gap-2 text-sm text-destructive">
            <AlertTriangle className="size-3.5 shrink-0" />
            <span>{actionError}</span>
          </div>
        </div>
      )}

      {/* Content: fills 100% remaining height */}
      <div className="flex min-h-0 flex-1 flex-col px-4">
        <div className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col">
          {/* Feature Queue -- always visible */}
          <section className="shrink-0 pt-4 pb-3">
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

          {/* Timeline -- fills remaining space with ScrollArea */}
          {hasTimeline && (
            <section className="flex min-h-0 flex-1 flex-col">
              <h3 className="mb-2 shrink-0 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Timeline
              </h3>
              <ScrollArea data-testid="mission-progress-timeline" className="min-h-0 flex-1">
                <div className="relative ml-3">
                  <div className="absolute top-2 bottom-2 left-0 w-px bg-border" />
                  {timeline.map((entry, index) => (
                    <div
                      key={`${entry.timestampLabel}-${entry.eventLabel}-${index}`}
                      className="relative flex min-w-0 items-baseline gap-3 py-1.5 pl-5"
                    >
                      <span className="absolute top-[11px] left-[-2px] size-[5px] rounded-full bg-muted-foreground/40" />
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
              </ScrollArea>
            </section>
          )}

          {/* Handoffs -- preview rows, click to open detail sub-page */}
          {hasHandoffs && (
            <section className="shrink-0 pb-4">
              <h3 className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Handoffs
              </h3>
              {handoffs.map((handoff) => (
                <HandoffRow key={handoff.key} handoff={handoff} onSelect={setActiveHandoff} />
              ))}
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
