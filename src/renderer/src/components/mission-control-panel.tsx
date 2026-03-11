import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  List,
  LoaderCircle,
  Maximize2,
  MessagesSquare,
  Minimize2,
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
  getMissionFeatureDetail,
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
): 'default' | 'secondary' | 'destructive' | 'warning' | 'outline' {
  const normalized = stateLabel.toLowerCase()
  if (normalized === 'completed') return 'default'
  if (normalized === 'running') return 'secondary'
  if (normalized === 'validation pending') return 'secondary'
  if (normalized === 'paused') return 'warning'
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
  const [selectedFeatureId, setSelectedFeatureId] = useState<string | null>(null)
  const [workerFollowupInput, setWorkerFollowupInput] = useState('')
  const [logsExpanded, setLogsExpanded] = useState(false)
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
  const hasTimeline = timeline.length > 0
  const activeFeatureId = selectedFeatureId ?? mission?.currentFeatureId ?? null
  const selectedHandoff = useMemo(
    () => handoffs.find((h) => h.featureId === activeFeatureId),
    [handoffs, activeFeatureId],
  )
  const selectedFeature = useMemo(
    () => featureQueue.find((f) => f.id === activeFeatureId),
    [featureQueue, activeFeatureId],
  )
  const selectedFeatureDetail = useMemo(
    () => getMissionFeatureDetail(mission, activeFeatureId),
    [mission, activeFeatureId],
  )
  const canSubmitWorkerFollowup =
    actionState.canMessagePausedWorker &&
    Boolean(actionState.pausedWorkerSessionId) &&
    workerFollowupState !== 'sending' &&
    Boolean(workerFollowupInput.trim())
  const runtimeLogsEmptyState =
    runtimeLogState?.message ||
    (mission?.currentState === 'running'
      ? 'Waiting for worker session transcript...'
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

  return (
    <div
      data-testid="mission-control-view"
      className="flex min-w-0 flex-1 flex-col overflow-hidden"
    >
      {/* Header: status + actions + view toggles */}
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
        <div className="flex items-center gap-3">
          <Badge data-testid="mission-status" variant={getStateBadgeVariant(status.stateLabel)}>
            {status.stateLabel}
          </Badge>
          <span className="text-sm text-muted-foreground">{status.progressLabel}</span>

          {runtimeStatus.tone !== 'default' && (
            <span data-testid="mission-runtime-status" className="text-xs text-muted-foreground/70">
              {runtimeStatus.title}
            </span>
          )}

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
              <>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  data-testid="mission-worker-list-toggle"
                  onClick={() => onViewChange('worker-list')}
                  className="h-7 px-2 text-xs text-muted-foreground"
                >
                  <List className="size-3.5" />
                  WorkerList
                </Button>
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
              </>
            )}
          </div>
        </div>
      </div>

      {/* Action error */}
      {actionError && (
        <div className="shrink-0 border-b border-destructive/30 bg-destructive/5 px-4 py-1.5">
          <div className="flex items-center gap-2 text-xs text-destructive">
            <AlertTriangle className="size-3 shrink-0" />
            <span>{actionError}</span>
          </div>
        </div>
      )}

      {/* Paused worker follow-up: compact inline input */}
      {actionState.canMessagePausedWorker && actionState.pausedWorkerSessionId && (
        <div className="shrink-0 border-b border-border bg-muted/5 px-4 py-2">
          <div className="flex items-center gap-2">
            <span className="shrink-0 text-xs text-muted-foreground">Follow-up</span>
            <input
              type="text"
              value={workerFollowupInput}
              onChange={(event) => {
                setWorkerFollowupInput(event.target.value)
                if (workerFollowupState !== 'idle') setWorkerFollowupState('idle')
                if (workerFollowupError) setWorkerFollowupError(null)
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  handleWorkerFollowupSubmit()
                }
              }}
              placeholder="Send guidance to the paused worker..."
              className="min-w-0 flex-1 rounded-md border border-input bg-background px-2.5 py-1.5 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground/60 focus:border-ring focus:ring-1 focus:ring-ring/20"
            />
            <Button
              type="button"
              size="sm"
              onClick={handleWorkerFollowupSubmit}
              disabled={!canSubmitWorkerFollowup}
              className="h-7 px-3 text-xs"
            >
              {workerFollowupState === 'sending' ? (
                <LoaderCircle className="size-3 animate-spin" />
              ) : null}
              Send
            </Button>
            {workerFollowupState === 'sent' && (
              <span className="shrink-0 text-[10px] text-emerald-600">Sent</span>
            )}
            {workerFollowupState === 'failed' && (
              <span className="shrink-0 text-[10px] text-destructive">
                {workerFollowupError || 'Failed'}
              </span>
            )}
          </div>
        </div>
      )}

      {/* Main content: left features + right (detail + timeline) */}
      <div className={cn('flex min-h-0 flex-1 flex-col', logsExpanded && 'hidden')}>
        <div className="flex min-h-0 flex-1">
          {/* Left: Feature list (master) */}
          <section className="flex w-[320px] shrink-0 flex-col border-r border-border">
            <div className="shrink-0 px-3 pt-3 pb-2">
              <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Features
              </h3>
            </div>
            <ScrollArea data-testid="mission-feature-queue" className="min-h-0 flex-1 px-3">
              <div className="space-y-0.5 pb-3">
                {featureQueue.length === 0 ? (
                  <p className="py-4 text-xs text-muted-foreground/60">No features queued yet.</p>
                ) : (
                  featureQueue.map((feature) => {
                    const isSelected = feature.id === activeFeatureId
                    return (
                      <button
                        key={feature.id}
                        type="button"
                        data-testid={feature.testId}
                        onClick={() => setSelectedFeatureId(feature.id)}
                        className={cn(
                          'flex w-full min-w-0 items-center gap-2 rounded-md border px-2 py-1.5 text-left transition-colors',
                          isSelected
                            ? 'border-primary/20 bg-primary/5'
                            : 'border-transparent hover:bg-muted/30',
                        )}
                      >
                        <span
                          className={cn(
                            'size-1.5 shrink-0 rounded-full',
                            getFeatureStatusDot(feature.status, feature.isCurrent),
                          )}
                        />
                        <span
                          className={cn(
                            'min-w-0 flex-1 truncate text-xs',
                            isSelected || feature.isCurrent
                              ? 'text-foreground'
                              : feature.status === 'completed' || feature.status === 'cancelled'
                                ? 'text-muted-foreground'
                                : 'text-foreground',
                          )}
                          title={feature.description}
                        >
                          {feature.description}
                        </span>
                        {feature.isValidator && (
                          <ShieldCheck className="size-3 shrink-0 text-violet-500" />
                        )}
                        <span
                          className={cn(
                            'shrink-0 text-[10px]',
                            isSelected || feature.isCurrent
                              ? 'font-medium text-primary'
                              : 'text-muted-foreground',
                          )}
                        >
                          {feature.statusLabel}
                        </span>
                      </button>
                    )
                  })
                )}
              </div>
            </ScrollArea>
          </section>

          {/* Right: Feature detail (top) + Timeline (bottom) */}
          <div className="flex min-w-0 flex-1 flex-col">
            {/* Top-right: Feature detail */}
            <section className="flex min-h-0 flex-1 flex-col border-b border-border">
              <div className="shrink-0 px-3 pt-3 pb-2">
                <div className="flex items-center gap-2">
                  <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    {selectedFeatureDetail?.title ||
                      selectedFeature?.description ||
                      'Feature Detail'}
                  </h3>
                  {selectedHandoff && (
                    <Badge
                      variant={getSuccessBadgeVariant(selectedHandoff.successState)}
                      className="ml-auto"
                    >
                      {selectedHandoff.successState}
                    </Badge>
                  )}
                </div>
              </div>
              <ScrollArea className="min-h-0 flex-1 px-3">
                {selectedFeatureDetail ? (
                  <div className="space-y-4 pb-3">
                    {(selectedFeatureDetail.skillName || selectedFeatureDetail.milestone) && (
                      <div className="grid gap-2 sm:grid-cols-2">
                        {selectedFeatureDetail.skillName && (
                          <div>
                            <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                              Skill
                            </h4>
                            <p className="text-xs text-foreground">
                              {selectedFeatureDetail.skillName}
                            </p>
                          </div>
                        )}
                        {selectedFeatureDetail.milestone && (
                          <div>
                            <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                              Milestone
                            </h4>
                            <p className="text-xs text-foreground">
                              {selectedFeatureDetail.milestone}
                            </p>
                          </div>
                        )}
                      </div>
                    )}

                    {selectedFeatureDetail.preconditions.length > 0 && (
                      <div>
                        <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          Preconditions
                        </h4>
                        <ul className="space-y-1">
                          {selectedFeatureDetail.preconditions.map((item) => (
                            <li key={item} className="text-xs text-foreground">
                              {item}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}

                    {selectedFeatureDetail.expectedBehavior.length > 0 && (
                      <div>
                        <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          Expected Behavior
                        </h4>
                        <ul className="space-y-1">
                          {selectedFeatureDetail.expectedBehavior.map((item) => (
                            <li key={item} className="text-xs text-foreground">
                              {item}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}

                    {selectedFeatureDetail.verificationSteps.length > 0 && (
                      <div>
                        <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          Verification Steps
                        </h4>
                        <ul className="space-y-1">
                          {selectedFeatureDetail.verificationSteps.map((item) => (
                            <li key={item} className="text-xs text-foreground">
                              {item}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}

                    {selectedFeatureDetail.description && (
                      <div>
                        <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          Description
                        </h4>
                        <p className="text-xs text-foreground">
                          {selectedFeatureDetail.description}
                        </p>
                      </div>
                    )}

                    {selectedFeatureDetail.handoff && (
                      <>
                        <div>
                          <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                            Summary
                          </h4>
                          <p className="text-xs text-foreground">
                            {selectedFeatureDetail.handoff.salientSummary}
                          </p>
                        </div>
                        <div>
                          <h4 className="mb-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                            Implementation
                          </h4>
                          <p className="text-xs text-muted-foreground">
                            {selectedFeatureDetail.handoff.whatWasImplemented}
                          </p>
                        </div>
                      </>
                    )}

                    {selectedFeatureDetail.handoff &&
                      selectedFeatureDetail.handoff.commandResults.length > 0 && (
                        <div>
                          <h4 className="mb-1 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                            <TerminalSquare className="size-3" />
                            Commands
                          </h4>
                          <ul className="space-y-1">
                            {selectedFeatureDetail.handoff.commandResults.map((item) => (
                              <li
                                key={item}
                                className="rounded-md bg-muted/40 px-2.5 py-1 font-mono text-[11px]"
                              >
                                {item}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                    {selectedFeatureDetail.handoff &&
                      selectedFeatureDetail.handoff.interactiveResults.length > 0 && (
                        <div>
                          <h4 className="mb-1 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                            <CheckCircle2 className="size-3" />
                            Checks
                          </h4>
                          <ul className="space-y-1">
                            {selectedFeatureDetail.handoff.interactiveResults.map((item) => (
                              <li
                                key={item}
                                className="rounded-md bg-muted/40 px-2.5 py-1 text-[11px]"
                              >
                                {item}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                  </div>
                ) : (
                  <div className="flex h-full min-h-[80px] items-center justify-center py-6">
                    <p className="text-xs text-muted-foreground/60">
                      {selectedFeature
                        ? 'Feature in progress — no handoff data yet.'
                        : 'Select a feature to view details.'}
                    </p>
                  </div>
                )}
              </ScrollArea>
            </section>

            {/* Bottom-right: Timeline (compact) */}
            <section className="flex h-[180px] shrink-0 flex-col">
              <div className="shrink-0 px-3 pt-2 pb-1.5">
                <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Timeline
                </h3>
              </div>
              <ScrollArea data-testid="mission-progress-timeline" className="min-h-0 flex-1 px-3">
                {hasTimeline ? (
                  <div className="relative ml-2 pb-2">
                    <div className="absolute top-2 bottom-2 left-0 w-px bg-border" />
                    {timeline.map((entry, index) => (
                      <div
                        key={`${entry.timestampLabel}-${entry.eventLabel}-${index}`}
                        className="relative flex min-w-0 items-baseline gap-2 py-0.5 pl-4"
                      >
                        <span className="absolute top-[7px] left-[-2px] size-[5px] rounded-full bg-muted-foreground/40" />
                        <span className="shrink-0 font-mono text-[10px] text-muted-foreground/50">
                          {formatTimeOnly(entry.timestampLabel)}
                        </span>
                        <span className="shrink-0 text-[11px] text-foreground">
                          {entry.eventLabel}
                        </span>
                        {entry.detailLabel && (
                          <span className="min-w-0 truncate text-[10px] text-muted-foreground/50">
                            {entry.detailLabel}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="px-2 py-3 text-xs text-muted-foreground/60">
                    No timeline events yet.
                  </p>
                )}
              </ScrollArea>
            </section>
          </div>
        </div>
      </div>

      {/* Bottom: Runtime Logs (collapsible / expandable) */}
      <section
        className={cn(
          'flex flex-col border-t border-border',
          logsExpanded ? 'min-h-0 flex-1' : 'h-[200px] shrink-0',
        )}
      >
        <div className="flex shrink-0 items-center gap-2 px-3 py-1.5">
          <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Runtime Logs
          </h3>
          {followLogs && hasRuntimeLogs && (
            <Badge variant="outline" className="h-4 px-1 text-[9px]">
              Live
            </Badge>
          )}
          <button
            type="button"
            onClick={() => setLogsExpanded((v) => !v)}
            className="ml-auto rounded p-1 text-muted-foreground/50 transition-colors hover:bg-muted/40 hover:text-muted-foreground"
            title={logsExpanded ? 'Collapse logs' : 'Expand logs'}
          >
            {logsExpanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </button>
        </div>
        <ScrollArea
          data-testid="mission-runtime-logs"
          className="min-h-0 flex-1 border-t border-border/50 bg-muted/5"
          viewportRef={logsViewportRef}
        >
          {hasRuntimeLogs ? (
            <div className="space-y-0.5 p-3 font-mono text-xs">
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
            <div className="flex h-full min-h-[100px] items-center justify-center px-4 py-4 text-xs text-muted-foreground/60">
              {runtimeLogsEmptyState}
            </div>
          )}
        </ScrollArea>
      </section>
    </div>
  )
}
