import { useState, useCallback, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import {
  useAppStore,
  useMessages,
  useIsRunning,
  useModel,
  useAutoLevel,
  useReasoningEffort,
  useActiveProjectDir,
  useActiveSessionId,
  useShowDebugTrace,
  useSetupScript,
  useIsSetupBlocked,
  useCustomModels,
  usePendingSendMessageIds,
  usePendingPermissionRequest,
  usePendingAskUserRequest,
  useIsCancelling,
  usePendingNewSession,
  useIsCreatingSession,
  useWorkingState,
  useMissionModelSettings,
  useActions,
} from '@/store'
import { DEFAULT_MODEL } from '@/state/appReducer'
import ChatView from '@/components/ChatView'
import { InputBar } from '@/components/InputBar'
import { TodoPanel } from '@/components/TodoPanel'
import { DebugTracePanel } from '@/components/DebugTracePanel'
import { SessionConfigPage } from '@/components/SessionConfigPage'
import { PermissionCard } from '@/components/PermissionCard'
import { AskUserCard } from '@/components/AskUserCard'
import { isExitSpecPermission } from '@/components/SpecReviewCard'
import { getPendingSessionDraftKey } from '@/store/projectHelpers'
import { getPreferredMissionView } from '@/lib/missionPage'
import { resolveSessionRuntimeSelection } from '@/lib/missionModelState'

interface ChatPageProps {
  forceInputDisabled?: boolean
  forceDisabledPlaceholder?: string
}

export function ChatPage({
  forceInputDisabled = false,
  forceDisabledPlaceholder,
}: ChatPageProps = {}) {
  const messages = useMessages()
  const isRunning = useIsRunning()
  const workingState = useWorkingState()
  const model = useModel()
  const autoLevel = useAutoLevel()
  const reasoningEffort = useReasoningEffort()
  const missionModelSettings = useMissionModelSettings()
  const activeProjectDir = useActiveProjectDir()
  const activeSessionId = useActiveSessionId()
  const pendingNewSession = usePendingNewSession()
  const isCreatingSession = useIsCreatingSession()
  const showDebugTrace = useShowDebugTrace()
  const setupScript = useSetupScript()
  const isSetupBlocked = useIsSetupBlocked()
  const customModels = useCustomModels()
  const pendingSendMessageIds = usePendingSendMessageIds()
  const pendingPermissionRequest = usePendingPermissionRequest()
  const pendingAskUserRequest = usePendingAskUserRequest()
  const isCancelling = useIsCancelling()
  const activeSessionBuffer = useAppStore((state) =>
    activeSessionId ? state.sessionBuffers.get(activeSessionId) : undefined,
  )
  const mission = activeSessionBuffer?.mission
  const {
    setModel,
    setAutoLevel,
    setReasoningEffort,
    handleSend,
    handleCancel,
    handleForceCancel,
    handleRetrySetupScript,
    handleSkipSetupScript,
    handleRespondPermission,
    handleRespondAskUser,
  } = useActions()

  const [specChangesMode, setSpecChangesMode] = useState(false)

  const [workspacePrepSessionId, setWorkspacePrepSessionId] = useState<string | null>(null)
  const prevIsCreatingRef = useRef(isCreatingSession)
  useEffect(() => {
    const prev = prevIsCreatingRef.current
    prevIsCreatingRef.current = isCreatingSession
    if (prev && !isCreatingSession) {
      setWorkspacePrepSessionId(activeSessionId || null)
    }
  }, [activeSessionId, isCreatingSession])

  const workspacePrepStatus: 'running' | 'completed' | null = isCreatingSession
    ? 'running'
    : workspacePrepSessionId && activeSessionId === workspacePrepSessionId
      ? 'completed'
      : null

  const handleRequestSpecChanges = useCallback(() => {
    setSpecChangesMode(true)
  }, [])

  const handleSendWrapped = useCallback(
    (...args: Parameters<typeof handleSend>) => {
      setSpecChangesMode(false)
      handleSend(...args)
    },
    [handleSend],
  )

  const effectiveProjectDir =
    pendingNewSession?.projectDir || pendingNewSession?.repoRoot || activeProjectDir
  const noProject = !effectiveProjectDir
  const noSession = !activeSessionId
  const missionPreferredView = getPreferredMissionView(mission)
  const missionInputLocked = missionPreferredView === 'mission-control'
  const isMissionSession =
    activeSessionBuffer?.isMission === true || activeSessionBuffer?.sessionKind === 'mission'
  const missionRuntimeSelection = resolveSessionRuntimeSelection({
    isMission: isMissionSession,
    sessionModel: model,
    sessionReasoningEffort: reasoningEffort,
    missionModelSettings,
  })
  const missionOrchestratorModel = missionRuntimeSelection.model || DEFAULT_MODEL
  const missionWorkerModel = missionModelSettings.workerModel || missionOrchestratorModel
  const missionValidatorModel =
    missionModelSettings.validationWorkerModel || missionOrchestratorModel

  const disabledPlaceholder =
    forceDisabledPlaceholder ||
    (noProject
      ? 'Select a project to start...'
      : isCreatingSession
        ? 'Preparing workspace...'
        : pendingNewSession
          ? 'Type a message to create this session...'
          : noSession
            ? 'Create or select a session to start...'
            : setupScript?.status === 'running'
              ? 'Setup script is running...'
              : setupScript?.status === 'failed'
                ? 'Setup script failed. Retry or skip to continue.'
                : undefined)

  const pendingKey = getPendingSessionDraftKey(pendingNewSession)
  const inputKey = pendingNewSession ? pendingKey : activeSessionId || 'no-session'
  const draftKey = pendingNewSession ? pendingKey : activeSessionId
  const inputDisabled =
    forceInputDisabled ||
    missionInputLocked ||
    isCreatingSession ||
    noProject ||
    (!pendingNewSession && noSession) ||
    (!pendingNewSession && isSetupBlocked)

  const hasPermission =
    Boolean(pendingPermissionRequest) && !isExitSpecPermission(pendingPermissionRequest)
  const hasAskUser = Boolean(pendingAskUserRequest)

  return (
    <>
      {pendingNewSession ? (
        <SessionConfigPage />
      ) : (
        <ChatView
          sessionId={activeSessionId}
          messages={messages}
          isRunning={isRunning}
          noProject={noProject}
          activeProjectDir={effectiveProjectDir}
          workingState={workingState}
          pendingPermissionRequest={pendingPermissionRequest}
          pendingSendMessageIds={pendingSendMessageIds}
          setupScript={setupScript}
          workspacePrepStatus={workspacePrepStatus}
          onRetrySetupScript={() => void handleRetrySetupScript(activeSessionId)}
          onSkipSetupScript={() => handleSkipSetupScript(activeSessionId)}
          onRespondPermission={handleRespondPermission}
          onRequestSpecChanges={handleRequestSpecChanges}
        />
      )}
      {showDebugTrace && <DebugTracePanel />}
      {!pendingNewSession && <TodoPanel messages={messages} />}
      <AnimatePresence mode="wait" initial={false}>
        {hasPermission && pendingPermissionRequest ? (
          <motion.div
            key="permission"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
          >
            <PermissionCard
              request={pendingPermissionRequest}
              onRespond={handleRespondPermission}
            />
          </motion.div>
        ) : hasAskUser && pendingAskUserRequest ? (
          <motion.div
            key="askuser"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
          >
            <AskUserCard request={pendingAskUserRequest} onRespond={handleRespondAskUser} />
          </motion.div>
        ) : (
          <motion.div
            key={`input-${inputKey}`}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.15, ease: [0.16, 1, 0.3, 1] }}
          >
            <InputBar
              key={inputKey}
              draftKey={draftKey}
              model={model}
              readonlyModelId={isMissionSession ? missionOrchestratorModel : undefined}
              readonlyMissionModels={
                isMissionSession
                  ? {
                      orchestrator: missionOrchestratorModel,
                      worker: missionWorkerModel,
                      validator: missionValidatorModel,
                    }
                  : undefined
              }
              autoLevel={autoLevel}
              reasoningEffort={reasoningEffort}
              customModels={customModels}
              onModelChange={setModel}
              onAutoLevelChange={setAutoLevel}
              onReasoningEffortChange={setReasoningEffort}
              onSend={handleSendWrapped}
              onCancel={handleCancel}
              onForceCancel={handleForceCancel}
              isCancelling={isCancelling}
              isRunning={isRunning}
              disabled={inputDisabled}
              disabledPlaceholder={disabledPlaceholder}
              activeProjectDir={effectiveProjectDir}
              specChangesMode={specChangesMode}
            />
          </motion.div>
        )}
      </AnimatePresence>
    </>
  )
}
