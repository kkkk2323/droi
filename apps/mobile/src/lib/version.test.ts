import { describe, expect, test } from 'vitest'
import { versionMismatch } from './version'

describe('versionMismatch', () => {
  test('the same release is no mismatch, a v prefix aside', () => {
    expect(versionMismatch('1.2.0', '1.2.0')).toBe(false)
    expect(versionMismatch('1.2.0', 'v1.2.0')).toBe(false)
  })

  test('another release is', () => {
    expect(versionMismatch('1.2.0', '1.3.0')).toBe(true)
  })

  test('an unknown version says nothing', () => {
    expect(versionMismatch('1.2.0', '')).toBe(false)
  })
})
