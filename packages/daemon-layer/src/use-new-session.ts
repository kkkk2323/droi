// Create a Session in a Workspace. Recent Workspaces come from the Sessions the computer
// lists; a typed path is checked by the computer (the Gateway's `/workspace`) before use. A
// worktree is made by the Gateway with git on the computer, and the conversation works in it.
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { clientAutonomy, kernelAutonomy } from './autonomy'
import type { KernelConnection } from './connection'
import { useKernelConnection } from './connection-context'
import { kernelStore } from './kernel-state'
import { sessionIdOf, type ModelInfo } from './kernel-wire'
import {
  SESSIONS_QUERY_KEY,
  isScratch,
  isScratchSession,
  workspaceLabel,
  workspaceOf,
  type SessionSummary,
  type SessionTag,
} from './sessions'
import type { ToolMode } from './tool-mode'
import { uuid } from './uuid'
import type { SessionWorktree, WorktreeRequest } from './worktree'

export interface RecentWorkspace {
  path: string
  label: string
  lastUsedAt: number
}

/** Most recent first, one entry per Workspace path; Scratch Workspaces are left out. */
export function recentWorkspaces(sessions: readonly SessionSummary[]): RecentWorkspace[] {
  const byPath = new Map<string, RecentWorkspace>()
  for (const session of sessions) {
    if (isScratchSession(session)) continue
    const path = workspaceOf(session)
    if (!path) continue
    const existing = byPath.get(path)
    if (!existing || existing.lastUsedAt < session.updatedAt) {
      byPath.set(path, { path, label: workspaceLabel(path), lastUsedAt: session.updatedAt })
    }
  }
  return [...byPath.values()].sort((a, b) => b.lastUsedAt - a.lastUsedAt)
}

export interface NewSessionSettings {
  modelId: string | null
  reasoningEffort: string | null
  autonomyLevel: string | null
  /** `direct_only` takes the scripts (the codemode extension) away; null keeps them. */
  toolExecutionMode?: ToolMode | null
}

export interface NewSessionActions {
  /** Resolves to the Session's id and, for a worktree, the checkout the computer made. */
  create(
    path: string,
    settings?: NewSessionSettings,
    tags?: SessionTag[],
    worktree?: WorktreeRequest,
  ): Promise<{ sessionId: string; worktree: SessionWorktree | null } | null>
  isCreating: boolean
  error: string | null
}

/** The extension that offers a conversation's model scripts, on by default. */
const CODEMODE = 'codemode'

/** The agent a new conversation asks for: a model the Kernel offers, with a thinking level it takes. */
export function agentFor(
  models: readonly ModelInfo[],
  settings: NewSessionSettings | undefined,
): Record<string, unknown> | null {
  const agent: Record<string, unknown> = {}
  const model = settings?.modelId ? models.find((m) => m.id === settings.modelId) : undefined
  if (model) {
    agent['model'] = { provider: model.provider, modelId: model.id }
    const effort = settings?.reasoningEffort
    if (effort && model.thinking?.includes(effort)) agent['thinkingLevel'] = effort
  }
  if (settings?.toolExecutionMode === 'direct_only') agent['extensions'] = { remove: [CODEMODE] }
  return Object.keys(agent).length > 0 ? agent : null
}

/**
 * Checks the folder, makes the worktree asked for, and creates the conversation there.
 * Resolves to the Session and its worktree, or to the message to show when the folder is
 * unusable.
 */
export async function openSession(
  connection: Pick<KernelConnection, 'call' | 'workspaces'>,
  path: string,
  options: {
    settings?: NewSessionSettings
    tags?: SessionTag[]
    worktree?: WorktreeRequest
    models?: readonly ModelInfo[]
    /** The autonomy new conversations have by default, in the Client's words. */
    defaultAutonomy?: string | null
  } = {},
): Promise<{ sessionId: string; worktree: SessionWorktree | null } | { error: string }> {
  let found
  try {
    found = await connection.workspaces.inspect(path)
  } catch (cause) {
    return { error: cause instanceof Error ? cause.message : String(cause) }
  }
  const { settings, tags, worktree } = options
  const scratch = isScratch(tags)
  let cwd = found.path
  let made: SessionWorktree | null = null
  if (worktree && found.isGitRepository && !scratch) {
    const added = await connection.workspaces.addWorktree(
      cwd,
      worktree.baseBranch ?? '',
      worktree.promptSlug?.trim() ?? '',
    )
    cwd = added.path
    made = {
      path: added.path,
      branch: added.branch,
      lifecycle: worktree.lifecycle,
      repoRoot: added.repoRoot,
    }
  }
  const agent = agentFor(options.models ?? [], settings)
  const created = await connection.call<{ conversationId: number }>('conversation.create', {
    cwd,
    kind: scratch ? 'scratch' : 'chat',
    requestId: uuid(),
    ...(agent ? { agent } : {}),
  })
  // A level picked on the page is the conversation's own, set before its first message can run
  // a tool; the default's is left to follow the default.
  const level = settings?.autonomyLevel
  if (level && level !== options.defaultAutonomy) {
    await connection.call('policy.set', {
      id: created.conversationId,
      policy: { autonomy: kernelAutonomy(level) },
    })
  }
  return { sessionId: sessionIdOf(created.conversationId), worktree: made }
}

export function useNewSession(): NewSessionActions {
  const connection = useKernelConnection()
  const queryClient = useQueryClient()
  const [isCreating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const create = useCallback(
    async (
      path: string,
      settings?: NewSessionSettings,
      tags?: SessionTag[],
      worktree?: WorktreeRequest,
    ) => {
      const trimmed = path.trim()
      if (!trimmed) {
        setError('Enter a directory path.')
        return null
      }
      setCreating(true)
      setError(null)
      try {
        const kernel = kernelStore(connection).snapshot()
        const opened = await openSession(connection, trimmed, {
          ...(settings ? { settings } : {}),
          ...(tags?.length ? { tags } : {}),
          ...(worktree ? { worktree } : {}),
          models: kernel.info?.models ?? [],
          defaultAutonomy: clientAutonomy(kernel.policies.get(null)?.autonomy),
        })
        if ('error' in opened) {
          setError(opened.error)
          return null
        }
        await queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
        return opened
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return null
      } finally {
        setCreating(false)
      }
    },
    [connection, queryClient],
  )

  return { create, isCreating, error }
}
