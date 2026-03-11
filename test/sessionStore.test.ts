import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createSessionStore } from '../src/backend/storage/sessionStore.ts'
import { buildRestoredSessionBuffer } from '../src/renderer/src/state/sessionRestore.ts'

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

test('sessionStore preserves explicit mission metadata, missionDir, and missionBaseSessionId across save, load, clear, and replace', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-session-'))
  const store = createSessionStore({ baseDir: dir })

  const saved = await store.save({
    id: 'mission_1',
    projectDir: '/repo/mission',
    model: 'gpt',
    autoLevel: 'high',
    missionDir: '/Users/clive/.factory/missions/mission_1',
    missionBaseSessionId: 'mission_1',
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
  assert.equal(saved?.missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(saved?.missionBaseSessionId, 'mission_1')

  const loaded = await store.load('mission_1')
  assert.ok(loaded)
  assert.equal(loaded?.isMission, true)
  assert.equal(loaded?.sessionKind, 'mission')
  assert.equal(loaded?.interactionMode, 'agi')
  assert.equal(loaded?.autonomyLevel, 'high')
  assert.equal(loaded?.decompSessionType, 'orchestrator')
  assert.equal(loaded?.missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(loaded?.missionBaseSessionId, 'mission_1')

  const listed = await store.list()
  assert.equal(listed.length, 1)
  assert.equal(listed[0].isMission, true)
  assert.equal(listed[0].sessionKind, 'mission')
  assert.equal(listed[0].interactionMode, 'agi')
  assert.equal(listed[0].autonomyLevel, 'high')
  assert.equal(listed[0].decompSessionType, 'orchestrator')
  assert.equal(listed[0].missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(listed[0].missionBaseSessionId, 'mission_1')

  const cleared = await store.clearContext('mission_1')
  assert.ok(cleared)
  assert.equal(cleared?.isMission, true)
  assert.equal(cleared?.sessionKind, 'mission')
  assert.equal(cleared?.interactionMode, 'agi')
  assert.equal(cleared?.autonomyLevel, 'high')
  assert.equal(cleared?.decompSessionType, 'orchestrator')
  assert.equal(cleared?.missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(cleared?.missionBaseSessionId, 'mission_1')

  const replaced = await store.replaceSessionId('mission_1', 'mission_2')
  assert.ok(replaced)
  assert.equal(replaced?.id, 'mission_2')
  assert.equal(replaced?.isMission, true)
  assert.equal(replaced?.sessionKind, 'mission')
  assert.equal(replaced?.interactionMode, 'agi')
  assert.equal(replaced?.autonomyLevel, 'high')
  assert.equal(replaced?.decompSessionType, 'orchestrator')
  assert.equal(replaced?.missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(replaced?.missionBaseSessionId, 'mission_1')

  const loadedReplaced = await store.load('mission_2')
  assert.ok(loadedReplaced)
  assert.equal(loadedReplaced?.isMission, true)
  assert.equal(loadedReplaced?.sessionKind, 'mission')
  assert.equal(loadedReplaced?.interactionMode, 'agi')
  assert.equal(loadedReplaced?.autonomyLevel, 'high')
  assert.equal(loadedReplaced?.decompSessionType, 'orchestrator')
  assert.equal(loadedReplaced?.missionDir, '/Users/clive/.factory/missions/mission_1')
  assert.equal(loadedReplaced?.missionBaseSessionId, 'mission_1')
})

test('buildRestoredSessionBuffer keeps Mission identity and snapshot data across lifecycle stages', () => {
  const baseMeta = {
    autoLevel: 'high',
    isMission: true,
    sessionKind: 'mission' as const,
    interactionMode: 'agi' as const,
    autonomyLevel: 'high' as const,
    decompSessionType: 'orchestrator' as const,
    missionDir: '/Users/clive/.factory/missions/base-session-123',
    missionBaseSessionId: 'base-session-123',
    model: 'gpt-5.4',
  }

  const stages = [
    {
      label: 'running',
      mission: {
        state: {
          state: 'running',
          currentFeatureId: 'feature-1',
          currentWorkerSessionId: 'worker-live',
          completedFeatures: 0,
          totalFeatures: 2,
          updatedAt: '2026-03-09T00:00:00.000Z',
        },
        features: [
          { id: 'feature-1', status: 'in_progress' },
          { id: 'feature-2', status: 'pending' },
        ],
        progressLog: [{ timestamp: '2026-03-09T00:00:01.000Z', type: 'worker_started' }],
      },
      expectedState: 'running',
      expectedCompleted: false,
    },
    {
      label: 'paused',
      mission: {
        state: {
          state: 'paused',
          currentFeatureId: 'feature-1',
          completedFeatures: 0,
          totalFeatures: 2,
          updatedAt: '2026-03-09T00:02:00.000Z',
        },
        features: [
          { id: 'feature-1', status: 'in_progress' },
          { id: 'feature-2', status: 'pending' },
        ],
      },
      expectedState: 'paused',
      expectedCompleted: false,
    },
    {
      label: 'validator-injected running',
      mission: {
        state: {
          state: 'running',
          currentFeatureId: 'scrutiny-validator',
          currentWorkerSessionId: 'validator-worker',
          completedFeatures: 1,
          totalFeatures: 3,
          updatedAt: '2026-03-09T00:04:00.000Z',
        },
        features: [
          { id: 'feature-1', status: 'completed' },
          { id: 'scrutiny-validator', status: 'in_progress', skillName: 'scrutiny-validator' },
          {
            id: 'user-testing-validator',
            status: 'pending',
            skillName: 'user-testing-validator',
          },
        ],
        handoffs: {
          'feature-1.json': { featureId: 'feature-1', successState: 'success' },
        },
        validationState: {
          assertions: {
            'VAL-001': { status: 'pending' },
          },
        },
      },
      expectedState: 'running',
      expectedCompleted: false,
    },
    {
      label: 'completed',
      mission: {
        state: {
          state: 'completed',
          completedFeatures: 2,
          totalFeatures: 2,
          updatedAt: '2026-03-09T00:06:00.000Z',
        },
        features: [
          { id: 'feature-1', status: 'completed' },
          { id: 'feature-2', status: 'completed' },
        ],
        validationState: {
          assertions: {
            'VAL-001': { status: 'passed' },
            'VAL-002': { status: 'passed' },
          },
        },
      },
      expectedState: 'completed',
      expectedCompleted: true,
    },
    {
      label: 'completed from flat snapshot fields',
      mission: {
        currentState: 'completed',
        completedFeatures: 3,
        totalFeatures: 3,
        currentFeatureId: 'user-testing-validator-runtime-smoke',
        features: [
          { id: 'runtime-smoke-root-inspection', status: 'completed' },
          {
            id: 'scrutiny-validator-runtime-smoke',
            status: 'completed',
            skillName: 'scrutiny-validator',
          },
          {
            id: 'user-testing-validator-runtime-smoke',
            status: 'completed',
            skillName: 'user-testing-validator',
          },
        ],
        validationState: {
          assertions: {
            'VAL-ROOT-001': { status: 'passed' },
          },
        },
        handoffs: {
          'runtime-smoke-root-inspection.json': {
            featureId: 'runtime-smoke-root-inspection',
            successState: 'success',
          },
        },
      },
      expectedState: 'completed',
      expectedCompleted: true,
    },
  ]

  for (const stage of stages) {
    const restored = buildRestoredSessionBuffer({
      projectDir: '/repo',
      workspace: { repoRoot: '/repo', workspaceDir: '/repo', branch: 'main', workspaceType: 'branch' },
      meta: baseMeta,
      data: {
        id: `session-${stage.label}`,
        projectDir: '/repo',
        title: stage.label,
        savedAt: 1,
        messages: [],
        model: 'gpt-5.4',
        autoLevel: 'high',
        missionDir: baseMeta.missionDir,
        missionBaseSessionId: baseMeta.missionBaseSessionId,
        isMission: true,
        sessionKind: 'mission',
        interactionMode: 'agi',
        autonomyLevel: 'high',
        decompSessionType: 'orchestrator',
        mission: stage.mission as any,
      },
    })

    assert.equal(restored.isMission, true, stage.label)
    assert.equal(restored.sessionKind, 'mission', stage.label)
    assert.equal(restored.interactionMode, 'agi', stage.label)
    assert.equal(restored.autonomyLevel, 'high', stage.label)
    assert.equal(restored.decompSessionType, 'orchestrator', stage.label)
    assert.equal(restored.missionDir, baseMeta.missionDir, stage.label)
    assert.equal(restored.missionBaseSessionId, baseMeta.missionBaseSessionId, stage.label)
    assert.equal(restored.mission?.currentState, stage.expectedState, stage.label)
    assert.equal(restored.mission?.isCompleted, stage.expectedCompleted, stage.label)
    assert.equal(restored.mission?.liveWorkerSessionId, undefined, stage.label)
  }
})
