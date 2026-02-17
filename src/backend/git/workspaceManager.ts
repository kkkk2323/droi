import { execFile } from 'child_process'
import { mkdir, realpath, stat } from 'fs/promises'
import { basename, dirname, join, resolve, sep } from 'path'

export type WorkspaceType = 'branch' | 'worktree'

export interface WorkspaceInfo {
  repoRoot: string
  projectDir: string
  branch: string
  workspaceType: WorkspaceType
  baseBranch?: string
}

function runGit(args: string[], cwd: string, opts?: { timeoutMs?: number }): Promise<string> {
  const timeoutMs = typeof opts?.timeoutMs === 'number' ? opts.timeoutMs : 8000
  return new Promise((resolvePromise, rejectPromise) => {
    execFile('git', args, { cwd, timeout: timeoutMs }, (err, stdout, stderr) => {
      if (err) {
        rejectPromise(new Error(String(stderr || err.message || 'git command failed').trim()))
        return
      }
      resolvePromise(String(stdout || '').trim())
    })
  })
}

function sanitizeBranchForPath(branch: string): string {
  return String(branch || '')
    .trim()
    .replace(/[\\/]+/g, '--')
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export async function getRepoRoot(projectDir: string): Promise<string> {
  const cwd = await normalizeFsPath(projectDir)
  const commonDirRaw = await runGit(['rev-parse', '--git-common-dir'], cwd)
  const commonDirAbs = resolve(cwd, commonDirRaw)
  if (basename(commonDirAbs) === '.git') return normalizeFsPath(dirname(commonDirAbs))
  // Best-effort fallback for unusual repo layouts (e.g. bare repos).
  return normalizeFsPath(dirname(commonDirAbs))
}

export async function getWorktreeRoot(projectDir: string): Promise<string> {
  const cwd = await normalizeFsPath(projectDir)
  const out = await runGit(['rev-parse', '--show-toplevel'], cwd)
  return normalizeFsPath(out)
}

export async function getCurrentBranch(projectDir: string): Promise<string> {
  return runGit(['rev-parse', '--abbrev-ref', 'HEAD'], projectDir)
}

export async function listBranches(projectDir: string): Promise<string[]> {
  const out = await runGit(['for-each-ref', '--format=%(refname:short)', 'refs/heads'], projectDir)
  return out.split('\n').map((x) => x.trim()).filter(Boolean)
}

export async function checkoutBranch(projectDir: string, branch: string): Promise<void> {
  await runGit(['checkout', branch], projectDir)
}

export async function createBranch(projectDir: string, branch: string, baseBranch?: string): Promise<void> {
  const args = ['checkout', '-b', branch]
  if (baseBranch && baseBranch.trim()) args.push(baseBranch.trim())
  await runGit(args, projectDir)
}

export function resolveSiblingWorktreePath(repoRoot: string, branch: string): string {
  const safeBranch = sanitizeBranchForPath(branch) || 'worktree'
  return join(repoRoot, '.worktrees', safeBranch)
}

function isPathInside(parentDir: string, maybeChild: string): boolean {
  const parent = resolve(parentDir)
  const child = resolve(maybeChild)
  if (child === parent) return false
  const prefix = parent.endsWith(sep) ? parent : `${parent}${sep}`
  return child.startsWith(prefix)
}

async function normalizeFsPath(p: string): Promise<string> {
  try {
    return await realpath(p)
  } catch {
    return resolve(p)
  }
}

async function pathExists(p: string): Promise<boolean> {
  try {
    await stat(p)
    return true
  } catch {
    return false
  }
}

async function isRegisteredWorktree(repoRoot: string, worktreeDir: string): Promise<boolean> {
  const target = resolve(worktreeDir)
  const out = await runGit(['worktree', 'list', '--porcelain'], repoRoot, { timeoutMs: 60000 })
  for (const rawLine of out.split('\n')) {
    const line = rawLine.trim()
    if (!line.startsWith('worktree ')) continue
    const listed = line.slice('worktree '.length).trim()
    if (!listed) continue
    if (resolve(listed) === target) return true
  }
  return false
}

export async function createWorktree(params: {
  repoRoot: string
  branch: string
  worktreePath: string
  baseBranch?: string
  useExistingBranch?: boolean
}): Promise<void> {
  await mkdir(dirname(params.worktreePath), { recursive: true })
  const args = ['worktree', 'add']
  if (!params.useExistingBranch) args.push('-b', params.branch)
  args.push(params.worktreePath)
  if (params.useExistingBranch) args.push(params.branch)
  else if (params.baseBranch && params.baseBranch.trim()) args.push(params.baseBranch.trim())
  await runGit(args, params.repoRoot)
}

export async function getWorkspaceInfo(projectDir: string): Promise<WorkspaceInfo> {
  const repoRoot = await getRepoRoot(projectDir)
  const worktreeRoot = await getWorktreeRoot(projectDir)
  const branch = await getCurrentBranch(projectDir)
  const workspaceType: WorkspaceType = resolve(worktreeRoot) === resolve(repoRoot) ? 'branch' : 'worktree'
  return {
    repoRoot,
    projectDir: resolve(worktreeRoot),
    branch,
    workspaceType,
  }
}

export async function switchWorkspaceBranch(params: { projectDir: string; branch: string }): Promise<WorkspaceInfo> {
  await checkoutBranch(params.projectDir, params.branch)
  return getWorkspaceInfo(params.projectDir)
}

export async function createWorkspace(params: {
  projectDir: string
  mode: 'branch' | 'worktree'
  branch: string
  baseBranch?: string
  useExistingBranch?: boolean
}): Promise<WorkspaceInfo> {
  const repoRoot = await getRepoRoot(params.projectDir)

  if (params.mode === 'branch') {
    if (params.useExistingBranch) await checkoutBranch(params.projectDir, params.branch)
    else await createBranch(params.projectDir, params.branch, params.baseBranch)
    return { ...(await getWorkspaceInfo(params.projectDir)), baseBranch: params.baseBranch }
  }

  const worktreePath = resolveSiblingWorktreePath(repoRoot, params.branch)
  await createWorktree({
    repoRoot,
    branch: params.branch,
    worktreePath,
    baseBranch: params.baseBranch,
    useExistingBranch: params.useExistingBranch,
  })
  return { ...(await getWorkspaceInfo(worktreePath)), baseBranch: params.baseBranch }
}

export async function removeWorktree(params: { repoRoot: string; worktreeDir: string; force?: boolean }): Promise<void> {
  const repoRoot = await normalizeFsPath(params.repoRoot)
  const worktreeDir = await normalizeFsPath(params.worktreeDir)
  const containerDir = join(repoRoot, '.worktrees')

  if (resolve(repoRoot) === resolve(worktreeDir)) {
    throw new Error('Refusing to remove repo root as a worktree')
  }
  if (!isPathInside(containerDir, worktreeDir)) {
    throw new Error(`Refusing to remove worktree outside ${containerDir}`)
  }

  const args = ['worktree', 'remove']
  if (params.force) args.push('--force')
  args.push(worktreeDir)

  const registered = await isRegisteredWorktree(repoRoot, worktreeDir)
  const exists = await pathExists(worktreeDir)

  if (registered && exists) {
    await runGit(args, repoRoot, { timeoutMs: 60000 })
  }

  await runGit(['worktree', 'prune'], repoRoot, { timeoutMs: 60000 })
}

export async function pushBranch(params: { projectDir: string; remote?: string; branch?: string }): Promise<{ remote: string; branch: string }> {
  const projectDir = resolve(params.projectDir)
  const remote = typeof params.remote === 'string' && params.remote.trim() ? params.remote.trim() : 'origin'
  const branch = typeof params.branch === 'string' && params.branch.trim()
    ? params.branch.trim()
    : await getCurrentBranch(projectDir)

  if (!branch || branch === 'HEAD') throw new Error('Cannot push detached HEAD')

  await runGit(['push', '--set-upstream', remote, branch], projectDir, { timeoutMs: 120000 })
  return { remote, branch }
}
