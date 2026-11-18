import { describe, expect, test } from 'vitest'
import type { SessionTimesSnapshot } from './gateway'
import {
  applySnapshot,
  createSessionTimes,
  createTimesStore,
  isUnread,
  type SessionTimesSource,
} from './session-times'

const config = { gatewayUrl: 'http://127.0.0.1:4000', pairingToken: 'tok' }

describe('createSessionTimes', () => {
  test('reads every Session’s Times, then what changed after a revision', async () => {
    const urls: string[] = []
    const source = createSessionTimes(config, async (input) => {
      urls.push(String(input))
      return Response.json({ rev: 4, full: true, sessions: { a: { sentAt: 10, endedAt: 12 } } })
    })
    expect(await source.read(0)).toEqual({
      rev: 4,
      full: true,
      sessions: { a: { sentAt: 10, endedAt: 12 } },
    })
    await source.read(4)
    expect(urls).toEqual([
      'http://127.0.0.1:4000/session-times?token=tok',
      'http://127.0.0.1:4000/session-times?token=tok&after=4',
    ])
  })

  test('keeps only the times it knows', async () => {
    const source = createSessionTimes(config, async () =>
      Response.json({ rev: 1, sessions: { a: { sentAt: 'x', endedAt: -1, viewedAt: 7 } } }),
    )
    expect(await source.read(0)).toEqual({ rev: 1, full: false, sessions: { a: { viewedAt: 7 } } })
  })

  test('a Gateway that keeps none is null; one that fails throws', async () => {
    const none = createSessionTimes(config, async () => new Response(null, { status: 404 }))
    expect(await none.read(0)).toBeNull()
    const failing = createSessionTimes(config, async () => new Response(null, { status: 500 }))
    await expect(failing.read(0)).rejects.toThrow('HTTP 500')
  })

  test('tells the computer a Session is on screen', async () => {
    const asked: Array<{ url: string; method: string | undefined }> = []
    const source = createSessionTimes(config, async (input, init) => {
      asked.push({ url: String(input), method: init?.method })
      return new Response(null, { status: 204 })
    })
    await source.viewed('s 1')
    expect(asked).toEqual([
      { url: 'http://127.0.0.1:4000/session-times/viewed?token=tok&sessionId=s+1', method: 'POST' },
    ])
  })
})

test('applySnapshot replaces with a full one and merges a change', () => {
  const before = new Map([
    ['a', { sentAt: 1 }],
    ['b', { sentAt: 2 }],
  ])
  expect([...applySnapshot(before, { rev: 2, sessions: { b: { sentAt: 3 } } })]).toEqual([
    ['a', { sentAt: 1 }],
    ['b', { sentAt: 3 }],
  ])
  expect([...applySnapshot(before, { rev: 1, full: true, sessions: { c: {} } })]).toEqual([
    ['c', {}],
  ])
  expect(before.get('b')).toEqual({ sentAt: 2 })
})

test('a Session is unread when its turn ended after it was last shown', () => {
  expect(isUnread({ endedAt: 5, viewedAt: 4 })).toBe(true)
  expect(isUnread({ endedAt: 5, viewedAt: 5 })).toBe(false)
  expect(isUnread({ sentAt: 9 })).toBe(false)
  expect(isUnread(undefined)).toBe(false)
})

/** A Gateway the test answers by hand: each read waits for answer(). */
function fakeGateway() {
  const reads: number[] = []
  const viewed: string[] = []
  let pending: ((snapshot: SessionTimesSnapshot | null) => void) | null = null
  const sessionTimes: SessionTimesSource = {
    read: (after, signal) =>
      new Promise((resolve, reject) => {
        reads.push(after)
        pending = resolve
        signal?.addEventListener('abort', () => reject(new Error('aborted')))
      }),
    viewed: async (sessionId) => {
      viewed.push(sessionId)
    },
  }
  const connection = {
    sessionTimes,
    getState: () => ({ status: 'connected' as const }),
    subscribe: () => () => {},
  }
  return {
    store: createTimesStore(connection as unknown as Parameters<typeof createTimesStore>[0]),
    reads,
    viewed,
    async answer(snapshot: SessionTimesSnapshot | null) {
      const resolve = pending
      pending = null
      resolve?.(snapshot)
      await settle()
    },
  }
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('the Session Times store', () => {
  test('follows every Session’s Times, then each change, while anyone listens', async () => {
    const gw = fakeGateway()
    const stop = gw.store.subscribe(() => {})
    expect(gw.store.getSnapshot().kept).toBe(false)
    await gw.answer({ rev: 3, full: true, sessions: { a: { sentAt: 10 } } })
    expect(gw.store.getSnapshot()).toEqual({ kept: true, times: new Map([['a', { sentAt: 10 }]]) })
    await gw.answer({ rev: 4, sessions: { b: { endedAt: 20 } } })
    expect([...gw.store.getSnapshot().times.keys()]).toEqual(['a', 'b'])
    expect(gw.reads).toEqual([0, 3, 4])
    stop()
    await settle()
    expect(gw.reads).toEqual([0, 3, 4])
  })

  test('a Gateway that keeps none leaves the Client its own marks', async () => {
    const gw = fakeGateway()
    gw.store.subscribe(() => {})
    await gw.answer(null)
    expect(gw.store.getSnapshot().kept).toBe(false)
    expect(gw.reads).toEqual([0])
  })

  test('a Session shown here reads at once, then by the computer’s own clock', async () => {
    const gw = fakeGateway()
    gw.store.subscribe(() => {})
    // The computer's clock is far behind this one: its times are small.
    await gw.answer({ rev: 1, full: true, sessions: { a: { endedAt: 100, viewedAt: 50 } } })
    expect(isUnread(gw.store.getSnapshot().times.get('a'))).toBe(true)

    gw.store.viewed('a')
    expect(isUnread(gw.store.getSnapshot().times.get('a'))).toBe(false)
    await settle()
    expect(gw.viewed).toEqual(['a'])

    await gw.answer({ rev: 2, sessions: { a: { endedAt: 100, viewedAt: 120 } } })
    expect(gw.store.getSnapshot().times.get('a')).toEqual({ endedAt: 100, viewedAt: 120 })
    // A turn that ends after the view is unread, though this clock is ahead of it.
    await gw.answer({ rev: 3, sessions: { a: { endedAt: 130, viewedAt: 120 } } })
    expect(isUnread(gw.store.getSnapshot().times.get('a'))).toBe(true)
  })
})
