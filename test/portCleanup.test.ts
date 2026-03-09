import assert from 'node:assert/strict'
import test from 'node:test'

import {
  cleanupListeningPorts,
  findListeningPidsForPort,
  MISSION_E2E_PORTS,
} from '../src/backend/utils/portCleanup.ts'

test('findListeningPidsForPort returns parsed listening pids from lsof output', async () => {
  const pids = await findListeningPidsForPort(9222, {
    execFile: async (_file, args) => {
      assert.deepEqual(args, ['-tiTCP:9222', '-sTCP:LISTEN'])
      return { stdout: '101\n202\n' }
    },
  })

  assert.deepEqual(pids, [101, 202])
})

test('findListeningPidsForPort returns empty when lsof fails', async () => {
  const pids = await findListeningPidsForPort(5173, {
    execFile: async () => {
      throw new Error('no listeners')
    },
  })

  assert.deepEqual(pids, [])
})

test('cleanupListeningPorts kills unique listening pids across mission e2e ports', async () => {
  const execCalls: string[][] = []
  const killed: number[] = []

  const cleaned = await cleanupListeningPorts(MISSION_E2E_PORTS, {
    execFile: async (_file, args) => {
      execCalls.push(args)
      const [portArg] = args
      if (portArg === '-tiTCP:9222') return { stdout: '111\n222\n' }
      if (portArg === '-tiTCP:5173') return { stdout: '222\n333\n' }
      if (portArg === '-tiTCP:3002') return { stdout: '' }
      throw new Error('unexpected port')
    },
    kill: (pid) => {
      killed.push(pid)
    },
  })

  assert.deepEqual(execCalls, [
    ['-tiTCP:9222', '-sTCP:LISTEN'],
    ['-tiTCP:5173', '-sTCP:LISTEN'],
    ['-tiTCP:3002', '-sTCP:LISTEN'],
  ])
  assert.deepEqual(cleaned, [111, 222, 333])
  assert.deepEqual(killed, [111, 222, 333])
})

test('cleanupListeningPorts ignores kill errors so relaunch stays idempotent', async () => {
  const killed: number[] = []

  const cleaned = await cleanupListeningPorts([3002], {
    execFile: async () => ({ stdout: '444\n555\n' }),
    kill: (pid) => {
      killed.push(pid)
      if (pid === 555) throw new Error('already exited')
    },
  })

  assert.deepEqual(cleaned, [444, 555])
  assert.deepEqual(killed, [444, 555])
})
