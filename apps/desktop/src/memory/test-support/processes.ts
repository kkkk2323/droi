import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const RUN_TS = fileURLToPath(new URL('./run-ts.mjs', import.meta.url))

export interface Finished {
  code: number | null
  stdout: string
  stderr: string
}

/** Runs a TypeScript entry in its own Node process, feeding it stdin, until it exits. */
export function runTs(
  entry: string,
  options: { args?: string[]; stdin?: string; env?: NodeJS.ProcessEnv; cwd?: string } = {},
): Promise<Finished> {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['--import', RUN_TS, entry, ...(options.args ?? [])], {
      cwd: options.cwd,
      env: { ...process.env, ...options.env },
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (chunk: Buffer) => (stdout += chunk.toString()))
    child.stderr.on('data', (chunk: Buffer) => (stderr += chunk.toString()))
    child.on('error', reject)
    child.on('exit', (code) => resolve({ code, stdout, stderr }))
    child.stdin.end(options.stdin ?? '')
  })
}

/** Starts a TypeScript entry and leaves it running, for a line-based protocol over stdio. */
export function startTs(entry: string, options: { env?: NodeJS.ProcessEnv; cwd?: string } = {}) {
  return spawn(process.execPath, ['--import', RUN_TS, entry], {
    cwd: options.cwd,
    env: { ...process.env, ...options.env },
    stdio: ['pipe', 'pipe', 'pipe'],
  })
}
