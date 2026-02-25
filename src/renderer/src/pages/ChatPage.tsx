import { useState, useCallback, useEffect, useRef } from 'react'
import {
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
  useActions,
} from '@/store'
import ChatView from '@/components/ChatView'
import { InputBar } from '@/components/InputBar'
import { TodoPanel } from '@/components/TodoPanel'
import { DebugTracePanel } from '@/components/DebugTracePanel'
import { SessionConfigPage } from '@/components/SessionConfigPage'
import { PermissionCard } from '@/components/PermissionCard'
import { AskUserCard } from '@/components/AskUserCard'
import { isExitSpecPermission } from '@/components/SpecReviewCard'

export function ChatPage() {
  const messages = useMessages()
  const isRunning = useIsRunning()
  const workingState = useWorkingState()
  const model = useModel()
  const autoLevel = useAutoLevel()
  const reasoningEffort = useReasoningEffort()
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

  const effectiveProjectDir = pendingNewSession?.repoRoot || activeProjectDir
  const noProject = !effectiveProjectDir
  const noSession = !activeSessionId
  const disabledPlaceholder = noProject
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
              : undefined

  const pendingKey = pendingNewSession?.repoRoot ? `pending:${pendingNewSession.repoRoot}` : ''
  const inputKey = pendingNewSession ? pendingKey : activeSessionId || 'no-session'
  const draftKey = pendingNewSession ? pendingKey : activeSessionId
  const inputDisabled =
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
      {hasPermission && pendingPermissionRequest ? (
        <PermissionCard request={pendingPermissionRequest} onRespond={handleRespondPermission} />
      ) : hasAskUser && pendingAskUserRequest ? (
        <AskUserCard request={pendingAskUserRequest} onRespond={handleRespondAskUser} />
      ) : (
        <InputBar
          key={inputKey}
          draftKey={draftKey}
          model={model}
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
      )}
    </>
  )
}
