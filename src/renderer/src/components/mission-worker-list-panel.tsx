import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, ChevronRight, Clock3, LoaderCircle } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { getDroidClient } from '@/droidClient'
import {
  filterMissionWorkers,
  getMissionWorkerCounts,
  getMissionWorkerHandoffs,
  getMissionWorkerProgressItems,
  getMissionWorkerStateCopy,
  getMissionWorkerStatusVariant,
  getMissionWorkerSummaries,
  type MissionWorkerListFilter,
  type MissionWorkerSummary,
} from '@/lib/missionWorkerList'
import { cn } from '@/lib/utils'
import type { MissionState } from '@/state/missionState'
import type { MissionRuntimeSnapshot, RuntimeLogEntry } from '@/types'

const droid = getDroidClient()
const EMPTY_RUNTIME_ENTRIES: RuntimeLogEntry[] = []

function formatDateTime(value?: number): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(value))
}

function formatDuration(durationMs?: number): string {
  if (!Number.isFinite(durationMs)) return '—'
  const totalSeconds = Math.max(0, Math.floor((durationMs || 0) / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `${hours}h ${minutes}m ${seconds}s`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
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

function formatShortSessionId(workerSessionId: string): string {
  if (workerSessionId.length <= 12) return workerSessionId
  return `${workerSessionId.slice(0, 6)}…${workerSessionId.slice(-4)}`
}

function RuntimeLogBlock({
  snapshot,
  loading,
  defaultCollapsed,
  isWorkerActive,
}: {
  snapshot?: MissionRuntimeSnapshot
  loading: boolean
  defaultCollapsed?: boolean
  isWorkerActive?: boolean
}) {
  const entries = snapshot?.entries ?? EMPTY_RUNTIME_ENTRIES
  const [collapsed, setCollapsed] = useState(defaultCollapsed ?? false)
  const showLive = isWorkerActive && snapshot?.status === 'ready'

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <button
        type="button"
        onClick={() => setCollapsed((c) => !c)}
        className="mb-2 flex items-center gap-2"
      >
        <ChevronRight
          className={cn('size-3 text-muted-foreground transition-transform', !collapsed && 'rotate-90')}
        />
        <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Runtime logs
        </h3>
        {loading ? (
          <Badge variant="outline" className="h-5 gap-1 px-1.5 text-[10px]">
            <LoaderCircle className="size-3 animate-spin" />
            Loading
          </Badge>
        ) : showLive ? (
          <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
            Live
          </Badge>
        ) : null}
      </button>

      {!collapsed && (
        <ScrollArea className="max-h-[calc(100vh-18rem)] min-h-[220px] rounded-md border border-border/70 bg-muted/10">
          {entries.length > 0 ? (
            <div className="space-y-1 p-3 font-mono text-xs">
              {entries.map((entry, index) => (
                <div key={`${entry.ts}-${entry.stream}-${index}`} className="flex min-w-0 gap-2">
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
              {snapshot?.message || 'No runtime logs captured for this worker yet.'}
            </div>
          )}
        </ScrollArea>
      )}
    </div>
  )
}

type DetailTab = 'timeline' | 'logs' | 'result'

function WorkerDetailView({
  mission,
  worker,
  sessionId,
  missionDir,
  missionBaseSessionId,
  workingDirectory,
  onBackToList,
  onBackToMission,
}: {
  mission?: MissionState | null
  worker: MissionWorkerSummary
  sessionId: string
  missionDir?: string
  missionBaseSessionId?: string
  workingDirectory?: string
  onBackToList: () => void
  onBackToMission: () => void
}) {
  const [runtimeSnapshot, setRuntimeSnapshot] = useState<MissionRuntimeSnapshot | undefined>()
  const [runtimeLoading, setRuntimeLoading] = useState(false)
  const isTerminal = worker.status === 'success' || worker.status === 'partial' || worker.status === 'failed'
  const [activeTab, setActiveTab] = useState<DetailTab>(isTerminal ? 'result' : 'timeline')

  const progressItems = useMemo(
    () => getMissionWorkerProgressItems(mission, worker.workerSessionId),
    [mission, worker.workerSessionId],
  )
  const handoffs = useMemo(() => getMissionWorkerHandoffs(mission, worker), [mission, worker])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setInterval> | undefined

    const syncRuntime = async () => {
      setRuntimeLoading(true)
      try {
        const result = await droid.readMissionRuntime({
          sessionId,
          missionDir,
          missionBaseSessionId,
          workingDirectory,
          workerSessionId: worker.workerSessionId,
        })
        if (!cancelled) setRuntimeSnapshot(result?.snapshot)
      } catch (error) {
        if (!cancelled) {
          const message = error instanceof Error ? error.message : String(error)
          setRuntimeSnapshot({
            sessionId,
            workerSessionId: worker.workerSessionId,
            workingDirectory,
            exists: false,
            status: 'unavailable',
            source: 'none',
            message: message || 'Failed to read worker runtime logs.',
            entries: [],
          })
        }
      } finally {
        if (!cancelled) setRuntimeLoading(false)
      }
    }

    void syncRuntime()
    timer = setInterval(() => {
      void syncRuntime()
    }, 2000)

    return () => {
      cancelled = true
      if (timer) clearInterval(timer)
    }
  }, [missionBaseSessionId, missionDir, sessionId, worker.workerSessionId, workingDirectory])

  const tabs: Array<{ key: DetailTab; label: string; count?: number }> = [
    { key: 'timeline', label: 'Timeline', count: progressItems.length },
    { key: 'logs', label: 'Logs' },
    { key: 'result', label: 'Result', count: handoffs.length },
  ]

  return (
    <div
      data-testid="mission-worker-detail-view"
      className="flex min-w-0 flex-1 flex-col overflow-hidden"
    >
      {/* Header: back nav + inline metadata */}
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2.5 backdrop-blur">
        <div className="mx-auto max-w-5xl space-y-1.5">
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={onBackToList}
              className="h-7 gap-1 px-2 text-xs text-muted-foreground"
            >
              <ArrowLeft className="size-3.5" />
              Workers
            </Button>
            <span className="text-muted-foreground/40">/</span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={onBackToMission}
              className="h-7 px-2 text-xs text-muted-foreground"
            >
              Mission
            </Button>
          </div>
          {/* Feature title row */}
          <div className="flex items-center gap-2">
            <span className="min-w-0 text-sm font-medium text-foreground line-clamp-2">
              {worker.featureTitle}
            </span>
            <Badge
              variant={getMissionWorkerStatusVariant(worker.status)}
              className="shrink-0"
            >
              {worker.statusLabel}
            </Badge>
            {worker.isCurrent && (
              <Badge variant="outline" className="shrink-0">
                Current
              </Badge>
            )}
          </div>
          {/* Time metadata row */}
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <span className="shrink-0">{formatDateTime(worker.startedAt)}</span>
            {worker.endedAt && (
              <>
                <span className="shrink-0 text-muted-foreground/30">→</span>
                <span className="shrink-0">{formatDateTime(worker.endedAt)}</span>
              </>
            )}
            <span className="flex shrink-0 items-center gap-1">
              <Clock3 className="size-3" />
              {formatDuration(worker.durationMs)}
            </span>
            <span
              className="shrink-0 font-mono text-[10px] text-muted-foreground/50"
              title={worker.workerSessionId}
            >
              {formatShortSessionId(worker.workerSessionId)}
            </span>
          </div>
          {/* Status message */}
          {worker.failureReason ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-1.5 text-xs text-destructive">
              {worker.failureReason}
            </div>
          ) : null}
        </div>
      </div>

      {/* Tab bar */}
      <div className="shrink-0 border-b border-border bg-background px-4">
        <div className="mx-auto flex max-w-5xl items-center gap-1">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key)}
              className={cn(
                'relative px-3 py-2 text-xs font-medium transition-colors',
                activeTab === tab.key
                  ? 'text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              <span className="flex items-center gap-1.5">
                {tab.label}
                {typeof tab.count === 'number' && tab.count > 0 && (
                  <span className="text-[10px] text-muted-foreground/60">{tab.count}</span>
                )}
              </span>
              {activeTab === tab.key && (
                <span className="absolute inset-x-0 -bottom-px h-px bg-foreground" />
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Tab content */}
      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto max-w-5xl px-4 py-5">
          {activeTab === 'timeline' && (
            <div className="space-y-2">
              {progressItems.length > 0 ? (
                <div className="relative ml-3">
                  <div className="absolute top-2 bottom-2 left-0 w-px bg-border" />
                  {progressItems.map((item, index) => (
                    <div
                      key={`${item.timestampMs || item.timestampLabel}-${index}`}
                      className="relative flex min-w-0 items-baseline gap-3 py-2 pl-5"
                    >
                      <span className="absolute top-[13px] left-[-2px] size-[5px] rounded-full bg-muted-foreground/40" />
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground/50">
                        {item.timestampLabel}
                      </span>
                      <span className="text-sm font-medium text-foreground">
                        {item.eventLabel}
                      </span>
                      {item.detailLabel && (
                        <span className="min-w-0 truncate text-xs text-muted-foreground/60">
                          {cleanDetailLabel(item.detailLabel)}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-6 text-center text-sm text-muted-foreground">
                  No timeline entries captured for this worker.
                </div>
              )}
            </div>
          )}

          {activeTab === 'logs' && (
            <RuntimeLogBlock
              snapshot={runtimeSnapshot}
              loading={runtimeLoading}
              isWorkerActive={!isTerminal}
            />
          )}

          {activeTab === 'result' && (
            <div className="space-y-4">
              <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-2 text-sm text-muted-foreground">
                {getMissionWorkerStateCopy(worker)}
              </div>
              {handoffs.length > 0 ? (
                handoffs.map((handoff) => (
                  <div
                    key={handoff.key}
                    className="rounded-lg border border-border/70 bg-background p-4"
                  >
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{handoff.title}</span>
                      <Badge variant="outline" className="ml-auto">
                        {handoff.successState}
                      </Badge>
                    </div>
                    <p className="mt-2 text-sm text-foreground">{handoff.salientSummary}</p>
                    <p className="mt-2 text-xs text-muted-foreground">
                      {handoff.whatWasImplemented}
                    </p>
                  </div>
                ))
              ) : (
                <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-6 text-center text-sm text-muted-foreground">
                  No handoff summary recorded for this worker yet.
                </div>
              )}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

function cleanDetailLabel(detail: string): string {
  return detail.replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, '').replace(/\s*·\s*·\s*/g, ' · ').replace(/^[\s·]+|[\s·]+$/g, '').trim() || detail
}

export function MissionWorkerListPanel({
  mission,
  sessionId,
  missionDir,
  missionBaseSessionId,
  projectDir,
  selectedWorkerSessionId,
  onSelectWorker,
  onBackToList,
  onBackToMission,
}: {
  mission?: MissionState | null
  sessionId?: string | null
  missionDir?: string
  missionBaseSessionId?: string
  projectDir?: string
  selectedWorkerSessionId?: string
  onSelectWorker: (workerSessionId: string) => void
  onBackToList: () => void
  onBackToMission: () => void
}) {
  const [filter, setFilter] = useState<MissionWorkerListFilter>('all')
  const workers = useMemo(() => getMissionWorkerSummaries(mission), [mission])
  const counts = useMemo(() => getMissionWorkerCounts(workers), [workers])
  const filteredWorkers = useMemo(() => filterMissionWorkers(workers, filter), [filter, workers])
  const selectedWorker = useMemo(
    () => workers.find((worker) => worker.workerSessionId === selectedWorkerSessionId),
    [selectedWorkerSessionId, workers],
  )
  const workingDirectory =
    String((mission?.state as any)?.workingDirectory || projectDir || '').trim() || undefined

  if (selectedWorker && sessionId) {
    return (
      <WorkerDetailView
        mission={mission}
        worker={selectedWorker}
        sessionId={sessionId}
        missionDir={missionDir}
        missionBaseSessionId={missionBaseSessionId}
        workingDirectory={workingDirectory}
        onBackToList={onBackToList}
        onBackToMission={onBackToMission}
      />
    )
  }

  const filterOptions: Array<{ key: MissionWorkerListFilter; label: string; count: number }> = [
    { key: 'all', label: 'All', count: counts.all },
    { key: 'active', label: 'Active', count: counts.active },
    { key: 'completed', label: 'Completed', count: counts.completed },
    { key: 'failed', label: 'Failed', count: counts.failed },
  ]

  return (
    <div
      data-testid="mission-worker-list-view"
      className="flex min-w-0 flex-1 flex-col overflow-hidden"
    >
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
        <div className="mx-auto flex max-w-5xl items-center gap-3">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onBackToMission}
            className="h-7 gap-1 px-2 text-xs text-muted-foreground"
          >
            <ArrowLeft className="size-3.5" />
            Mission
          </Button>
          <span className="text-sm font-medium text-foreground">Workers</span>
          <Badge variant="outline">{counts.all}</Badge>
          {mission?.currentState ? <Badge variant="outline">{mission.currentState}</Badge> : null}
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col px-4 py-4">
        <div className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col gap-3">
          <section className="shrink-0">
            <div className="flex flex-wrap items-center gap-2">
              {filterOptions.map((option) => {
                const isActive = filter === option.key
                return (
                  <Button
                    key={option.key}
                    type="button"
                    size="sm"
                    variant={isActive ? 'default' : 'outline'}
                    onClick={() => setFilter(option.key)}
                    className="h-8 gap-2 px-3 text-xs"
                  >
                    {option.label}
                    <Badge
                      variant="outline"
                      className={cn(
                        'h-5 px-1.5 text-[10px]',
                        isActive && 'border-primary-foreground/30 text-primary-foreground',
                      )}
                    >
                      {option.count}
                    </Badge>
                  </Button>
                )
              })}
            </div>
          </section>

          <section className="min-h-0 flex-1">
            <ScrollArea className="min-h-0 h-full px-4">
              {filteredWorkers.length > 0 ? (
                <div className="space-y-2">
                  {filteredWorkers.map((worker) => (
                    <button
                      key={worker.workerSessionId}
                      type="button"
                      data-testid={`mission-worker-row-${worker.workerSessionId}`}
                      onClick={() => onSelectWorker(worker.workerSessionId)}
                      className={cn(
                        'group flex w-full items-center gap-4 rounded-lg border px-4 py-3.5 text-left transition-colors',
                        'hover:bg-muted/30',
                        worker.isCurrent
                          ? 'border-primary/20 bg-primary/5'
                          : 'border-border/50 bg-background',
                      )}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start gap-2">
                          <span className="text-sm leading-snug font-medium text-foreground line-clamp-2">
                            {worker.featureTitle}
                          </span>
                          {worker.isCurrent && (
                            <Badge variant="outline" className="mt-0.5 shrink-0 text-[10px]">
                              Current
                            </Badge>
                          )}
                        </div>
                        <div className="mt-1.5 flex items-center gap-2 text-[11px] text-muted-foreground/70">
                          <span
                            className="font-mono"
                            title={worker.workerSessionId}
                          >
                            {formatShortSessionId(worker.workerSessionId)}
                          </span>
                          {worker.hasHandoff && (
                            <>
                              <span className="text-muted-foreground/30">·</span>
                              <span>Handoff</span>
                            </>
                          )}
                          {worker.failureReason && (
                            <>
                              <span className="text-muted-foreground/30">·</span>
                              <span className="truncate text-destructive/80">
                                {worker.failureReason}
                              </span>
                            </>
                          )}
                        </div>
                      </div>

                      <Badge
                        variant={getMissionWorkerStatusVariant(worker.status)}
                        className="shrink-0"
                      >
                        {worker.statusLabel}
                      </Badge>

                      <span className="w-16 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
                        {formatDuration(worker.durationMs)}
                      </span>

                      <ChevronRight className="size-4 shrink-0 text-muted-foreground/30 transition-colors group-hover:text-muted-foreground/60" />
                    </button>
                  ))}
                </div>
              ) : (
                <div className="flex h-full min-h-[220px] items-center justify-center px-6 text-sm text-muted-foreground">
                  No workers match the current filter.
                </div>
              )}
            </ScrollArea>
          </section>
        </div>
      </div>
    </div>
  )
}
