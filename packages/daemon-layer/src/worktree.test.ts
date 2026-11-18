import { describe, expect, it } from 'vitest'
import { archiveDeletesWorktree, isWorktreeLifecycle, worktreeParams } from './worktree'

describe('worktreeParams', () => {
  it('asks for a worktree with the lifecycle and the first message', () => {
    expect(worktreeParams({ lifecycle: 'persistent', promptSlug: ' Fix the login bug ' })).toEqual({
      worktree: true,
      worktreeLifecycle: 'persistent',
      worktreePromptSlug: 'Fix the login bug',
    })
  })

  it('leaves the slug out when there is no message', () => {
    expect(worktreeParams({ lifecycle: 'ephemeral', promptSlug: '  ' })).toEqual({
      worktree: true,
      worktreeLifecycle: 'ephemeral',
    })
  })

  it('copies a chosen base branch', () => {
    expect(worktreeParams({ lifecycle: 'ephemeral', baseBranch: 'develop' })).toMatchObject({
      worktreeBaseBranch: 'develop',
      worktreeBranchMode: 'copy',
    })
  })
})

describe('archiveDeletesWorktree', () => {
  it('is true for an ephemeral worktree only', () => {
    expect(archiveDeletesWorktree({ worktree: { lifecycle: 'ephemeral' } })).toBe(true)
    expect(archiveDeletesWorktree({ worktree: { lifecycle: 'persistent' } })).toBe(false)
    expect(archiveDeletesWorktree({ worktree: null })).toBe(false)
  })
})

describe('isWorktreeLifecycle', () => {
  it('accepts the two lifecycles', () => {
    expect(isWorktreeLifecycle('ephemeral')).toBe(true)
    expect(isWorktreeLifecycle('persistent')).toBe(true)
    expect(isWorktreeLifecycle('other')).toBe(false)
    expect(isWorktreeLifecycle(null)).toBe(false)
  })
})
