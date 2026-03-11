import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, Clock3, LoaderCircle } from 'lucide-react'

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
import { truncateWorkerSessionId } from '@/lib/missionPage'
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

function RuntimeLogBlock({
  snapshot,
  loading,
}: {
  snapshot?: MissionRuntimeSnapshot
  loading: boolean
}) {
  const entries = snapshot?.entries ?? EMPTY_RUNTIME_ENTRIES

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="mb-2 flex items-center gap-2">
        <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Runtime logs
        </h3>
        {loading ? (
          <Badge variant="outline" className="h-5 gap-1 px-1.5 text-[10px]">
            <LoaderCircle className="size-3 animate-spin" />
            Loading
          </Badge>
        ) : snapshot?.status === 'ready' ? (
          <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
            Live
          </Badge>
        ) : null}
      </div>

      <ScrollArea className="min-h-[220px] flex-1 rounded-md border border-border/70 bg-muted/10">
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
    </div>
  )
}

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

  return (
    <div
      data-testid="mission-worker-detail-view"
      className="flex min-w-0 flex-1 flex-col overflow-hidden"
    >
      <div className="shrink-0 border-b border-border bg-background/95 px-4 py-2 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center gap-3">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onBackToList}
            className="h-7 gap-1 px-2 text-xs text-muted-foreground"
          >
            <ArrowLeft className="size-3.5" />
            WorkerList
          </Button>
          <span className="text-sm font-medium text-foreground">{worker.featureTitle}</span>
          <Badge variant={getMissionWorkerStatusVariant(worker.status)}>{worker.statusLabel}</Badge>
          <Badge variant="outline" className="font-mono text-[10px]">
            {worker.workerSessionId}
          </Badge>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onBackToMission}
            className="ml-auto h-7 px-2 text-xs text-muted-foreground"
          >
            Back to Mission
          </Button>
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-5">
          <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <div className="rounded-lg border border-border/70 bg-muted/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Session
              </div>
              <div className="mt-1 font-mono text-sm text-foreground">{worker.workerSessionId}</div>
            </div>
            <div className="rounded-lg border border-border/70 bg-muted/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Feature
              </div>
              <div className="mt-1 text-sm text-foreground">{worker.featureTitle}</div>
            </div>
            <div className="rounded-lg border border-border/70 bg-muted/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Started
              </div>
              <div className="mt-1 text-sm text-foreground">{formatDateTime(worker.startedAt)}</div>
            </div>
            <div className="rounded-lg border border-border/70 bg-muted/10 p-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Duration
              </div>
              <div className="mt-1 flex items-center gap-1.5 text-sm text-foreground">
                <Clock3 className="size-3.5 text-muted-foreground" />
                {formatDuration(worker.durationMs)}
              </div>
            </div>
          </section>

          <section className="grid gap-3 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
            <div className="rounded-lg border border-border/70 bg-background p-4">
              <div className="mb-3 flex items-center gap-2">
                <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Worker status
                </h3>
                {worker.isCurrent ? <Badge variant="outline">Current</Badge> : null}
              </div>
              <div className="space-y-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <span className="text-muted-foreground">State</span>
                  <Badge variant={getMissionWorkerStatusVariant(worker.status)}>
                    {worker.statusLabel}
                  </Badge>
                </div>
                <div className="flex items-start justify-between gap-3">
                  <span className="text-muted-foreground">Ended</span>
                  <span className="text-right text-foreground">
                    {formatDateTime(worker.endedAt)}
                  </span>
                </div>
                <div className="flex items-start justify-between gap-3">
                  <span className="text-muted-foreground">Runtime source</span>
                  <span className="text-right text-foreground">
                    {runtimeSnapshot?.source === 'worker_session'
                      ? 'Worker session transcript'
                      : 'Mission state only'}
                  </span>
                </div>
                {worker.failureReason ? (
                  <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-destructive">
                    {worker.failureReason}
                  </div>
                ) : (
                  <div className="rounded-md border border-border/70 bg-muted/10 px-3 py-2 text-muted-foreground">
                    {getMissionWorkerStateCopy(worker)}
                  </div>
                )}
              </div>
            </div>

            <div className="rounded-lg border border-border/70 bg-background p-4">
              <div className="mb-3 flex items-center gap-2">
                <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Worker timeline
                </h3>
                <Badge variant="outline">{progressItems.length}</Badge>
              </div>
              {progressItems.length > 0 ? (
                <div className="space-y-2">
                  {progressItems.map((item, index) => (
                    <div
                      key={`${item.timestampMs || item.timestampLabel}-${index}`}
                      className="rounded-md border border-border/60 bg-muted/10 px-3 py-2"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-sm font-medium text-foreground">
                          {item.eventLabel}
                        </span>
                        <span className="text-[11px] text-muted-foreground">
                          {item.timestampLabel}
                        </span>
                      </div>
                      {item.detailLabel ? (
                        <div className="mt-1 text-xs text-muted-foreground">{item.detailLabel}</div>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-4 text-sm text-muted-foreground">
                  No worker timeline entries were captured for this session.
                </div>
              )}
            </div>
          </section>

          <section className="grid min-h-0 gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
            <div className="min-h-0 rounded-lg border border-border/70 bg-background p-4">
              <RuntimeLogBlock snapshot={runtimeSnapshot} loading={runtimeLoading} />
            </div>

            <div className="rounded-lg border border-border/70 bg-background p-4">
              <div className="mb-3 flex items-center gap-2">
                <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Result summary
                </h3>
                {handoffs.length > 0 ? <Badge variant="outline">{handoffs.length}</Badge> : null}
              </div>
              {handoffs.length > 0 ? (
                <div className="space-y-3">
                  {handoffs.map((handoff) => (
                    <div
                      key={handoff.key}
                      className="rounded-md border border-border/60 bg-muted/10 p-3"
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
                  ))}
                </div>
              ) : (
                <div className="rounded-md border border-border/60 bg-muted/10 px-3 py-4 text-sm text-muted-foreground">
                  No handoff summary has been recorded for this worker yet.
                </div>
              )}
            </div>
          </section>
        </div>
      </ScrollArea>
    </div>
  )
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
        <div className="mx-auto flex max-w-6xl items-center gap-3">
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
          <span className="text-sm font-medium text-foreground">WorkerList</span>
          <Badge variant="outline">{counts.all} workers</Badge>
          {mission?.currentState ? <Badge variant="outline">{mission.currentState}</Badge> : null}
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col px-4 py-4">
        <div className="mx-auto flex min-h-0 w-full max-w-6xl flex-1 flex-col gap-4">
          <section className="shrink-0">
            <div className="flex flex-wrap items-center gap-2">
              {filterOptions.map((option) => (
                <Button
                  key={option.key}
                  type="button"
                  size="sm"
                  variant={filter === option.key ? 'default' : 'outline'}
                  onClick={() => setFilter(option.key)}
                  className="h-8 gap-2 px-3 text-xs"
                >
                  {option.label}
                  <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                    {option.count}
                  </Badge>
                </Button>
              ))}
            </div>
          </section>

          <section className="min-h-0 flex-1 rounded-lg border border-border/70 bg-background">
            <div className="grid grid-cols-[56px_minmax(0,1.2fr)_132px_112px_104px_minmax(0,1.8fr)] gap-3 border-b border-border/70 px-4 py-2 text-[11px] uppercase tracking-wide text-muted-foreground">
              <span>#</span>
              <span>Session</span>
              <span>Start</span>
              <span>Duration</span>
              <span>Status</span>
              <span>Feature</span>
            </div>

            <ScrollArea className="min-h-0 h-full">
              {filteredWorkers.length > 0 ? (
                <div className="divide-y divide-border/60">
                  {filteredWorkers.map((worker, index) => (
                    <button
                      key={worker.workerSessionId}
                      type="button"
                      data-testid={`mission-worker-row-${worker.workerSessionId}`}
                      onClick={() => onSelectWorker(worker.workerSessionId)}
                      className={cn(
                        'grid w-full grid-cols-[56px_minmax(0,1.2fr)_132px_112px_104px_minmax(0,1.8fr)] gap-3 px-4 py-3 text-left transition-colors',
                        'hover:bg-muted/40',
                        worker.isCurrent ? 'bg-primary/5' : 'bg-transparent',
                      )}
                    >
                      <span className="text-sm text-muted-foreground">{index + 1}</span>
                      <div className="min-w-0">
                        <div className="truncate font-mono text-sm text-foreground">
                          {truncateWorkerSessionId(worker.workerSessionId)}
                        </div>
                        {worker.isCurrent ? (
                          <div className="mt-1 text-[11px] text-primary">Current worker</div>
                        ) : null}
                      </div>
                      <span className="text-sm text-foreground">
                        {formatDateTime(worker.startedAt)}
                      </span>
                      <span className="text-sm text-foreground">
                        {formatDuration(worker.durationMs)}
                      </span>
                      <div>
                        <Badge variant={getMissionWorkerStatusVariant(worker.status)}>
                          {worker.statusLabel}
                        </Badge>
                      </div>
                      <div className="min-w-0">
                        <div className="truncate text-sm text-foreground">
                          {worker.featureTitle}
                        </div>
                        <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
                          {worker.hasHandoff ? <span>Handoff</span> : null}
                          {worker.failureReason ? (
                            <span className="truncate text-destructive">
                              {worker.failureReason}
                            </span>
                          ) : null}
                        </div>
                      </div>
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

          <section className="shrink-0 rounded-lg border border-border/70 bg-muted/10 px-4 py-3 text-sm text-muted-foreground">
            Select a worker to inspect its timeline, runtime transcript, and latest handoff or
            failure state.
          </section>
        </div>
      </div>
    </div>
  )
}
