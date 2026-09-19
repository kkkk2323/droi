import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

export type PortCleanupDeps = {
  execFile: (file: string, args: string[]) => Promise<{ stdout?: string }>
  kill: (pid: number) => void
}

export const MISSION_E2E_PORTS = [9222, 5173, 3002] as const

function normalizePidList(raw: string | undefined): number[] {
  return String(raw || '')
    .split(/\s+/)
    .map((value) => Number(value.trim()))
    .filter((value) => Number.isInteger(value) && value > 0)
}

export async function findListeningPidsForPort(
  port: number,
  deps: Pick<PortCleanupDeps, 'execFile'> = {
    execFile: (file, args) => execFileAsync(file, args),
  },
): Promise<number[]> {
  try {
    const { stdout } = await deps.execFile('lsof', ['-tiTCP:' + String(port), '-sTCP:LISTEN'])
    return normalizePidList(stdout)
  } catch {
    return []
  }
}

export async function cleanupListeningPorts(
  ports: readonly number[],
  deps: PortCleanupDeps = {
    execFile: (file, args) => execFileAsync(file, args),
    kill: (pid) => process.kill(pid, 'SIGTERM'),
  },
): Promise<number[]> {
  const uniquePids = new Set<number>()

  for (const port of ports) {
    const pids = await findListeningPidsForPort(port, deps)
    for (const pid of pids) uniquePids.add(pid)
  }

  for (const pid of uniquePids) {
    try {
      deps.kill(pid)
    } catch {
      // Ignore already-exited processes so relaunch remains idempotent.
    }
  }

  return [...uniquePids]
}
