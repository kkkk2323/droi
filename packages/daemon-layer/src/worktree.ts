// Git worktrees for new Sessions. The Daemon makes and removes them itself;
// the Client asks on `daemon.initialize_session` and shows what comes back.
export type WorktreeLifecycle = 'ephemeral' | 'persistent'

/** The worktree a Session runs in, as the Daemon lists it (gone once removed). */
export interface SessionWorktree {
  path: string
  branch: string
  /** Null for a checkout the Daemon adopted rather than made; nothing may treat it as ephemeral. */
  lifecycle: WorktreeLifecycle | null
  /** The main checkout the worktree was made from. */
  repoRoot: string
}

/** What a new Session asks of the Daemon: a fresh worktree of its Workspace. */
export interface WorktreeRequest {
  lifecycle: WorktreeLifecycle
  /** The first message; the Daemon names the new branch after its words. */
  promptSlug?: string
  /** Branch the worktree starts from; the Workspace's current one when absent. */
  baseBranch?: string
}

export const WORKTREE_LIFECYCLES: readonly WorktreeLifecycle[] = ['ephemeral', 'persistent']

export const DEFAULT_WORKTREE_LIFECYCLE: WorktreeLifecycle = 'ephemeral'

export function isWorktreeLifecycle(value: unknown): value is WorktreeLifecycle {
  return value === 'ephemeral' || value === 'persistent'
}

// The wording is the Factory App's.
export const WORKTREE_LIFECYCLE_LABELS: Record<WorktreeLifecycle, string> = {
  ephemeral: 'Ephemeral',
  persistent: 'Persistent',
}
export const WORKTREE_LIFECYCLE_DESCRIPTIONS: Record<WorktreeLifecycle, string> = {
  ephemeral: 'Single-session worktrees that are cleaned up automatically.',
  persistent: 'Multi-session worktrees that can only be deleted manually.',
}

/** Shown before archiving a Session in an ephemeral worktree. */
export const ARCHIVE_WORKTREE_WARNING = 'Archiving this session will delete its worktree.'

export function archiveDeletesWorktree(session: {
  worktree: Pick<SessionWorktree, 'lifecycle'> | null
}): boolean {
  return session.worktree?.lifecycle === 'ephemeral'
}

/** A creation can take a while: the Daemon makes the checkout and runs its setup first. */
export const WORKTREE_INIT_TIMEOUT_MS = 120_000

/** The `daemon.initialize_session` params that ask for a worktree. */
export function worktreeParams(request: WorktreeRequest) {
  const slug = request.promptSlug?.trim()
  return {
    worktree: true as const,
    worktreeLifecycle: request.lifecycle as never,
    ...(slug ? { worktreePromptSlug: slug } : {}),
    ...(request.baseBranch
      ? { worktreeBaseBranch: request.baseBranch, worktreeBranchMode: 'copy' as never }
      : {}),
  }
}
