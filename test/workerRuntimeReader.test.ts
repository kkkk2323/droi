import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import {
  readMissionRuntimeSnapshot,
  resolveWorkerSessionPath,
} from '../src/backend/mission/workerRuntimeReader.ts'

test('readMissionRuntimeSnapshot reads worker session jsonl and extracts commands plus tool results', async () => {
  const factoryHome = await mkdtemp(join(tmpdir(), 'droi-factory-home-'))
  const workingDirectory = '/repo/project'
  const workerSessionId = 'worker-123'
  const previousFactoryHome = process.env['FACTORY_HOME_OVERRIDE']
  process.env['FACTORY_HOME_OVERRIDE'] = factoryHome

  try {
    const sessionPath = resolveWorkerSessionPath({ workingDirectory, workerSessionId })
    await mkdir(dirname(sessionPath), { recursive: true })
    await writeFile(
      sessionPath,
      [
        JSON.stringify({ type: 'session_start', id: workerSessionId, cwd: workingDirectory }),
        JSON.stringify({
          type: 'message',
          timestamp: '2026-03-11T08:00:00.000Z',
          message: {
            role: 'assistant',
            content: [
              { type: 'tool_use', name: 'Execute', input: { command: 'git status --short' } },
            ],
          },
        }),
        JSON.stringify({
          type: 'message',
          timestamp: '2026-03-11T08:00:01.000Z',
          message: {
            role: 'assistant',
            content: [
              { type: 'tool_result', content: { success: true, content: 'working tree clean' } },
            ],
          },
        }),
        JSON.stringify({
          type: 'message',
          timestamp: '2026-03-11T08:00:02.000Z',
          message: {
            role: 'user',
            content: [{ type: 'text', text: 'Please continue with the worker.' }],
          },
        }),
      ].join('\n'),
    )

    const snapshot = await readMissionRuntimeSnapshot({
      sessionId: 'mission-1',
      workerSessionId,
      workingDirectory,
    })

    assert.equal(snapshot.status, 'ready')
    assert.equal(snapshot.exists, true)
    assert.equal(snapshot.entries.length, 3)
    assert.equal(snapshot.entries[0]?.kind, 'command')
    assert.equal(snapshot.entries[0]?.text, 'git status --short')
    assert.equal(snapshot.entries[1]?.kind, 'result')
    assert.match(snapshot.entries[1]?.text || '', /working tree clean/)
    assert.equal(snapshot.entries[2]?.kind, 'message')
    assert.match(snapshot.entries[2]?.text || '', /Please continue with the worker/)
  } finally {
    if (previousFactoryHome === undefined) delete process.env['FACTORY_HOME_OVERRIDE']
    else process.env['FACTORY_HOME_OVERRIDE'] = previousFactoryHome
  }
})

test('readMissionRuntimeSnapshot falls back to missionDir working_directory.txt', async () => {
  const factoryHome = await mkdtemp(join(tmpdir(), 'droi-factory-home-'))
  const missionDir = join(factoryHome, '.factory', 'missions', 'mission-base')
  const workingDirectory = '/repo/from-mission-dir'
  const workerSessionId = 'worker-456'
  const previousFactoryHome = process.env['FACTORY_HOME_OVERRIDE']
  process.env['FACTORY_HOME_OVERRIDE'] = factoryHome

  try {
    await mkdir(missionDir, { recursive: true })
    await writeFile(join(missionDir, 'working_directory.txt'), `${workingDirectory}\n`)

    const sessionPath = resolveWorkerSessionPath({ workingDirectory, workerSessionId })
    await mkdir(dirname(sessionPath), { recursive: true })
    await writeFile(
      sessionPath,
      JSON.stringify({
        type: 'message',
        timestamp: '2026-03-11T08:05:00.000Z',
        message: {
          role: 'assistant',
          content: [{ type: 'tool_use', name: 'Execute', input: { command: 'pnpm test' } }],
        },
      }),
    )

    const snapshot = await readMissionRuntimeSnapshot({
      sessionId: 'mission-2',
      missionDir,
      workerSessionId,
    })

    assert.equal(snapshot.workingDirectory, workingDirectory)
    assert.equal(snapshot.status, 'ready')
    assert.equal(snapshot.entries[0]?.text, 'pnpm test')
  } finally {
    if (previousFactoryHome === undefined) delete process.env['FACTORY_HOME_OVERRIDE']
    else process.env['FACTORY_HOME_OVERRIDE'] = previousFactoryHome
  }
})
