import { describe, expect, test } from 'vitest'
import { defaultPick } from './use-workspace-choice'

describe('defaultPick', () => {
  test('the most recent Workspace, None when there is none, nothing while the list loads', () => {
    expect(defaultPick([{ path: '/w/app', label: 'app', lastUsedAt: 1 }])).toEqual({
      kind: 'recent',
      path: '/w/app',
    })
    expect(defaultPick([])).toEqual({ kind: 'scratch' })
    expect(defaultPick(null)).toBeNull()
  })
})
