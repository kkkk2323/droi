import { useEffect, useMemo, useState } from 'react'
import { PanelsLeftRight, MessagesSquare } from 'lucide-react'

import { ChatPage } from './ChatPage'
import { Button } from '@/components/ui/button'
import { useAppStore, useActiveSessionId } from '@/store'
import {
  getMissionStatusSummary,
  getPreferredMissionView,
  shouldApplyMissionAutoSwitch,
  type MissionViewMode,
} from '@/lib/missionPage'

function MissionControlPlaceholder() {
  return (
    <div
      data-testid="mission-control-view"
      className="flex flex-1 items-center justify-center px-6 py-8"
    >
      <div className="w-full max-w-3xl rounded-2xl border border-border bg-card px-6 py-8 shadow-sm">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-xl bg-muted">
            <PanelsLeftRight className="size-5 text-muted-foreground" />
          </div>
          <div>
            <h2 className="text-sm font-semibold text-foreground">Mission Control</h2>
            <p className="text-sm text-muted-foreground">
              Mission-specific control panels stay on the same orchestrator session.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

function MissionStatusBar() {
  const activeSessionId = useActiveSessionId()
  const mission = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.mission : undefined,
  )
  const summary = useMemo(() => getMissionStatusSummary(mission), [mission])

  return (
    <div
      data-testid="mission-statusbar"
      className="sticky bottom-0 z-10 border-t border-border bg-background/95 px-4 py-2 backdrop-blur"
    >
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-3 text-xs text-muted-foreground">
        <div className="min-w-0">
          <span className="font-medium text-foreground">State:</span> {summary.stateLabel}
        </div>
        <div className="min-w-0">
          <span className="font-medium text-foreground">Progress:</span> {summary.progressLabel}
        </div>
        <div className="min-w-0 flex-1 truncate">
          <span className="font-medium text-foreground">Current feature:</span>{' '}
          <span title={summary.currentFeatureLabel}>{summary.currentFeatureLabel}</span>
        </div>
        <div className="min-w-0">
          <span className="font-medium text-foreground">Worker:</span> {summary.workerLabel}
        </div>
      </div>
    </div>
  )
}

export function MissionPage() {
  const activeSessionId = useActiveSessionId()
  const mission = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.mission : undefined,
  )
  const [viewMode, setViewMode] = useState<MissionViewMode>(
    () => getPreferredMissionView(mission) ?? 'chat',
  )
  const [manualOverrideAt, setManualOverrideAt] = useState<number | undefined>(undefined)

  const preferredView = getPreferredMissionView(mission)

  useEffect(() => {
    if (shouldApplyMissionAutoSwitch({ currentView: viewMode, preferredView, manualOverrideAt })) {
      setViewMode(preferredView as MissionViewMode)
    }
  }, [manualOverrideAt, preferredView, viewMode])

  const setManualView = (nextView: MissionViewMode) => {
    setViewMode(nextView)
    setManualOverrideAt(Date.now())
  }

  return (
    <div data-testid="mission-page" className="flex h-full min-h-0 flex-col">
      <div className="border-b border-border px-4 pb-3">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-3 pt-1">
          <div>
            <h1 className="text-sm font-semibold text-foreground">Mission Session</h1>
            <p className="text-xs text-muted-foreground">
              Toggle between the shared chat shell and Mission Control for this orchestrator
              session.
            </p>
          </div>
          <div
            data-testid="mission-view-toggle"
            className="inline-flex items-center gap-1 rounded-xl border border-border bg-muted/40 p-1"
          >
            <Button
              type="button"
              size="sm"
              variant={viewMode === 'chat' ? 'secondary' : 'ghost'}
              onClick={() => setManualView('chat')}
            >
              <MessagesSquare className="size-4" />
              Chat
            </Button>
            <Button
              type="button"
              size="sm"
              variant={viewMode === 'mission-control' ? 'secondary' : 'ghost'}
              onClick={() => setManualView('mission-control')}
            >
              <PanelsLeftRight className="size-4" />
              Mission Control
            </Button>
          </div>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        {viewMode === 'chat' ? (
          <ChatPage
            forceInputDisabled={preferredView === 'mission-control'}
            forceDisabledPlaceholder={
              preferredView === 'mission-control'
                ? 'Mission is running. Pause to send a message.'
                : undefined
            }
          />
        ) : (
          <MissionControlPlaceholder />
        )}
      </div>

      <MissionStatusBar />
    </div>
  )
}
