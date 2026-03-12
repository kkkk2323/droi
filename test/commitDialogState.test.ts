import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveCommitDialogHostState } from '../src/renderer/src/lib/commitDialogState.ts'

test('resolveCommitDialogHostState keeps the originally requested project open', () => {
  const state = resolveCommitDialogHostState({
    activeProjectDir: '/repo-b',
    requestedProjectDir: '/repo-a',
  })

  assert.deepEqual(state, {
    open: true,
    projectDir: '/repo-a',
  })
})

test('resolveCommitDialogHostState falls back to the active project when closed', () => {
  const state = resolveCommitDialogHostState({
    activeProjectDir: '/repo-a',
    requestedProjectDir: null,
  })

  assert.deepEqual(state, {
    open: false,
    projectDir: '/repo-a',
  })
})

test('resolveCommitDialogHostState ignores blank requested project paths', () => {
  const state = resolveCommitDialogHostState({
    activeProjectDir: ' /repo-a ',
    requestedProjectDir: '   ',
  })

  assert.deepEqual(state, {
    open: false,
    projectDir: '/repo-a',
  })
})
