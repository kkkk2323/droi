import assert from 'node:assert/strict'
import test from 'node:test'

import { getGitWorkspaceDir, getLaunchProjectDir } from '../src/renderer/src/lib/workspaceType.ts'

test('getGitWorkspaceDir prefers workspaceDir for git operations', () => {
  assert.equal(
    getGitWorkspaceDir({
      workspaceDir: '/repo/.worktrees/feature-x',
      projectDir: '/repo/.worktrees/feature-x/packages/app',
    }),
    '/repo/.worktrees/feature-x',
  )
})

test('getLaunchProjectDir keeps projectDir for local launches', () => {
  assert.equal(
    getLaunchProjectDir({
      repoRoot: '/repo',
      workspaceDir: '/repo',
      projectDir: '/repo/packages/app',
    }),
    '/repo/packages/app',
  )
})

test('getLaunchProjectDir falls back to workspaceDir then repoRoot', () => {
  assert.equal(
    getLaunchProjectDir({
      repoRoot: '/repo',
      workspaceDir: '/repo/.worktrees/feature-x',
    }),
    '/repo/.worktrees/feature-x',
  )

  assert.equal(
    getLaunchProjectDir({
      repoRoot: '/repo',
    }),
    '/repo',
  )
})
