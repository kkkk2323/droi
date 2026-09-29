import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { existsSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { openMemoryStore } from '../../memory/store'
import { createMemoryController, type MemoryController } from './memory-controller'
import type { MemorySessionRequest } from './memory-session'

const DEFAULTS = fileURLToPath(new URL('../../../resources/memory', import.meta.url))

let root: string
let memoryDir: string
let controller: MemoryController
let requests: MemorySessionRequest[]
let changes: number
let daemonUp: boolean

beforeEach(() => {
  root = realpathSync(mkdtempSync(join(tmpdir(), 'droi-memory-controller-')))
  memoryDir = join(root, 'memory')
  requests = []
  changes = 0
  daemonUp = true
  controller = createMemoryController({
    memoryDir,
    defaultsDir: DEFAULTS,
    modelId: () => 'glm-5.3-flash',
    onChange: () => void (changes += 1),
    runner: () =>
      daemonUp
        ? async (request) => {
            requests.push(request)
            if (request.title.startsWith('Memory: extract')) {
              return { entries: [{ scope: 'project', category: 'insight', text: 'learned' }] }
            }
            const { entries } = JSON.parse(request.input) as { entries: Array<{ id: string }> }
            return { keep: entries.map((e) => e.id), rewrite: [], remove: [], merge: [] }
          }
        : null,
  })
})

afterEach(() => {
  controller.stop()
  rmSync(root, { recursive: true, force: true })
})

function seed(workspace: string, count: number) {
  const store = openMemoryStore(memoryDir)
  for (let i = 0; i < count; i++)
    store.add({ scope: 'project', workspace }, 'convention', `fact ${i}`)
  store.close()
}

function writeRequest(sessionId: string) {
  const transcript = join(root, `${sessionId}.jsonl`)
  writeFileSync(
    transcript,
    JSON.stringify({
      type: 'message',
      message: { role: 'user', content: [{ type: 'text', text: 'hi' }] },
    }),
  )
  mkdirSync(join(memoryDir, 'requests'), { recursive: true })
  const file = join(memoryDir, 'requests', `${sessionId}.json`)
  writeFileSync(
    file,
    JSON.stringify({
      sessionId,
      transcriptPath: transcript,
      cwd: root,
      event: 'SessionEnd',
      requestedAt: '',
    }),
  )
  return file
}

describe('Memory controller', () => {
  it('lists every Project Memory and the Global Memory with its limits', () => {
    seed('/Users/dev/app', 3)
    expect(controller.overview()).toEqual({
      rows: [
        {
          workspace: '/Users/dev/app',
          entries: 3,
          chars: 18,
          softLimit: 200_000,
          overSoftLimit: false,
          lastConsolidated: null,
          consolidating: false,
        },
        expect.objectContaining({ workspace: null, softLimit: 40_000, entries: 0 }),
      ],
    })
  })

  it('consolidates one Memory with the chosen model and says how the slices went', async () => {
    seed('/Users/dev/app', 3)
    const result = await controller.consolidate('/Users/dev/app')
    expect(result).toEqual({ applied: 1, rejected: 0 })
    expect(requests[0]).toMatchObject({
      modelId: 'glm-5.3-flash',
      prompt: expect.stringContaining('consolidating'),
    })
    expect(controller.overview().rows[0]?.lastConsolidated).not.toBeNull()
    expect(changes).toBeGreaterThanOrEqual(2)
  })

  it('refuses to consolidate without a Daemon', async () => {
    daemonUp = false
    await expect(controller.consolidate(null)).rejects.toThrow(/not running/)
  })

  it('runs an extraction for every request file, then removes it', async () => {
    const file = writeRequest('s-1')
    controller.watchRequests()
    await expect.poll(() => existsSync(file)).toBe(false)
    expect(requests.map((r) => r.title)).toEqual(['Memory: extract from s-1'])
    const later = writeRequest('s-2')
    await expect.poll(() => existsSync(later)).toBe(false)
    expect(requests).toHaveLength(2)
    expect(controller.overview().rows[0]).toMatchObject({ workspace: root, entries: 2 })
  })

  it('leaves requests waiting while there is no Daemon', async () => {
    daemonUp = false
    const file = writeRequest('s-3')
    await controller.processRequests()
    expect(existsSync(file)).toBe(true)
    daemonUp = true
    await controller.processRequests()
    expect(existsSync(file)).toBe(false)
  })
})
