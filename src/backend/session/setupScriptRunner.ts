import { spawn, type ChildProcessByStdio } from 'child_process'
import type { Readable } from 'stream'
import type { SetupScriptEvent } from '../../shared/protocol'
import { homedir } from 'os'
import { join } from 'path'

// Extended PATH for macOS GUI apps
const EXTENDED_PATH = [
  '/opt/homebrew/bin',
  '/usr/local/bin',
  join(homedir(), '.local/bin'),
  join(homedir(), '.cargo/bin'),
  process.env.PATH || '',
]
  .filter(Boolean)
  .join(':')

export interface RunSetupScriptParams {
  sessionId: string
  projectDir: string
  script: string
  env?: Record<string, string | undefined>
}

export class SetupScriptRunner {
  private readonly listeners = new Set<(event: SetupScriptEvent) => void>()
  private readonly processes = new Map<string, ChildProcessByStdio<null, Readable, Readable>>()

  onEvent(callback: (event: SetupScriptEvent) => void): () => void {
    this.listeners.add(callback)
    return () => this.listeners.delete(callback)
  }

  async run(params: RunSetupScriptParams): Promise<void> {
    const sessionId = String(params.sessionId || '').trim()
    const projectDir = String(params.projectDir || '').trim()
    const script = String(params.script || '').trim()
    if (!sessionId) throw new Error('Missing sessionId')
    if (!projectDir) throw new Error('Missing projectDir')
    if (!script) throw new Error('Missing setup script')
    if (this.processes.has(sessionId))
      throw new Error('Setup script is already running for this session')

    const shell = process.env['SHELL'] || '/bin/bash'
    const child = spawn(shell, ['-lc', script], {
      cwd: projectDir,
      env: { ...process.env, PATH: EXTENDED_PATH, ...(params.env || {}) },
      stdio: ['ignore', 'pipe', 'pipe'],
    })

    this.processes.set(sessionId, child)
    this.emit({ type: 'started', sessionId, projectDir, script })

    let finished = false
    const finalize = (args: {
      success: boolean
      exitCode: number | null
      signal: string | null
      error?: string
    }) => {
      if (finished) return
      finished = true
      this.processes.delete(sessionId)
      this.emit({
        type: 'finished',
        sessionId,
        success: args.success,
        exitCode: args.exitCode,
        signal: args.signal,
        ...(args.error ? { error: args.error } : {}),
      })
    }

    child.stdout.on('data', (chunk: Buffer) => {
      const data = String(chunk || '')
      if (!data) return
      this.emit({ type: 'output', sessionId, stream: 'stdout', data })
    })

    child.stderr.on('data', (chunk: Buffer) => {
      const data = String(chunk || '')
      if (!data) return
      this.emit({ type: 'output', sessionId, stream: 'stderr', data })
    })

    child.once('error', (err) => {
      finalize({
        success: false,
        exitCode: null,
        signal: null,
        error: err instanceof Error ? err.message : String(err),
      })
    })

    child.once('close', (code, signal) => {
      const exitCode = typeof code === 'number' ? code : null
      const signalValue = signal ? String(signal) : null
      finalize({ success: exitCode === 0, exitCode, signal: signalValue })
    })
  }

  cancel(sessionId: string): void {
    const sid = String(sessionId || '').trim()
    if (!sid) return
    const child = this.processes.get(sid)
    if (!child) return
    child.kill('SIGTERM')
  }

  disposeAll(): number {
    const ids = Array.from(this.processes.keys())
    for (const id of ids) this.cancel(id)
    return ids.length
  }

  private emit(event: SetupScriptEvent): void {
    for (const listener of this.listeners) listener(event)
  }
}
