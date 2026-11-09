import { describe, expect, test } from 'vitest'
import type { Entry } from './kernel-wire'
import { summarised } from './use-compact'

const entry = (id: number, kind: string, head?: number): Entry => ({
  id,
  conversationId: 1,
  kind,
  ...(head === undefined ? {} : { head }),
})

describe('summarised', () => {
  test('counts the messages before the summary’s first kept entry, since the last boundary', () => {
    const entries = [
      entry(1, 'pi.user'),
      entry(2, 'pi.assistant'),
      entry(3, 'pi.compaction', 2),
      entry(4, 'pi.user'),
      entry(5, 'pi.assistant'),
      entry(6, 'pi.tool-result'),
      entry(7, 'pi.user'),
      entry(8, 'pi.assistant'),
      entry(9, 'pi.compaction', 7),
    ]
    expect(summarised(entries, 8)).toBe(2)
  })

  test('is 0 when no compaction came after the given entry', () => {
    const entries = [entry(1, 'pi.user'), entry(2, 'pi.compaction', 1)]
    expect(summarised(entries, 2)).toBe(0)
  })
})
