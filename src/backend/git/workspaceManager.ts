import { execFile } from 'child_process'
import { mkdir, realpath, stat } from 'fs/promises'
import { basename, dirname, join, relative, resolve, sep } from 'path'

export type WorkspaceType = 'branch' | 'worktree'

export interface WorkspaceInfo {
  repoRoot: string
  projectDir: string
  workspaceDir: string
  branch: string
  workspaceType: WorkspaceType
  cwdSubpath?: string
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

function normalizeCwdSubpath(subpath?: string): string {
  const raw = String(subpath || '').trim()
  if (!raw) return ''

  const normalized = raw.replace(/[\\/]+/g, sep)
  if (!normalized || normalized === '.') return ''

  const rooted = resolve(sep, normalized)
  const rel = relative(sep, rooted)
  if (!rel || rel === '.') return ''
  if (rel === '..' || rel.startsWith(`..${sep}`) || rel.includes(`${sep}..${sep}`)) return ''
  return rel
}

function deriveCwdSubpath(workspaceDir: string, projectDir: string): string {
  const relPath = relative(resolve(workspaceDir), resolve(projectDir))
  if (!relPath || relPath === '.') return ''
  if (relPath === '..' || relPath.startsWith(`..${sep}`) || relPath.includes(`${sep}..${sep}`))
    return ''
  return normalizeCwdSubpath(relPath)
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
  return out
    .split('\n')
    .map((x) => x.trim())
    .filter(Boolean)
}

export async function checkoutBranch(projectDir: string, branch: string): Promise<void> {
  await runGit(['checkout', branch], projectDir)
}

export async function createBranch(
  projectDir: string,
  branch: string,
  baseBranch?: string,
): Promise<void> {
  let effectiveBase = baseBranch?.trim() || ''
  if (effectiveBase) {
    const fetched = await fetchRemoteBranch(projectDir, effectiveBase)
    if (fetched) effectiveBase = `origin/${effectiveBase}`
  }
  const args = ['checkout', '-b', branch]
  if (effectiveBase) args.push(effectiveBase)
  await runGit(args, projectDir)
  await clearBranchUpstream(projectDir, branch)
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

async function isDirectoryPath(p: string): Promise<boolean> {
  try {
    return (await stat(p)).isDirectory()
  } catch {
    return false
  }
}

async function resolveEffectiveProjectDir(
  workspaceDir: string,
  cwdSubpath?: string,
): Promise<string> {
  const normalizedWorkspaceDir = await normalizeFsPath(workspaceDir)
  const subpath = normalizeCwdSubpath(cwdSubpath)
  if (!subpath) return normalizedWorkspaceDir

  const candidate = join(normalizedWorkspaceDir, subpath)
  if (!(await isDirectoryPath(candidate))) return normalizedWorkspaceDir
  return normalizeFsPath(candidate)
}

async function buildWorkspaceInfo(params: {
  workspaceDir: string
  projectDir?: string
  cwdSubpath?: string
  baseBranch?: string
}): Promise<WorkspaceInfo> {
  const workspaceDir = await normalizeFsPath(params.workspaceDir)
  const projectDir = params.projectDir ? await normalizeFsPath(params.projectDir) : undefined
  const repoRoot = await getRepoRoot(workspaceDir)
  const branch = await getCurrentBranch(workspaceDir)
  const workspaceType: WorkspaceType =
    resolve(workspaceDir) === resolve(repoRoot) ? 'branch' : 'worktree'
  const cwdSubpath = normalizeCwdSubpath(
    params.cwdSubpath || (projectDir ? deriveCwdSubpath(workspaceDir, projectDir) : ''),
  )
  const effectiveProjectDir = await resolveEffectiveProjectDir(workspaceDir, cwdSubpath)

  return {
    repoRoot,
    projectDir: effectiveProjectDir,
    workspaceDir,
    branch,
    workspaceType,
    ...(cwdSubpath ? { cwdSubpath } : {}),
    ...(params.baseBranch ? { baseBranch: params.baseBranch } : {}),
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

async function localBranchExists(repoRoot: string, branch: string): Promise<boolean> {
  const b = String(branch || '').trim()
  if (!b) return false
  try {
    await runGit(['show-ref', '--verify', `refs/heads/${b}`], repoRoot)
    return true
  } catch {
    return false
  }
}

async function isBranchCheckedOutInAnyWorktree(repoRoot: string, branch: string): Promise<boolean> {
  const b = String(branch || '').trim()
  if (!b) return false
  const target = `refs/heads/${b}`
  const out = await runGit(['worktree', 'list', '--porcelain'], repoRoot, { timeoutMs: 60000 })
  for (const rawLine of out.split('\n')) {
    const line = rawLine.trim()
    if (!line.startsWith('branch ')) continue
    const ref = line.slice('branch '.length).trim()
    if (ref === target) return true
  }
  return false
}

export async function listWorktreeBranchesInUse(params: {
  repoRoot: string
}): Promise<Array<{ branch: string; worktreeDir: string }>> {
  const repoRoot = await normalizeFsPath(params.repoRoot)
  const out = await runGit(['worktree', 'list', '--porcelain'], repoRoot, { timeoutMs: 60000 })

  type Row = { worktreeDir: string; branchRef: string }
  const rows: Row[] = []
  let cur: Row | null = null

  for (const rawLine of out.split('\n')) {
    const line = rawLine.trim()
    if (line.startsWith('worktree ')) {
      if (cur) rows.push(cur)
      cur = { worktreeDir: line.slice('worktree '.length).trim(), branchRef: '' }
      continue
    }
    if (!cur) continue
    if (line.startsWith('branch ')) {
      cur.branchRef = line.slice('branch '.length).trim()
    }
  }
  if (cur) rows.push(cur)

  const repoResolved = resolve(repoRoot)
  const outRows: Array<{ branch: string; worktreeDir: string }> = []

  for (const r of rows) {
    const worktreeDirRaw = String(r.worktreeDir || '').trim()
    const branchRef = String(r.branchRef || '').trim()
    if (!worktreeDirRaw) continue
    if (!branchRef.startsWith('refs/heads/')) continue

    const branch = branchRef.slice('refs/heads/'.length).trim()
    if (!branch) continue

    let worktreeDir = worktreeDirRaw
    try {
      worktreeDir = await normalizeFsPath(worktreeDirRaw)
    } catch {
      // ignore
    }

    // Only report branches checked out in OTHER worktrees (not the main repoRoot).
    if (resolve(worktreeDir) === repoResolved) continue

    outRows.push({ branch, worktreeDir })
  }

  // De-dupe by branch.
  const seen = new Set<string>()
  return outRows.filter((x) => {
    if (seen.has(x.branch)) return false
    seen.add(x.branch)
    return true
  })
}

async function fetchRemoteBranch(cwd: string, branch: string, remote = 'origin'): Promise<boolean> {
  try {
    await runGit(['fetch', remote, branch], cwd, { timeoutMs: 30000 })
    return true
  } catch {
    return false
  }
}

async function clearBranchUpstream(cwd: string, branch: string): Promise<void> {
  const b = String(branch || '').trim()
  if (!b) return
  try {
    await runGit(['branch', '--unset-upstream', '--', b], cwd)
  } catch {
    // Best-effort: branch may not have upstream configured.
  }
}

export async function createWorktree(params: {
  repoRoot: string
  branch: string
  worktreePath: string
  baseBranch?: string
  useExistingBranch?: boolean
}): Promise<void> {
  await mkdir(dirname(params.worktreePath), { recursive: true })

  let effectiveBase = params.baseBranch?.trim() || ''
  if (effectiveBase && !params.useExistingBranch) {
    const fetched = await fetchRemoteBranch(params.repoRoot, effectiveBase)
    if (fetched) effectiveBase = `origin/${effectiveBase}`
  }

  const args = ['worktree', 'add']
  if (!params.useExistingBranch) args.push('-b', params.branch)
  args.push(params.worktreePath)
  if (params.useExistingBranch) args.push(params.branch)
  else if (effectiveBase) args.push(effectiveBase)
  await runGit(args, params.repoRoot)
  if (!params.useExistingBranch) await clearBranchUpstream(params.repoRoot, params.branch)
}

export async function getWorkspaceInfo(
  projectDir: string,
  opts?: { cwdSubpath?: string },
): Promise<WorkspaceInfo> {
  const workspaceDir = await getWorktreeRoot(projectDir)
  return buildWorkspaceInfo({ workspaceDir, projectDir, cwdSubpath: opts?.cwdSubpath })
}

async function pullBranch(projectDir: string, branch: string): Promise<void> {
  const b = String(branch || '').trim()
  if (!b) return
  try {
    // Pull from the upstream tracking branch if configured
    await runGit(['pull'], projectDir, { timeoutMs: 60000 })
  } catch {
    // Best-effort: pull may fail due to network, conflicts, or no upstream configured
  }
}

export async function switchWorkspaceBranch(params: {
  workspaceDir: string
  branch: string
  cwdSubpath?: string
}): Promise<WorkspaceInfo> {
  const workspaceDir = await getWorktreeRoot(params.workspaceDir)
  await checkoutBranch(workspaceDir, params.branch)
  await pullBranch(workspaceDir, params.branch)
  return buildWorkspaceInfo({
    workspaceDir,
    cwdSubpath: params.cwdSubpath,
  })
}

export async function createWorkspace(params: {
  workspaceDir: string
  projectDir: string
  mode: 'branch' | 'worktree'
  branch: string
  baseBranch?: string
  useExistingBranch?: boolean
  cwdSubpath?: string
}): Promise<WorkspaceInfo> {
  const workspaceDir = await getWorktreeRoot(params.workspaceDir)
  const repoRoot = await getRepoRoot(workspaceDir)
  const normalizedProjectDir = await normalizeFsPath(params.projectDir)
  const cwdSubpath = normalizeCwdSubpath(
    params.cwdSubpath || deriveCwdSubpath(workspaceDir, normalizedProjectDir),
  )

  if (params.mode === 'branch') {
    if (params.useExistingBranch) await checkoutBranch(workspaceDir, params.branch)
    else await createBranch(workspaceDir, params.branch, params.baseBranch)
    return buildWorkspaceInfo({
      workspaceDir,
      projectDir: normalizedProjectDir,
      cwdSubpath,
      baseBranch: params.baseBranch,
    })
  }

  const worktreePath = resolveSiblingWorktreePath(repoRoot, params.branch)
  await createWorktree({
    repoRoot,
    branch: params.branch,
    worktreePath,
    baseBranch: params.baseBranch,
    useExistingBranch: params.useExistingBranch,
  })
  return buildWorkspaceInfo({
    workspaceDir: worktreePath,
    projectDir: normalizedProjectDir,
    cwdSubpath,
    baseBranch: params.baseBranch,
  })
}

export async function removeWorktree(params: {
  repoRoot: string
  worktreeDir: string
  force?: boolean
  deleteBranch?: boolean
  branch?: string
}): Promise<void> {
  const repoRoot = await normalizeFsPath(params.repoRoot)
  let worktreeDir = await normalizeFsPath(params.worktreeDir)
  if (await pathExists(worktreeDir)) {
    try {
      worktreeDir = await getWorktreeRoot(worktreeDir)
    } catch {
      // Best-effort: fall back to the provided path for already-removed or invalid worktrees.
    }
  }
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

  // Optional: explicitly delete the local branch after worktree removal.
  // Default is NO deletion to avoid accidentally removing user branches.
  const deleteBranch = Boolean(params.deleteBranch)
  const branch = String(params.branch || '').trim()
  const protectedBranches = new Set(['main', 'master', 'HEAD'])
  if (deleteBranch && branch && !protectedBranches.has(branch)) {
    try {
      const branchExists = await localBranchExists(repoRoot, branch)
      const checkedOut = branchExists
        ? await isBranchCheckedOutInAnyWorktree(repoRoot, branch)
        : false
      if (branchExists && !checkedOut) {
        await runGit(['branch', '-D', '--', branch], repoRoot, { timeoutMs: 8000 })
      }
    } catch {
      // Best-effort cleanup; ignore.
    }
  }
}

export async function pushBranch(params: {
  projectDir: string
  remote?: string
  branch?: string
}): Promise<{ remote: string; branch: string }> {
  const projectDir = resolve(params.projectDir)
  const remote =
    typeof params.remote === 'string' && params.remote.trim() ? params.remote.trim() : 'origin'
  const branch =
    typeof params.branch === 'string' && params.branch.trim()
      ? params.branch.trim()
      : await getCurrentBranch(projectDir)

  if (!branch || branch === 'HEAD') throw new Error('Cannot push detached HEAD')

  await runGit(['push', '--set-upstream', remote, branch], projectDir, { timeoutMs: 120000 })
  return { remote, branch }
}
