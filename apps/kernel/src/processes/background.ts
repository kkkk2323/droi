import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { closeSync, mkdirSync, openSync, readSync, fstatSync } from 'node:fs'
import { join } from 'node:path'
import { randomBytes } from 'node:crypto'
import type { Context } from '@earendil-works/chord'
import type { Harness } from '@earendil-works/pi-durable'
import type { BunSqliteDatabase } from '../storage/facade.ts'
import {
  groupIsOurs,
  processSnapshot,
  type ShellGroup,
  type ToolProcesses,
} from '../tool-processes.ts'
import { Code, RpcError } from '../protocol/rpc.ts'
import { info, warn } from '../log.ts'

/** How long `process.stop` waits after SIGTERM before SIGKILL, as the protocol says. */
const TERM_GRACE_MS = 5000
/** How much of the log a `process.changed` carries. */
const TAIL_BYTES = 4096
/** A start time read from `ps` is truncated to the second: the margin a restored record allows. */
const START_SLACK_MS = 1000

/** The words `process.list` and `process.changed` use; a `droi.process` entry says `done` for `exited`. */
export type ProcessStatus = 'running' | 'exited' | 'stopped' | 'failed'

/** One background process, as `process.list` answers it. */
export type ProcessInfo = {
  id: string
  pid: number
  command: string
  logPath: string
  startedAt: number
  status: ProcessStatus
  exitCode?: number
  endedAt?: number
  /** The `bash_background` call that started it, so a client can put it beside that call. */
  callId?: string
}

type Live = ProcessInfo & {
  conversationId: number
  /** This run's handle; absent for one a previous run started. */
  child?: ChildProcess
  group?: ShellGroup
  exited?: () => void
  stopping?: boolean
  /** Settles once the start is recorded: an end is recorded after it, however fast the command was. */
  recorded?: Promise<void>
  /** Settles once the end is recorded. */
  finished?: Promise<void>
}

export type BackgroundOptions = {
  harness: Harness
  db: BunSqliteDatabase
  /** Where the logs go: `<dataDir>/logs/<conversationId>/<processId>.log`. */
  dataDir: string
  tools: ToolProcesses
  /** The shell `bash` uses: Git Bash's path on Windows, or none for PowerShell. */
  shell: { kind: 'bash' | 'powershell'; shellPath?: string }
  platform: NodeJS.Platform
  notify: (method: string, payload: unknown) => void
  context: Context
}

/**
 * `bash_background` and `process.*` (design/m3-kernel.md §7): a command that keeps running after the tool returns,
 * with its output in a log file. Its process group joins the Kernel's tool groups, so it ends with the Kernel and a
 * stop uses the same evidence a shutdown does (tool-processes.ts): nothing is ever signalled by a bare pid.
 *
 * The transcript keeps every process's records (`droi.process`); `droi_processes` lists the ones still running,
 * so a restart finds them without reading every conversation.
 */
export class Background {
  readonly #options: BackgroundOptions
  readonly #live = new Map<string, Live>()
  #closed = false

  constructor(options: BackgroundOptions) {
    this.#options = options
  }

  /** Make the table, and settle the processes a previous run left: still ours, or stopped. */
  async open(): Promise<void> {
    await this.#options.db.exec(`CREATE TABLE IF NOT EXISTS droi_processes (
      process_id TEXT PRIMARY KEY,
      conversation_id INTEGER NOT NULL,
      record TEXT NOT NULL
    )`)
    const rows = await this.#options.db.all<{
      process_id: string
      conversation_id: number
      record: string
    }>('SELECT process_id, conversation_id, record FROM droi_processes')
    if (rows.length === 0) return
    // Windows: the Host's Job Object ended them with the process that started them.
    const snapshot = this.#options.platform === 'win32' ? undefined : processSnapshot()
    for (const row of rows) {
      let record: Live
      try {
        record = { ...(JSON.parse(row.record) as ProcessInfo), conversationId: row.conversation_id }
      } catch {
        await this.#options.db.run(
          'DELETE FROM droi_processes WHERE process_id = ?',
          row.process_id,
        )
        continue
      }
      // The leader started by `startedAt`; a group whose members all started later is a stranger's, or ours
      // with its leader gone, and is left alone either way.
      const group: ShellGroup = {
        pgid: record.pid,
        spawnedAtMs: record.startedAt,
        exitedAtMs: record.startedAt + START_SLACK_MS,
      }
      if (snapshot !== undefined && groupIsOurs(group, snapshot.get(record.pid) ?? [])) {
        const { exited } = this.#options.tools.adopt(record.pid, group.exitedAtMs)
        this.#live.set(record.id, { ...record, status: 'running', group, exited })
        continue
      }
      await this.#finish(record, 'stopped', undefined).catch(() => undefined)
    }
  }

  /** The tool: start `command`, and answer at once. */
  async start(
    command: string,
    cwd: string | undefined,
    conversationId: number,
    callId?: string,
  ): Promise<ProcessInfo> {
    if (this.#closed) throw new Error('The kernel is closing; the command was not started.')
    const id = `p-${randomBytes(4).toString('hex')}`
    const dir = join(this.#options.dataDir, 'logs', String(conversationId))
    mkdirSync(dir, { recursive: true })
    const logPath = join(dir, `${id}.log`)
    const fd = openSync(logPath, 'a')
    let child: ChildProcess
    try {
      child = this.#spawn(command, cwd, fd)
    } finally {
      closeSync(fd)
    }
    const pid = await new Promise<number>((resolve, reject) => {
      child.once('spawn', () => resolve(child.pid!))
      child.once('error', reject)
    })
    const live: Live = {
      id,
      pid,
      command,
      logPath,
      startedAt: Date.now(),
      status: 'running',
      ...(callId === undefined ? {} : { callId }),
      conversationId,
      child,
      ...(this.#options.platform === 'win32'
        ? {}
        : {
            group: { pgid: pid, spawnedAtMs: Date.now() },
            exited: this.#options.tools.adopt(pid).exited,
          }),
    }
    this.#live.set(id, live)
    child.once('exit', (code) => {
      if (live.group !== undefined) live.group.exitedAtMs = Date.now()
      live.exited?.()
      const status: ProcessStatus = live.stopping ? 'stopped' : code === 0 ? 'exited' : 'failed'
      live.finished = this.#finish(live, status, code ?? undefined).catch((failure: unknown) =>
        warn('background process end not recorded', { processId: id, error: String(failure) }),
      )
    })
    live.recorded = (async () => {
      await this.#options.db.run(
        'INSERT OR REPLACE INTO droi_processes (process_id, conversation_id, record) VALUES (?, ?, ?)',
        id,
        conversationId,
        JSON.stringify(publicOf(live)),
      )
      await this.#record(conversationId, live, 'running')
      this.#options.notify('process.changed', {
        id: conversationId,
        processId: id,
        status: 'running',
        tail: '',
      })
    })()
    await live.recorded
    info('background process started', { conversationId, processId: id, pid })
    return publicOf(live)
  }

  /** `process.list`: every process the conversation's transcript names, with the live state of the running ones. */
  async list(conversationId: number): Promise<ProcessInfo[]> {
    const byId = new Map<string, ProcessInfo>()
    const conversation = await this.#options.harness.conversation(
      conversationId as never,
      this.#options.context,
    )
    if (conversation === undefined)
      throw new RpcError(Code.conversationNotFound, 'conversation not found', {
        id: conversationId,
      })
    let cursor: never | undefined
    do {
      const page = await conversation.entries(
        { order: 'ascending' },
        500,
        cursor,
        this.#options.context,
      )
      for (const entry of page.items) {
        if (entry.kind !== 'droi.process') continue
        const data = entry.data as Record<string, unknown>
        const id = data['processId']
        if (typeof id !== 'string') continue
        const status = data['status'] === 'done' ? 'exited' : (data['status'] as ProcessStatus)
        const earlier = byId.get(id)
        byId.set(id, {
          id,
          pid: Number(data['pid'] ?? earlier?.pid ?? 0),
          command: String(data['command'] ?? earlier?.command ?? ''),
          logPath: String(data['logPath'] ?? earlier?.logPath ?? ''),
          startedAt: earlier?.startedAt ?? Number(data['at'] ?? 0),
          status,
          ...(typeof (data['callId'] ?? earlier?.callId) === 'string'
            ? { callId: (data['callId'] ?? earlier?.callId) as string }
            : {}),
          ...(typeof data['exitCode'] === 'number' ? { exitCode: data['exitCode'] } : {}),
          ...(status === 'running' ? {} : { endedAt: Number(data['at'] ?? 0) }),
        })
      }
      cursor = page.next as never
    } while (cursor !== undefined)
    // A restored process has no handle to tell its end: ask the table whether its group is still there.
    for (const live of [...this.#live.values()]) {
      if (live.child !== undefined || live.conversationId !== conversationId) continue
      if (this.#ours(live)) continue
      await this.#finish(live, 'stopped', undefined).catch(() => undefined)
      byId.set(live.id, { ...publicOf(live), status: 'stopped', endedAt: Date.now() })
    }
    for (const live of this.#live.values()) {
      if (live.conversationId === conversationId && !byId.has(live.id))
        byId.set(live.id, publicOf(live))
    }
    return [...byId.values()]
  }

  /** `process.stop`: end the process and what it started, when the evidence says the group is still ours. */
  async stop(conversationId: number, processId: string): Promise<'stopped' | 'gone'> {
    const live = this.#live.get(processId)
    if (live === undefined || live.conversationId !== conversationId) {
      const known = (await this.list(conversationId)).some((p) => p.id === processId)
      if (!known) throw new RpcError(Code.processNotFound, 'process not found', { processId })
      return 'gone'
    }
    live.stopping = true
    if (this.#options.platform === 'win32') {
      // Only while this run holds the child: the handle keeps its PID from naming another program.
      if (
        live.child === undefined ||
        live.child.exitCode !== null ||
        live.child.signalCode !== null
      ) {
        await this.#finish(live, 'stopped', undefined).catch(() => undefined)
        return 'gone'
      }
      spawnSync('taskkill', ['/T', '/F', '/PID', String(live.pid)], {
        windowsHide: true,
        timeout: 6000,
      })
      return 'stopped'
    }
    if (!this.#ours(live)) {
      await this.#finish(live, 'stopped', undefined).catch(() => undefined)
      return 'gone'
    }
    signal(live.pid, 'SIGTERM')
    const deadline = Date.now() + TERM_GRACE_MS
    while (Date.now() < deadline && groupAlive(live.pid)) await Bun.sleep(100)
    if (groupAlive(live.pid) && this.#ours(live)) signal(live.pid, 'SIGKILL')
    // A leader this run holds records its own end; a restored one is recorded here.
    if (live.child === undefined)
      await this.#finish(live, 'stopped', undefined).catch(() => undefined)
    return 'stopped'
  }

  /**
   * The kernel is closing: its background processes end with it (as every tool process does), and the
   * transcript says so while it can still be written.
   */
  async close(): Promise<void> {
    this.#closed = true
    const ending = [...this.#live.values()]
    for (const live of ending) {
      live.stopping = true
      if (this.#options.platform === 'win32') {
        if (
          live.child !== undefined &&
          live.child.exitCode === null &&
          live.child.signalCode === null
        ) {
          spawnSync('taskkill', ['/T', '/F', '/PID', String(live.pid)], {
            windowsHide: true,
            timeout: 6000,
          })
        }
      } else if (this.#ours(live)) {
        signal(live.pid, 'SIGKILL')
      }
    }
    const deadline = Date.now() + 2000
    for (const live of ending) {
      if (live.child === undefined) {
        await this.#finish(live, 'stopped', undefined).catch(() => undefined)
        continue
      }
      while (live.finished === undefined && Date.now() < deadline) await Bun.sleep(20)
      await live.finished
    }
  }

  /** Whether `live`'s group may be signalled, by the same rule a shutdown uses. */
  #ours(live: Live): boolean {
    const group = live.group
    if (group === undefined) return false
    if (group.exitedAtMs === undefined) return true
    const snapshot = processSnapshot()
    return snapshot !== undefined && groupIsOurs(group, snapshot.get(live.pid) ?? [])
  }

  async #finish(live: Live, status: ProcessStatus, exitCode: number | undefined): Promise<void> {
    if (!this.#live.has(live.id) && live.child !== undefined) return
    this.#live.delete(live.id)
    await live.recorded?.catch(() => undefined)
    await this.#options.db
      .run('DELETE FROM droi_processes WHERE process_id = ?', live.id)
      .catch(() => undefined)
    await this.#record(live.conversationId, live, status === 'exited' ? 'done' : status, exitCode)
    this.#options.notify('process.changed', {
      id: live.conversationId,
      processId: live.id,
      status,
      ...(exitCode === undefined ? {} : { exitCode }),
      tail: tailOf(live.logPath),
    })
  }

  async #record(
    conversationId: number,
    live: ProcessInfo,
    status: string,
    exitCode?: number,
  ): Promise<void> {
    await this.#options.harness.commit(async (tx) => {
      await tx.appendEntry(conversationId as never, {
        kind: 'droi.process',
        data: {
          processId: live.id,
          pid: live.pid,
          command: live.command,
          logPath: live.logPath,
          ...(live.callId === undefined ? {} : { callId: live.callId }),
          status,
          ...(exitCode === undefined ? {} : { exitCode }),
          at: Date.now(),
        } as never,
      })
    }, this.#options.context)
  }

  #spawn(command: string, cwd: string | undefined, fd: number): ChildProcess {
    const stdio: ['ignore', number, number] = ['ignore', fd, fd]
    if (this.#options.platform === 'win32') {
      if (this.#options.shell.kind === 'bash' && this.#options.shell.shellPath !== undefined) {
        return spawn(this.#options.shell.shellPath, ['-c', command], {
          cwd,
          stdio,
          windowsHide: true,
        })
      }
      return spawn('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', command], {
        cwd,
        stdio,
        windowsHide: true,
      })
    }
    // A group of its own, as Pi's tool shells: a Ctrl-C aimed at the Kernel must not reach it.
    return spawn('bash', ['-c', command], { cwd, stdio, detached: true })
  }
}

function publicOf(live: Live): ProcessInfo {
  return {
    id: live.id,
    pid: live.pid,
    command: live.command,
    logPath: live.logPath,
    startedAt: live.startedAt,
    status: live.status,
    ...(live.exitCode === undefined ? {} : { exitCode: live.exitCode }),
    ...(live.callId === undefined ? {} : { callId: live.callId }),
  }
}

function signal(pgid: number, name: NodeJS.Signals): void {
  try {
    process.kill(-pgid, name)
  } catch {
    // ESRCH: the group ended first.
  }
}

function groupAlive(pgid: number): boolean {
  try {
    process.kill(-pgid, 0)
    return true
  } catch {
    return false
  }
}

/** The newest output, up to TAIL_BYTES, as text. */
function tailOf(path: string): string {
  let fd: number | undefined
  try {
    fd = openSync(path, 'r')
    const size = fstatSync(fd).size
    const length = Math.min(size, TAIL_BYTES)
    const buffer = Buffer.alloc(length)
    readSync(fd, buffer, 0, length, size - length)
    return buffer.toString('utf8')
  } catch {
    return ''
  } finally {
    if (fd !== undefined) closeSync(fd)
  }
}
