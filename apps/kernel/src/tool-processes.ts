import { spawn } from 'node:child_process'
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node'
import type { Context } from '@earendil-works/chord'

/**
 * The tool processes one Kernel started, and how to end them.
 *
 * Pi runs a tool shell with `detached: true` on Unix (`pi-durable/dist/env/node.js`), which puts the shell in
 * a process group of its own. That is deliberate — the shell must not receive a Ctrl-C aimed at the Kernel —
 * and it has a consequence: the Kernel's own process group does not contain the tools, so killing the Kernel
 * does not kill them. A `sleep 100 &` a tool started keeps running, reparented to PID 1.
 *
 * Pi does keep the shells it is waiting on (`NodeExecutionEnv.activeChildPids` and its `cleanup()`), which
 * covers a tool still running at shutdown. It forgets a shell when that shell exits — and a shell that
 * started a background process exits at once, leaving the background process behind in the shell's group.
 * So the Kernel records every shell pid it ever saw: the process group keeps its name after the leader is
 * gone, and `kill(-pgid)` still reaches the members.
 *
 * Windows has no process groups. A detached flag is not set there, and the Host's process tree (the Job
 * Object #228 gives the Kernel) owns the descendants; this file still asks `taskkill /T` for the pids it
 * recorded, which covers the tool tree while the Kernel they belong to is still running.
 */
export type ToolProcesses = {
  /** The `env` a Harness is opened with: one environment per conversation, each one recorded. */
  env: (target: { cwd?: string }) => NodeExecutionEnv
  /** Stop every tool this Kernel started, including a background process it left behind. */
  stop: (context: Context) => Promise<void>
}

/** A set that tells a visitor about every pid added to it, and nothing else. */
class RecordingPids extends Set<number> {
  readonly #record: Set<number>

  constructor(record: Set<number>) {
    super()
    this.#record = record
  }

  override add(pid: number): this {
    this.#record.add(pid)
    return super.add(pid)
  }
}

export function createToolProcesses(options: {
  /** The shell Pi must use for `bash`, on a Windows install that has Git Bash. */
  shellPath?: string
  /** Where a conversation's shell starts when its agent names no directory. */
  cwd?: string
}): ToolProcesses {
  /** Every shell pid this Kernel has seen; the group of a shell outlives the shell. */
  const spawned = new Set<number>()
  const environments: NodeExecutionEnv[] = []

  return {
    env: (target) => {
      const environment = new NodeExecutionEnv({
        cwd: target.cwd ?? options.cwd ?? process.cwd(),
        ...(options.shellPath === undefined ? {} : { shellPath: options.shellPath }),
      })
      // Pi's own set stays the live one it cleans up with; ours keeps the names for good. `activeChildPids`
      // is a private field of compiled JavaScript, which at run time is an ordinary property.
      const live = (environment as unknown as { activeChildPids?: Set<number> }).activeChildPids
      if (live !== undefined) {
        ;(environment as unknown as { activeChildPids: Set<number> }).activeChildPids =
          new RecordingPids(spawned)
      }
      environments.push(environment)
      return environment
    },
    stop: async (context) => {
      // First Pi's own cleanup: a tool still running is a shell Pi is waiting on, and Pi knows how to end
      // that tree (it signals the group on Unix and asks `taskkill /T` on Windows).
      for (const environment of environments) {
        await environment.cleanup(context).catch(() => undefined)
      }
      environments.length = 0
      for (const pid of spawned) killProcessGroup(pid)
      spawned.clear()
    },
  }
}

/**
 * End one shell's process group, and the group's background members with it.
 *
 * `-pid` is the group: the shell was spawned `detached`, so it is the leader of a group named by its pid, and
 * the name still resolves after the leader is gone for as long as any member lives. A pid that is no longer
 * a group leader answers ESRCH, which is the ordinary case of a tool that ended by itself.
 */
function killProcessGroup(pid: number): void {
  if (process.platform === 'win32') {
    // Windows has no groups: a tool tree is the Host's Job Object, and this is the best a pid can do.
    try {
      spawn('taskkill', ['/F', '/T', '/PID', String(pid)], {
        stdio: 'ignore',
        detached: true,
        windowsHide: true,
      }).once('error', () => undefined)
    } catch {
      // Nothing to end, or no taskkill: not a reason to keep a Kernel from closing.
    }
    return
  }
  try {
    process.kill(-pid, 'SIGKILL')
  } catch {
    // The group is empty, which is what a tool that finished leaves behind.
  }
}
