// The Prompts of one Session: approvals the Kernel asks for before a call runs, and the
// questions of its ask_user tool. Every connection hears each request; the first answer
// wins and every connection then hears it resolved, so a Prompt leaves here whether this
// Client answered it or another one did.
import { useCallback, useMemo, useState } from 'react'
import { useKernelConnection } from './connection-context'
import { KernelError } from './kernel-socket'
import { kernelStore, useKernelSnapshot } from './kernel-state'
import { conversationIdOf, KernelCode, type ApprovalRequest, type AskRequest } from './kernel-wire'

export interface Prompts {
  permissions: ApprovalRequest[]
  askUser: AskRequest[]
}

const NONE: Prompts = { permissions: [], askUser: [] }

export function usePrompts(sessionId: string): Prompts {
  const { approvals, asks } = useKernelSnapshot()
  const id = conversationIdOf(sessionId)
  return useMemo(() => {
    const permissions = [...approvals.values()].filter((r) => r.id === id)
    const askUser = [...asks.values()].filter((r) => r.id === id)
    return permissions.length + askUser.length === 0 ? NONE : { permissions, askUser }
  }, [approvals, asks, id])
}

/** One answer of an ask: the options picked, a text of the person's own, or both. */
export interface AskAnswer {
  questionId: string
  selected?: string[]
  text?: string
}

export interface PromptActions {
  /** `optionId` is one of the approval's own options (proceed_once, cancel, …). */
  answerPermission(permission: ApprovalRequest, optionId: string): Promise<void>
  answerQuestions(request: AskRequest, answers: AskAnswer[]): Promise<void>
  cancelQuestions(request: AskRequest): Promise<void>
  error: string | null
}

export function usePromptActions(_sessionId: string): PromptActions {
  const connection = useKernelConnection()
  const store = kernelStore(connection)
  const [error, setError] = useState<string | null>(null)

  const answer = useCallback(
    async (method: string, requestId: string, params: Record<string, unknown>) => {
      setError(null)
      try {
        await connection.call(method, { requestId, ...params })
        store.answered(requestId)
      } catch (cause) {
        // Another Client got there first; the Kernel tells every Client so, and the card goes.
        if (cause instanceof KernelError && cause.code === KernelCode.requestNotFound) {
          store.answered(requestId)
          return
        }
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    },
    [connection, store],
  )

  return {
    answerPermission: (permission, optionId) =>
      answer('approval.respond', permission.requestId, { outcome: optionId }),
    answerQuestions: (request, answers) => answer('ask.respond', request.requestId, { answers }),
    cancelQuestions: (request) => answer('ask.respond', request.requestId, { cancelled: true }),
    error,
  }
}
