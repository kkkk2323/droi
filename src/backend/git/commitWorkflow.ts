import { execFile } from 'child_process'
import { mkdtemp, rm, writeFile } from 'fs/promises'
import { tmpdir } from 'os'
import { join } from 'path'

import type { CommitWorkflowRequest, CommitWorkflowResult, GitToolsInfo, WorkflowStepProgress } from '../../shared/protocol.ts'
import { pushBranch, getCurrentBranch } from './workspaceManager.ts'
import { parseGitRemoteHost, recommendPrTool } from './prTooling.ts'

// Extended PATH for macOS GUI apps (Electron main process doesn't inherit shell PATH)
const EXTENDED_PATH = [
  '/opt/homebrew/bin',      // macOS Apple Silicon (Homebrew)
  '/usr/local/bin',         // macOS Intel (Homebrew) / Linux
  process.env.HOME ? join(process.env.HOME, '.local/bin') : '',
  process.env.HOME ? join(process.env.HOME, '.cargo/bin') : '',
  process.env.PATH || '',
].filter(Boolean).join(':')

function exec(cmd: string, args: string[], opts: { cwd: string; timeoutMs?: number }) {
  const timeout = typeof opts.timeoutMs === 'number' ? opts.timeoutMs : 60000
  return new Promise<{ stdout: string; stderr: string }>((resolve, reject) => {
    execFile(cmd, args, {
      cwd: opts.cwd,
      timeout,
      maxBuffer: 10 * 1024 * 1024,
      env: { ...process.env, PATH: EXTENDED_PATH },
    }, (err, stdout, stderr) => {
      if (err) {
        const msg = String(stderr || stdout || (err as any).message || err)
        const e = new Error(msg.trim() || `Failed: ${cmd} ${args.join(' ')}`)
        ;(e as any).code = (err as any).code
        return reject(e)
      }
      resolve({ stdout: String(stdout || ''), stderr: String(stderr || '') })
    })
  })
}

async function commandExists(cmd: string): Promise<boolean> {
  try {
    await exec('which', [cmd], { cwd: process.cwd(), timeoutMs: 5000 })
    return true
  } catch {
    return false
  }
}

async function getOriginUrl(projectDir: string): Promise<string | null> {
  try {
    const res = await exec('git', ['remote', 'get-url', 'origin'], { cwd: projectDir, timeoutMs: 8000 })
    const out = res.stdout.trim()
    return out ? out : null
  } catch {
    return null
  }
}

export async function detectGitTools(params: { projectDir: string }): Promise<GitToolsInfo> {
  const projectDir = String(params.projectDir || '').trim()
  if (!projectDir) return { hasGh: false, hasFlow: false, prTool: null, prDisabledReason: 'Missing projectDir' }

  const [hasGh, hasFlow, originUrl] = await Promise.all([
    commandExists('gh'),
    commandExists('flow'),
    getOriginUrl(projectDir),
  ])

  const originHost = originUrl ? parseGitRemoteHost(originUrl) : null
  const rec = recommendPrTool({ originHost, hasGh, hasFlow })
  return {
    hasGh,
    hasFlow,
    ...(originHost ? { originHost } : {}),
    prTool: rec.prTool,
    ...(rec.disabledReason ? { prDisabledReason: rec.disabledReason } : {}),
  }
}

async function hasStagedChanges(projectDir: string): Promise<boolean> {
  return new Promise<boolean>((resolve, reject) => {
    execFile('git', ['diff', '--cached', '--quiet'], {
      cwd: projectDir,
      timeout: 20000,
      env: { ...process.env, PATH: EXTENDED_PATH },
    }, (err) => {
      if (!err) return resolve(false)
      const code = (err as any).code
      if (code === 1) return resolve(true)
      reject(err)
    })
  })
}

async function gitCommitWithMessage(params: { projectDir: string; message: string }): Promise<string> {
  const msg = String(params.message || '').trim()
  if (!msg) throw new Error('Commit message is empty')

  const baseDir = await mkdtemp(join(tmpdir(), 'droi-commit-'))
  const msgPath = join(baseDir, 'COMMIT_EDITMSG')
  try {
    await writeFile(msgPath, `${msg}\n`, 'utf8')
    await exec('git', ['commit', '-F', msgPath], { cwd: params.projectDir, timeoutMs: 120000 })
    const res = await exec('git', ['rev-parse', 'HEAD'], { cwd: params.projectDir, timeoutMs: 20000 })
    return res.stdout.trim()
  } finally {
    await rm(baseDir, { recursive: true, force: true })
  }
}

async function gitMergeIntoBranch(params: { projectDir: string; sourceBranch: string; targetBranch: string }): Promise<void> {
  const source = params.sourceBranch.trim()
  const target = params.targetBranch.trim()
  if (!source || !target) throw new Error('Missing source/target branch')
  if (source === target) return

  await exec('git', ['checkout', target], { cwd: params.projectDir, timeoutMs: 60000 })
  await exec('git', ['merge', '--no-edit', source], { cwd: params.projectDir, timeoutMs: 120000 })
  await exec('git', ['checkout', source], { cwd: params.projectDir, timeoutMs: 60000 })
}

async function ghCreatePr(params: { projectDir: string; baseBranch: string; headBranch: string; title: string; body: string }): Promise<string> {
  if (!(await commandExists('gh'))) throw new Error('GitHub CLI (gh) is not installed')
  const base = params.baseBranch.trim()
  const head = params.headBranch.trim()
  if (!base) throw new Error('Missing PR base branch')
  if (!head) throw new Error('Missing PR head branch')
  const title = String(params.title || '').trim()
  const body = String(params.body || '')
  if (!title) throw new Error('Missing PR title')

  const baseDir = await mkdtemp(join(tmpdir(), 'droi-pr-'))
  const bodyPath = join(baseDir, 'PR_BODY.md')
  try {
    await writeFile(bodyPath, body, 'utf8')
    const res = await exec(
      'gh',
      ['pr', 'create', '--base', base, '--head', head, '--title', title, '--body-file', bodyPath],
      { cwd: params.projectDir, timeoutMs: 180000 },
    )
    return res.stdout.trim()
  } finally {
    await rm(baseDir, { recursive: true, force: true })
  }
}

async function flowCreatePr(params: { projectDir: string; baseBranch: string; headBranch: string; title: string; body: string }): Promise<string> {
  if (!(await commandExists('flow'))) throw new Error('flow-cli (`flow`) is not installed')
  const base = params.baseBranch.trim()
  const head = params.headBranch.trim()
  if (!base) throw new Error('Missing PR base branch')
  if (!head) throw new Error('Missing PR head branch')
  const title = String(params.title || '').trim()
  const body = String(params.body || '')
  if (!title) throw new Error('Missing PR title')

  const res = await exec(
    'flow',
    ['pr', 'create', '--source', head, '--target', base, '--title', title, '--description', body],
    { cwd: params.projectDir, timeoutMs: 180000 },
  )
  return res.stdout.trim()
}

export async function commitWorkflow(
  req: CommitWorkflowRequest,
  onProgress?: (progress: WorkflowStepProgress) => void,
): Promise<CommitWorkflowResult> {
  const projectDir = String(req.projectDir || '').trim()
  if (!projectDir) throw new Error('Missing projectDir')

  const includeUnstaged = Boolean(req.includeUnstaged)
  const workflow = req.workflow
  if (workflow !== 'commit' && workflow !== 'commit_push' && workflow !== 'commit_push_pr') throw new Error('Invalid workflow')

  const emit = onProgress || (() => {})

  const branch = await getCurrentBranch(projectDir)

  // Stage
  if (includeUnstaged) {
    emit({ step: 'stage', status: 'running' })
    try {
      await exec('git', ['add', '-A'], { cwd: projectDir, timeoutMs: 60000 })
      emit({ step: 'stage', status: 'done' })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      emit({ step: 'stage', status: 'error', detail: msg })
      throw err
    }
  }

  // Commit
  emit({ step: 'commit', status: 'running' })
  let commitHash: string
  try {
    if (!(await hasStagedChanges(projectDir))) throw new Error('No staged changes to commit')
    commitHash = await gitCommitWithMessage({ projectDir, message: req.commitMessage })
    emit({ step: 'commit', status: 'done', detail: commitHash })
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    emit({ step: 'commit', status: 'error', detail: msg })
    throw err
  }

  // Merge
  if (req.mergeEnabled && req.mergeBranch) {
    emit({ step: 'merge', status: 'running' })
    try {
      await gitMergeIntoBranch({ projectDir, sourceBranch: branch, targetBranch: req.mergeBranch })
      emit({ step: 'merge', status: 'done' })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      emit({ step: 'merge', status: 'error', detail: msg })
      throw err
    }
  }

  // Push
  let remote: string | undefined
  if (workflow === 'commit_push' || workflow === 'commit_push_pr') {
    emit({ step: 'push', status: 'running' })
    try {
      const pushed = await pushBranch({ projectDir })
      remote = pushed.remote
      emit({ step: 'push', status: 'done', detail: pushed.remote })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      emit({ step: 'push', status: 'error', detail: msg })
      throw err
    }
  }

  // Create PR
  let prUrl: string | undefined
  if (workflow === 'commit_push_pr') {
    emit({ step: 'create_pr', status: 'running' })
    try {
      const baseBranch = String(req.prBaseBranch || '').trim()
      const prTitle = String(req.prTitle || '').trim()
      const prBody = String(req.prBody || '')

      const tools = await detectGitTools({ projectDir })
      if (!tools.prTool) throw new Error(tools.prDisabledReason || 'No PR tool available')

      if (tools.prTool === 'gh') {
        prUrl = await ghCreatePr({ projectDir, baseBranch, headBranch: branch, title: prTitle, body: prBody })
      } else {
        prUrl = await flowCreatePr({ projectDir, baseBranch, headBranch: branch, title: prTitle, body: prBody })
      }
      emit({ step: 'create_pr', status: 'done', detail: prUrl })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      emit({ step: 'create_pr', status: 'error', detail: msg })
      throw err
    }
  }

  return { ok: true, branch, commitHash, ...(remote ? { remote } : {}), ...(prUrl ? { prUrl } : {}) }
}
