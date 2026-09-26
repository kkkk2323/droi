import { describe, expect, test } from 'vitest'
import { droidBuildOf, isDroidReplaced } from './droid-build'

const PATH = '/Users/me/.local/bin/droid'

function fileAt(ino: number, mtimeMs: number) {
  return () => ({ ino, mtimeMs })
}

const missing = () => {
  throw new Error('ENOENT')
}

describe('isDroidReplaced', () => {
  const running = droidBuildOf(PATH, fileAt(1, 100))

  test('is false while the Daemon runs the file on disk', () => {
    expect(isDroidReplaced(running, fileAt(1, 100))).toBe(false)
  })

  test('is true once an update swapped the file for a new one', () => {
    expect(isDroidReplaced(running, fileAt(2, 200))).toBe(true)
  })

  test('is true when the file was rewritten in place', () => {
    expect(isDroidReplaced(running, fileAt(1, 200))).toBe(true)
  })

  test('is false while the file is missing mid-update', () => {
    expect(isDroidReplaced(running, missing)).toBe(false)
  })

  test('is false before a Daemon started', () => {
    expect(isDroidReplaced(null, fileAt(2, 200))).toBe(false)
    expect(droidBuildOf(PATH, missing)).toBeNull()
  })
})
