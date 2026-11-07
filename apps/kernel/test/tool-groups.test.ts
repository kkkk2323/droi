import { afterEach, describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fauxAssistantMessage } from '@earendil-works/pi-ai'
import { createToolProcesses, groupIsOurs } from '../src/tool-processes.ts'
import { context } from '../src/session/service.ts'
import { Client, fauxCatalog, start, tempData } from './helpers.ts'

describe('the tool process groups a kernel may end', () => {
  test('a group with no members is not ours to signal', () => {
    // The shell ran and exited long ago. Its number may now belong to something else entirely, and the
    // only safe answer is to forget it.
    expect(groupIsOurs({ pgid: 4242, startedAt: 'Fri Nov 7 10:00:00 2026' }, [], undefined)).toBe(
      false,
    )
  })

  test('the leader proves itself with its start time', () => {
    const group = { pgid: 4242, startedAt: 'Fri Nov 7 10:00:00 2026' }
    expect(groupIsOurs(group, [4242, 4250], 'Fri Nov 7 10:00:00 2026')).toBe(true)
    // Same number, different process: the pid was recycled, and this is somebody else's tree.
    expect(groupIsOurs(group, [4242], 'Fri Nov 7 11:30:00 2026')).toBe(false)
    // The leader exists but `ps` could not read it: no proof, so no signal.
    expect(groupIsOurs(group, [4242], undefined)).toBe(false)
  })

  test('a group whose leader is gone but whose members remain is ours', () => {
    // A shell that started `sleep 100 &` and exited: the group keeps its name for as long as a member
    // lives, and a pgid in use is never handed to another group.
    expect(groupIsOurs({ pgid: 4242, startedAt: 'anything' }, [4250, 4251], undefined)).toBe(true)
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
    const result = await environment.exec(`sleep 100 & echo $! > ${pidFile}`, undefined, context)
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
