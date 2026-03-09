import {
  Activity,
  CheckCircle2,
  Clock3,
  ListTodo,
  ShieldCheck,
  Sparkles,
  TerminalSquare,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import {
  getMissionControlStatus,
  getMissionFeatureQueueItems,
  getMissionHandoffCards,
  getMissionProgressTimelineItems,
} from '@/lib/missionControl'
import { getMissionRuntimeStatus } from '@/lib/missionUiSemantics'
import type { MissionState } from '@/state/missionState'
import { useActiveSessionId, useAppStore } from '@/store'

function getStateBadgeVariant(
  stateLabel: string,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  const normalized = stateLabel.toLowerCase()
  if (normalized === 'completed') return 'default'
  if (normalized === 'running') return 'secondary'
  if (normalized === 'paused') return 'destructive'
  return 'outline'
}

function getFeatureStatusClasses(status: string, isCurrent: boolean): string {
  const normalized = status.toLowerCase()
  if (isCurrent) {
    return 'border-primary/60 bg-primary/5 shadow-sm ring-1 ring-primary/20'
  }
  if (normalized === 'completed') return 'border-emerald-500/30 bg-emerald-500/5'
  if (normalized === 'cancelled') return 'border-destructive/30 bg-destructive/5'
  if (normalized === 'in_progress' || normalized === 'running') {
    return 'border-sky-500/30 bg-sky-500/5'
  }
  return 'border-border bg-card'
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

export function MissionControlPanel({ mission }: { mission?: MissionState | null }) {
  const activeSessionId = useActiveSessionId()
  const messages = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.messages || [] : [],
  )
  const status = getMissionControlStatus(mission)
  const featureQueue = getMissionFeatureQueueItems(mission)
  const timeline = getMissionProgressTimelineItems(mission)
  const handoffs = getMissionHandoffCards(mission)
  const runtimeStatus = getMissionRuntimeStatus({ mission, messages })

  return (
    <div
      data-testid="mission-control-view"
      className="flex flex-1 flex-col overflow-y-auto px-4 py-4"
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        <section className="rounded-2xl border border-border bg-card p-4 shadow-sm">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <div className="flex size-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
                  <Activity className="size-4" />
                </div>
                <div>
                  <h2 className="text-sm font-semibold text-foreground">Mission status</h2>
                  <p className="text-sm text-muted-foreground">
                    Live state, progress, and validator-aware run status.
                  </p>
                </div>
              </div>
            </div>

            <Badge data-testid="mission-status" variant={getStateBadgeVariant(status.stateLabel)}>
              {status.stateLabel}
            </Badge>
          </div>

          <div className="mt-4 grid gap-3 md:grid-cols-3">
            <div className="rounded-xl border border-border/70 bg-background/60 p-3">
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Progress
              </div>
              <div className="mt-1 text-lg font-semibold text-foreground">
                {status.progressLabel}
              </div>
            </div>
            <div className="rounded-xl border border-border/70 bg-background/60 p-3 md:col-span-2">
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Current feature
              </div>
              <div className="mt-1 text-sm font-medium text-foreground">
                {status.currentFeatureLabel}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">{status.phaseLabel}</div>
            </div>
          </div>

          <div className="mt-3 rounded-xl border border-border/70 bg-muted/20 px-3 py-3">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Recovery / control state
            </div>
            <div className="mt-1 text-sm font-medium text-foreground">{runtimeStatus.title}</div>
            <div className="mt-1 text-sm text-muted-foreground">{runtimeStatus.description}</div>
          </div>
        </section>

        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <section className="rounded-2xl border border-border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-3">
              <div className="flex size-9 items-center justify-center rounded-xl bg-muted text-foreground">
                <ListTodo className="size-4" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-foreground">Feature Queue</h2>
                <p className="text-sm text-muted-foreground">
                  Ordered exactly like <code>features.json</code>, with validator work clearly
                  marked.
                </p>
              </div>
            </div>

            <div data-testid="mission-feature-queue" className="mt-4 space-y-2">
              {featureQueue.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                  Mission features will appear here when the mission queue is available.
                </div>
              ) : (
                featureQueue.map((feature) => (
                  <article
                    key={feature.id}
                    data-testid={feature.testId}
                    className={cn(
                      'rounded-xl border px-3 py-3 transition-colors',
                      getFeatureStatusClasses(feature.status, feature.isCurrent),
                    )}
                  >
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="truncate text-sm font-medium text-foreground">
                            {feature.description}
                          </p>
                          {feature.isCurrent ? <Badge variant="secondary">Current</Badge> : null}
                          {feature.isValidator ? (
                            <Badge
                              variant="outline"
                              className="gap-1 border-violet-500/40 bg-violet-500/5"
                            >
                              <ShieldCheck className="size-3.5" />
                              Validator
                            </Badge>
                          ) : null}
                        </div>
                        <p className="mt-1 break-all font-mono text-[11px] text-muted-foreground">
                          {feature.id}
                        </p>
                      </div>
                      <Badge variant={feature.isCurrent ? 'secondary' : 'outline'}>
                        {feature.statusLabel}
                      </Badge>
                    </div>
                  </article>
                ))
              )}
            </div>
          </section>

          <section className="rounded-2xl border border-border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-3">
              <div className="flex size-9 items-center justify-center rounded-xl bg-muted text-foreground">
                <Clock3 className="size-4" />
              </div>
              <div>
                <h2 className="text-sm font-semibold text-foreground">Progress Timeline</h2>
                <p className="text-sm text-muted-foreground">
                  Timestamped mission events in chronological order with duplicate entries
                  collapsed.
                </p>
              </div>
            </div>

            <div data-testid="mission-progress-timeline" className="mt-4 space-y-3">
              {timeline.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                  Mission progress entries will appear here once the run starts logging events.
                </div>
              ) : (
                timeline.map((entry, index) => (
                  <div
                    key={`${entry.timestampLabel}-${entry.eventLabel}-${index}`}
                    className="flex gap-3"
                  >
                    <div className="flex flex-col items-center">
                      <span className="mt-1 size-2 rounded-full bg-primary" />
                      {index < timeline.length - 1 ? (
                        <span className="mt-1 h-full w-px bg-border" />
                      ) : null}
                    </div>
                    <div className="min-w-0 flex-1 rounded-xl border border-border/70 bg-background/60 p-3">
                      <div className="flex flex-wrap items-start justify-between gap-2">
                        <p className="text-sm font-medium text-foreground">{entry.eventLabel}</p>
                        <span className="text-xs text-muted-foreground">
                          {entry.timestampLabel}
                        </span>
                      </div>
                      {entry.detailLabel ? (
                        <p className="mt-1 text-sm text-muted-foreground">{entry.detailLabel}</p>
                      ) : null}
                    </div>
                  </div>
                ))
              )}
            </div>
          </section>
        </div>

        <section className="rounded-2xl border border-border bg-card p-4 shadow-sm">
          <div className="flex items-center gap-3">
            <div className="flex size-9 items-center justify-center rounded-xl bg-muted text-foreground">
              <Sparkles className="size-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-foreground">Worker handoffs</h2>
              <p className="text-sm text-muted-foreground">
                Completed worker summaries remain visible while validator features continue and
                after restart recovery.
              </p>
            </div>
          </div>

          <div className="mt-4 grid gap-3 lg:grid-cols-2">
            {handoffs.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border px-4 py-6 text-sm text-muted-foreground lg:col-span-2">
                Completed worker handoffs will appear here when the mission writes handoff files.
              </div>
            ) : (
              handoffs.map((handoff) => (
                <article
                  key={handoff.testId}
                  data-testid={handoff.testId}
                  className="rounded-xl border border-border/70 bg-background/60 p-4"
                >
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-semibold text-foreground">{handoff.title}</p>
                      <p className="mt-1 break-all font-mono text-[11px] text-muted-foreground">
                        {handoff.featureId}
                      </p>
                    </div>
                    <Badge variant={getSuccessBadgeVariant(handoff.successState)}>
                      {handoff.successState}
                    </Badge>
                  </div>

                  <div className="mt-4 space-y-3 text-sm">
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
                        <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                          <TerminalSquare className="size-3.5" />
                          Verification commands
                        </div>
                        <ul className="mt-2 space-y-2 text-muted-foreground">
                          {handoff.commandResults.length === 0 ? (
                            <li>No command verification recorded.</li>
                          ) : (
                            handoff.commandResults.map((item) => <li key={item}>{item}</li>)
                          )}
                        </ul>
                      </div>

                      <div>
                        <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                          <CheckCircle2 className="size-3.5" />
                          Interactive checks
                        </div>
                        <ul className="mt-2 space-y-2 text-muted-foreground">
                          {handoff.interactiveResults.length === 0 ? (
                            <li>No interactive checks recorded.</li>
                          ) : (
                            handoff.interactiveResults.map((item) => <li key={item}>{item}</li>)
                          )}
                        </ul>
                      </div>
                    </div>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      </div>
    </div>
  )
}
