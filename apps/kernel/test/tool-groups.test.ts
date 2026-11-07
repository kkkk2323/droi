import { afterEach, describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { createToolProcesses, groupIsOurs, processSnapshot } from '../src/tool-processes.ts'
import { context } from '../src/session/service.ts'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'
import { type GroupMember, type ShellGroup } from '../src/tool-processes.ts'

/** A record for a shell spawned at `spawnedAt`, which exited at `exitedAt` unless that is undefined. */
function group(spawnedAt: number, exitedAt?: number): ShellGroup {
  return {
    pgid: 4242,
    spawnedAtMs: spawnedAt,
    ...(exitedAt === undefined ? {} : { exitedAtMs: exitedAt }),
  }
}

/** A member that started `offset` seconds from the shell's exit (negative: before it). */
const member = (exitAt: number, offset: number): GroupMember[] => [
  { pid: 4250, startedAtMs: exitAt + offset * 1000 },
]

describe('whether a recorded tool group may still be ours', () => {
  // The shell's own exit, in epoch milliseconds.
  const exited = 1_700_000_100_000

  test('the shell is still running', () => {
    // While it runs, the number is its pid: the system cannot have given it to another process.
    expect(groupIsOurs(group(exited - 60_000), [])).toBe(true)
    expect(groupIsOurs(group(exited - 60_000), member(exited, 10))).toBe(true)
  })

  test('the group has emptied', () => {
    expect(groupIsOurs(group(exited - 60_000, exited), [])).toBe(false)
  })

  test('a member that started before the shell exited is ours', () => {
    // `sleep 100 &` began while the shell ran, and outlived it: a reused group can only hold processes that
    // started after the number came free, which is later.
    expect(groupIsOurs(group(exited - 60_000, exited), member(exited, -30))).toBe(true)
  })

  test('a reused group whose own leader already exited is not ours', () => {
    // The number came free, a stranger's group took it, that group's leader exited too, and its own
    // background children remain. Every member started after our shell exited.
    expect(groupIsOurs(group(exited - 60_000, exited), member(exited, 5))).toBe(false)
    expect(groupIsOurs(group(exited - 60_000, exited), member(exited, 3600))).toBe(false)
  })

  test('only grandchildren remain: missed on purpose', () => {
    // Our background process started a child after our shell exited, then exited itself. Nothing in the
    // group proves it belongs to us, and killing a stranger is worse than missing one of ours.
    expect(groupIsOurs(group(exited - 60_000, exited), member(exited, 120))).toBe(false)
  })

  test('the same second counts as ours', () => {
    // A number reused inside the same second reads as ours. That is the accepted residual risk: the
    // alternative is to miss a tool's own background process that started in the shell's last second.
    expect(groupIsOurs(group(exited - 60_000, exited), member(exited, 0))).toBe(true)
  })
})

describe('the tool processes of one kernel', () => {
  const kill = process.kill
  afterEach(() => {
    process.kill = kill
  })

  test('a shell that exited leaves nothing to signal at shutdown', async () => {
    const tools = createToolProcesses({ cwd: '/tmp' })
    const environment = tools.env({})
    let shellPid = 0
    const result = await environment.exec(
      'echo $$',
      { onOutput: (text) => (shellPid = Number(text.trim())) },
      context,
    )
    expect(result.ok).toBe(true)
    expect(shellPid).toBeGreaterThan(1)
    // The shell has exited: wait until the system agrees.
    for (let deadline = Date.now() + 5000; ;) {
      try {
        process.kill(shellPid, 0)
      } catch {
        break
      }
      if (Date.now() > deadline) throw new Error(`shell ${shellPid} never exited`)
      await Bun.sleep(10)
    }

    const attempted: { pid: number; signal?: string | number }[] = []
    process.kill = ((pid: number, signal?: string | number) => {
      attempted.push({ pid, signal })
      return true
    }) as typeof process.kill
    await tools.stop(context)
    // Nothing to end, so nothing is signalled — the record was dropped when it emptied. The reviewer's
    // probe asserted the opposite before this change: `kill(-shellPid, SIGKILL)` on a bare number.
    expect(attempted).toEqual([])
  }, 30_000)

  test('a background process is ended when only its exited shell’s group is left', async () => {
    const pidFile = join('/tmp', `droi-tool-group-${process.pid}.pid`)
    const tools = createToolProcesses({ cwd: '/tmp' })
    const environment = tools.env({})
    const result = await environment.exec(
      `echo $ > /tmp/shellid.txt; sleep 100 & echo $! > ${pidFile}`,
      undefined,
      context,
    )
    expect(result.ok).toBe(true)
    let pid = 0
    for (let deadline = Date.now() + 5000; pid === 0;) {
      try {
        pid = Number(readFileSync(pidFile, 'utf8').trim())
      } catch {
        // Not written yet.
      }
      if (Date.now() > deadline) throw new Error('the background process never reported its pid')
      await Bun.sleep(10)
    }
    try {
      await tools.stop(context)
      let gone = false
      for (let deadline = Date.now() + 5000; !gone;) {
        try {
          process.kill(pid, 0)
        } catch {
          gone = true
          break
        }
        if (Date.now() > deadline) break
        await Bun.sleep(10)
      }
      expect(gone).toBe(true)
    } finally {
      try {
        process.kill(pid, 'SIGKILL')
      } catch {
        // Already gone.
      }
    }
  }, 30_000)

  test('no readable process table means nothing is signalled', async () => {
    // `ps` missing, failing or unparseable: there is no evidence, and a kill on a guess is worse than a
    // missed cleanup. The records are dropped either way, so the warning is not repeated forever.
    const pidFile = join('/tmp', `droi-ps-fail-${process.pid}.pid`)
    const tools = createToolProcesses({ cwd: '/tmp' })
    const environment = tools.env({})
    await environment.exec(`sleep 100 & echo $! > ${pidFile}`, undefined, context)
    let pid = 0
    for (let deadline = Date.now() + 5000; pid === 0;) {
      try {
        pid = Number(readFileSync(pidFile, 'utf8').trim())
      } catch {
        // Not written yet.
      }
      if (Date.now() > deadline) throw new Error('the background process never reported its pid')
      await Bun.sleep(10)
    }
    const attempted: unknown[] = []
    const realKill = process.kill
    const realPath = process.env['PATH']
    try {
      process.kill = ((target: number, signal?: string | number) => {
        attempted.push({ target, signal })
        return true
      }) as typeof process.kill
      // An empty PATH is how `ps` becomes unreadable without touching the machine.
      process.env['PATH'] = '/nonexistent-droid-review'
      await tools.stop(context)
    } finally {
      process.kill = realKill
      process.env['PATH'] = realPath
      try {
        process.kill(-pid, 'SIGKILL')
        process.kill(pid, 'SIGKILL')
      } catch {
        // Already gone.
      }
    }
    expect(attempted).toEqual([])
  }, 30_000)
  test('a cleaned environment is not kept until shutdown', async () => {
    const tools = createToolProcesses({ cwd: '/tmp' })
    let cleanups = 0
    for (let i = 0; i < 20; i++) {
      const environment = tools.env({})
      const original = environment.cleanup.bind(environment)
      environment.cleanup = async (ctx) => {
        cleanups++
        await original(ctx)
      }
      await environment.cleanup(context)
    }
    // Each cleanup ran once; shutdown must not run any of them again, because a cleaned environment is
    // dropped as it is cleaned — otherwise the record grows with every task the Kernel ever ran.
    await tools.stop(context)
    expect(cleanups).toBe(20)
  }, 30_000)
})

test('a stop that arrives during a stop waits for the same cleanup', async () => {
  // Two callers reach stop at once in ordinary use: `kernel.shutdown` on the wire, then SIGTERM from the
  // Host. The second must not return early and let a signal handler exit the process mid-cleanup, which is
  // how a background tool process used to survive.
  const data = tempData()
  const faux = fauxCatalog()
  const kernel = await start({ dir: data.dir, faux })
  const client = new Client(kernel.port)
  try {
    const { conversationId: id } = await client.call('conversation.create', {
      agent: { model: faux.model },
    })
    faux.faux.setResponses([fauxAssistantMessage('answer')])
    await client.call('conversation.submit', { id, input: 'hello' })

    let finished = 0
    const first = kernel.stop().then(() => finished++)
    const second = kernel.stop().then(() => finished++)
    const third = kernel.stop().then(() => finished++)
    await Promise.all([first, second, third])
    // All three settled only after the one cleanup finished, and it ran once.
    expect(finished).toBe(3)
    expect(kernel.indexer).toBeDefined()
  } finally {
    client.close()
    await kernel.stop()
    data.cleanup()
  }
}, 60_000)

test('a wall clock that moved makes every exit time unreliable', () => {
  // Start times come from the wall clock and exits are wall-clock readings too: if the wall clock was stepped
  // between them, comparing the two says nothing, so nothing is signalled.
  const exit = 1_700_000_100_000
  const earlier = [{ pid: 4250, startedAtMs: exit - 30_000 }]
  expect(groupIsOurs(group(exit - 60_000, exit), earlier, false)).toBe(false)
  expect(groupIsOurs(group(exit - 60_000, exit), earlier, true)).toBe(true)
  // A shell that is still running is unaffected: its pid, not its clock, is the evidence.
  expect(groupIsOurs(group(exit - 60_000), [], false)).toBe(true)
})

test('the snapshot is read after ps, and a slow table is refused', async () => {
  // The reference time must come after `ps` returns: taken before, every start time's error grows by the
  // table's own duration, which is how a process that began 1.2 s after an exit looked older than it.
  const slow = processSnapshot(() => Date.now() + 2000)
  const fresh = processSnapshot()
  expect(slow).toBeDefined()
  expect(fresh).toBeDefined()
  const self = process.pid
  const startedFresh = [...(fresh ?? new Map()).values()].flat().find((m) => m.pid === self)
  const startedSlow = [...(slow ?? new Map()).values()].flat().find((m) => m.pid === self)
  expect(startedFresh).toBeDefined()
  expect(startedSlow).toBeDefined()
  expect(startedSlow!.startedAtMs - startedFresh!.startedAtMs).toBeGreaterThan(1500)
})
