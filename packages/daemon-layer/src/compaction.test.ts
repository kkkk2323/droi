import { describe, expect, test, vi } from 'vitest'
import { CompactionLog } from './compaction'

describe('CompactionLog', () => {
  test('a Session is pending from start to finish, then remembered as done where the user landed', () => {
    const log = new CompactionLog()
    const done = vi.fn()
    log.onDone(done)

    log.start('parent')
    expect(log.snapshot().pending.has('parent')).toBe(true)

    log.finish('parent', 'child', 12, 1000)
    const after = log.snapshot()
    expect(after.pending.has('parent')).toBe(false)
    expect(after.finished.get('child')).toEqual({ removedCount: 12, finishedAt: 1000 })
    expect(after.finished.has('parent')).toBe(false)
    expect(done).toHaveBeenCalledWith({
      sessionId: 'parent',
      shownIn: 'child',
      removedCount: 12,
      finishedAt: 1000,
    })
  })

  test('a failure clears pending without a done event', () => {
    const log = new CompactionLog()
    const done = vi.fn()
    log.onDone(done)
    log.start('s')
    log.fail('s')
    expect(log.snapshot().pending.size).toBe(0)
    expect(done).not.toHaveBeenCalled()
  })

  test('each change hands out a new snapshot and tells subscribers', () => {
    const log = new CompactionLog()
    const changed = vi.fn()
    const off = log.subscribe(changed)
    const before = log.snapshot()
    log.start('s')
    expect(log.snapshot()).not.toBe(before)
    expect(changed).toHaveBeenCalledTimes(1)
    off()
    log.fail('s')
    expect(changed).toHaveBeenCalledTimes(1)
  })
})
