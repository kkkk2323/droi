import { execFile } from 'child_process'
import type { KeyStoreAPI } from '../keys/keyStore.ts'
import type { DroidExecManager } from '../droid/droidExecRunner.ts'
import type { GenerateCommitMetaRequest, GenerateCommitMetaResult, PersistedAppState } from '../../shared/protocol.ts'
import { extractFirstJsonObject, runDroidAndCaptureAssistantText, stripCodeFences } from '../droid/textCapture.ts'

const DEFAULT_COMMIT_MODEL_ID = 'minimax-m2.5'

function exec(cmd: string, args: string[], opts: { cwd: string; timeoutMs?: number }) {
  const timeout = typeof opts.timeoutMs === 'number' ? opts.timeoutMs : 20000
  return new Promise<{ stdout: string; stderr: string }>((resolve, reject) => {
    execFile(cmd, args, { cwd: opts.cwd, timeout, maxBuffer: 10 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) {
        const msg = String(stderr || stdout || (err as any).message || err)
        return reject(new Error(msg.trim() || `Failed: ${cmd} ${args.join(' ')}`))
      }
      resolve({ stdout: String(stdout || ''), stderr: String(stderr || '') })
    })
  })
}

function truncate(text: string, maxChars: number): string {
  const t = String(text || '')
  if (t.length <= maxChars) return t
  return `${t.slice(0, maxChars)}\n\n…(truncated)`
}

async function collectGitContext(params: { projectDir: string; includeUnstaged: boolean }) {
  const { projectDir, includeUnstaged } = params
  const [status, stagedStat, stagedDiff] = await Promise.all([
    exec('git', ['status', '--porcelain', '-uall'], { cwd: projectDir }),
    exec('git', ['diff', '--cached', '--stat'], { cwd: projectDir }).catch(() => ({ stdout: '', stderr: '' })),
    exec('git', ['diff', '--cached'], { cwd: projectDir }).catch(() => ({ stdout: '', stderr: '' })),
  ])

  let unstagedStat = ''
  let unstagedDiff = ''
  let untracked = ''
  if (includeUnstaged) {
    const [uStat, uDiff, uTracked] = await Promise.all([
      exec('git', ['diff', '--stat'], { cwd: projectDir }).catch(() => ({ stdout: '', stderr: '' })),
      exec('git', ['diff'], { cwd: projectDir }).catch(() => ({ stdout: '', stderr: '' })),
      exec('git', ['ls-files', '--others', '--exclude-standard'], { cwd: projectDir }).catch(() => ({ stdout: '', stderr: '' })),
    ])
    unstagedStat = uStat.stdout
    unstagedDiff = uDiff.stdout
    untracked = uTracked.stdout
  }

  return {
    status: status.stdout,
    stagedStat: stagedStat.stdout,
    stagedDiff: stagedDiff.stdout,
    unstagedStat,
    unstagedDiff,
    untracked,
  }
}

function getModelIdFromState(state: PersistedAppState | null | undefined): string {
  const raw = (state as any)?.commitMessageModelId
  return typeof raw === 'string' && raw.trim() ? raw.trim() : DEFAULT_COMMIT_MODEL_ID
}

function buildPrompt(params: {
  includeUnstaged: boolean
  wantPrMeta: boolean
  prBaseBranch?: string
  git: Awaited<ReturnType<typeof collectGitContext>>
}): string {
  const { includeUnstaged, wantPrMeta, prBaseBranch, git } = params
  const limits = {
    status: 4000,
    stagedStat: 2000,
    stagedDiff: 7000,
    unstagedStat: 2000,
    unstagedDiff: 3000,
    untracked: 2000,
  } as const
  const pieces = [
    'You are an expert software engineer. Generate a high-quality Git commit message for the changes below.',
    '',
    'Rules:',
    '- Output ONLY the requested content (no explanations, no markdown fences).',
    '- Do NOT use tools or attempt to run any commands.',
    '- Use Conventional Commits style when possible (e.g. feat:, fix:, refactor:, chore:).',
    '- Keep the subject line concise and specific.',
    '',
    'Git status (porcelain):',
    truncate(git.status, limits.status) || '(empty)',
    '',
    'Staged diff --stat:',
    truncate(git.stagedStat, limits.stagedStat) || '(empty)',
    '',
    'Staged diff (truncated excerpt):',
    truncate(git.stagedDiff, limits.stagedDiff) || '(empty)',
  ]

  if (includeUnstaged) {
    pieces.push(
      '',
      'Unstaged diff --stat (will also be included in the commit):',
      truncate(git.unstagedStat, limits.unstagedStat) || '(empty)',
      '',
      'Unstaged diff (truncated excerpt, will also be included in the commit):',
      truncate(git.unstagedDiff, limits.unstagedDiff) || '(empty)',
      '',
      'Untracked files (names only; will also be included in the commit):',
      truncate(git.untracked, limits.untracked) || '(none)',
    )
  }

  pieces.push('', 'Output format:')

  if (wantPrMeta) {
    pieces.push(
      `Return ONLY valid JSON with keys: commitMessage, prTitle, prBody. prBaseBranch=${prBaseBranch || ''}`,
      'Example:',
      '{"commitMessage":"feat: add X","prTitle":"Add X","prBody":"Summary..."}',
    )
  } else {
    pieces.push('Return ONLY the commit message text.')
  }

  return pieces.join('\n')
}

function parseModelOutput(params: { text: string; wantPrMeta: boolean }): { commitMessage: string; prTitle?: string; prBody?: string } {
  const raw = stripCodeFences(params.text)
  if (!raw) throw new Error('Generated text was empty')

  if (!params.wantPrMeta) return { commitMessage: raw.trim() }

  const json = extractFirstJsonObject(raw)
  if (!json) throw new Error('Failed to parse PR metadata (expected JSON)')
  let obj: any
  try {
    obj = JSON.parse(json)
  } catch {
    throw new Error('Failed to parse PR metadata JSON')
  }
  const commitMessage = typeof obj?.commitMessage === 'string' ? obj.commitMessage.trim() : ''
  if (!commitMessage) throw new Error('Generated commit message was empty')
  const prTitle = typeof obj?.prTitle === 'string' ? obj.prTitle.trim() : ''
  const prBody = typeof obj?.prBody === 'string' ? String(obj.prBody) : ''
  return {
    commitMessage,
    ...(prTitle ? { prTitle } : {}),
    ...(prBody ? { prBody } : {}),
  }
}

export async function generateCommitMeta(params: {
  req: GenerateCommitMetaRequest
  state: PersistedAppState
  execManager: DroidExecManager
  keyStore: KeyStoreAPI
}): Promise<GenerateCommitMetaResult> {
  const projectDir = String(params.req.projectDir || '').trim()
  if (!projectDir) throw new Error('Missing projectDir')

  const includeUnstaged = Boolean(params.req.includeUnstaged)
  const wantPrMeta = Boolean(params.req.wantPrMeta)
  const prBaseBranch = typeof params.req.prBaseBranch === 'string' ? params.req.prBaseBranch.trim() : ''
  const modelId = getModelIdFromState(params.state)
  const machineId = (params.state as any)?.machineId
  if (typeof machineId !== 'string' || !machineId.trim()) throw new Error('Missing machineId in app state')

  const git = await collectGitContext({ projectDir, includeUnstaged })
  const prompt = buildPrompt({ includeUnstaged, wantPrMeta, prBaseBranch, git })

  const sid = `commit-meta:${Date.now()}:${Math.random().toString(16).slice(2)}`
  const env: Record<string, string | undefined> = { ...process.env }
  const activeKey = await params.keyStore.getActiveKey()
  if (activeKey) env['FACTORY_API_KEY'] = activeKey
  else if ((params.state as any)?.apiKey) env['FACTORY_API_KEY'] = (params.state as any).apiKey

  const text = await runDroidAndCaptureAssistantText({
    execManager: params.execManager,
    send: {
      sessionId: sid,
      machineId,
      prompt,
      cwd: projectDir,
      modelId,
      autonomyLevel: 'spec',
      env,
    },
  })

  const parsed = parseModelOutput({ text, wantPrMeta })
  return { ...parsed, modelId }
}
