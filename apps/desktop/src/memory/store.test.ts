import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { LIMITS, openMemoryStore, type MemorySlot, type MemoryStore } from './store'
import { runTs } from './test-support/processes'

const project: MemorySlot = { scope: 'project', workspace: '/Users/dev/app' }
const other: MemorySlot = { scope: 'project', workspace: '/Users/dev/other' }
const global: MemorySlot = { scope: 'global' }

let dir: string
let store: MemoryStore

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'droi-memory-'))
  store = openMemoryStore(dir)
})

afterEach(() => {
  store.close()
  rmSync(dir, { recursive: true, force: true })
})

function add(
  slot: MemorySlot,
  text: string,
  category: Parameters<MemoryStore['add']>[1] = 'insight',
) {
  const result = store.add(slot, category, text)
  if (!result.ok) throw new Error(result.reason)
  return result.entry
}

describe('Memory store', () => {
  it('adds, reads, replaces and removes entries', () => {
    const entry = add(project, '  pnpm check runs format, lint and typecheck  ', 'convention')
    expect(entry).toEqual({
      id: expect.stringMatching(/^[0-9a-f]{8}$/),
      scope: 'project',
      workspace: '/Users/dev/app',
      category: 'convention',
      day: new Date().toISOString().slice(0, 10),
      text: 'pnpm check runs format, lint and typecheck',
    })
    expect(store.get(entry.id)).toEqual(entry)

    const replaced = store.replace(entry.id, 'pnpm check runs format, lint and all typechecks')
    expect(replaced).toMatchObject({
      ok: true,
      entry: { id: entry.id, text: expect.stringContaining('all') },
    })

    expect(store.remove(entry.id)).toBe(true)
    expect(store.get(entry.id)).toBeNull()
    expect(store.remove(entry.id)).toBe(false)
    expect(store.replace(entry.id, 'x')).toMatchObject({ ok: false })
  })

  it('gives every entry its own 8-character hex id', () => {
    const ids = new Set(Array.from({ length: 300 }, (_, i) => add(project, `note ${i}`).id))
    expect(ids.size).toBe(300)
    for (const id of ids) expect(id).toMatch(/^[0-9a-f]{8}$/)
  })

  it('keeps each Workspace and the Global Memory apart', () => {
    add(project, 'app uses vitest')
    add(other, 'other uses jest')
    add(global, 'answer in Chinese', 'preference')
    expect(store.list(project).map((e) => e.text)).toEqual(['app uses vitest'])
    expect(store.list(other).map((e) => e.text)).toEqual(['other uses jest'])
    expect(store.list(global)).toEqual([
      expect.objectContaining({ scope: 'global', workspace: null, category: 'preference' }),
    ])
  })

  it('lists one category when asked', () => {
    add(project, 'a failure', 'failure')
    add(project, 'a convention', 'convention')
    expect(store.list(project, 'failure').map((e) => e.text)).toEqual(['a failure'])
  })

  it('refuses text that carries a secret, and says why', () => {
    const result = store.add(
      project,
      'tool-quirk',
      'the proxy key is fk-AbCdEfGhIjKlMnOpQrStUv123456',
    )
    expect(result).toEqual({ ok: false, reason: expect.stringMatching(/Factory API key/) })
    expect(store.list(project)).toEqual([])
    const entry = add(project, 'the proxy needs a key')
    expect(store.replace(entry.id, 'password=hunter2')).toMatchObject({ ok: false })
    expect(store.get(entry.id)?.text).toBe('the proxy needs a key')
  })

  it('marks the soft limit and refuses writes past the hard one', () => {
    const chunk = 'x'.repeat(49_000)
    for (let i = 0; i < 4; i++) {
      expect(store.add(project, 'insight', `${i}${chunk}`)).toMatchObject({
        ok: true,
        overSoftLimit: false,
      })
    }
    expect(store.add(project, 'insight', `4${chunk}`)).toMatchObject({
      ok: true,
      overSoftLimit: true,
    })
    expect(store.summaries()[0]).toMatchObject({
      workspace: project.workspace,
      overSoftLimit: true,
    })

    add(project, 'z'.repeat(LIMITS.project.hard - store.size(project)))
    const refused = store.add(project, 'insight', 'one more')
    expect(refused).toEqual({ ok: false, reason: expect.stringMatching(/Settings → Memory/) })
    // Other Memories are unaffected.
    expect(store.add(other, 'insight', 'fine')).toMatchObject({ ok: true })
  })

  it('has smaller limits for Global Memory', () => {
    add(global, 'y'.repeat(LIMITS.global.hard))
    expect(store.add(global, 'preference', 'more')).toMatchObject({ ok: false })
  })

  it('searches by trigram, anywhere in a word', () => {
    add(project, 'Run the E2E suite with pnpm test:e2e')
    add(project, 'Typecheck with pnpm typecheck')
    add(other, 'pnpm here too')
    expect(store.search({ query: 'e2e', slot: project }).map((e) => e.text)).toEqual([
      'Run the E2E suite with pnpm test:e2e',
    ])
    expect(store.search({ query: 'pnpm', slot: project })).toHaveLength(2)
    expect(store.search({ query: 'typecheck missing', slot: project }).map((e) => e.text)).toEqual([
      'Typecheck with pnpm typecheck',
    ])
    expect(store.search({ query: 'pnpm', slot: project, limit: 1 })).toHaveLength(1)
  })

  it('finds two-character Chinese words through the LIKE fallback', () => {
    add(project, '查日志要用 anlan 命令', 'tool-quirk')
    add(project, '部署前先跑测试', 'convention')
    expect(store.search({ query: '日志', slot: project }).map((e) => e.text)).toEqual([
      '查日志要用 anlan 命令',
    ])
    expect(store.search({ query: '部署', slot: project, category: 'failure' })).toEqual([])
    expect(store.search({ query: '部署前先跑', slot: project })).toHaveLength(1)
  })

  it('treats search syntax as plain text', () => {
    add(project, 'quotes "inside" and 100%_done')
    expect(store.search({ query: '"inside"', slot: project })).toHaveLength(1)
    expect(store.search({ query: '%_', slot: project })).toHaveLength(1)
    expect(store.search({ query: 'OR NOT', slot: project })).toEqual([])
  })

  it('keeps search in step with replace and remove', () => {
    const entry = add(project, 'uses webpack')
    store.replace(entry.id, 'uses vite')
    expect(store.search({ query: 'webpack', slot: project })).toEqual([])
    expect(store.search({ query: 'vite', slot: project })).toHaveLength(1)
    store.remove(entry.id)
    expect(store.search({ query: 'vite', slot: project })).toEqual([])
  })

  it('gives the corrections newest first, within both caps', () => {
    for (let i = 0; i < 25; i++) add(project, `correction ${i}`, 'correction')
    add(project, 'not a correction', 'insight')
    const found = store.corrections(project, { entries: 20, chars: 2_000 })
    expect(found).toHaveLength(20)
    expect(found[0]?.text).toBe('correction 24')
    expect(store.corrections(project, { entries: 20, chars: 30 }).map((e) => e.text)).toEqual([
      'correction 24',
      'correction 23',
    ])
  })

  it('summarises every Project Memory and the Global Memory', () => {
    expect(store.summaries()).toEqual([
      {
        scope: 'global',
        workspace: null,
        entries: 0,
        chars: 0,
        lastConsolidated: null,
        overSoftLimit: false,
      },
    ])
    add(project, 'abc')
    add(project, 'de')
    store.markConsolidated(project)
    expect(store.summaries()[0]).toEqual({
      scope: 'project',
      workspace: '/Users/dev/app',
      entries: 2,
      chars: 5,
      lastConsolidated: expect.stringMatching(/^\d{4}-\d\d-\d\dT/),
      overSoftLimit: false,
    })
  })

  it('lets two processes write at once', async () => {
    const writer = fileURLToPath(new URL('./test-support/writer.ts', import.meta.url))
    const [a, b] = await Promise.all([
      runTs(writer, { args: [dir, 'a', '150'] }),
      runTs(writer, { args: [dir, 'b', '150'] }),
    ])
    expect(a).toMatchObject({ code: 0, stderr: '' })
    expect(b).toMatchObject({ code: 0, stderr: '' })
    expect(store.list({ scope: 'project', workspace: '/w' })).toHaveLength(300)
  })
})

describe('applying a consolidation slice', () => {
  it('rewrites, removes and merges only the entries that were sent', () => {
    const [a, b, c, d] = ['a one', 'b two', 'c three', 'd four'].map((t) =>
      add(project, t, 'convention'),
    )
    const untouched = add(project, 'not sent', 'convention')
    const result = store.applySlice([a!, b!, c!, d!], {
      keep: [a!.id],
      rewrite: [{ id: b!.id, text: 'b rewritten' }],
      remove: [],
      merge: [{ ids: [c!.id, d!.id], text: 'c and d' }],
    })
    expect(result).toEqual({ ok: true })
    expect(
      store
        .list(project, 'convention')
        .map((e) => e.text)
        .sort(),
    ).toEqual(['a one', 'b rewritten', 'c and d', 'not sent'].sort())
    expect(store.get(untouched.id)?.text).toBe('not sent')
    expect(store.list(project).find((e) => e.text === 'c and d')?.category).toBe('convention')
  })

  it('rejects an answer that names an id it was not sent, and changes nothing', () => {
    const a = add(project, 'a', 'convention')
    const outsider = add(project, 'outsider', 'convention')
    const result = store.applySlice([a], {
      keep: [],
      rewrite: [],
      remove: [outsider.id],
      merge: [{ ids: [a.id], text: 'x' }],
    })
    expect(result).toEqual({ ok: false, reason: expect.stringContaining(outsider.id) })
    expect(store.list(project)).toHaveLength(2)
  })

  it('rejects an answer that drops an entry without saying so', () => {
    const [a, b] = [add(project, 'a'), add(project, 'b')]
    expect(store.applySlice([a, b], { keep: [a.id], rewrite: [], remove: [], merge: [] })).toEqual({
      ok: false,
      reason: expect.stringContaining(b.id),
    })
    expect(store.list(project)).toHaveLength(2)
  })

  it('rejects a slice that changed while the model worked', () => {
    const a = add(project, 'a')
    store.replace(a.id, 'a changed meanwhile')
    expect(
      store.applySlice([a], { keep: [], rewrite: [], remove: [a.id], merge: [] }),
    ).toMatchObject({
      ok: false,
    })
    expect(store.get(a.id)?.text).toBe('a changed meanwhile')
  })

  it('rejects rewritten text that carries a secret', () => {
    const a = add(project, 'a')
    expect(
      store.applySlice([a], {
        keep: [],
        rewrite: [{ id: a.id, text: 'password=x1' }],
        remove: [],
        merge: [],
      }),
    ).toMatchObject({ ok: false })
  })
})
