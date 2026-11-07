import { execFileSync } from 'node:child_process'
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node'
import type { Context } from '@earendil-works/chord'

/**
 * The tool processes one Kernel started, and how to end them — safely.
 *
 * Pi runs a tool shell with `detached: true` on Unix (`pi-durable/dist/env/node.js`), which puts the shell in
 * a process group of its own. That is deliberate — the shell must not receive a Ctrl-C aimed at the Kernel —
 * and it has a consequence: the Kernel's own process group does not contain the tools, so killing the Kernel
 * does not kill them. A `sleep 100 &` a tool started keeps running, reparented to PID 1.
 *
 * Pi does keep the shells it is waiting on (`NodeExecutionEnv.activeChildPids` and its `cleanup()`), which
 * covers a tool still running at shutdown. It forgets a shell when that shell exits — and a shell that
 * started a background process exits at once, leaving the background process behind in the shell's group.
 * So this records the shells Pi forgets.
 *
 * **A pid is not an identity.** A shell that exited hours ago leaves a number, and the system may hand that
 * number to an unrelated process — as its pid, or as a new group's pgid. Signalling `kill(-number)` because
 * the Kernel once saw that number would kill a stranger's tree when the user closes Droi. So each record
 * carries the shell's **start time**, and the group is signalled only when the evidence still points at it:
 *
 * 1. No process has this pgid → the group is empty; forget it and signal nothing.
 * 2. The leader (pid == pgid) still exists → compare its start time with the recorded one. Equal: the group
 *    is ours, signal it. Different: the number was reused, forget it and leave it alone.
 * 3. The leader is gone but members remain → the number is still in use by this group, and a pgid in use is
 *    never handed to another group, so those members are this shell's background processes. Signal it.
 *
 * Empty records are dropped as they are noticed (when a new shell is recorded, and at shutdown), so a
 * long-lived Kernel does not accumulate numbers.
 *
 * Windows has no process groups: a tool tree belongs to the Host's process tree, the Job Object #228 gives the
 * Kernel, whose handle ends its members when the Kernel exits. This file therefore does nothing on Windows —
 * asking `taskkill /T` for a remembered pid would repeat the very pid-reuse mistake above, and the Job Object
 * already owns the answer.
 */
export type ToolProcesses = {
  /** The `env` a Harness is opened with: one environment per conversation, each one recorded. */
  env: (target: { cwd?: string }) => NodeExecutionEnv
  /** Stop every tool this Kernel started, including a background process it left behind. */
  stop: (context: Context) => Promise<void>
}

/** One tool shell's group, and the identity that keeps its number from being mistaken for a stranger's. */
export type ShellGroup = {
  /** The group id, which is the shell's pid: `detached` spawn makes the shell a group leader. */
  pgid: number
  /** The shell's start time as `ps` prints it; a recycled number has a different one. */
  startedAt: string
}

/** A set that tells a visitor about every pid added to it, and nothing else. */
class RecordingPids extends Set<number> {
  readonly #record: (pid: number) => void

  constructor(record: (pid: number) => void) {
    super()
    this.#record = record
  }

  override add(pid: number): this {
    if (!this.has(pid)) this.#record(pid)
    return super.add(pid)
  }
}

export function createToolProcesses(options: {
  /** The shell Pi must use for `bash`, on a Windows install that has Git Bash. */
  shellPath?: string
  /** Where a conversation's shell starts when its agent names no directory. */
  cwd?: string
}): ToolProcesses {
  /** Shell groups this Kernel may own, with the identity that proves it. */
  const groups = new Map<number, ShellGroup>()
  const environments: NodeExecutionEnv[] = []

  const record = (pid: number): void => {
    // Windows has no groups: the Host's Job Object owns the tree (see the comment above the type).
    if (process.platform === 'win32') return
    const startedAt = startTime(pid)
    // A shell that is already gone has nothing to own; a zombie left by Pi's reaping keeps its start time,
    // and its group is empty, which the shutdown check skips on its own.
    if (startedAt === undefined) return
    groups.set(pid, { pgid: pid, startedAt })
    forgetEmptyGroups(groups)
  }

  return {
    env: (target) => {
      const environment = new NodeExecutionEnv({
        cwd: target.cwd ?? options.cwd ?? process.cwd(),
        ...(options.shellPath === undefined ? {} : { shellPath: options.shellPath }),
      })
      // Pi's own set stays the live one it cleans up with; ours reports each shell as Pi spawns it.
      // `activeChildPids` is a private field of compiled JavaScript, which at run time is an ordinary
      // property.
      const live = (environment as unknown as { activeChildPids?: Set<number> }).activeChildPids
      if (live !== undefined) {
        ;(environment as unknown as { activeChildPids: Set<number> }).activeChildPids =
          new RecordingPids(record)
      }
      // Once a task's environment is cleaned up, Pi will not use it again; holding it until shutdown would
      // grow with every task. So the record drops itself when it is cleaned.
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
      // First Pi's own cleanup: a tool still running is a shell Pi is waiting on, and Pi knows how to end
      // that tree. Each cleanup drops its environment from the list, so iterate a copy.
      for (const environment of [...environments]) {
        await environment.cleanup(context).catch(() => undefined)
      }
      environments.length = 0
      if (process.platform === 'win32') return
      const groupsWithMembers = groupMembers()
      for (const [pgid, group] of groups) {
        const members = groupsWithMembers.get(pgid) ?? []
        if (members.length === 0) {
          // Empty: the shell and everything it started are gone.
          groups.delete(pgid)
          continue
        }
        const leaderStartedAt = members.includes(pgid) ? startTime(pgid) : undefined
        if (!groupIsOurs(group, members, leaderStartedAt)) {
          // A number that was reused, or a group that emptied between the listing and now.
          groups.delete(pgid)
          continue
        }
        try {
          process.kill(-pgid, 'SIGKILL')
        } catch {
          // ESRCH: the group ended between the listing and the signal. Nothing to do.
        }
        groups.delete(pgid)
      }
      groups.clear()
    },
  }
}

/** Forget records whose group has no processes left, so a long-lived Kernel keeps only live ones. */
function forgetEmptyGroups(groups: Map<number, ShellGroup>): void {
  if (groups.size === 0) return
  const members = groupMembers()
  for (const pgid of [...groups.keys()]) {
    if ((members.get(pgid) ?? []).length === 0) groups.delete(pgid)
  }
}

/** Every live process, by the group it belongs to: `ps -A -o pid=,pgid=`. */
function groupMembers(): Map<number, number[]> {
  const byGroup = new Map<number, number[]>()
  let listing: string
  try {
    listing = execFileSync('ps', ['-A', '-o', 'pid=', '-o', 'pgid='], {
      encoding: 'utf8',
      timeout: 5000,
    })
  } catch {
    // No `ps` (a stripped container image): nothing can be verified, so nothing is signalled.
    return byGroup
  }
  for (const line of listing.split('\n')) {
    const fields = line.trim().split(/\s+/)
    if (fields.length < 2) continue
    const pid = Number(fields[0])
    const pgid = Number(fields[1])
    if (!Number.isInteger(pid) || !Number.isInteger(pgid)) continue
    const group = byGroup.get(pgid)
    if (group === undefined) byGroup.set(pgid, [pid])
    else group.push(pid)
  }
  return byGroup
}

/** When a process started, as `ps` prints it: the same string only for the same process. */
function startTime(pid: number): string | undefined {
  try {
    const printed = execFileSync('ps', ['-o', 'lstart=', '-p', String(pid)], {
      encoding: 'utf8',
      timeout: 5000,
    }).trim()
    return printed === '' ? undefined : printed
  } catch {
    // The process is gone, or `ps` could not answer: either way there is nothing to identify.
    return undefined
  }
}

/**
 * Whether a listed group is still the one this Kernel started.
 *
 * The three answers, in order, are the whole safety argument:
 *
 * - no members → the group is empty; there is nothing to signal (`false`).
 * - the leader (pid == pgid) is present → only the recorded start time proves it is the same process; a
 *   different one means the number was recycled and a stranger leads a group by that name (`false`).
 * - the leader is gone but members remain → a pgid that is in use is never handed to another group, so those
 *   members are this shell's background processes (`true`).
 */
export function groupIsOurs(
  group: ShellGroup,
  members: readonly number[],
  leaderStartedAt: string | undefined,
): boolean {
  if (members.length === 0) return false
  if (!members.includes(group.pgid)) return true
  return leaderStartedAt === group.startedAt
}
