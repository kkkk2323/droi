// The Client's side of the Gateway's folder requests: what a folder on the computer is, what
// its working tree changed, and a worktree made for a new Session.
import {
  GATEWAY_GIT_CHANGES_PATH,
  GATEWAY_WORKSPACE_PATH,
  GATEWAY_WORKTREE_PATH,
  gatewayWorkspaceUrl,
  type GitChangesFound,
  type WorkspaceFound,
  type WorktreeMade,
} from './gateway'

export interface Workspaces {
  /** The folder as the computer resolves it; throws with the computer's reason when it cannot be one. */
  inspect(path: string): Promise<WorkspaceFound>
  changes(path: string): Promise<GitChangesFound>
  addWorktree(path: string, base: string, prompt: string): Promise<WorktreeMade>
}

export function createWorkspaces(
  config: { gatewayUrl: string; pairingToken: string | null },
  fetchImpl: typeof fetch,
): Workspaces {
  const ask = async <T>(
    method: 'GET' | 'POST',
    endpoint: string,
    query: Record<string, string>,
  ): Promise<T> => {
    const response = await fetchImpl(
      gatewayWorkspaceUrl(config.gatewayUrl, config.pairingToken ?? '', endpoint, query),
      { method, cache: 'no-store' },
    )
    if (response.ok) return (await response.json()) as T
    const reason = await response
      .json()
      .then((body: { error?: unknown }) => (typeof body.error === 'string' ? body.error : null))
      .catch(() => null)
    throw new Error(reason ?? `The computer did not answer (HTTP ${response.status}).`)
  }
  return {
    inspect: (path) => ask('GET', GATEWAY_WORKSPACE_PATH, { path }),
    changes: (path) => ask('GET', GATEWAY_GIT_CHANGES_PATH, { path }),
    addWorktree: (path, base, prompt) => ask('POST', GATEWAY_WORKTREE_PATH, { path, base, prompt }),
  }
}
