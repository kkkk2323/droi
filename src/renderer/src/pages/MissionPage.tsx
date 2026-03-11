import { useEffect, useMemo, useState } from 'react'
import { PanelsLeftRight, MessagesSquare } from 'lucide-react'

import { ChatPage } from './ChatPage'
import { MissionControlPanel } from '@/components/mission-control-panel'
import { Button } from '@/components/ui/button'
import { getMissionInputSemantics } from '@/lib/missionUiSemantics'
import { useAppStore, useActiveSessionId } from '@/store'
import {
  getMissionSessionViewState,
  getPreferredMissionView,
  shouldApplyMissionAutoSwitch,
  type MissionSessionViewState,
  type MissionViewMode,
} from '@/lib/missionPage'

export function MissionPage() {
  const activeSessionId = useActiveSessionId()
  const mission = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.mission : undefined,
  )
  const [sessionViewStates, setSessionViewStates] = useState<
    Record<string, MissionSessionViewState>
  >({})

  const sessionViewState = useMemo(
    () =>
      getMissionSessionViewState({
        sessionId: activeSessionId,
        mission,
        sessionViewStates,
      }),
    [activeSessionId, mission, sessionViewStates],
  )

  const preferredView = getPreferredMissionView(mission)
  const inputSemantics = getMissionInputSemantics(mission)
  const viewMode = sessionViewState.viewMode
  const manualOverrideAt = sessionViewState.manualOverrideAt

  const hasMissionState = Boolean(
    mission?.currentState || (mission?.features && mission.features.length > 0),
  )

  useEffect(() => {
    if (!activeSessionId) return
    if (shouldApplyMissionAutoSwitch({ currentView: viewMode, preferredView, manualOverrideAt })) {
      setSessionViewStates((current) => {
        const previous = current[activeSessionId]
        if (previous?.viewMode === preferredView) return current
        return {
          ...current,
          [activeSessionId]: {
            ...previous,
            viewMode: preferredView as MissionViewMode,
          },
        }
      })
    }
  }, [activeSessionId, manualOverrideAt, preferredView, viewMode])

  const setManualView = (nextView: MissionViewMode) => {
    if (!activeSessionId) return
    setSessionViewStates((current) => ({
      ...current,
      [activeSessionId]: {
        viewMode: nextView,
        manualOverrideAt: Date.now(),
      },
    }))
  }

  if (!hasMissionState) {
    return (
      <div data-testid="mission-page" className="flex h-full min-h-0 flex-col">
        <ChatPage />
      </div>
    )
  }

  return (
    <div
      data-testid="mission-page"
      className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden"
    >
      <div className="border-b border-border px-4 py-2">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-3">
          <h1 className="shrink-0 text-sm font-semibold text-foreground">Mission</h1>
          <div
            data-testid="mission-view-toggle"
            className="inline-flex items-center gap-1 rounded-lg border border-border bg-muted/40 p-0.5"
          >
            <Button
              type="button"
              size="sm"
              variant={viewMode === 'chat' ? 'secondary' : 'ghost'}
              onClick={() => setManualView('chat')}
              className="h-7 px-2.5 text-xs"
            >
              <MessagesSquare className="size-3.5" />
              Chat
            </Button>
            <Button
              type="button"
              size="sm"
              variant={viewMode === 'mission-control' ? 'secondary' : 'ghost'}
              onClick={() => setManualView('mission-control')}
              className="h-7 px-2.5 text-xs"
            >
              <PanelsLeftRight className="size-3.5" />
              Control
            </Button>
          </div>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        {viewMode === 'chat' ? (
          <ChatPage
            forceInputDisabled={inputSemantics.disabled}
            forceDisabledPlaceholder={inputSemantics.placeholder}
          />
        ) : (
          <MissionControlPanel mission={mission} />
        )}
      </div>
    </div>
  )
}
