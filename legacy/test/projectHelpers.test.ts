import assert from 'node:assert/strict'
import test from 'node:test'

import { getPendingSessionDraftKey, upsertSessionMeta } from '../src/renderer/src/store/projectHelpers.ts'

test('getPendingSessionDraftKey keeps repo-root sessions on the repo key', () => {
  assert.equal(
    getPendingSessionDraftKey({
      repoRoot: '/repo',
      workspaceDir: '/repo',
      projectDir: '/repo',
    }),
    'pending:/repo',
  )
})

test('getPendingSessionDraftKey isolates drafts for different subdirectories in the same repo', () => {
  const fooKey = getPendingSessionDraftKey({
    repoRoot: '/repo',
    workspaceDir: '/repo',
    projectDir: '/repo/packages/foo',
    cwdSubpath: 'packages/foo',
  })
  const barKey = getPendingSessionDraftKey({
    repoRoot: '/repo',
    workspaceDir: '/repo',
    projectDir: '/repo/packages/bar',
    cwdSubpath: 'packages/bar',
  })

  assert.equal(fooKey, 'pending:/repo:packages/foo')
  assert.equal(barKey, 'pending:/repo:packages/bar')
  assert.notEqual(fooKey, barKey)
})

test('upsertSessionMeta preserves local workspace projects', () => {
  const next = upsertSessionMeta([], {
    id: 'local-1',
    projectDir: '/tmp/local-project',
    workspaceDir: '/tmp/local-project',
    repoRoot: '/tmp/local-project',
    workspaceType: 'local',
    branch: '',
    title: 'Untitled',
    savedAt: 1,
    messageCount: 0,
    model: 'kimi-k2.5',
    autoLevel: 'default',
  })

  assert.equal(next.length, 1)
  assert.equal(next[0]?.dir, '/tmp/local-project')
  assert.equal(next[0]?.workspaceType, 'local')
})
