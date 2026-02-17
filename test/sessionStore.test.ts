import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createSessionStore } from '../src/backend/storage/sessionStore.ts'

test('sessionStore save/load/list roundtrip', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const meta = await store.save({
    id: 'abc_123',
    projectDir: '/repo',
    baseBranch: 'main',
    model: 'gpt',
    autoLevel: 'default',
    apiKeyFingerprint: 'fp123',
    messages: [
      { id: 'm1', role: 'user', blocks: [{ kind: 'text', content: 'Hello world' }], timestamp: 1 },
      { id: 'm2', role: 'assistant', blocks: [{ kind: 'text', content: 'Hi' }], timestamp: 2 },
    ]
  })

  assert.ok(meta)
  assert.equal(meta?.id, 'abc_123')
  assert.equal(meta?.projectDir, '/repo')
  assert.equal(meta?.messageCount, 2)
  assert.equal(meta?.autoLevel, 'default')
  assert.equal(meta?.baseBranch, 'main')
  assert.equal(meta?.apiKeyFingerprint, 'fp123')

  const list = await store.list()
  assert.equal(list.length, 1)
  assert.equal(list[0].id, 'abc_123')
  assert.equal(list[0].baseBranch, 'main')
  assert.equal(list[0].apiKeyFingerprint, 'fp123')

  const loaded = await store.load('abc_123')
  assert.ok(loaded)
  assert.equal(loaded?.id, 'abc_123')
  assert.equal(loaded?.projectDir, '/repo')
  assert.equal(loaded?.messages.length, 2)
  assert.equal(loaded?.baseBranch, 'main')
  assert.equal(loaded?.apiKeyFingerprint, 'fp123')
})

test('sessionStore clearContext clears messages but preserves title', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const meta1 = await store.save({
    id: 's1',
    projectDir: '/repo',
    model: 'gpt',
    autoLevel: 'default',
    apiKeyFingerprint: 'fp1',
    messages: [
      { id: 'm1', role: 'user', blocks: [{ kind: 'text', content: 'Hello world' }], timestamp: 1 },
    ]
  })

  assert.ok(meta1)
  const title1 = meta1?.title
  assert.ok(title1)

  const cleared = await store.clearContext('s1')
  assert.ok(cleared)
  assert.equal(cleared?.id, 's1')
  assert.equal(cleared?.projectDir, '/repo')
  assert.equal(cleared?.messageCount, 0)
  assert.equal(cleared?.title, title1)
  assert.equal(cleared?.apiKeyFingerprint, 'fp1')

  const loaded = await store.load('s1')
  assert.ok(loaded)
  assert.equal(loaded?.messages.length, 0)
  assert.equal(loaded?.title, title1)
  assert.equal(loaded?.apiKeyFingerprint, 'fp1')
})

test('sessionStore replaceSessionId migrates and clears context', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const meta1 = await store.save({
    id: 's1',
    projectDir: '/repo',
    model: 'gpt',
    autoLevel: 'default',
    apiKeyFingerprint: 'fp1',
    messages: [
      { id: 'm1', role: 'user', blocks: [{ kind: 'text', content: 'Hello world' }], timestamp: 1 },
    ]
  })
  assert.ok(meta1)
  const title1 = meta1?.title
  assert.ok(title1)

  const replaced = await store.replaceSessionId('s1', 's2')
  assert.ok(replaced)
  assert.equal(replaced?.id, 's2')
  assert.equal(replaced?.projectDir, '/repo')
  assert.equal(replaced?.messageCount, 0)
  assert.equal(replaced?.title, title1)
  assert.equal(replaced?.apiKeyFingerprint, 'fp1')

  assert.equal(await store.load('s1'), null)
  const loaded2 = await store.load('s2')
  assert.ok(loaded2)
  assert.equal(loaded2?.messages.length, 0)
  assert.equal(loaded2?.title, title1)
  assert.equal(loaded2?.apiKeyFingerprint, 'fp1')

  const store2 = createSessionStore({ baseDir: dir })
  const list2 = await store2.list()
  assert.equal(list2.length, 1)
  assert.equal(list2[0].id, 's2')
  assert.equal(list2[0].apiKeyFingerprint, 'fp1')
})

test('sessionStore save persists empty session with branch-derived title', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const meta = await store.save({
    id: 'empty_1',
    projectDir: '/repo',
    branch: 'droi/calm-whale-0phf',
    model: 'gpt',
    autoLevel: 'default',
    messages: [],
  })

  assert.ok(meta)
  assert.equal(meta?.messageCount, 0)
  assert.equal(meta?.title, 'calm-whale-0phf')

  const listed = await store.list()
  assert.equal(listed.length, 1)
  assert.equal(listed[0].id, 'empty_1')
  assert.equal(listed[0].title, 'calm-whale-0phf')

  const loaded = await store.load('empty_1')
  assert.ok(loaded)
  assert.equal(loaded?.messages.length, 0)
  assert.equal(loaded?.title, 'calm-whale-0phf')
})
