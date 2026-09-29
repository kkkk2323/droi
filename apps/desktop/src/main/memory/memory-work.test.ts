import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { existsSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { openMemoryStore, type MemoryEntry, type MemoryStore } from '../../memory/store'
import { markdownPath } from '../../memory/markdown'
import type { MemorySessionRequest } from './memory-session'
import { consolidate, extract, slicesOf, SLICE_CHARS } from './memory-work'

let root: string
let store: MemoryStore
let workspace: string
const project = () => ({ scope: 'project' as const, workspace })

beforeEach(() => {
  root = realpathSync(mkdtempSync(join(tmpdir(), 'droi-memory-work-')))
  workspace = root
  store = openMemoryStore(join(root, 'memory'))
})

afterEach(() => {
  store.close()
  rmSync(root, { recursive: true, force: true })
})

function add(text: string, category: Parameters<MemoryStore['add']>[1] = 'convention') {
  const result = store.add(project(), category, text)
  if (!result.ok) throw new Error(result.reason)
  return result.entry
}

/** A stand-in Memory Session that records what it was asked and answers with `answer`. */
function runner(answer: (request: MemorySessionRequest, sent: MemoryEntry[]) => unknown) {
  const requests: MemorySessionRequest[] = []
  return {
    requests,
    run: async (request: MemorySessionRequest) => {
      requests.push(request)
      const input = JSON.parse(request.input) as { entries?: MemoryEntry[] }
      return answer(request, input.entries ?? [])
    },
  }
}

describe('consolidation', () => {
  it('sends one category slice at a time and applies the changes to the ids it sent', async () => {
    const [a, b, c] = [add('uses pnpm'), add('uses pnpm workspaces'), add('tests next to code')]
    const failures = [
      add('build broke on node 20', 'failure'),
      add('build broke on node 20 again', 'failure'),
    ]
    const lonely = add('only preference', 'preference')
    const fake = runner((request, sent) =>
      request.title.endsWith('/ convention')
        ? {
            keep: [c!.id],
            rewrite: [],
            remove: [],
            merge: [{ ids: [a!.id, b!.id], text: 'uses pnpm workspaces' }],
          }
        : { keep: [], rewrite: [], remove: [sent[1]!.id], merge: [] },
    )
    const outcomes = await consolidate(
      { store, run: fake.run, modelId: 'glm-5.3-flash', prompt: 'P' },
      project(),
    )
    // A category with one entry has nothing to consolidate.
    expect(fake.requests.map((r) => r.title)).toEqual([
      `Memory: consolidate ${root.split('/').at(-1)} / failure`,
      `Memory: consolidate ${root.split('/').at(-1)} / convention`,
    ])
    expect(fake.requests[0]).toMatchObject({
      cwd: workspace,
      modelId: 'glm-5.3-flash',
      prompt: 'P',
    })
    expect(outcomes).toEqual([
      {
        category: 'failure',
        entries: 2,
        ok: false,
        reason: expect.stringContaining(failures[0]!.id),
      },
      { category: 'convention', entries: 3, ok: true, reason: null },
    ])
    // The failure slice named only one of its two ids, so nothing changed there.
    expect(store.list(project(), 'failure')).toHaveLength(2)
    expect(
      store
        .list(project(), 'convention')
        .map((e) => e.text)
        .sort(),
    ).toEqual(['tests next to code', 'uses pnpm workspaces'].sort())
    expect(store.get(lonely.id)).not.toBeNull()
    expect(store.summaries()[0]?.lastConsolidated).not.toBeNull()
    expect(readFileSync(join(store.dir, markdownPath(project())), 'utf8')).toContain(
      'uses pnpm workspaces',
    )
  })

  it('rejects a reply that touches an entry it was not sent, and one that is not the schema', async () => {
    const [a, b] = [add('a1'), add('b2')]
    const outsider = add('outsider', 'insight')
    add('other insight', 'insight')
    let calls = 0
    const fake = runner(() => {
      calls += 1
      return calls === 1
        ? { keep: [a.id, b.id], rewrite: [], remove: [outsider.id], merge: [] }
        : 'prose'
    })
    const outcomes = await consolidate(
      { store, run: fake.run, modelId: 'm', prompt: 'P' },
      project(),
    )
    expect(outcomes.map((o) => o.ok)).toEqual([false, false])
    expect(store.list(project())).toHaveLength(4)
    // Nothing held up, so the Memory does not count as consolidated.
    expect(store.summaries()[0]?.lastConsolidated).toBeNull()
  })

  it('keeps going when one Memory Session fails', async () => {
    add('x1', 'failure')
    add('x2', 'failure')
    const [a, b] = [add('c1'), add('c2')]
    const fake = runner((request) => {
      if (request.title.endsWith('/ failure')) throw new Error('Daemon went away')
      return { keep: [a.id, b.id], rewrite: [], remove: [], merge: [] }
    })
    const outcomes = await consolidate(
      { store, run: fake.run, modelId: 'm', prompt: 'P' },
      project(),
    )
    expect(outcomes).toEqual([
      { category: 'failure', entries: 2, ok: false, reason: 'Daemon went away' },
      { category: 'convention', entries: 2, ok: true, reason: null },
    ])
  })

  it('splits a large category into slices', () => {
    const entries = Array.from({ length: 5 }, (_, i) => ({
      text: 'x'.repeat(SLICE_CHARS / 2 - 1),
      id: String(i),
    }))
    expect(slicesOf(entries as MemoryEntry[]).map((s) => s.length)).toEqual([2, 2, 1])
  })

  it('runs a Global Memory consolidation in the home folder', async () => {
    const a = store.add({ scope: 'global' }, 'preference', 'terse')
    const b = store.add({ scope: 'global' }, 'preference', 'very terse')
    if (!a.ok || !b.ok) throw new Error('add failed')
    const fake = runner(() => ({
      keep: [],
      rewrite: [],
      remove: [a.entry.id],
      merge: [{ ids: [b.entry.id], text: 'terse' }],
    }))
    await consolidate({ store, run: fake.run, modelId: 'm', prompt: 'P' }, { scope: 'global' })
    expect(fake.requests[0]?.title).toBe('Memory: consolidate Global Memory / preference')
    expect(store.list({ scope: 'global' }).map((e) => e.text)).toEqual(['terse'])
  })
})

describe('extraction', () => {
  function transcript(): string {
    const path = join(root, 'session.jsonl')
    writeFileSync(
      path,
      [
        { type: 'session_start', cwd: workspace },
        {
          type: 'message',
          message: { role: 'user', content: [{ type: 'text', text: 'Use pnpm here, not npm' }] },
        },
        {
          type: 'message',
          message: { role: 'assistant', content: [{ type: 'text', text: 'Switching to pnpm.' }] },
        },
      ]
        .map((l) => JSON.stringify(l))
        .join('\n'),
    )
    return path
  }

  it('sends the conversation and inserts the entries it gets back', async () => {
    add('already known')
    const fake = runner(() => ({
      entries: [
        { scope: 'project', category: 'correction', text: 'Use pnpm, not npm' },
        { scope: 'global', category: 'preference', text: 'Likes short answers' },
        { scope: 'project', category: 'gossip', text: 'not a category' },
        { scope: 'project', category: 'insight', text: 'token is password=abc123' },
      ],
    }))
    const result = await extract(
      { store, run: fake.run, modelId: 'glm-5.3-flash', prompt: 'E' },
      {
        sessionId: '0427515a-a1da-4056-9e5e-c4d2e1780ed1',
        transcriptPath: transcript(),
        cwd: workspace,
        event: 'SessionEnd',
        requestedAt: new Date().toISOString(),
      },
    )
    expect(result).toEqual({ added: 2, refused: 2 })
    expect(fake.requests[0]).toMatchObject({
      title: 'Memory: extract from 0427515a',
      cwd: workspace,
      prompt: 'E',
    })
    const input = JSON.parse(fake.requests[0]!.input) as { transcript: string; existing: unknown[] }
    expect(input.transcript).toBe('User: Use pnpm here, not npm\n\nAssistant: Switching to pnpm.')
    expect(input.existing).toEqual([{ category: 'convention', text: 'already known' }])
    expect(store.list(project(), 'correction').map((e) => e.text)).toEqual(['Use pnpm, not npm'])
    expect(store.list({ scope: 'global' }).map((e) => e.text)).toEqual(['Likes short answers'])
    expect(existsSync(join(store.dir, 'global.md'))).toBe(true)
    // A later request for the same Session is skipped by the hook.
    expect(store.hasWrite('0427515a-a1da-4056-9e5e-c4d2e1780ed1')).toBe(true)
  })

  it('does nothing for a transcript that is gone', async () => {
    const fake = runner(() => ({ entries: [] }))
    const result = await extract(
      { store, run: fake.run, modelId: 'm', prompt: 'E' },
      {
        sessionId: 's',
        transcriptPath: join(root, 'missing.jsonl'),
        cwd: workspace,
        event: 'PreCompact',
        requestedAt: '',
      },
    )
    expect(result).toEqual({ added: 0, refused: 0 })
    expect(fake.requests).toEqual([])
  })
})
