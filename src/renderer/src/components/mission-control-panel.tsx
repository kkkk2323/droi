import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
import type { MissionRuntimeSnapshot, RuntimeLogEntry } from '@/types'
import { useActions, useActiveSessionId, useAppStore } from '@/store'

const EMPTY_MESSAGES: never[] = []
const EMPTY_RUNTIME_LOGS: RuntimeLogEntry[] = []

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

function formatRuntimeLogTime(ts: number): string {
  try {
    return new Date(ts).toLocaleTimeString([], {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return '--:--:--'
  }
}

function getRuntimeLogTone(stream: RuntimeLogEntry['stream']): string {
  if (stream === 'stderr') return 'text-destructive'
  if (stream === 'system') return 'text-sky-600 dark:text-sky-400'
  return 'text-foreground'
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
  runtimeLogs = EMPTY_RUNTIME_LOGS,
  runtimeLogState,
  onViewChange,
}: {
  mission?: MissionState | null
  runtimeLogs?: RuntimeLogEntry[]
  runtimeLogState?: MissionRuntimeSnapshot
  onViewChange?: (view: MissionViewMode) => void
}) {
  const activeSessionId = useActiveSessionId()
  const messages = useAppStore((state) =>
    activeSessionId
      ? state.sessionBuffers.get(activeSessionId)?.messages || EMPTY_MESSAGES
      : EMPTY_MESSAGES,
  )
  const { handleCancel, handleKillWorker, handleSendWorkerFollowup } = useActions()
  const [pendingAction, setPendingAction] = useState<'pause' | 'kill' | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [activeHandoff, setActiveHandoff] = useState<HandoffData | null>(null)
  const [workerFollowupInput, setWorkerFollowupInput] = useState('')
  const [workerFollowupState, setWorkerFollowupState] = useState<
    'idle' | 'sending' | 'sent' | 'failed'
  >('idle')
  const [workerFollowupError, setWorkerFollowupError] = useState<string | null>(null)
  const logsViewportRef = useRef<HTMLDivElement | null>(null)
  const [followLogs, setFollowLogs] = useState(true)

  const status = getMissionControlStatus(mission)
  const featureQueue = getMissionFeatureQueueItems(mission)
  const timeline = getMissionProgressTimelineItems(mission)
  const handoffs = getMissionHandoffCards(mission)
  const actionState = useMemo(() => getMissionActionState(mission), [mission])
  const runtimeStatus = useMemo(
    () => getMissionRuntimeStatus({ mission, messages, pendingAction }),
    [messages, mission, pendingAction],
  )
  const hasRuntimeLogs = runtimeLogs.length > 0
  const followupUnavailableReason = useMemo(() => {
    if (!actionState.canMessagePausedWorker || !actionState.pausedWorkerSessionId) return undefined
    if (!runtimeLogState) return 'Waiting for paused worker session logs…'
    if (runtimeLogState.workerSessionId !== actionState.pausedWorkerSessionId) {
      return 'Paused worker session logs are not ready yet.'
    }
    if (!runtimeLogState.exists) {
      return runtimeLogState.message || 'Paused worker session is unavailable.'
    }
    return undefined
  }, [actionState.canMessagePausedWorker, actionState.pausedWorkerSessionId, runtimeLogState])
  const canSubmitWorkerFollowup =
    !followupUnavailableReason &&
    workerFollowupState !== 'sending' &&
    Boolean(workerFollowupInput.trim())
  const runtimeLogsEmptyState =
    runtimeLogState?.message ||
    (mission?.currentState === 'running'
      ? 'Waiting for worker session transcript…'
      : 'No runtime logs captured for this worker yet.')

  useEffect(() => {
    if (!mission) {
      setPendingAction(null)
      setActionError(null)
      setWorkerFollowupInput('')
      setWorkerFollowupState('idle')
      setWorkerFollowupError(null)
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

  useEffect(() => {
    if (actionState.canMessagePausedWorker) return
    setWorkerFollowupInput('')
    setWorkerFollowupState('idle')
    setWorkerFollowupError(null)
  }, [actionState.canMessagePausedWorker])

  useEffect(() => {
    const viewport = logsViewportRef.current
    if (!viewport || !followLogs) return
    viewport.scrollTop = viewport.scrollHeight
  }, [followLogs, runtimeLogs])

  const handleLogsScroll = useCallback(() => {
    const viewport = logsViewportRef.current
    if (!viewport) return
    const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
    const nextFollowLogs = distanceFromBottom < 24
    setFollowLogs((current) => (current === nextFollowLogs ? current : nextFollowLogs))
  }, [])

  useEffect(() => {
    const viewport = logsViewportRef.current
    if (!viewport) return
    viewport.addEventListener('scroll', handleLogsScroll)
    return () => viewport.removeEventListener('scroll', handleLogsScroll)
  }, [handleLogsScroll])

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

  const handleWorkerFollowupSubmit = () => {
    if (!actionState.pausedWorkerSessionId) return
    if (followupUnavailableReason) {
      setWorkerFollowupState('failed')
      setWorkerFollowupError(followupUnavailableReason)
      return
    }
    const prompt = workerFollowupInput.trim()
    if (!prompt || workerFollowupState === 'sending') return

    setWorkerFollowupState('sending')
    setWorkerFollowupError(null)
    void handleSendWorkerFollowup({
      workerSessionId: actionState.pausedWorkerSessionId,
      prompt,
    })
      .then(() => {
        setWorkerFollowupInput('')
        setWorkerFollowupState('sent')
      })
      .catch((error) => {
        const message = error instanceof Error ? error.message : String(error)
        setWorkerFollowupError(message || 'Failed to send follow-up to paused worker.')
        setWorkerFollowupState('failed')
      })
  }

  const hasTimeline = timeline.length > 0
  const hasHandoffs = handoffs.length > 0

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

      {actionState.canMessagePausedWorker && actionState.pausedWorkerSessionId && (
        <div className="shrink-0 border-b border-border bg-muted/10 px-4 py-3">
          <div className="mx-auto max-w-5xl space-y-2">
            <div className="flex items-center gap-2 text-xs uppercase tracking-wide text-muted-foreground">
              <span>Paused worker follow-up</span>
              <Badge variant="outline" className="font-mono text-[10px] normal-case">
                {actionState.pausedWorkerSessionId}
              </Badge>
            </div>
            <div className="flex items-start gap-2">
              <textarea
                value={workerFollowupInput}
                onChange={(event) => {
                  setWorkerFollowupInput(event.target.value)
                  if (workerFollowupState !== 'idle') setWorkerFollowupState('idle')
                  if (workerFollowupError) setWorkerFollowupError(null)
                }}
                onKeyDown={(event) => {
                  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
                    event.preventDefault()
                    handleWorkerFollowupSubmit()
                  }
                }}
                rows={2}
                placeholder="Send a focused note to the paused worker before resuming Mission execution..."
                className="min-h-[64px] flex-1 resize-none rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground shadow-xs outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20"
              />
              <Button
                type="button"
                size="sm"
                onClick={handleWorkerFollowupSubmit}
                disabled={!canSubmitWorkerFollowup}
                className="h-9 px-3"
              >
                {workerFollowupState === 'sending' ? (
                  <LoaderCircle className="size-3.5 animate-spin" />
                ) : null}
                Send
              </Button>
            </div>
            <div className="text-xs text-muted-foreground">
              {workerFollowupState === 'sending'
                ? 'Sending to the paused worker and requesting Mission resume...'
                : workerFollowupState === 'sent'
                  ? 'Follow-up delivered and Mission resume requested.'
                  : workerFollowupState === 'failed'
                    ? workerFollowupError || 'Failed to send follow-up to the paused worker.'
                    : followupUnavailableReason ||
                      'This sends guidance to the paused worker, then automatically asks the Mission orchestrator to resume the run.'}
            </div>
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

          <div className="flex min-h-0 flex-1 flex-col gap-4 pb-4">
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

            <section className={cn('flex min-h-0 flex-col', hasTimeline ? 'max-h-56' : 'flex-1')}>
              <div className="mb-2  flex items-center gap-2">
                <h3 className="text-xs py-1 font-medium uppercase tracking-wide text-muted-foreground">
                  Runtime Logs
                </h3>
                {followLogs && hasRuntimeLogs ? (
                  <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                    Live
                  </Badge>
                ) : null}
              </div>
              <ScrollArea
                data-testid="mission-runtime-logs"
                className={cn(
                  'min-h-0 rounded-md border border-border/70 bg-muted/10',
                  hasTimeline ? 'flex-1' : 'min-h-[220px] flex-1',
                )}
                viewportRef={logsViewportRef}
              >
                {hasRuntimeLogs ? (
                  <div className="space-y-1 p-3 font-mono text-xs">
                    {runtimeLogs.map((entry, index) => (
                      <div
                        key={`${entry.ts}-${entry.stream}-${index}`}
                        className="flex min-w-0 items-start gap-2"
                      >
                        <span className="shrink-0 text-[10px] text-muted-foreground/60">
                          {formatRuntimeLogTime(entry.ts)}
                        </span>
                        <span className="shrink-0 text-[10px] uppercase text-muted-foreground/60">
                          {entry.stream}
                        </span>
                        <span
                          className={cn(
                            'min-w-0 whitespace-pre-wrap break-words',
                            getRuntimeLogTone(entry.stream),
                          )}
                        >
                          {entry.text}
                        </span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="flex h-full min-h-[140px] items-center justify-center px-4 py-6 text-sm text-muted-foreground/70">
                    {runtimeLogsEmptyState}
                  </div>
                )}
              </ScrollArea>
            </section>
          </div>

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
