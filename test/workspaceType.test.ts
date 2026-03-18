import assert from 'node:assert/strict'
import test from 'node:test'

import { getGitWorkspaceLookupDir } from '../src/renderer/src/lib/workspaceType.ts'

test('getGitWorkspaceLookupDir prefers workspaceDir for git workspaces', () => {
  assert.equal(
    getGitWorkspaceLookupDir({
      workspaceType: 'worktree',
      workspaceDir: '/repo/.worktrees/feature-x',
      projectDir: '/repo/.worktrees/feature-x/packages/app',
    }),
    '/repo/.worktrees/feature-x',
  )
})

test('getGitWorkspaceLookupDir keeps projectDir for local workspaces', () => {
  assert.equal(
    getGitWorkspaceLookupDir({
      workspaceType: 'local',
      workspaceDir: '/repo',
      projectDir: '/repo/packages/app',
    }),
    '/repo/packages/app',
  )
})
