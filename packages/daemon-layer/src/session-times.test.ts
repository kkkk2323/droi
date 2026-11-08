import { describe, expect, test } from 'vitest'
import { isUnread } from './session-times'

describe('isUnread', () => {
  test('activity after the last look, or a dot asked for', () => {
    expect(isUnread({ activityAt: 10, viewedAt: 5 })).toBe(true)
    expect(isUnread({ activityAt: 10, viewedAt: 10 })).toBe(false)
    expect(isUnread({ activityAt: 10, viewedAt: 20 })).toBe(false)
    expect(isUnread({ activityAt: 10, viewedAt: null })).toBe(true)
  })
})
