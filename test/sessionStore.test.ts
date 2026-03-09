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
    projectDir: '/repo/packages/foo',
    workspaceDir: '/repo',
    cwdSubpath: 'packages/foo',
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
  assert.equal(meta?.projectDir, '/repo/packages/foo')
  assert.equal(meta?.workspaceDir, '/repo')
  assert.equal(meta?.cwdSubpath, 'packages/foo')
  assert.equal(meta?.messageCount, 2)
  assert.equal(meta?.autoLevel, 'default')
  assert.equal(meta?.baseBranch, 'main')
  assert.equal(meta?.apiKeyFingerprint, 'fp123')

  const list = await store.list()
  assert.equal(list.length, 1)
  assert.equal(list[0].id, 'abc_123')
  assert.equal(list[0].workspaceDir, '/repo')
  assert.equal(list[0].cwdSubpath, 'packages/foo')
  assert.equal(list[0].baseBranch, 'main')
  assert.equal(list[0].apiKeyFingerprint, 'fp123')

  const loaded = await store.load('abc_123')
  assert.ok(loaded)
  assert.equal(loaded?.id, 'abc_123')
  assert.equal(loaded?.projectDir, '/repo/packages/foo')
  assert.equal(loaded?.workspaceDir, '/repo')
  assert.equal(loaded?.cwdSubpath, 'packages/foo')
  assert.equal(loaded?.messages.length, 2)
  assert.equal(loaded?.baseBranch, 'main')
  assert.equal(loaded?.apiKeyFingerprint, 'fp123')
})

test('sessionStore clearContext clears messages but preserves title', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const meta1 = await store.save({
    id: 's1',
    projectDir: '/repo/packages/foo',
    workspaceDir: '/repo',
    cwdSubpath: 'packages/foo',
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
  assert.equal(cleared?.projectDir, '/repo/packages/foo')
  assert.equal(cleared?.workspaceDir, '/repo')
  assert.equal(cleared?.cwdSubpath, 'packages/foo')
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
    projectDir: '/repo/packages/foo',
    workspaceDir: '/repo',
    cwdSubpath: 'packages/foo',
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
  assert.equal(replaced?.projectDir, '/repo/packages/foo')
  assert.equal(replaced?.workspaceDir, '/repo')
  assert.equal(replaced?.cwdSubpath, 'packages/foo')
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

test('sessionStore preserves explicit mission metadata across save, load, clear, and replace', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const saved = await store.save({
    id: 'mission_1',
    projectDir: '/repo/mission',
    model: 'gpt',
    autoLevel: 'high',
    isMission: true,
    sessionKind: 'mission',
    interactionMode: 'agi',
    autonomyLevel: 'high',
    decompSessionType: 'orchestrator',
    messages: [
      {
        id: 'm1',
        role: 'user',
        blocks: [{ kind: 'text', content: 'Run the mission' }],
        timestamp: 1,
      },
    ],
  })

  assert.ok(saved)
  assert.equal(saved?.isMission, true)
  assert.equal(saved?.sessionKind, 'mission')
  assert.equal(saved?.interactionMode, 'agi')
  assert.equal(saved?.autonomyLevel, 'high')
  assert.equal(saved?.decompSessionType, 'orchestrator')

  const loaded = await store.load('mission_1')
  assert.ok(loaded)
  assert.equal(loaded?.isMission, true)
  assert.equal(loaded?.sessionKind, 'mission')
  assert.equal(loaded?.interactionMode, 'agi')
  assert.equal(loaded?.autonomyLevel, 'high')
  assert.equal(loaded?.decompSessionType, 'orchestrator')

  const listed = await store.list()
  assert.equal(listed.length, 1)
  assert.equal(listed[0].isMission, true)
  assert.equal(listed[0].sessionKind, 'mission')
  assert.equal(listed[0].interactionMode, 'agi')
  assert.equal(listed[0].autonomyLevel, 'high')
  assert.equal(listed[0].decompSessionType, 'orchestrator')

  const cleared = await store.clearContext('mission_1')
  assert.ok(cleared)
  assert.equal(cleared?.isMission, true)
  assert.equal(cleared?.sessionKind, 'mission')
  assert.equal(cleared?.interactionMode, 'agi')
  assert.equal(cleared?.autonomyLevel, 'high')
  assert.equal(cleared?.decompSessionType, 'orchestrator')

  const replaced = await store.replaceSessionId('mission_1', 'mission_2')
  assert.ok(replaced)
  assert.equal(replaced?.id, 'mission_2')
  assert.equal(replaced?.isMission, true)
  assert.equal(replaced?.sessionKind, 'mission')
  assert.equal(replaced?.interactionMode, 'agi')
  assert.equal(replaced?.autonomyLevel, 'high')
  assert.equal(replaced?.decompSessionType, 'orchestrator')

  const loadedReplaced = await store.load('mission_2')
  assert.ok(loadedReplaced)
  assert.equal(loadedReplaced?.isMission, true)
  assert.equal(loadedReplaced?.sessionKind, 'mission')
  assert.equal(loadedReplaced?.interactionMode, 'agi')
  assert.equal(loadedReplaced?.autonomyLevel, 'high')
  assert.equal(loadedReplaced?.decompSessionType, 'orchestrator')
})
