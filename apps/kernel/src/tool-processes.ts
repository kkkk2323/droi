import { execFileSync } from 'node:child_process'
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node'
import type { Context } from '@earendil-works/chord'
import { warn } from './log.ts'

/**
 * The tool processes one Kernel started, and how to end them — safely, and without paying for a process
 * table scan per tool call.
 *
 * Pi runs a tool shell with `detached: true` on Unix (`pi-durable/dist/env/node.js`), which puts the shell in
 * a process group of its own. That is deliberate — the shell must not receive a Ctrl-C aimed at the Kernel —
 * and it has a consequence: the Kernel's own process group does not contain the tools, so killing the Kernel
 * does not kill them. A `sleep 100 &` a tool started keeps running, reparented to PID 1.
 *
 * Pi keeps the shells it is waiting on (`NodeExecutionEnv.activeChildPids`, ended by its own `cleanup()`),
 * and forgets a shell as soon as it exits — exactly when a background process is left behind. So this records
 * the shells Pi forgets, and decides at shutdown which of those groups may still be signalled.
 *
 * **A pid is not an identity, and neither is a group id.** A record can outlive the group it names: the
 * shell exits, its background processes exit, the group empties, and the system later hands that number to an
 * unrelated process group. Signalling it because the Kernel once saw the number would kill a stranger's tree.
 *
 * The rule, and why it is sound:
 *
 * - While the shell runs, the number is still its pid, so the system cannot have given it to anything else.
 * - When the shell exits, we note the wall-clock time (`exitedAt`). For the number to be reused, **every**
 *   process of our group must have exited first — a group id stays in use while any member lives.
 * - So if a member's start time is at or before `exitedAt`, it belongs to our group (a reused group can only
 *   hold processes that started after the number came free). If every member started after `exitedAt`, the
 *   evidence is against us or inconclusive, and nothing is signalled: a process that should have been killed
 *   is better than a stranger's process that was.
 *
 * Two accepted risks, both recorded in the protocol README: a group whose original members all exited leaving
 * only processes *they* started later is missed (its members started after `exitedAt`), and a reuse that
 * happens inside the same second reads as ours. Two costs are avoided: no `ps` on the spawn path, and one
 * snapshot at shutdown, not one per record.
 *
 * Windows has no process groups: a tool's tree belongs to the Host's process tree — the Job Object the Host
 * gives the Kernel — whose handle ends its members when the Kernel exits. This file signals nothing by pid
 * there.
 */
export type ToolProcesses = {
  /** The `env` a Harness is opened with: one environment per conversation, each one recorded. */
  env: (target: { cwd?: string }) => NodeExecutionEnv
  /** Stop every tool this Kernel started, including a background process it left behind. */
  stop: (context: Context) => Promise<void>
}

/** One tool shell's group, and when it stopped being a live claim on its number. */
export type ShellGroup = {
  /** The group id, which is the shell's pid: a `detached` spawn makes the shell a group leader. */
  pgid: number
  /** When the shell was spawned, in epoch milliseconds. Diagnostics only. */
  spawnedAtMs: number
  /** When the shell exited, in epoch milliseconds; absent while it runs. */
  exitedAtMs?: number
}

/** One process in the snapshot: its group, and when it started. */
export type GroupMember = { pid: number; startedAtMs: number }

/** Above this many records, one snapshot prunes the groups that have emptied. Not on the spawn path. */
const PRUNE_AT = 1000

/**
 * Whether a recorded group still holds a process that could be ours.
 *
 * `group.exitedAtMs === undefined` means the shell was still running when the record was taken: its pid is
 * still in use by it, so the group cannot have been reused (`kill`).
 *
 * Otherwise the group is ours only if some member started at or before the shell exited. A reused group id
 * can only hold processes that started after the number came free, which is later. Same-second comparisons
 * count as "or before": id wrap inside one second is the accepted residual risk.
 */
export function groupIsOurs(group: ShellGroup, members: readonly GroupMember[]): boolean {
  if (group.exitedAtMs === undefined) return true
  if (members.length === 0) return false
  const exitedAt = group.exitedAtMs
  return members.some((member) => member.startedAtMs <= exitedAt)
}

/** A set that reports every pid Pi adds and every pid Pi finishes with, and nothing else. */
class RecordingPids extends Set<number> {
  readonly #spawned: (pid: number) => void
  readonly #exited: (pid: number) => void

  constructor(spawned: (pid: number) => void, exited: (pid: number) => void) {
    super()
    this.#spawned = spawned
    this.#exited = exited
  }

  override add(pid: number): this {
    if (!this.has(pid)) this.#spawned(pid)
    return super.add(pid)
  }

  override delete(pid: number): boolean {
    // Pi deletes a pid when its shell settles, which is the exit: the one moment a background process can be
    // left behind in a group that will empty.
    this.#exited(pid)
    return super.delete(pid)
  }
}

export function createToolProcesses(options: {
  /** The shell Pi must use for `bash`, on a Windows install that has Git Bash. */
  shellPath?: string
  /** Where a conversation's shell starts when its agent names no directory. */
  cwd?: string
}): ToolProcesses {
  /** Shell groups this Kernel may own. Growth is bounded by pruning; nothing is scanned per spawn. */
  const groups = new Map<number, ShellGroup>()
  const environments: NodeExecutionEnv[] = []

  const spawned = (pid: number): void => {
    // Windows has no groups: the Host's Job Object owns the tree (see the comment above the type).
    if (process.platform === 'win32') return
    groups.set(pid, { pgid: pid, spawnedAtMs: Date.now() })
    if (groups.size > PRUNE_AT) prune()
  }
  const exited = (pid: number): void => {
    const group = groups.get(pid)
    if (group !== undefined) group.exitedAtMs = Date.now()
  }
  const prune = (): void => {
    const snapshot = processSnapshot()
    if (snapshot === undefined) return
    for (const pgid of [...groups.keys()]) {
      if ((snapshot.get(pgid) ?? []).length === 0) groups.delete(pgid)
    }
  }

  return {
    env: (target) => {
      const environment = new NodeExecutionEnv({
        cwd: target.cwd ?? options.cwd ?? process.cwd(),
        ...(options.shellPath === undefined ? {} : { shellPath: options.shellPath }),
      })
      // Pi's own set stays the live one it cleans up with; ours hears every spawn and every exit.
      const live = (environment as unknown as { activeChildPids?: Set<number> }).activeChildPids
      if (live !== undefined) {
        ;(environment as unknown as { activeChildPids: Set<number> }).activeChildPids =
          new RecordingPids(spawned, exited)
      }
      // A cleaned environment is dropped as it is cleaned: Pi builds one per task, and holding them until
      // shutdown would grow with every task the Kernel ever ran.
      const drop = (): void => {
        const index = environments.indexOf(environment)
        if (index >= 0) environments.splice(index, 1)
      }
      const cleanup = environment.cleanup.bind(environment)
      environment.cleanup = async (context: Context): Promise<void> => {
        try {
          await cleanup(context)
        } finally {
          drop()
        }
      }
      environments.push(environment)
      return environment
    },
    stop: async (context) => {
      // First Pi's own cleanup: a tool still running is a shell Pi is waiting on, and Pi ends that tree. Each
      // cleanup drops its environment from the list, so iterate a copy.
      for (const environment of [...environments]) {
        await environment.cleanup(context).catch(() => undefined)
      }
      environments.length = 0
      if (process.platform === 'win32') return
      if (groups.size === 0) return

      // One snapshot for every record. Without it, no evidence, so nothing is signalled.
      const snapshot = processSnapshot()
      if (snapshot === undefined) {
        warn('the tool process groups could not be inspected; leaving them alone', {
          groups: groups.size,
        })
        groups.clear()
        return
      }
      for (const [pgid, group] of groups) {
        const members = snapshot.get(pgid) ?? []
        if (!groupIsOurs(group, members)) {
          groups.delete(pgid)
          continue
        }
        try {
          process.kill(-pgid, 'SIGKILL')
        } catch {
          // ESRCH: the group ended between the snapshot and the signal. Nothing to do.
        }
        groups.delete(pgid)
      }
      groups.clear()
    },
  }
}

/** How long a snapshot may take: `ps` reads a table, it does not wait for anything. */
const SNAPSHOT_TIMEOUT_MS = 5000

/**
 * Every live process by the group it belongs to, with when it started, on this process's clock.
 *
 * One `ps -A -o pid= -o pgid= -o etime=` under `LC_ALL=C`. Elapsed time, not a printed wall-clock date: `ps`
 * prints a date in the machine's time zone, while the JavaScript a test runner gives us may be in another one
 * (`bun test` reports a zero offset on a +08:00 machine), and a timestamp built from those two readings is
 * hours off. Elapsed seconds subtracted from `Date.now()` are in the same frame as the times recorded here.
 *
 * Elapsed seconds are truncated, so the true start lies inside a one-second window. The estimate taken is the
 * window's **earlier** end, which is the same choice the comparison rule accepts: a group whose member looks
 * at most a second older than it is reads as ours, and a group id reused inside that second reads as ours too —
 * the residual risk the rule is written around. Taking the later end instead would miss a tool's own background
 * process that started in the shell's last second, which is the case the code exists for.
 *
 * `undefined` means the table could not be read — no `ps`, a timeout, or a line this cannot parse — and the
 * caller then signals nothing.
 */
export function processSnapshot(now = Date.now()): Map<number, GroupMember[]> | undefined {
  let listing: string
  try {
    listing = execFileSync('ps', ['-A', '-o', 'pid=', '-o', 'pgid=', '-o', 'etime='], {
      encoding: 'utf8',
      timeout: SNAPSHOT_TIMEOUT_MS,
      env: { ...process.env, LC_ALL: 'C', LANG: 'C' },
    })
  } catch {
    return undefined
  }
  const byGroup = new Map<number, GroupMember[]>()
  for (const line of listing.split('\n')) {
    const trimmed = line.trim()
    if (trimmed === '') continue
    const fields = trimmed.split(/\s+/)
    if (fields.length !== 3) return undefined
    const pid = Number(fields[0])
    const pgid = Number(fields[1])
    const elapsed = parseElapsedSeconds(fields[2]!)
    if (!Number.isInteger(pid) || !Number.isInteger(pgid) || elapsed === undefined) return undefined
    // The earlier end of the second the start could fall in: see `processSnapshot`.
    const member: GroupMember = { pid, startedAtMs: now - elapsed * 1000 - 999 }
    const group = byGroup.get(pgid)
    if (group === undefined) byGroup.set(pgid, [member])
    else group.push(member)
  }
  return byGroup
}

/**
 * `etime` as seconds: `[[dd-]hh:]mm:ss`.
 *
 * Two readings of the same form exist — BSD prints `[[dd-]hh:]mm:ss`, older GNU also allows a bare `mm:ss` —
 * and both are accepted, because a wrong parse of one line must not decide which processes are killed.
 */
export function parseElapsedSeconds(value: string): number | undefined {
  const match = /^(?:(\d+)-)?(?:(\d+):)?(\d{1,2}):(\d{2})$/.exec(value.trim())
  if (match === null) return undefined
  const days = Number(match[1] ?? 0)
  const hours = Number(match[2] ?? 0)
  const minutes = Number(match[3])
  const seconds = Number(match[4])
  if ([days, hours, minutes, seconds].some((part) => !Number.isInteger(part))) return undefined
  return days * 86_400 + hours * 3600 + minutes * 60 + seconds
}
