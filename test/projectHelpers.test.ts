import assert from 'node:assert/strict'
import test from 'node:test'

import { getPendingSessionDraftKey } from '../src/renderer/src/store/projectHelpers.ts'

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
