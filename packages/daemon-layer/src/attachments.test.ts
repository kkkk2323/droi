import { describe, expect, test } from 'vitest'
import { fitWithin } from './attachments'

describe('attachments', () => {
  test('fitWithin scales the longest edge down and never up', () => {
    expect(fitWithin(4000, 2000, 1568)).toEqual([1568, 784])
    expect(fitWithin(500, 3136, 1568)).toEqual([250, 1568])
    expect(fitWithin(800, 600, 1568)).toEqual([800, 600])
  })
})
