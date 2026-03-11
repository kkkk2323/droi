import { useEffect, useMemo, useState } from 'react'
import { PanelsLeftRight } from 'lucide-react'

import { ChatPage } from './ChatPage'
import { MissionControlPanel } from '@/components/mission-control-panel'
import { MissionWorkerListPanel } from '@/components/mission-worker-list-panel'
import { Button } from '@/components/ui/button'
import { getMissionInputSemantics } from '@/lib/missionUiSemantics'
import { useAppStore, useActiveSessionId } from '@/store'
import type { MissionRuntimeSnapshot, RuntimeLogEntry } from '@/types'
import {
  getMissionSessionViewState,
  getPreferredMissionView,
  shouldApplyMissionAutoSwitch,
  type MissionSessionViewState,
  type MissionViewMode,
} from '@/lib/missionPage'

const EMPTY_RUNTIME_LOGS: RuntimeLogEntry[] = []

export function MissionPage() {
  const activeSessionId = useActiveSessionId()
  const mission = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId)?.mission : undefined,
  )
  const runtimeLogs = useAppStore((state) =>
    activeSessionId
      ? ((state.sessionBuffers.get(activeSessionId)?.runtimeLogs as
          | RuntimeLogEntry[]
          | undefined) ?? EMPTY_RUNTIME_LOGS)
      : EMPTY_RUNTIME_LOGS,
  )
  const runtimeLogState = useAppStore((state) =>
    activeSessionId
      ? ((state.sessionBuffers.get(activeSessionId)?.runtimeLogState as
          | MissionRuntimeSnapshot
          | undefined) ?? undefined)
      : undefined,
  )
  const sessionBuffer = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId) : undefined,
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
  const selectedWorkerSessionId = sessionViewState.selectedWorkerSessionId

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

  const setManualView = (nextView: MissionViewMode, params?: { workerSessionId?: string }) => {
    if (!activeSessionId) return
    setSessionViewStates((current) => ({
      ...current,
      [activeSessionId]: {
        viewMode: nextView,
        manualOverrideAt: Date.now(),
        selectedWorkerSessionId:
          nextView === 'worker-detail'
            ? params?.workerSessionId
            : nextView === 'worker-list'
              ? current[activeSessionId]?.selectedWorkerSessionId
              : undefined,
      },
    }))
  }

  const handleOpenWorkerDetail = (workerSessionId: string) => {
    setManualView('worker-detail', { workerSessionId })
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
      className="relative flex h-full min-h-0 min-w-0 flex-col overflow-hidden"
    >
      {viewMode === 'chat' ? (
        <>
          <ChatPage
            forceInputDisabled={inputSemantics.disabled}
            forceDisabledPlaceholder={inputSemantics.placeholder}
          />
          <div className="absolute right-4 top-1 z-20">
            <Button
              type="button"
              size="sm"
              variant="outline"
              data-testid="mission-view-toggle"
              onClick={() => setManualView('mission-control')}
              className="h-7 gap-1 bg-background/90 px-2 text-xs shadow-sm backdrop-blur"
            >
              <PanelsLeftRight className="size-3.5" />
              Control
            </Button>
          </div>
        </>
      ) : viewMode === 'mission-control' ? (
        <MissionControlPanel
          mission={mission}
          runtimeLogs={runtimeLogs}
          runtimeLogState={runtimeLogState}
          onViewChange={setManualView}
        />
      ) : (
        <MissionWorkerListPanel
          mission={mission}
          sessionId={activeSessionId}
          missionDir={sessionBuffer?.missionDir}
          missionBaseSessionId={sessionBuffer?.missionBaseSessionId}
          projectDir={sessionBuffer?.projectDir}
          selectedWorkerSessionId={
            viewMode === 'worker-detail' ? selectedWorkerSessionId : undefined
          }
          onSelectWorker={handleOpenWorkerDetail}
          onBackToList={() => setManualView('worker-list')}
          onBackToMission={() => setManualView('mission-control')}
        />
      )}
    </div>
  )
}
