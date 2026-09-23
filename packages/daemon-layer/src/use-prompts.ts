// Pending Prompts (permission requests and ask-user questions) for one Session,
// read from the SDK controller. The Daemon sends every Prompt to every attached
// Client and resolves it for all of them when the first answer lands (ADR
// 0003); the controller mirrors that, so a Prompt disappears here whether this
// Client answered it or another one did.
import type { PendingAskUserRequest, PendingPermission } from '@factory/droid-sdk'
import { useCallback, useRef, useState, useSyncExternalStore } from 'react'
import { useDaemonConnection } from './connection-context'

export interface Prompts {
  permissions: PendingPermission[]
  askUser: PendingAskUserRequest[]
}

const NONE: Prompts = { permissions: [], askUser: [] }

export function usePrompts(sessionId: string): Prompts {
  const { controller } = useDaemonConnection()
  const version = useRef(0)
  const snapshot = useRef<{ version: number; value: Prompts }>({ version: -1, value: NONE })

  const subscribe = useCallback(
    (listener: () => void) => {
      const bump = () => {
        version.current += 1
        listener()
      }
      controller.on('permissionRequested', bump)
      controller.on('permissionResolved', bump)
      controller.on('permissionTimeout', bump)
      controller.on('askUserRequested', bump)
      controller.on('askUserResolved', bump)
      controller.on('sessionLoaded', bump)
      return () => {
        controller.off('permissionRequested', bump)
        controller.off('permissionResolved', bump)
        controller.off('permissionTimeout', bump)
        controller.off('askUserRequested', bump)
        controller.off('askUserResolved', bump)
        controller.off('sessionLoaded', bump)
      }
    },
    [controller],
  )

  const getSnapshot = useCallback((): Prompts => {
    if (snapshot.current.version === version.current) return snapshot.current.value
    const value: Prompts = {
      permissions: controller.getPendingPermissionsForSession(sessionId),
      askUser: controller.getPendingAskUserRequests().filter((r) => r.sessionId === sessionId),
    }
    snapshot.current = { version: version.current, value }
    return value
  }, [controller, sessionId])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

export interface PromptActions {
  answerPermission(permission: PendingPermission, selectedOption: string): Promise<void>
  answerQuestions(
    request: PendingAskUserRequest,
    answers: Array<{ index: number; question: string; answer: string }>,
  ): Promise<void>
  cancelQuestions(request: PendingAskUserRequest): Promise<void>
  error: string | null
}

export function usePromptActions(sessionId: string): PromptActions {
  const { controller } = useDaemonConnection()
  const [error, setError] = useState<string | null>(null)

  const answerPermission = useCallback(
    async (permission: PendingPermission, selectedOption: string) => {
      try {
        await controller.respondToPermission({
          permissionId: permission.requestId,
          sessionId,
          selectedOption: selectedOption as never,
        })
      } catch (cause) {
        // Another Client got there first; the Daemon already told us so via
        // permission_resolved and the card is gone. Nothing to show.
        if (!isAlreadyResolved(cause)) setError(describe(cause))
      }
    },
    [controller, sessionId],
  )

  const answerQuestions = useCallback(
    async (request: PendingAskUserRequest, answers: PromptAnswers) => {
      try {
        await controller.respondToAskUser({
          requestId: request.requestId,
          sessionId,
          result: { answers },
        })
      } catch (cause) {
        if (!isAlreadyResolved(cause)) setError(describe(cause))
      }
    },
    [controller, sessionId],
  )

  const cancelQuestions = useCallback(
    async (request: PendingAskUserRequest) => {
      try {
        await controller.respondToAskUser({
          requestId: request.requestId,
          sessionId,
          result: { cancelled: true, answers: [] },
        })
      } catch (cause) {
        if (!isAlreadyResolved(cause)) setError(describe(cause))
      }
    },
    [controller, sessionId],
  )

  return { answerPermission, answerQuestions, cancelQuestions, error }
}

type PromptAnswers = Array<{ index: number; question: string; answer: string }>

function isAlreadyResolved(error: unknown): boolean {
  const message = describe(error)
  return /no pending|not found/i.test(message)
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
