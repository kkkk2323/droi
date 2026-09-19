import { afterEach, describe, expect, test } from 'vitest'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { connect } from 'node:net'
import { DaemonSupervisor, type DaemonState } from './daemon-supervisor'

// A stand-in for `droid daemon`: listens on the given port until killed.
const FAKE_DAEMON = `
  const net = require('node:net');
  const server = net.createServer();
  server.listen(Number(process.argv[1]), '127.0.0.1');
  process.on('SIGTERM', () => process.exit(0));
`

function spawnFakeDaemon(port: number) {
  return spawn(process.execPath, ['-e', FAKE_DAEMON, String(port)], { stdio: 'ignore' })
}

function isListening(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = connect(port, '127.0.0.1')
    socket.once('connect', () => {
      socket.destroy()
      resolve(true)
    })
    socket.once('error', () => resolve(false))
  })
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

async function waitForStatus(
  supervisor: DaemonSupervisor,
  status: DaemonState['status'],
): Promise<DaemonState> {
  if (supervisor.state.status === status) return supervisor.state
  for (;;) {
    const [state] = (await once(supervisor, 'state')) as [DaemonState]
    if (state.status === status) return state
  }
}

describe('DaemonSupervisor', () => {
  let supervisor: DaemonSupervisor | undefined

  afterEach(async () => {
    await supervisor?.stop()
  })

  test('starts one daemon and reports running with its port', async () => {
    supervisor = new DaemonSupervisor({ spawn: spawnFakeDaemon })
    supervisor.start()
    const state = await waitForStatus(supervisor, 'running')
    if (state.status !== 'running') throw new Error('unreachable')
    expect(await isListening(state.port)).toBe(true)
    expect(supervisor.daemonUrl).toBe(`ws://127.0.0.1:${state.port}`)
  })

  test('stop ends the daemon process and does not restart it', async () => {
    supervisor = new DaemonSupervisor({ spawn: spawnFakeDaemon })
    supervisor.start()
    const running = await waitForStatus(supervisor, 'running')
    if (running.status !== 'running') throw new Error('unreachable')
    await supervisor.stop()
    expect(supervisor.state.status).toBe('stopped')
    expect(isAlive(running.pid)).toBe(false)
    expect(supervisor.daemonUrl).toBeNull()
  })

  test('restarts with backoff after the daemon dies', async () => {
    const states: DaemonState[] = []
    supervisor = new DaemonSupervisor({ spawn: spawnFakeDaemon, backoffMs: [50, 100] })
    supervisor.on('state', (state) => states.push(state))
    supervisor.start()
    const first = await waitForStatus(supervisor, 'running')
    if (first.status !== 'running') throw new Error('unreachable')

    process.kill(first.pid, 'SIGKILL')
    const restarting = await waitForStatus(supervisor, 'restarting')
    expect(restarting).toMatchObject({ status: 'restarting', delayMs: 50 })

    const second = await waitForStatus(supervisor, 'running')
    if (second.status !== 'running') throw new Error('unreachable')
    expect(second.pid).not.toBe(first.pid)
    expect(isAlive(first.pid)).toBe(false)
  })

  test('backoff grows on repeated failures and caps at the last step', async () => {
    supervisor = new DaemonSupervisor({
      spawn: (port) => spawn(process.execPath, ['-e', 'process.exit(1)', String(port)]),
      backoffMs: [10, 20],
      readyTimeoutMs: 500,
    })
    const delays: number[] = []
    supervisor.on('state', (state) => {
      if (state.status === 'restarting') delays.push(state.delayMs)
    })
    supervisor.start()
    while (delays.length < 3) await once(supervisor, 'state')
    expect(delays.slice(0, 3)).toEqual([10, 20, 20])
  })
})
