import { useState, useCallback } from 'react'
import {
  useMessages, useIsRunning, useModel, useAutoLevel, useReasoningEffort,
  useActiveProjectDir, useActiveSessionId, useShowDebugTrace,
  useSetupScript, useIsSetupBlocked, useCustomModels,
  usePendingSendMessageIds, usePendingPermissionRequest, usePendingAskUserRequest,
  useIsCancelling,
  usePendingNewSession,
  useActions,
} from '@/store'
import ChatView from '@/components/ChatView'
import { InputBar } from '@/components/InputBar'
import { TodoPanel } from '@/components/TodoPanel'
import { DebugTracePanel } from '@/components/DebugTracePanel'
import { SessionConfigPage } from '@/components/SessionConfigPage'


export function ChatPage() {
  const messages = useMessages()
  const isRunning = useIsRunning()
  const model = useModel()
  const autoLevel = useAutoLevel()
  const reasoningEffort = useReasoningEffort()
  const activeProjectDir = useActiveProjectDir()
  const activeSessionId = useActiveSessionId()
  const pendingNewSession = usePendingNewSession()
  const showDebugTrace = useShowDebugTrace()
  const setupScript = useSetupScript()
  const isSetupBlocked = useIsSetupBlocked()
  const customModels = useCustomModels()
  const pendingSendMessageIds = usePendingSendMessageIds()
  const pendingPermissionRequest = usePendingPermissionRequest()
  const pendingAskUserRequest = usePendingAskUserRequest()
  const isCancelling = useIsCancelling()
  const {
    setModel, setAutoLevel, setReasoningEffort, handleSend, handleCancel, handleForceCancel,
    handleRetrySetupScript, handleSkipSetupScript,
    appendUiDebugTrace, handleRespondPermission, handleRespondAskUser,
  } = useActions()

  const [specChangesMode, setSpecChangesMode] = useState(false)

  const handleRequestSpecChanges = useCallback(() => {
    setSpecChangesMode(true)
  }, [])

  const handleSendWrapped = useCallback((...args: Parameters<typeof handleSend>) => {
    setSpecChangesMode(false)
    handleSend(...args)
  }, [handleSend])

  const effectiveProjectDir = pendingNewSession?.repoRoot || activeProjectDir
  const noProject = !effectiveProjectDir
  const noSession = !activeSessionId
  const disabledPlaceholder = noProject
    ? 'Select a project to start...'
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
  const inputKey = pendingNewSession ? pendingKey : (activeSessionId || 'no-session')
  const draftKey = pendingNewSession ? pendingKey : activeSessionId
  const inputDisabled = noProject || (!pendingNewSession && noSession) || (!pendingNewSession && isSetupBlocked)

  return (
    <>
      {pendingNewSession
        ? <SessionConfigPage />
        : (
          <ChatView
            messages={messages}
            isRunning={isRunning}
            noProject={noProject}
            activeProjectDir={effectiveProjectDir}
            pendingPermissionRequest={pendingPermissionRequest}
            pendingSendMessageIds={pendingSendMessageIds}
            setupScript={setupScript}
            onRetrySetupScript={() => void handleRetrySetupScript(activeSessionId)}
            onSkipSetupScript={() => handleSkipSetupScript(activeSessionId)}
            onRespondPermission={handleRespondPermission}
            onRequestSpecChanges={handleRequestSpecChanges}
          />
        )}
      {showDebugTrace && <DebugTracePanel />}
      {!pendingNewSession && <TodoPanel messages={messages} />}
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
        onUiDebug={appendUiDebugTrace}
        pendingPermissionRequest={pendingPermissionRequest}
        onRespondPermission={handleRespondPermission}
        pendingAskUserRequest={pendingAskUserRequest}
        onRespondAskUser={handleRespondAskUser}
        specChangesMode={specChangesMode}
      />
    </>
  )
}
