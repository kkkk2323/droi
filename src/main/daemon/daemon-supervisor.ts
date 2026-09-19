// Keeps exactly one Daemon child alive while the Desktop Shell runs: picks a
// free loopback port, waits until the Daemon accepts connections, and restarts
// it with backoff if it exits. `stop()` ends the child and disarms restarts.
import { EventEmitter } from 'node:events'
import type { ChildProcess } from 'node:child_process'
import { connect, createServer } from 'node:net'

export type DaemonState =
  | { status: 'stopped' }
  | { status: 'starting'; port: number; attempt: number }
  | { status: 'running'; port: number; pid: number }
  | { status: 'restarting'; delayMs: number; attempt: number; reason: string }

export interface DaemonSupervisorOptions {
  spawn: (port: number) => ChildProcess
  /** Delays between restart attempts; the last value repeats. */
  backoffMs?: number[]
  /** How long a spawned Daemon may take to accept TCP connections. */
  readyTimeoutMs?: number
  /** A Daemon that survived this long resets the backoff. */
  stableAfterMs?: number
}

const DEFAULT_BACKOFF_MS = [1_000, 2_000, 5_000, 10_000, 30_000]
const LOOPBACK = '127.0.0.1'

export class DaemonSupervisor extends EventEmitter<{ state: [DaemonState] }> {
  #state: DaemonState = { status: 'stopped' }
  #child: ChildProcess | null = null
  #attempt = 0
  #restartTimer: NodeJS.Timeout | null = null
  #running = false
  readonly #options: Required<DaemonSupervisorOptions>

  constructor(options: DaemonSupervisorOptions) {
    super()
    this.#options = {
      backoffMs: DEFAULT_BACKOFF_MS,
      readyTimeoutMs: 30_000,
      stableAfterMs: 30_000,
      ...options,
    }
  }

  get state(): DaemonState {
    return this.#state
  }

  get daemonUrl(): string | null {
    return this.#state.status === 'running' ? `ws://${LOOPBACK}:${this.#state.port}` : null
  }

  start(): void {
    if (this.#running) return
    this.#running = true
    this.#attempt = 0
    void this.#launch()
  }

  async stop(): Promise<void> {
    this.#running = false
    if (this.#restartTimer) clearTimeout(this.#restartTimer)
    this.#restartTimer = null
    const child = this.#child
    this.#child = null
    if (child && child.exitCode === null && !child.killed) {
      const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
      child.kill('SIGTERM')
      const forceKill = setTimeout(() => child.kill('SIGKILL'), 3_000)
      await exited
      clearTimeout(forceKill)
    }
    this.#setState({ status: 'stopped' })
  }

  async #launch(): Promise<void> {
    const port = await pickFreePort()
    if (!this.#running) return
    this.#attempt += 1
    this.#setState({ status: 'starting', port, attempt: this.#attempt })

    const child = this.#options.spawn(port)
    this.#child = child
    const startedAt = Date.now()
    let exited = false

    child.once('exit', (code, signal) => {
      exited = true
      if (this.#child !== child) return
      this.#child = null
      if (!this.#running) return
      const lived = Date.now() - startedAt
      if (lived >= this.#options.stableAfterMs) this.#attempt = 0
      this.#scheduleRestart(`exited with ${signal ?? code}`)
    })
    child.once('error', (error) => {
      if (this.#child !== child) return
      this.#child = null
      if (this.#running) this.#scheduleRestart(error.message)
    })

    const ready = await waitForPort(port, this.#options.readyTimeoutMs, () => exited)
    if (this.#child !== child || !this.#running) return
    if (ready) {
      this.#setState({ status: 'running', port, pid: child.pid ?? -1 })
    } else if (!exited) {
      // Spawned but never listened; kill it so the exit handler restarts it.
      child.kill('SIGKILL')
    }
  }

  #scheduleRestart(reason: string): void {
    const steps = this.#options.backoffMs
    const delayMs =
      steps[Math.min(this.#attempt, steps.length) - 1] ?? steps[steps.length - 1] ?? 1_000
    this.#setState({ status: 'restarting', delayMs, attempt: this.#attempt, reason })
    this.#restartTimer = setTimeout(() => {
      this.#restartTimer = null
      if (this.#running) void this.#launch()
    }, delayMs)
  }

  #setState(state: DaemonState): void {
    this.#state = state
    this.emit('state', state)
  }
}

export function pickFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer()
    server.once('error', reject)
    server.listen(0, LOOPBACK, () => {
      const address = server.address()
      if (!address || typeof address === 'string') {
        reject(new Error('no port assigned'))
        return
      }
      server.close(() => resolve(address.port))
    })
  })
}

async function waitForPort(
  port: number,
  timeoutMs: number,
  gaveUp: () => boolean,
): Promise<boolean> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline && !gaveUp()) {
    if (await canConnect(port)) return true
    await new Promise((r) => setTimeout(r, 100))
  }
  return false
}

function canConnect(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = connect(port, LOOPBACK)
    socket.once('connect', () => {
      socket.destroy()
      resolve(true)
    })
    socket.once('error', () => resolve(false))
  })
}
